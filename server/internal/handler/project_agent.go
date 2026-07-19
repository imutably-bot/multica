package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectAgentsRequest struct {
	AgentIDs []string `json:"agent_ids"`
}

func isProjectLead(member db.Member, project db.Project) bool {
	return project.LeadType.Valid && project.LeadType.String == "member" &&
		project.LeadID.Valid && project.LeadID == member.ID
}

// GET /api/projects/{id}/agents
func (h *Handler) ListProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Ensure project exists and lies in workspace tenant boundary
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	_, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// 2. Fetch assigned agents
	agents, err := h.Queries.ListAgentsInProject(r.Context(), projUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project agents")
		return
	}

	// 3. Serialize output using the standard agent response mapper
	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": resp, "total": len(resp)})
}

// POST /api/projects/{id}/agents (Batch Add)
func (h *Handler) AddAgentsToProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Tenant Check & Project Permission Verification
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Guard: Only workspace owners/admins, or project lead can assign agents
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	isAdminOrOwner := member.Role == "owner" || member.Role == "admin"
	if !isAdminOrOwner && !isProjectLead(member, project) {
		writeError(w, http.StatusForbidden, "insufficient permission to manage project agents")
		return
	}

	var req ProjectAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 2. Insert relations within a transaction
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	for _, idStr := range req.AgentIDs {
		agentUUID, err := parseUUIDOrErr(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Verify agent belongs to the same workspace to prevent cross-tenant mapping
		agent, err := qtx.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: agentUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("agent %s not found in workspace", idStr))
			return
		}

		if err := qtx.AddAgentToProject(r.Context(), db.AddAgentToProjectParams{
			ProjectID: projUUID,
			AgentID:   agent.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent to project")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/projects/{id}/agents (Sync / Overwrite)
func (h *Handler) SetProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Tenant Check & Project Permission Verification
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Guard: Only workspace owners/admins, or project lead can assign agents
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	isAdminOrOwner := member.Role == "owner" || member.Role == "admin"
	if !isAdminOrOwner && !isProjectLead(member, project) {
		writeError(w, http.StatusForbidden, "insufficient permission to manage project agents")
		return
	}

	var req ProjectAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 2. Clear and sync within a transaction
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Clear existing ones first
	if err := qtx.ClearProjectAgents(r.Context(), projUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear project agents")
		return
	}

	for _, idStr := range req.AgentIDs {
		agentUUID, err := parseUUIDOrErr(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		// Verify agent belongs to the same workspace to prevent cross-tenant mapping
		agent, err := qtx.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
			ID: agentUUID, WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("agent %s not found in workspace", idStr))
			return
		}

		if err := qtx.AddAgentToProject(r.Context(), db.AddAgentToProjectParams{
			ProjectID: projUUID,
			AgentID:   agent.ID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to add agent to project")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/projects/{id}/agents (Batch Remove)
func (h *Handler) RemoveAgentsFromProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Tenant Check & Project Permission Verification
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Guard: Only workspace owners/admins, or project lead can assign agents
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	isAdminOrOwner := member.Role == "owner" || member.Role == "admin"
	if !isAdminOrOwner && !isProjectLead(member, project) {
		writeError(w, http.StatusForbidden, "insufficient permission to manage project agents")
		return
	}

	var req ProjectAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 2. Remove within a transaction
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	for _, idStr := range req.AgentIDs {
		agentUUID, err := parseUUIDOrErr(idStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if err := qtx.RemoveAgentFromProject(r.Context(), db.RemoveAgentFromProjectParams{
			ProjectID: projUUID,
			AgentID:   agentUUID,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to remove agent from project")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Helper function to parse UUID and return standard error
func parseUUIDOrErr(s string) (pgtype.UUID, error) {
	var uuid pgtype.UUID
	err := uuid.Scan(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid UUID format: %s", s)
	}
	return uuid, nil
}

// GET /api/agents/{id}/projects
func (h *Handler) ListAgentProjects(w http.ResponseWriter, r *http.Request) {
	agentID := chi.URLParam(r, "id")
	agentUUID, ok := parseUUIDOrBadRequest(w, agentID, "agent id")
	if !ok {
		return
	}

	// Ensure workspace boundaries are respected
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	_, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
		ID:          agentUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}

	// Fetch projects the agent is assigned to
	projects, err := h.Queries.ListProjectsForAgent(r.Context(), agentUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agent projects")
		return
	}

	resp := make([]ProjectResponse, len(projects))
	for i, p := range projects {
		resp[i] = projectToResponse(p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"projects": resp, "total": len(resp)})
}
