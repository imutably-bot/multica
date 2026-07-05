package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/creack/pty"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

type issueShellManager struct {
	daemon *Daemon

	mu       sync.Mutex
	sessions map[string]*issueShellSession
}

type issueShellSession struct {
	id       string
	runtimeID string
	provider string
	cmd      *exec.Cmd
	ptyFile  *os.File
	cancel   context.CancelFunc
	env      *execenv.Environment
	cleanup  func()
}

func newIssueShellManager(d *Daemon) *issueShellManager {
	return &issueShellManager{
		daemon:   d,
		sessions: make(map[string]*issueShellSession),
	}
}

func (m *issueShellManager) handleMessage(ctx context.Context, msg protocol.Message) {
	switch msg.Type {
	case protocol.EventDaemonIssueShellOpen:
		var payload protocol.IssueShellOpenPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			go m.open(ctx, payload)
		}
	case protocol.EventDaemonIssueShellInput:
		var payload protocol.IssueShellInputPayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			m.writeInput(payload)
		}
	case protocol.EventDaemonIssueShellResize:
		var payload protocol.IssueShellResizePayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			m.resize(payload)
		}
	case protocol.EventDaemonIssueShellClose:
		var payload protocol.IssueShellClosePayload
		if json.Unmarshal(msg.Payload, &payload) == nil {
			m.close(payload.SessionID)
		}
	}
}

func (m *issueShellManager) open(parent context.Context, payload protocol.IssueShellOpenPayload) {
	if payload.SessionID == "" || payload.RuntimeID == "" || payload.WorkspaceID == "" {
		m.sendError(payload.RuntimeID, payload.SessionID, "issue shell payload is incomplete")
		return
	}

	m.mu.Lock()
	if existing := m.sessions[payload.SessionID]; existing != nil {
		m.mu.Unlock()
		m.sendReady(payload.RuntimeID, payload.SessionID, existing.env.WorkDir)
		return
	}
	m.mu.Unlock()

	launch, env, cleanup, provider, err := m.buildCommand(payload)
	if err != nil {
		m.sendError(payload.RuntimeID, payload.SessionID, err.Error())
		return
	}

	runCtx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(runCtx, launch.Path, launch.Args[1:]...)
	cmd.Dir = env.WorkDir
	cmd.Env = buildIssueShellEnv(launch.EnvMap)

	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 32})
	if err != nil {
		cancel()
		cleanup()
		m.sendError(payload.RuntimeID, payload.SessionID, fmt.Sprintf("start shell: %v", err))
		return
	}

	session := &issueShellSession{
		id:        payload.SessionID,
		runtimeID: payload.RuntimeID,
		provider:  provider,
		cmd:       cmd,
		ptyFile:   ptmx,
		cancel:    cancel,
		env:       env,
		cleanup:   cleanup,
	}
	m.mu.Lock()
	m.sessions[payload.SessionID] = session
	m.mu.Unlock()

	m.sendReady(payload.RuntimeID, payload.SessionID, env.WorkDir)
	go m.streamOutput(session)
	go m.wait(session)
}

