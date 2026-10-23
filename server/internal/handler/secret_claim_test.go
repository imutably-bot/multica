package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createClaimSecret creates a workspace secret and returns its id so
// callers can grant it to a specific agent via grantClaimSecret.
func createClaimSecret(t *testing.T, ctx context.Context, name, value string) string {
	t.Helper()
	envelope, keyVersion, err := testHandler.Secrets.Encrypt([]byte(value))
	if err != nil {
		t.Fatalf("encrypt test secret %s: %v", name, err)
	}
	workspaceID := parseUUID(testWorkspaceID)
	created, err := testHandler.Queries.CreateWorkspaceSecret(ctx, db.CreateWorkspaceSecretParams{
		WorkspaceID:    workspaceID,
		Name:           name,
		ValueEncrypted: envelope,
		KeyVersion:     keyVersion,
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceSecret(%s): %v", name, err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteWorkspaceSecret(context.Background(), db.DeleteWorkspaceSecretParams{
			WorkspaceID: workspaceID,
			Name:        name,
		})
	})
	return uuidToString(created.ID)
}

// grantClaimSecret is the test-side equivalent of POST
// /api/agents/{id}/secrets — grants secretID to agentID directly via the
// generated query so claim tests don't need a live HTTP round trip just
// to set up fixture state.
func grantClaimSecret(t *testing.T, ctx context.Context, agentID, secretID string) {
	t.Helper()
	if _, err := testHandler.Queries.CreateAgentSecretGrant(ctx, db.CreateAgentSecretGrantParams{
		AgentID:  parseUUID(agentID),
		SecretID: parseUUID(secretID),
	}); err != nil {
		t.Fatalf("CreateAgentSecretGrant(%s, %s): %v", agentID, secretID, err)
	}
}

// TestClaimTaskByRuntime_ResolvesSecretReferences covers the claim-time
// half of KHI-972 P2: a ${secret:NAME} reference nested in the agent's
// mcp_config (under headers) and one in custom_env must both be resolved
// to their decrypted values by the time the daemon receives the claim
// response, with no literal reference text left anywhere in the payload.
func TestClaimTaskByRuntime_ResolvesSecretReferences(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Secret resolve runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Secret resolve agent")

	obotSecretID := createClaimSecret(t, ctx, "claim-test-obot-token", "obot-secret-value")
	dbPassSecretID := createClaimSecret(t, ctx, "claim-test-db-pass", "db-secret-value")
	grantClaimSecret(t, ctx, agentID, obotSecretID)
	grantClaimSecret(t, ctx, agentID, dbPassSecretID)

	mcpConfig := `{"mcpServers":{"n8n-obot":{"url":"https://n8n.example.com/mcp","headers":{"Authorization":"Bearer ${secret:claim-test-obot-token}"}}}}`
	customEnv := `{"DB_PASS":"${secret:claim-test-db-pass}","PLAIN":"unchanged"}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1, custom_env = $2 WHERE id = $3`, mcpConfig, customEnv, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config/custom_env: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "secret-resolve-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.Agent == nil {
		t.Fatalf("missing task/agent in claim response: %s", w.Body.String())
	}

	if strings.Contains(string(claimResp.Task.Agent.McpConfig), "${secret:") {
		t.Fatalf("claim response mcp_config still contains a literal reference: %s", claimResp.Task.Agent.McpConfig)
	}
	if !strings.Contains(string(claimResp.Task.Agent.McpConfig), "Bearer obot-secret-value") {
		t.Fatalf("mcp_config header reference not resolved: %s", claimResp.Task.Agent.McpConfig)
	}
	if got := claimResp.Task.Agent.CustomEnv["DB_PASS"]; got != "db-secret-value" {
		t.Fatalf("custom_env reference not resolved: %+v", claimResp.Task.Agent.CustomEnv)
	}
	if got := claimResp.Task.Agent.CustomEnv["PLAIN"]; got != "unchanged" {
		t.Fatalf("unrelated custom_env value mutated: %+v", claimResp.Task.Agent.CustomEnv)
	}
}

