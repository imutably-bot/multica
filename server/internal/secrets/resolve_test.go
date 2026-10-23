package secrets

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func createTestSecret(t *testing.T, ctx context.Context, enc *Encryptor, name, value string) {
	t.Helper()
	envelope, keyVersion, err := enc.Encrypt([]byte(value))
	if err != nil {
		t.Fatalf("Encrypt(%s): %v", name, err)
	}
	if _, err := testQueries.CreateWorkspaceSecret(ctx, db.CreateWorkspaceSecretParams{
		WorkspaceID:    testWorkspaceID,
		Name:           name,
		ValueEncrypted: envelope,
		KeyVersion:     keyVersion,
	}); err != nil {
		t.Fatalf("CreateWorkspaceSecret(%s): %v", name, err)
	}
	t.Cleanup(func() {
		testQueries.DeleteWorkspaceSecret(context.Background(), db.DeleteWorkspaceSecretParams{
			WorkspaceID: testWorkspaceID,
			Name:        name,
		})
	})
}

// allowSet builds a Resolver "allowed" grant set from a literal name
// list — the test-side equivalent of GetGrantedSecretNamesForAgent.
func allowSet(names ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, n := range names {
		out[n] = struct{}{}
	}
	return out
}

// TestResolveJSON_NestedURLHeadersEnv covers KHI-972 P2 test item
// "resolution in nested JSON (url, headers, env)": a reference buried
// three levels deep under mcpServers.<name>.{url,headers,env} must be
// substituted, repeated references to the same name resolve to the same
// value, and unrelated strings are left untouched.
func TestResolveJSON_NestedURLHeadersEnv(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_NESTED")
	createTestSecret(t, ctx, enc, "OBOT_TOKEN", "obot-secret-value")
	createTestSecret(t, ctx, enc, "DB_PASSWORD", "db-secret-value")

	resolver := NewResolver(enc)
	raw := json.RawMessage(`{
		"mcpServers": {
			"n8n-obot": {
				"type": "http",
				"url": "https://n8n.example.com/mcp?token=${secret:OBOT_TOKEN}",
				"headers": {"Authorization": "Bearer ${secret:OBOT_TOKEN}"},
				"env": {"DB_PASSWORD": "${secret:DB_PASSWORD}", "PLAIN": "unchanged"},
				"port": 8443
			}
		}
	}`)

	resolved, err := resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, allowSet("OBOT_TOKEN", "DB_PASSWORD"), raw)
	if err != nil {
		t.Fatalf("ResolveJSON: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(resolved, &decoded); err != nil {
		t.Fatalf("decode resolved: %v", err)
	}
	server := decoded["mcpServers"].(map[string]any)["n8n-obot"].(map[string]any)

	if got := server["url"].(string); got != "https://n8n.example.com/mcp?token=obot-secret-value" {
		t.Fatalf("url not resolved: %s", got)
	}
	headers := server["headers"].(map[string]any)
	if got := headers["Authorization"].(string); got != "Bearer obot-secret-value" {
		t.Fatalf("header not resolved: %s", got)
	}
	env := server["env"].(map[string]any)
	if got := env["DB_PASSWORD"].(string); got != "db-secret-value" {
		t.Fatalf("env not resolved: %s", got)
	}
	if got := env["PLAIN"].(string); got != "unchanged" {
		t.Fatalf("unrelated string value was mutated: %s", got)
	}
	if got := server["type"].(string); got != "http" {
		t.Fatalf("unrelated string value was mutated: %s", got)
	}
	// Numbers must round-trip exactly (json.Number, not float64
	// reformatting) — checked against the raw bytes since re-decoding
	// with the standard decoder would itself turn it back into float64.
	if !strings.Contains(string(resolved), `"port":8443`) {
		t.Fatalf("port number reformatted: %s", resolved)
	}

	if strings.Contains(string(resolved), "${secret:") {
		t.Fatalf("resolved JSON still contains a literal reference: %s", resolved)
	}
}

// TestResolveJSON_NoReferences_ReturnsInputUnchanged ensures configs with
// no ${secret:...} text are passed through byte-for-byte with no lookups
// — in particular with a nil/empty allowed set, which must still be a
// no-op when there is nothing to resolve.
func TestResolveJSON_NoReferences_ReturnsInputUnchanged(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_NOOP")
	resolver := NewResolver(enc)

	raw := json.RawMessage(`{"mcpServers":{"plain":{"url":"https://example.com","port":443}}}`)
	resolved, err := resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, nil, raw)
	if err != nil {
		t.Fatalf("ResolveJSON: %v", err)
	}
	if string(resolved) != string(raw) {
		t.Fatalf("ResolveJSON mutated a config with no references: got %s, want %s", resolved, raw)
	}
}

// TestResolveJSON_MissingSecretFails covers KHI-972 P2 test item "missing
// secret": a reference to a name that doesn't exist in the workspace must
// fail closed and must never let the literal reference through.
func TestResolveJSON_MissingSecretFails(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_MISSING")
	resolver := NewResolver(enc)

	raw := json.RawMessage(`{"mcpServers":{"x":{"url":"${secret:DOES_NOT_EXIST}"}}}`)
	if _, err := resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, allowSet("DOES_NOT_EXIST"), raw); err == nil {
		t.Fatalf("expected an error for a missing secret reference")
	}
}