func (m *issueShellManager) buildCommand(payload protocol.IssueShellOpenPayload) (*issueShellCommand, *execenv.Environment, func(), string, error) {
	m.daemon.mu.Lock()
	rt, ok := m.daemon.runtimeIndex[payload.RuntimeID]
	m.daemon.mu.Unlock()
	if !ok {
		return nil, nil, nil, "", fmt.Errorf("runtime %s is not hosted on this daemon", payload.RuntimeID)
	}
	provider := rt.Provider
	entry, ok := m.daemon.cfg.Agents[provider]
	var profileFixedArgs []string
	if customSpec, isCustom := m.daemon.customProfileLaunchForRuntime(payload.RuntimeID); isCustom {
		entry.Path = customSpec.path
		profileFixedArgs = customSpec.fixedArgs
		ok = true
	}
	if !ok {
		return nil, nil, nil, "", fmt.Errorf("provider %q does not support issue shell yet", provider)
	}

	taskCtx := execenv.TaskContextForEnv{
		IssueID:             payload.IssueID,
		AgentID:             payload.AgentID,
		AgentName:           payload.AgentName,
		WorkspaceContext:    payload.WorkspaceContext,
		PriorSessionResumed: payload.PriorSessionID != "",
	}

	codexVersion := m.daemon.agentVersion("codex")
	openclawBin := ""
	if provider == "openclaw" {
		openclawBin = entry.Path
	}

	var env *execenv.Environment
	if payload.PriorWorkDir != "" {
		env = execenv.Reuse(execenv.ReuseParams{
			WorkDir:      payload.PriorWorkDir,
			Provider:     provider,
			CodexVersion: codexVersion,
			OpenclawBin:  openclawBin,
			McpConfig:    payload.McpConfig,
			Task:         taskCtx,
		}, m.daemon.logger)
	}
	if env == nil {
		var err error
		env, err = execenv.Prepare(execenv.PrepareParams{
			WorkspacesRoot: m.daemon.cfg.WorkspacesRoot,
			WorkspaceID:    payload.WorkspaceID,
			TaskID:         payload.SessionID,
			AgentName:      payload.AgentName,
			Provider:       provider,
			CodexVersion:   codexVersion,
			OpenclawBin:    openclawBin,
			McpConfig:      payload.McpConfig,
			Task:           taskCtx,
		}, m.daemon.logger)
		if err != nil {
			return nil, nil, nil, "", fmt.Errorf("prepare execution environment: %w", err)
		}
	}
	if _, err := execenv.InjectRuntimeConfig(env.WorkDir, provider, taskCtx); err != nil {
		m.daemon.logger.Warn("issue shell: inject runtime config failed", "error", err, "provider", provider)
	}

	agentEnv := map[string]string{
		"MULTICA_SERVER_URL":   m.daemon.cfg.ServerBaseURL,
		"MULTICA_DAEMON_PORT":  strconv.Itoa(m.daemon.cfg.HealthPort),
		"MULTICA_WORKSPACE_ID": payload.WorkspaceID,
		"MULTICA_AGENT_NAME":   payload.AgentName,
		"MULTICA_AGENT_ID":     payload.AgentID,
	}
	if selfBin, err := os.Executable(); err == nil {
		binDir := filepath.Dir(selfBin)
		agentEnv["PATH"] = binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	if env.CodexHome != "" {
		agentEnv["CODEX_HOME"] = env.CodexHome
	}
	if env.CursorDataDir != "" {
		agentEnv["CURSOR_DATA_DIR"] = env.CursorDataDir
	}
	if env.OpenclawConfigPath != "" {
		agentEnv["OPENCLAW_CONFIG_PATH"] = env.OpenclawConfigPath
	}
	if rootsValue, ok := composeOpenclawIncludeRoots(env.OpenclawIncludeRoot, os.Getenv("OPENCLAW_INCLUDE_ROOTS")); ok {
		agentEnv["OPENCLAW_INCLUDE_ROOTS"] = rootsValue
	}
	for k, v := range payload.CustomEnv {
		if isBlockedEnvKey(k) {
			continue
		}
		agentEnv[k] = v
	}

	extraArgs := defaultArgsForProvider(m.daemon.cfg, provider)
	if len(profileFixedArgs) > 0 {
		extraArgs = append(append([]string{}, profileFixedArgs...), extraArgs...)
	}

	launchArgs, err := buildInteractiveShellArgs(provider, payload, env.WorkDir, extraArgs)
	if err != nil {
		return nil, nil, nil, "", err
	}

	cleanup := func() {
		if env.LocalDirectory {
			if err := execenv.CleanupRuntimeConfig(env.WorkDir, provider); err != nil {
				m.daemon.logger.Warn("issue shell: cleanup runtime config failed", "error", err)
			}
			if err := execenv.CleanupSidecars(env.RootDir); err != nil {
				m.daemon.logger.Warn("issue shell: cleanup sidecars failed", "error", err)
			}
			return
		}
		if payload.PriorWorkDir == "" && env.RootDir != "" {
			_ = os.RemoveAll(env.RootDir)
		}
	}

	return &issueShellCommand{
		Path:   entry.Path,
		Args:   append([]string{entry.Path}, launchArgs...),
		EnvMap: agentEnv,
	}, env, cleanup, provider, nil
}

func buildInteractiveShellArgs(provider string, payload protocol.IssueShellOpenPayload, workDir string, extraArgs []string) ([]string, error) {
	args := append([]string{}, extraArgs...)
	switch provider {
	case "claude", "codebuddy":
		if payload.Model != "" {
			args = append(args, "--model", payload.Model)
		}
		if payload.ThinkingLevel != "" {
			args = append(args, "--effort", payload.ThinkingLevel)
		}
		if payload.IssueIdentifier != "" {
			args = append(args, "--name", payload.IssueIdentifier)
		}
		if payload.PriorSessionID != "" {
			args = append(args, "--resume", payload.PriorSessionID)
		}
		args = append(args, payload.CustomArgs...)
		return args, nil
	case "codex":
		if payload.PriorSessionID != "" {
			args = append([]string{"resume", payload.PriorSessionID}, args...)
		}
		args = append(args, "--no-alt-screen", "-C", workDir)
		if payload.Model != "" {
			args = append(args, "--model", payload.Model)
		}
		args = append(args, payload.CustomArgs...)
		return args, nil
	default:
		return nil, fmt.Errorf("provider %q does not support issue shell yet", provider)
	}
}

func (m *issueShellManager) streamOutput(session *issueShellSession) {
	buf := make([]byte, 4096)
	for {
		n, err := session.ptyFile.Read(buf)
		if n > 0 {
			m.sendOutput(session.runtimeID, session.id, string(buf[:n]))
		}
		if err != nil {
			if err != io.EOF {
				m.sendError(session.runtimeID, session.id, fmt.Sprintf("shell output failed: %v", err))
			}
			return
		}
	}
}

func (m *issueShellManager) wait(session *issueShellSession) {
	exitCode := 0
	if err := session.cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			m.sendError(session.runtimeID, session.id, err.Error())
		}
	}
	_ = session.ptyFile.Close()
	session.cleanup()

	m.mu.Lock()
	delete(m.sessions, session.id)
	m.mu.Unlock()

	m.sendExit(session.runtimeID, session.id, exitCode)
}

