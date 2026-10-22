package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// createHandlerTestAgentOwnedBy inserts an agent on the shared handler
// test runtime, owned by ownerUserID, and schedules cleanup.
func createHandlerTestAgentOwnedBy(t *testing.T, ownerUserID, name string) string {
	t.Helper()
	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks, owner_id
		)
		VALUES ($1, $2, '', 'cloud', '{}'::jsonb, $3, 'private', 1, $4)
		RETURNING id
	`, testWorkspaceID, name, handlerTestRuntimeID(t), ownerUserID).Scan(&agentID); err != nil {
		t.Fatalf("create agent owned by %s: %v", ownerUserID, err)
	}
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID) })
	return agentID
}

// TestAgentSecretGrants_OwnerCanGrantListRevoke_MemberAndAgentForbidden
// covers the KHI-972 P2 review finding-1 grant API: permission checks
// (owner succeeds, plain member forbidden even when they own the agent,
// agent actor forbidden), the 404/409 edges, and the audit trail.
func TestAgentSecretGrants_OwnerCanGrantListRevoke_MemberAndAgentForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ctx := context.Background()

	ownerID := createProjectPermissionTestMember(t, "owner")
	memberID := createProjectPermissionTestMember(t, "member")
	agentID := createHandlerTestAgentOwnedBy(t, memberID, "agent-secret-grant-test-agent")

	secretW := createSecretAs(t, ownerID, "agent-secret-grant-test-secret", "grant-test-value")
	if secretW.Code != http.StatusCreated {
		t.Fatalf("create secret fixture: expected 201, got %d: %s", secretW.Code, secretW.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_secret WHERE name = $1`, "agent-secret-grant-test-secret")
	})

	// Plain member — even though they own the agent — cannot grant.
	w := httptest.NewRecorder()
	req := newRequestAs(memberID, "POST", "/api/agents/"+agentID+"/secrets", GrantAgentSecretRequest{SecretName: "agent-secret-grant-test-secret"})
	req = withURLParam(req, "id", agentID)
	testHandler.GrantAgentSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member grant: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Agent actor cannot grant either.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "POST", "/api/agents/"+agentID+"/secrets", GrantAgentSecretRequest{SecretName: "agent-secret-grant-test-secret"})
	req = withURLParam(req, "id", agentID)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", ownerID)
	testHandler.GrantAgentSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent actor grant: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Unknown secret name -> 404, not 500.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "POST", "/api/agents/"+agentID+"/secrets", GrantAgentSecretRequest{SecretName: "does-not-exist"})
	req = withURLParam(req, "id", agentID)
	testHandler.GrantAgentSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("grant unknown secret: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Owner succeeds.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "POST", "/api/agents/"+agentID+"/secrets", GrantAgentSecretRequest{SecretName: "agent-secret-grant-test-secret"})
	req = withURLParam(req, "id", agentID)
	testHandler.GrantAgentSecret(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("owner grant: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "grant-test-value") {
		t.Fatalf("grant response leaked the secret value: %s", w.Body.String())
	}
	var grant AgentSecretGrantResponse
	if err := json.Unmarshal(w.Body.Bytes(), &grant); err != nil {
		t.Fatalf("decode grant response: %v", err)
	}
	if grant.SecretName != "agent-secret-grant-test-secret" || grant.AgentID != agentID {
		t.Fatalf("unexpected grant response: %+v", grant)
	}

	// Duplicate grant -> 409.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "POST", "/api/agents/"+agentID+"/secrets", GrantAgentSecretRequest{SecretName: "agent-secret-grant-test-secret"})
	req = withURLParam(req, "id", agentID)
	testHandler.GrantAgentSecret(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate grant: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// Plain member cannot list grants either.
	w = httptest.NewRecorder()
	req = newRequestAs(memberID, "GET", "/api/agents/"+agentID+"/secrets", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentSecretGrants(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member list: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Owner can list and sees the grant, metadata only.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "GET", "/api/agents/"+agentID+"/secrets", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentSecretGrants(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "grant-test-value") {
		t.Fatalf("list response leaked the secret value: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "agent-secret-grant-test-secret") {
		t.Fatalf("list response missing the granted secret name: %s", w.Body.String())
	}

	// Plain member cannot revoke.
	w = httptest.NewRecorder()
	req = newRequestAs(memberID, "DELETE", "/api/agents/"+agentID+"/secrets/agent-secret-grant-test-secret", nil)
	req = withURLParams(req, "id", agentID, "name", "agent-secret-grant-test-secret")
	testHandler.RevokeAgentSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member revoke: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// Owner revokes; a second revoke 404s.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "DELETE", "/api/agents/"+agentID+"/secrets/agent-secret-grant-test-secret", nil)
	req = withURLParams(req, "id", agentID, "name", "agent-secret-grant-test-secret")
	testHandler.RevokeAgentSecret(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("owner revoke: expected 204, got %d: %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "DELETE", "/api/agents/"+agentID+"/secrets/agent-secret-grant-test-secret", nil)
	req = withURLParams(req, "id", agentID, "name", "agent-secret-grant-test-secret")
	testHandler.RevokeAgentSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second revoke: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Audit trail: granted then revoked, in order.
	rows, err := testPool.Query(ctx, `SELECT action FROM activity_log WHERE workspace_id = $1 AND details->>'agent_id' = $2 AND details->>'secret_name' = $3 ORDER BY created_at`, testWorkspaceID, agentID, "agent-secret-grant-test-secret")
	if err != nil {
		t.Fatalf("query activity_log: %v", err)
	}
	defer rows.Close()
	var actions []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatalf("scan action: %v", err)
		}
		actions = append(actions, a)
	}
	want := []string{agentSecretActivityGranted, agentSecretActivityRevoked}
	if len(actions) != len(want) || actions[0] != want[0] || actions[1] != want[1] {
		t.Fatalf("activity_log actions = %v, want %v", actions, want)
	}
}

// TestAgentSecretGrants_DisabledByFeatureFlag ensures the grant endpoints
// 503 (not 404/500) when secrets_store is off, same as the base
// /api/secrets surface.
func TestAgentSecretGrants_DisabledByFeatureFlag(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, false)
	ownerID := createProjectPermissionTestMember(t, "owner")
	agentID := createHandlerTestAgentOwnedBy(t, ownerID, "agent-secret-flag-off-agent")

	w := httptest.NewRecorder()
	req := newRequestAs(ownerID, "GET", "/api/agents/"+agentID+"/secrets", nil)
	req = withURLParam(req, "id", agentID)
	testHandler.ListAgentSecretGrants(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when secrets_store is disabled, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAgent_RejectsSecretRefUnlessOwnerAdmin is the KHI-972 P2
// review finding-1 stopgap regression test on the CREATE path: a plain
// member must not be able to wire up a ${secret:...} reference into
// their own new agent's custom_env or mcp_config, even though they are
// otherwise fully entitled to create the agent. An owner/admin submitting
// the identical payload succeeds.
func TestCreateAgent_RejectsSecretRefUnlessOwnerAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID, _, plainMemberID := runtimeVisibilityFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE agent_runtime SET visibility = 'public' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("flip runtime to public: %v", err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent WHERE workspace_id = $1 AND name LIKE 'secret-ref-create-test-%'`, testWorkspaceID)
	})

	memberBody := map[string]any{
		"name":                 "secret-ref-create-test-member",
		"runtime_id":           runtimeID,
		"max_concurrent_tasks": 1,
		"custom_env":           map[string]string{"TOKEN": "${secret:anything}"},
	}
	w := httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(plainMemberID, http.MethodPost, "/api/agents", memberBody))
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member create with secret ref in custom_env: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	memberBodyMcp := map[string]any{
		"name":                 "secret-ref-create-test-member-mcp",
		"runtime_id":           runtimeID,
		"max_concurrent_tasks": 1,
		"mcp_config":           map[string]any{"mcpServers": map[string]any{"x": map[string]any{"url": "${secret:anything}"}}},
	}
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(plainMemberID, http.MethodPost, "/api/agents", memberBodyMcp))
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member create with secret ref in mcp_config: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	ownerBody := map[string]any{
		"name":                 "secret-ref-create-test-owner",
		"runtime_id":           runtimeID,
		"max_concurrent_tasks": 1,
		"custom_env":           map[string]string{"TOKEN": "${secret:anything}"},
	}
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(testUserID, http.MethodPost, "/api/agents", ownerBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("owner create with secret ref: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// A plain member creating an agent with NO secret ref at all must
	// still succeed — the guard must not become a blanket block on plain
	// members creating agents.
	plainBody := map[string]any{
		"name":                 "secret-ref-create-test-member-plain",
		"runtime_id":           runtimeID,
		"max_concurrent_tasks": 1,
	}
	w = httptest.NewRecorder()
	testHandler.CreateAgent(w, newRequestAs(plainMemberID, http.MethodPost, "/api/agents", plainBody))
	if w.Code != http.StatusCreated {
		t.Fatalf("plain member create with no secret ref: expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateAgent_RejectsMcpConfigSecretRefUnlessOwnerAdmin is the UPDATE
// path twin: canManageAgent lets a plain member update their OWN agent's
// mcp_config (custom_env is already hard-rejected on this endpoint), but
// writing a ${secret:...} reference into it must still require
// owner/admin.
func TestUpdateAgent_RejectsMcpConfigSecretRefUnlessOwnerAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	memberID := createProjectPermissionTestMember(t, "member")
	agentID := createHandlerTestAgentOwnedBy(t, memberID, "secret-ref-update-test-agent")

	body := map[string]any{
		"mcp_config": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"url": "${secret:anything}"}}},
	}
	w := httptest.NewRecorder()
	req := newRequestAs(memberID, http.MethodPut, "/api/agents/"+agentID, body)
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent-owning plain member update with secret ref: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	// The same member CAN update mcp_config to something with no
	// reference — the guard is specific to ${secret:...}, not a blanket
	// block on the agent owner editing mcp_config.
	plainMcpBody := map[string]any{
		"mcp_config": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"url": "https://example.com"}}},
	}
	w = httptest.NewRecorder()
	req = newRequestAs(memberID, http.MethodPut, "/api/agents/"+agentID, plainMcpBody)
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("agent-owning plain member update with no secret ref: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// A workspace owner updating the SAME agent's mcp_config with a
	// secret ref succeeds.
	ownerBody := map[string]any{
		"mcp_config": map[string]any{"mcpServers": map[string]any{"x": map[string]any{"url": "${secret:anything}"}}},
	}
	w = httptest.NewRecorder()
	req = newRequestAs(testUserID, http.MethodPut, "/api/agents/"+agentID, ownerBody)
	req = withURLParam(req, "id", agentID)
	testHandler.UpdateAgent(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("owner update with secret ref: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}
