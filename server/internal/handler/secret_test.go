package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/secrets"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// withSecretsStore enables the secrets_store feature flag and (when
// enabled) wires a throwaway secrets.Service onto testHandler for the
// duration of the test, restoring both on cleanup. A fresh random master
// key per call means tests never share key state.
func withSecretsStore(t *testing.T, enabled bool) {
	t.Helper()
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.SecretsStore, featureflag.Rule{Default: enabled})
	flags := featureflag.NewService(provider)

	origFlags := testHandler.FeatureFlags
	origSecrets := testHandler.Secrets
	testHandler.FeatureFlags = flags
	if enabled {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			t.Fatalf("generate random master key: %v", err)
		}
		t.Setenv("SECRET_TEST_HANDLER_KEY", base64.StdEncoding.EncodeToString(key))
		kp, err := secrets.NewEnvKeyProvider("SECRET_TEST_HANDLER_KEY")
		if err != nil {
			t.Fatalf("NewEnvKeyProvider: %v", err)
		}
		testHandler.Secrets = secrets.NewService(kp)
	}
	t.Cleanup(func() {
		testHandler.FeatureFlags = origFlags
		testHandler.Secrets = origSecrets
	})
}

func decodeSecretResponse(t *testing.T, body []byte) SecretResponse {
	t.Helper()
	var resp SecretResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode secret response: %v (%s)", err, body)
	}
	return resp
}

func createSecretAs(t *testing.T, userID, name, value string) *httptest.ResponseRecorder {
	t.Helper()
	body := CreateSecretRequest{Name: name, Value: value}
	w := httptest.NewRecorder()
	req := newRequestAs(userID, "POST", "/api/secrets", body)
	testHandler.CreateSecret(w, req)
	return w
}

// TestCreateSecret_OwnerSucceedsAndValueNeverReturned covers the "happy
// path" plus the explicit "a check that values never appear in API
// output" test item: the raw request/response bytes are scanned for the
// plaintext value.
func TestCreateSecret_OwnerSucceedsAndValueNeverReturned(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ownerID := createProjectPermissionTestMember(t, "owner")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_secret WHERE name = $1`, "test-secret-create")
	})

	const value = "sk-super-secret-plaintext-value"
	w := createSecretAs(t, ownerID, "test-secret-create", value)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), value) {
		t.Fatalf("create response leaked the secret value: %s", w.Body.String())
	}
	resp := decodeSecretResponse(t, w.Body.Bytes())
	if resp.Name != "test-secret-create" || resp.ID == "" || resp.KeyVersion == 0 {
		t.Fatalf("unexpected create response: %+v", resp)
	}

	var plainCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM workspace_secret WHERE name = $1 AND value_encrypted::text LIKE '%' || $2 || '%'`, "test-secret-create", value).Scan(&plainCount); err != nil {
		t.Fatalf("scan plaintext check: %v", err)
	}
	if plainCount != 0 {
		t.Fatalf("stored value_encrypted contains the plaintext value")
	}
}

