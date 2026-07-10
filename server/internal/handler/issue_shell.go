package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/prompttmpl"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var issueShellUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

type IssueShellSessionResponse struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	WorkDir   string `json:"work_dir,omitempty"`
	Error     string `json:"error,omitempty"`
}

type browserIssueShellMessage struct {
	Type string `json:"type"`
	Data string `json:"data,omitempty"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

func (h *Handler) CreateIssueShellSession(w http.ResponseWriter, r *http.Request) {
	launch, snapshot, ok := h.resolveIssueShellLaunch(w, r)
	if !ok {
		return
	}

	snapshot, err := h.IssueShellService.EnsureSession(launch)
	if err != nil {
		switch err {
		case service.ErrIssueShellDaemonUnavailable:
			writeError(w, http.StatusServiceUnavailable, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to create issue shell")
		}
		return
	}

	writeJSON(w, http.StatusOK, IssueShellSessionResponse{
		SessionID: snapshot.SessionID,
		State:     snapshot.State,
		WorkDir:   snapshot.WorkDir,
		Error:     snapshot.Error,
	})
}

func (h *Handler) GetIssueShellSession(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.resolveIssueShellLaunch(w, r); !ok {
		return
	}
	issueID := chi.URLParam(r, "id")
	snapshot, found := h.IssueShellService.SnapshotByIssue(issueID)
	if !found {
		writeError(w, http.StatusNotFound, "issue shell not found")
		return
	}
	writeJSON(w, http.StatusOK, IssueShellSessionResponse{
		SessionID: snapshot.SessionID,
		State:     snapshot.State,
		WorkDir:   snapshot.WorkDir,
		Error:     snapshot.Error,
	})
}

func (h *Handler) IssueShellWebSocket(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.resolveIssueShellLaunch(w, r); !ok {
		return
	}
	issueID := chi.URLParam(r, "id")
	conn, err := issueShellUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	snapshot, viewer, err := h.IssueShellService.AttachViewer(issueID, conn)
	if err != nil {
		_ = conn.WriteJSON(map[string]any{"type": "error", "error": err.Error()})
		return
	}
	defer h.IssueShellService.DetachViewer(issueID, viewer)

	if err := viewer.WriteJSON(map[string]any{
		"type": "snapshot",
		"snapshot": IssueShellSessionResponse{
			SessionID: snapshot.SessionID,
			State:     snapshot.State,
			WorkDir:   snapshot.WorkDir,
			Error:     snapshot.Error,
		},
		"data": snapshot.Buffer,
	}); err != nil {
		return
	}

	for {
		var msg browserIssueShellMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		switch msg.Type {
		case "input":
			if !h.IssueShellService.ForwardInput(issueID, msg.Data) {
				_ = conn.WriteJSON(map[string]any{"type": "error", "error": "issue shell is unavailable"})
			}
		case "resize":
			if msg.Cols > 0 && msg.Rows > 0 {
				h.IssueShellService.ForwardResize(issueID, msg.Cols, msg.Rows)
			}
		case "close":
			h.IssueShellService.Close(issueID)
		}
	}
}

func (h *Handler) resolveIssueShellLaunch(w http.ResponseWriter, r *http.Request) (service.IssueShellLaunch, service.IssueShellSnapshot, bool) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	// Caller may request a specific agent via ?agent_id=; fall back to the
	// issue's current assignee when the param is absent.
	var agentUUID = issue.AssigneeID
	if agentIDParam := r.URL.Query().Get("agent_id"); agentIDParam != "" {
		parsed, ok2 := parseUUIDOrBadRequest(w, agentIDParam, "agent_id")
		if !ok2 {
			return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
		}
		agentUUID = parsed
	} else if !issue.AssigneeType.Valid || issue.AssigneeType.String != "agent" || !issue.AssigneeID.Valid {
		writeAgentUnavailable(w, "issue is not assigned to an agent")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	agent, err := h.Queries.GetAgent(r.Context(), agentUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}
	if agent.ArchivedAt.Valid {
		writeAgentUnavailable(w, "agent is archived")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	workspaceID := uuidToString(issue.WorkspaceID)
	userID, ok := requireUserID(w, r)
	if !ok {
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, h.invokeOriginatorFromRequest(r, actorType, actorID), workspaceID) {
		writeError(w, http.StatusForbidden, "you do not have access to this agent")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	if !agent.RuntimeID.Valid {
		writeAgentUnavailable(w, "agent has no runtime")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}
	if !h.isRuntimeOnline(r.Context(), agent.RuntimeID) {
		writeAgentUnavailable(w, "agent's runtime is offline")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	workspaceContext := ""
	workspaceName := ""
	workspaceInitPrompt := ""
	if workspace, err := h.Queries.GetWorkspace(r.Context(), issue.WorkspaceID); err == nil {
		workspaceName = workspace.Name
		if workspace.Context.Valid {
			workspaceContext = workspace.Context.String
		}
		workspaceInitPrompt = prompttmpl.EffectiveTemplates(
			prompttmpl.ExtractWorkspaceOverridesFromRaw(workspace.Settings, ""),
			nil,
		)[prompttmpl.WorkspaceInitKey]
	}

	issuePrefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
	identifier := issuePrefix + "-" + intToString(issue.Number)

	priorSessionID := ""
	priorWorkDir := ""
	lastSession, err := h.Queries.GetLastTaskSession(r.Context(), db.GetLastTaskSessionParams{
		AgentID: issue.AssigneeID,
		IssueID: issue.ID,
	})
	if err == nil && lastSession.RuntimeID == agent.RuntimeID {
		if lastSession.SessionID.Valid {
			priorSessionID = lastSession.SessionID.String
		}
		if lastSession.WorkDir.Valid {
			priorWorkDir = lastSession.WorkDir.String
		}
	} else if err != nil && err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to load issue shell context")
		return service.IssueShellLaunch{}, service.IssueShellSnapshot{}, false
	}

	var model string
	if agent.Model.Valid {
		model = agent.Model.String
	}
	var thinkingLevel string
	if agent.ThinkingLevel.Valid {
		thinkingLevel = agent.ThinkingLevel.String
	}

	customEnv := unmarshalCustomEnv(agent)

	var customArgs []string
	if agent.CustomArgs != nil {
		_ = json.Unmarshal(agent.CustomArgs, &customArgs)
	}

	launch := service.IssueShellLaunch{
		SessionID:           randomID(),
		WorkspaceID:         workspaceID,
		RuntimeID:           uuidToString(agent.RuntimeID),
		IssueID:             uuidToString(issue.ID),
		IssueIdentifier:     identifier,
		IssueTitle:          issue.Title,
		AgentID:             uuidToString(agent.ID),
		AgentName:           agent.Name,
		Model:               model,
		ThinkingLevel:       thinkingLevel,
		CustomEnv:           customEnv,
		CustomArgs:          customArgs,
		McpConfig:           agent.McpConfig,
		RuntimeConfig:       agent.RuntimeConfig,
		WorkspaceName:       workspaceName,
		WorkspaceContext:    workspaceContext,
		WorkspaceInitPrompt: workspaceInitPrompt,
		PriorSessionID:      priorSessionID,
		PriorWorkDir:        priorWorkDir,
	}
	snapshot := service.IssueShellSnapshot{
		SessionID: launch.SessionID,
		State:     "starting",
	}
	if existing, found := h.IssueShellService.SnapshotByIssue(launch.IssueID); found {
		snapshot = existing
		launch.SessionID = existing.SessionID
	}
	return launch, snapshot, true
}

func intToString(v int32) string {
	return strconv.Itoa(int(v))
}
