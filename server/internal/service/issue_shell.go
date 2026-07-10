package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const issueShellBufferLimit = 256 * 1024

var ErrIssueShellDaemonUnavailable = errors.New("issue shell daemon unavailable")

type IssueShellLaunch struct {
	SessionID           string
	WorkspaceID         string
	RuntimeID           string
	IssueID             string
	IssueIdentifier     string
	IssueTitle          string
	AgentID             string
	AgentName           string
	Model               string
	ThinkingLevel       string
	CustomEnv           map[string]string
	CustomArgs          []string
	McpConfig           json.RawMessage
	RuntimeConfig       json.RawMessage
	WorkspaceName       string
	WorkspaceContext    string
	WorkspaceInitPrompt string
	PriorSessionID      string
	PriorWorkDir        string
}

type IssueShellSnapshot struct {
	SessionID string `json:"session_id"`
	State     string `json:"state"`
	Buffer    string `json:"buffer,omitempty"`
	WorkDir   string `json:"work_dir,omitempty"`
	Error     string `json:"error,omitempty"`
}

type IssueShellService struct {
	daemonHub *daemonws.Hub

	mu        sync.Mutex
	byIssue   map[string]*issueShellSession
	bySession map[string]*issueShellSession
}

type issueShellSession struct {
	launch IssueShellLaunch

	state   string
	buffer  string
	workDir string
	errMsg  string

	viewers map[*IssueShellViewer]struct{}
}

type IssueShellViewer struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (v *IssueShellViewer) WriteJSON(payload any) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.conn.WriteJSON(payload)
}

func NewIssueShellService(daemonHub *daemonws.Hub) *IssueShellService {
	return &IssueShellService{
		daemonHub: daemonHub,
		byIssue:   make(map[string]*issueShellSession),
		bySession: make(map[string]*issueShellSession),
	}
}

func (s *IssueShellService) EnsureSession(launch IssueShellLaunch) (IssueShellSnapshot, error) {
	s.mu.Lock()
	existing := s.byIssue[launch.IssueID]
	if existing != nil && existing.state != "closed" && existing.state != "failed" &&
		existing.launch.RuntimeID == launch.RuntimeID && existing.launch.AgentID == launch.AgentID {
		snapshot := existing.snapshotLocked()
		s.mu.Unlock()
		return snapshot, nil
	}

	session := &issueShellSession{
		launch:  launch,
		state:   "starting",
		viewers: make(map[*IssueShellViewer]struct{}),
	}
	s.byIssue[launch.IssueID] = session
	s.bySession[launch.SessionID] = session
	snapshot := session.snapshotLocked()
	s.mu.Unlock()

	if s.daemonHub == nil || !s.daemonHub.SendRuntimeMessage(launch.RuntimeID, protocol.Message{
		Type: protocol.EventDaemonIssueShellOpen,
		Payload: mustMarshalRaw(protocol.IssueShellOpenPayload{
			SessionID:           launch.SessionID,
			WorkspaceID:         launch.WorkspaceID,
			RuntimeID:           launch.RuntimeID,
			IssueID:             launch.IssueID,
			IssueIdentifier:     launch.IssueIdentifier,
			IssueTitle:          launch.IssueTitle,
			AgentID:             launch.AgentID,
			AgentName:           launch.AgentName,
			Model:               launch.Model,
			ThinkingLevel:       launch.ThinkingLevel,
			CustomEnv:           launch.CustomEnv,
			CustomArgs:          launch.CustomArgs,
			McpConfig:           launch.McpConfig,
			RuntimeConfig:       launch.RuntimeConfig,
			WorkspaceName:       launch.WorkspaceName,
			WorkspaceContext:    launch.WorkspaceContext,
			WorkspaceInitPrompt: launch.WorkspaceInitPrompt,
			PriorSessionID:      launch.PriorSessionID,
			PriorWorkDir:        launch.PriorWorkDir,
		}),
	}) {
		s.markFailed(launch.SessionID, ErrIssueShellDaemonUnavailable.Error())
		return IssueShellSnapshot{}, ErrIssueShellDaemonUnavailable
	}

	return snapshot, nil
}

func (s *IssueShellService) SnapshotByIssue(issueID string) (IssueShellSnapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.byIssue[issueID]
	if session == nil {
		return IssueShellSnapshot{}, false
	}
	return session.snapshotLocked(), true
}

func (s *IssueShellService) AttachViewer(issueID string, conn *websocket.Conn) (IssueShellSnapshot, *IssueShellViewer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.byIssue[issueID]
	if session == nil {
		return IssueShellSnapshot{}, nil, errors.New("issue shell not found")
	}
	viewer := &IssueShellViewer{conn: conn}
	session.viewers[viewer] = struct{}{}
	return session.snapshotLocked(), viewer, nil
}

func (s *IssueShellService) DetachViewer(issueID string, viewer *IssueShellViewer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.byIssue[issueID]
	if session == nil {
		return
	}
	delete(session.viewers, viewer)
}