// TestClaimTaskByRuntime_MissingSecretFailsTaskInsteadOfLeakingReference
// covers the fail-closed contract: a reference to a secret that does not
// exist must fail the already-claimed task (status=failed, a recorded
// error, failure_reason=secret_resolution_failed) and the claim response
// for this round must report no task at all — never the literal
// ${secret:NAME} text, and never a 500.
func TestClaimTaskByRuntime_MissingSecretFailsTaskInsteadOfLeakingReference(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Secret missing runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Secret missing agent")

	mcpConfig := `{"mcpServers":{"x":{"url":"${secret:claim-test-does-not-exist}"}}}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, mcpConfig, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "secret-missing-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "${secret:") {
		t.Fatalf("claim response leaked the literal reference: %s", w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task != nil {
		t.Fatalf("expected no task in the claim response after a resolution failure, got: %s", w.Body.String())
	}

	var status, errMsg, failureReason string
	if err := testPool.QueryRow(ctx, `SELECT status, error, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &errMsg, &failureReason); err != nil {
		t.Fatalf("read back task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if failureReason != "secret_resolution_failed" {
		t.Fatalf("failure_reason = %q, want secret_resolution_failed", failureReason)
	}
	if !strings.Contains(errMsg, "claim-test-does-not-exist") {
		t.Fatalf("task error does not name the missing secret: %q", errMsg)
	}
}

// TestClaimTaskByRuntime_UngrantedSecretFailsTask is the KHI-972 P2 review
// finding 1 regression test: the "member-owned-agent exfiltration" case.
// A real, decryptable workspace secret exists (e.g. an admin's), but it
// was never granted to the claiming agent. The claim must fail the task
// closed — never decrypt it, never leak it into the response — exactly
// as if the secret didn't exist at all.
func TestClaimTaskByRuntime_UngrantedSecretFailsTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Ungranted secret runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Ungranted secret agent")

	// An admin secret exists and is valid — but is deliberately NOT
	// granted to this agent.
	createClaimSecret(t, ctx, "claim-test-admin-only-secret", "admin-plaintext-value")

	mcpConfig := `{"mcpServers":{"x":{"url":"${secret:claim-test-admin-only-secret}"}}}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, mcpConfig, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "ungranted-secret-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "admin-plaintext-value") {
		t.Fatalf("claim response leaked the ungranted secret's value: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "${secret:") {
		t.Fatalf("claim response leaked the literal reference: %s", w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task != nil {
		t.Fatalf("expected no task in the claim response for an ungranted secret, got: %s", w.Body.String())
	}

	var status, errMsg, failureReason string
	if err := testPool.QueryRow(ctx, `SELECT status, error, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &errMsg, &failureReason); err != nil {
		t.Fatalf("read back task: %v", err)
	}
	if status != "failed" {
		t.Fatalf("task status = %q, want failed", status)
	}
	if failureReason != "secret_resolution_failed" {
		t.Fatalf("failure_reason = %q, want secret_resolution_failed", failureReason)
	}
	if strings.Contains(errMsg, "admin-plaintext-value") {
		t.Fatalf("task error leaks the plaintext: %q", errMsg)
	}
	if !strings.Contains(errMsg, "claim-test-admin-only-secret") {
		t.Fatalf("task error does not name the ungranted secret: %q", errMsg)
	}
}