func (m *issueShellManager) writeInput(payload protocol.IssueShellInputPayload) {
	m.mu.Lock()
	session := m.sessions[payload.SessionID]
	m.mu.Unlock()
	if session == nil {
		return
	}
	_, _ = io.WriteString(session.ptyFile, payload.Input)
}

func (m *issueShellManager) resize(payload protocol.IssueShellResizePayload) {
	m.mu.Lock()
	session := m.sessions[payload.SessionID]
	m.mu.Unlock()
	if session == nil || payload.Cols <= 0 || payload.Rows <= 0 {
		return
	}
	_ = pty.Setsize(session.ptyFile, &pty.Winsize{
		Cols: uint16(payload.Cols),
		Rows: uint16(payload.Rows),
	})
}

func (m *issueShellManager) close(sessionID string) {
	m.mu.Lock()
	session := m.sessions[sessionID]
	m.mu.Unlock()
	if session == nil {
		return
	}
	session.cancel()
}

func (m *issueShellManager) sendReady(runtimeID, sessionID, workDir string) {
	m.send(runtimeID, protocol.EventDaemonIssueShellReady, protocol.IssueShellReadyPayload{
		SessionID: sessionID,
		WorkDir:   workDir,
	})
}

func (m *issueShellManager) sendOutput(runtimeID, sessionID, output string) {
	m.send(runtimeID, protocol.EventDaemonIssueShellOutput, protocol.IssueShellOutputPayload{
		SessionID: sessionID,
		Output:    output,
	})
}

func (m *issueShellManager) sendExit(runtimeID, sessionID string, exitCode int) {
	m.send(runtimeID, protocol.EventDaemonIssueShellExit, protocol.IssueShellExitPayload{
		SessionID: sessionID,
		ExitCode:  exitCode,
	})
}

func (m *issueShellManager) sendError(runtimeID, sessionID, errMsg string) {
	m.send(runtimeID, protocol.EventDaemonIssueShellError, protocol.IssueShellErrorPayload{
		SessionID: sessionID,
		Error:     errMsg,
	})
}

func (m *issueShellManager) send(runtimeID, eventType string, payload any) {
	_ = runtimeID
	frame, err := json.Marshal(protocol.Message{
		Type:    eventType,
		Payload: marshalRaw(payload),
	})
	if err != nil {
		return
	}
	m.daemon.sendWSFrame(frame)
}

type issueShellCommand struct {
	Path   string
	Args   []string
	EnvMap map[string]string
}

func buildIssueShellEnv(extra map[string]string) []string {
	env := append([]string{}, os.Environ()...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}