func (s *IssueShellService) ForwardInput(issueID string, input string) bool {
	s.mu.Lock()
	session := s.byIssue[issueID]
	s.mu.Unlock()
	if session == nil || s.daemonHub == nil {
		return false
	}
	return s.daemonHub.SendRuntimeMessage(session.launch.RuntimeID, protocol.Message{
		Type: protocol.EventDaemonIssueShellInput,
		Payload: mustMarshalRaw(protocol.IssueShellInputPayload{
			SessionID: session.launch.SessionID,
			Input:     input,
		}),
	})
}

func (s *IssueShellService) ForwardResize(issueID string, cols, rows int) bool {
	s.mu.Lock()
	session := s.byIssue[issueID]
	s.mu.Unlock()
	if session == nil || s.daemonHub == nil {
		return false
	}
	return s.daemonHub.SendRuntimeMessage(session.launch.RuntimeID, protocol.Message{
		Type: protocol.EventDaemonIssueShellResize,
		Payload: mustMarshalRaw(protocol.IssueShellResizePayload{
			SessionID: session.launch.SessionID,
			Cols:      cols,
			Rows:      rows,
		}),
	})
}

func (s *IssueShellService) Close(issueID string) bool {
	s.mu.Lock()
	session := s.byIssue[issueID]
	s.mu.Unlock()
	if session == nil || s.daemonHub == nil {
		return false
	}
	return s.daemonHub.SendRuntimeMessage(session.launch.RuntimeID, protocol.Message{
		Type: protocol.EventDaemonIssueShellClose,
		Payload: mustMarshalRaw(protocol.IssueShellClosePayload{
			SessionID: session.launch.SessionID,
		}),
	})
}

func (s *IssueShellService) HandleDaemonFrame(_ context.Context, _ daemonws.ClientIdentity, msg protocol.Message) {
	switch msg.Type {
	case protocol.EventDaemonIssueShellReady:
		var payload protocol.IssueShellReadyPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			s.markReady(payload.SessionID, payload.WorkDir)
		}
	case protocol.EventDaemonIssueShellOutput:
		var payload protocol.IssueShellOutputPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			s.appendOutput(payload.SessionID, payload.Output)
		}
	case protocol.EventDaemonIssueShellExit:
		var payload protocol.IssueShellExitPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			s.markClosed(payload.SessionID)
		}
	case protocol.EventDaemonIssueShellError:
		var payload protocol.IssueShellErrorPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			s.markFailed(payload.SessionID, payload.Error)
		}
	}
}

func (s *IssueShellService) markReady(sessionID, workDir string) {
	s.mu.Lock()
	session := s.bySession[sessionID]
	if session == nil {
		s.mu.Unlock()
		return
	}
	session.state = "running"
	session.workDir = workDir
	viewers := session.viewerListLocked()
	snapshot := session.snapshotLocked()
	s.mu.Unlock()
	s.broadcast(viewers, browserShellFrame{Type: "snapshot", Snapshot: &snapshot})
}

func (s *IssueShellService) appendOutput(sessionID, output string) {
	s.mu.Lock()
	session := s.bySession[sessionID]
	if session == nil {
		s.mu.Unlock()
		return
	}
	session.buffer += output
	if len(session.buffer) > issueShellBufferLimit {
		session.buffer = session.buffer[len(session.buffer)-issueShellBufferLimit:]
	}
	viewers := session.viewerListLocked()
	s.mu.Unlock()
	s.broadcast(viewers, browserShellFrame{Type: "output", Data: output})
}

func (s *IssueShellService) markClosed(sessionID string) {
	s.mu.Lock()
	session := s.bySession[sessionID]
	if session == nil {
		s.mu.Unlock()
		return
	}
	session.state = "closed"
	viewers := session.viewerListLocked()
	snapshot := session.snapshotLocked()
	s.mu.Unlock()
	s.broadcast(viewers, browserShellFrame{Type: "snapshot", Snapshot: &snapshot})
}

func (s *IssueShellService) markFailed(sessionID, errMsg string) {
	s.mu.Lock()
	session := s.bySession[sessionID]
	if session == nil {
		s.mu.Unlock()
		return
	}
	session.state = "failed"
	session.errMsg = errMsg
	viewers := session.viewerListLocked()
	snapshot := session.snapshotLocked()
	s.mu.Unlock()
	s.broadcast(viewers, browserShellFrame{Type: "snapshot", Snapshot: &snapshot})
}

func (s *IssueShellService) broadcast(viewers []*IssueShellViewer, frame browserShellFrame) {
	for _, viewer := range viewers {
		_ = viewer.WriteJSON(frame)
	}
}

func (s *issueShellSession) snapshotLocked() IssueShellSnapshot {
	return IssueShellSnapshot{
		SessionID: s.launch.SessionID,
		State:     s.state,
		Buffer:    s.buffer,
		WorkDir:   s.workDir,
		Error:     s.errMsg,
	}
}

func (s *issueShellSession) viewerListLocked() []*IssueShellViewer {
	viewers := make([]*IssueShellViewer, 0, len(s.viewers))
	for viewer := range s.viewers {
		viewers = append(viewers, viewer)
	}
	return viewers
}

type browserShellFrame struct {
	Type     string              `json:"type"`
	Data     string              `json:"data,omitempty"`
	Snapshot *IssueShellSnapshot `json:"snapshot,omitempty"`
}

func mustMarshalRaw(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