// TestCreateSecret_DuplicateNameConflicts exercises the per-workspace
// unique-name constraint via the API (409, not a 500).
func TestCreateSecret_DuplicateNameConflicts(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ownerID := createProjectPermissionTestMember(t, "owner")
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_secret WHERE name = $1`, "test-secret-dup")
	})

	w := createSecretAs(t, ownerID, "test-secret-dup", "value-one")
	if w.Code != http.StatusCreated {
		t.Fatalf("first create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	w = createSecretAs(t, ownerID, "test-secret-dup", "value-two")
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate create: expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSecretsAPI_RequiresOwnerOrAdmin covers the "permission checks" test
// item: a plain member is rejected on every mutating + read endpoint, an
// admin and an owner both succeed.
func TestSecretsAPI_RequiresOwnerOrAdmin(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	memberID := createProjectPermissionTestMember(t, "member")
	adminID := createProjectPermissionTestMember(t, "admin")

	w := createSecretAs(t, memberID, "test-secret-perm", "value")
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member create: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req := newRequestAs(memberID, "GET", "/api/secrets", nil)
	testHandler.ListSecrets(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member list: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = createSecretAs(t, adminID, "test-secret-perm", "value")
	if w.Code != http.StatusCreated {
		t.Fatalf("admin create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM workspace_secret WHERE name = $1`, "test-secret-perm")
	})

	// A plain member must not be able to rotate or delete it either.
	w = httptest.NewRecorder()
	req = newRequestAs(memberID, "POST", "/api/secrets/test-secret-perm/rotate", RotateSecretRequest{Value: "new-value"})
	req = withURLParam(req, "name", "test-secret-perm")
	testHandler.RotateSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member rotate: expected 403, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(memberID, "DELETE", "/api/secrets/test-secret-perm", nil)
	req = withURLParam(req, "name", "test-secret-perm")
	testHandler.DeleteSecret(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("plain member delete: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSecretsAPI_AgentActorForbidden mirrors the agent_env MUL-2600 guard:
// a request carrying an agent actor header must be rejected even though
// the auth context otherwise resolves to a real owner member.
func TestSecretsAPI_AgentActorForbidden(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ownerID := createProjectPermissionTestMember(t, "owner")

	w := httptest.NewRecorder()
	req := newRequestAs(ownerID, "GET", "/api/secrets", nil)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", ownerID)
	testHandler.ListSecrets(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("agent actor list: expected 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSecretsAPI_DisabledByFeatureFlag ensures the whole surface returns a
// clear 503 (never a leaky 404/500) when secrets_store is off, even for
// an owner, even if h.Secrets happens to be configured.
func TestSecretsAPI_DisabledByFeatureFlag(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, false)
	ownerID := createProjectPermissionTestMember(t, "owner")

	w := httptest.NewRecorder()
	req := newRequestAs(ownerID, "GET", "/api/secrets", nil)
	testHandler.ListSecrets(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when secrets_store is disabled, got %d: %s", w.Code, w.Body.String())
	}
}

// TestSecretsAPI_DisabledWhenMasterKeyMissing covers scope item 2: even
// with the feature flag on, an unconfigured h.Secrets (no
// MULTICA_SECRETS_KEY) must disable the API with a clear 503, not a panic
// or a 500.
func TestSecretsAPI_DisabledWhenMasterKeyMissing(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	provider := featureflag.NewStaticProvider()
	provider.Set(featureflags.SecretsStore, featureflag.Rule{Default: true})
	origFlags := testHandler.FeatureFlags
	origSecrets := testHandler.Secrets
	testHandler.FeatureFlags = featureflag.NewService(provider)
	testHandler.Secrets = nil
	t.Cleanup(func() {
		testHandler.FeatureFlags = origFlags
		testHandler.Secrets = origSecrets
	})

	ownerID := createProjectPermissionTestMember(t, "owner")
	w := httptest.NewRecorder()
	req := newRequestAs(ownerID, "GET", "/api/secrets", nil)
	testHandler.ListSecrets(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when h.Secrets is nil, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRotateSecret_ThenDeleteSecret covers the remaining CRUD surface end
// to end against the real API, including the audit trail and the
// not-found path for an unknown name.
func TestRotateSecret_ThenDeleteSecret(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	withSecretsStore(t, true)
	ownerID := createProjectPermissionTestMember(t, "owner")

	w := createSecretAs(t, ownerID, "test-secret-rotate", "initial-value")
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	created := decodeSecretResponse(t, w.Body.Bytes())

	w = httptest.NewRecorder()
	req := newRequestAs(ownerID, "POST", "/api/secrets/test-secret-rotate/rotate", RotateSecretRequest{Value: "rotated-value"})
	req = withURLParam(req, "name", "test-secret-rotate")
	testHandler.RotateSecret(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "rotated-value") {
		t.Fatalf("rotate response leaked the new value: %s", w.Body.String())
	}
	rotated := decodeSecretResponse(t, w.Body.Bytes())
	if rotated.ID != created.ID {
		t.Fatalf("rotate changed the secret identity: created=%s rotated=%s", created.ID, rotated.ID)
	}
	if rotated.RotatedAt == nil {
		t.Fatalf("rotated_at not stamped after rotate")
	}

	// Rotating an unknown name is a 404, not a 500.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "POST", "/api/secrets/does-not-exist/rotate", RotateSecretRequest{Value: "x"})
	req = withURLParam(req, "name", "does-not-exist")
	testHandler.RotateSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("rotate unknown: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// List should surface it with metadata only.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "GET", "/api/secrets", nil)
	testHandler.ListSecrets(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "rotated-value") || strings.Contains(w.Body.String(), "initial-value") {
		t.Fatalf("list response leaked a secret value: %s", w.Body.String())
	}

	// Delete, then verify it's gone and a second delete 404s.
	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "DELETE", "/api/secrets/test-secret-rotate", nil)
	req = withURLParam(req, "name", "test-secret-rotate")
	testHandler.DeleteSecret(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(ownerID, "DELETE", "/api/secrets/test-secret-rotate", nil)
	req = withURLParam(req, "name", "test-secret-rotate")
	testHandler.DeleteSecret(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("second delete: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	var actions []string
	rows, err := testPool.Query(context.Background(), `SELECT action FROM activity_log WHERE workspace_id = $1 AND details->>'secret_name' = $2 ORDER BY created_at`, testWorkspaceID, "test-secret-rotate")
	if err != nil {
		t.Fatalf("query activity_log: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var action string
		if err := rows.Scan(&action); err != nil {
			t.Fatalf("scan action: %v", err)
		}
		actions = append(actions, action)
	}
	want := []string{secretActivityCreated, secretActivityRotated, secretActivityDeleted}
	if len(actions) != len(want) {
		t.Fatalf("activity_log actions = %v, want %v", actions, want)
	}
	for i, a := range actions {
		if a != want[i] {
			t.Fatalf("activity_log actions = %v, want %v", actions, want)
		}
	}
}
