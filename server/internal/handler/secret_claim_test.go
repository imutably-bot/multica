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

func createClaimSecret(t *testing.T, ctx context.Context, name, value string) {
	t.Helper()
	envelope, keyVersion, err := testHandler.Secrets.Encrypt([]byte(value))
	if err != nil {
		t.Fatalf("encrypt test secret %s: %v", name, err)
	}
	workspaceID := parseUUID(testWorkspaceID)
	if _, err := testHandler.Queries.CreateWorkspaceSecret(ctx, db.CreateWorkspaceSecretParams{
		WorkspaceID:    workspaceID,
		Name:           name,
		ValueEncrypted: envelope,
		KeyVersion:     keyVersion,
	}); err != nil {
		t.Fatalf("CreateWorkspaceSecret(%s): %v", name, err)
	}
	t.Cleanup(func() {
		testHandler.Queries.DeleteWorkspaceSecret(context.Background(), db.DeleteWorkspaceSecretParams{
			WorkspaceID: workspaceID,
			Name:        name,
		})
	})
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

	createClaimSecret(t, ctx, "claim-test-obot-token", "obot-secret-value")
	createClaimSecret(t, ctx, "claim-test-db-pass", "db-secret-value")

	runtimeID := createClaimReclaimRuntime(t, ctx, "Secret resolve runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Secret resolve agent")

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