// TestClaimTaskByRuntime_FailsClosedWhenServiceNilButRefPresent covers
// KHI-972 P2 review finding 2: resolution is unavailable because
// h.Secrets is nil (e.g. MULTICA_SECRETS_KEY missing/invalid), but the
// agent's mcp_config still carries a ${secret:...} reference. The claim
// must fail the task rather than sending the literal reference text to
// the daemon.
func TestClaimTaskByRuntime_FailsClosedWhenServiceNilButRefPresent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// Flag enabled, but no secrets service configured at all.
	withSecretsStore(t, true)
	testHandler.Secrets = nil
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Nil service runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Nil service agent")

	mcpConfig := `{"mcpServers":{"x":{"url":"${secret:claim-test-whatever}"}}}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, mcpConfig, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "nil-service-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "${secret:") {
		t.Fatalf("claim response leaked the literal reference while resolution was unavailable: %s", w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task != nil {
		t.Fatalf("expected no task when the secrets service is nil and a reference is present, got: %s", w.Body.String())
	}

	var status, failureReason string
	if err := testPool.QueryRow(ctx, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &failureReason); err != nil {
		t.Fatalf("read back task: %v", err)
	}
	if status != "failed" || failureReason != "secret_resolution_failed" {
		t.Fatalf("status=%q failure_reason=%q, want failed/secret_resolution_failed", status, failureReason)
	}
}

// TestClaimTaskByRuntime_FailsClosedWhenFlagOffButRefPresent covers the
// other KHI-972 P2 review finding 2 trigger: the secrets_store flag is
// off (service IS configured), but a reference is present anyway — e.g.
// an operator rolled the flag back after agents were already wired up.
func TestClaimTaskByRuntime_FailsClosedWhenFlagOffButRefPresent(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// withSecretsStore(t, false) configures a disabled flag but leaves
	// h.Secrets untouched (nil, since no prior call configured it) — set
	// it explicitly here so this test isolates "flag off" from "service
	// nil" (covered separately above).
	withSecretsStore(t, true)
	flagOffButServiceConfigured := testHandler.Secrets
	withSecretsStore(t, false)
	testHandler.Secrets = flagOffButServiceConfigured
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "Flag off runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Flag off agent")

	mcpConfig := `{"mcpServers":{"x":{"url":"${secret:claim-test-whatever-2}"}}}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, mcpConfig, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "flag-off-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "${secret:") {
		t.Fatalf("claim response leaked the literal reference while the flag was off: %s", w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task != nil {
		t.Fatalf("expected no task when the flag is off and a reference is present, got: %s", w.Body.String())
	}

	var status, failureReason string
	if err := testPool.QueryRow(ctx, `SELECT status, failure_reason FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status, &failureReason); err != nil {
		t.Fatalf("read back task: %v", err)
	}
	if status != "failed" || failureReason != "secret_resolution_failed" {
		t.Fatalf("status=%q failure_reason=%q, want failed/secret_resolution_failed", status, failureReason)
	}
}

// TestClaimTaskByRuntime_NoReferenceStillSucceedsWhenResolutionUnavailable
// is the control: when resolution is unavailable (flag off) but the
// config has NO secret reference at all, the claim must proceed
// normally — the fail-closed guard must only trigger on an actual
// reference, never as a blanket "secrets_store off" failure.
func TestClaimTaskByRuntime_NoReferenceStillSucceedsWhenResolutionUnavailable(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, false)
	ctx := context.Background()

	runtimeID := createClaimReclaimRuntime(t, ctx, "No ref control runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "No ref control agent")

	mcpConfig := `{"mcpServers":{"plain":{"url":"https://example.com"}}}`
	if _, err := testPool.Exec(ctx, `UPDATE agent SET mcp_config = $1 WHERE id = $2`, mcpConfig, agentID); err != nil {
		t.Fatalf("setup: set agent mcp_config: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 0)
		RETURNING id
	`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "no-ref-control-daemon")
	req = withURLParam(req, "runtimeId", runtimeID)
	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.Agent == nil {
		t.Fatalf("expected a normal claim with no secret refs, got: %s", w.Body.String())
	}
	if !strings.Contains(string(claimResp.Task.Agent.McpConfig), "https://example.com") {
		t.Fatalf("mcp_config without any secret ref was unexpectedly altered: %s", claimResp.Task.Agent.McpConfig)
	}
}