// TestResolveJSON_UngrantedSecretFails is the KHI-972 P2 review finding 1
// regression test: a secret that exists and decrypts fine, but was never
// granted to the claiming agent, must fail the same way a missing secret
// does — and critically, lookup must never even reach the database (the
// grant check runs first), so an ungranted name can't be used to probe
// whether a secret with that name exists in the workspace.
func TestResolveJSON_UngrantedSecretFails(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_UNGRANTED")
	createTestSecret(t, ctx, enc, "ADMIN_ONLY_SECRET", "admin-plaintext-value")

	resolver := NewResolver(enc)
	raw := json.RawMessage(`{"mcpServers":{"x":{"url":"${secret:ADMIN_ONLY_SECRET}"}}}`)

	// No allowed set at all (nil) — the zero-value default for an agent
	// with no grants.
	_, err := resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, nil, raw)
	if err == nil {
		t.Fatalf("expected an error for an ungranted secret reference")
	}
	if strings.Contains(err.Error(), "admin-plaintext-value") {
		t.Fatalf("error leaks the plaintext: %v", err)
	}

	// An allowed set that grants a DIFFERENT secret must not let this one
	// through either.
	_, err = resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, allowSet("SOME_OTHER_SECRET"), raw)
	if err == nil {
		t.Fatalf("expected an error: allowed set does not include the referenced secret")
	}
}

// TestResolveJSON_UndecryptableSecretFails simulates a row that cannot be
// decrypted under the resolver's current master key (e.g. master key was
// rotated/changed out from under an old row) and asserts the claim-time
// resolver fails closed instead of emitting garbage or the ciphertext.
func TestResolveJSON_UndecryptableSecretFails(t *testing.T) {
	ctx := context.Background()
	writerEnc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_UNDECRYPTABLE_A")
	readerEnc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_UNDECRYPTABLE_B")
	createTestSecret(t, ctx, writerEnc, "STALE_KEY_SECRET", "value-encrypted-under-a-different-key")

	resolver := NewResolver(readerEnc)
	raw := json.RawMessage(`{"mcpServers":{"x":{"url":"${secret:STALE_KEY_SECRET}"}}}`)
	_, err := resolver.ResolveJSON(ctx, testQueries, testWorkspaceID, allowSet("STALE_KEY_SECRET"), raw)
	if err == nil {
		t.Fatalf("expected an error when the stored envelope can't be decrypted under the current key")
	}
	if strings.Contains(err.Error(), "value-encrypted-under-a-different-key") {
		t.Fatalf("error leaks the plaintext: %v", err)
	}
}

// TestResolveStringMap_CustomEnv covers substitution in agent custom_env,
// the other claim-time surface besides mcp_config.
func TestResolveStringMap_CustomEnv(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_CUSTOM_ENV")
	createTestSecret(t, ctx, enc, "API_KEY", "api-key-value")

	resolver := NewResolver(enc)
	resolved, err := resolver.ResolveStringMap(ctx, testQueries, testWorkspaceID, allowSet("API_KEY"), map[string]string{
		"FOO_API_KEY": "${secret:API_KEY}",
		"PLAIN":       "unchanged",
	})
	if err != nil {
		t.Fatalf("ResolveStringMap: %v", err)
	}
	if resolved["FOO_API_KEY"] != "api-key-value" {
		t.Fatalf("custom_env value not resolved: %+v", resolved)
	}
	if resolved["PLAIN"] != "unchanged" {
		t.Fatalf("unrelated custom_env value mutated: %+v", resolved)
	}
}

// TestResolveStringMap_MissingSecretFails mirrors the JSON case for the
// custom_env surface.
func TestResolveStringMap_MissingSecretFails(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_CUSTOM_ENV_MISSING")
	resolver := NewResolver(enc)

	if _, err := resolver.ResolveStringMap(ctx, testQueries, testWorkspaceID, allowSet("NOPE"), map[string]string{
		"X": "${secret:NOPE}",
	}); err == nil {
		t.Fatalf("expected an error for a missing secret reference in custom_env")
	}
}

// TestResolveStringMap_UngrantedSecretFails is the custom_env twin of
// TestResolveJSON_UngrantedSecretFails.
func TestResolveStringMap_UngrantedSecretFails(t *testing.T) {
	ctx := context.Background()
	enc := testEncryptor(t, "SECRETS_TEST_KEY_RESOLVE_CUSTOM_ENV_UNGRANTED")
	createTestSecret(t, ctx, enc, "ADMIN_ONLY_ENV_SECRET", "admin-env-plaintext")

	resolver := NewResolver(enc)
	_, err := resolver.ResolveStringMap(ctx, testQueries, testWorkspaceID, nil, map[string]string{
		"X": "${secret:ADMIN_ONLY_ENV_SECRET}",
	})
	if err == nil {
		t.Fatalf("expected an error for an ungranted secret reference in custom_env")
	}
	if strings.Contains(err.Error(), "admin-env-plaintext") {
		t.Fatalf("error leaks the plaintext: %v", err)
	}
}
