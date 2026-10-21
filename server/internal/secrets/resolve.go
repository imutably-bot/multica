package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Resolver substitutes ${secret:NAME} references with decrypted values at
// task claim time (daemon.ClaimTaskByRuntime, right after the MCP overlay
// merge). It holds no DB connection of its own — every call takes the
// *db.Queries to use, so resolution runs on whatever connection the caller
// already has open rather than owning a second one.
type Resolver struct {
	Encryptor *Encryptor
}

// NewResolver constructs a Resolver bound to the given Encryptor.
func NewResolver(enc *Encryptor) *Resolver {
	return &Resolver{Encryptor: enc}
}

// ResolveJSON walks every string leaf under raw (objects and arrays,
// recursively — covers mcp_config's nested url/headers/env shapes),
// resolves each distinct ${secret:NAME} reference at most once, and
// returns the substituted JSON. raw is returned unchanged when it
// contains no references at all (including when raw is empty), so a task
// with no secret refs pays zero extra DB round-trips.
//
// Numbers are decoded with json.Number (not float64) and written back
// byte-for-byte, so a port number or timeout value in the original config
// is never reformatted.
//
// Fails closed: if ANY referenced secret is missing or cannot be
// decrypted, this returns an error and NOT the original JSON — a
// resolution failure must never let the literal ${secret:NAME} text (or
// any other partially-resolved config) reach the daemon.
func (r *Resolver) ResolveJSON(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return raw, nil
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("secrets: parse config for reference resolution: %w", err)
	}

	names := map[string]struct{}{}
	collectRefs(v, names)
	if len(names) == 0 {
		return raw, nil
	}

	cache, err := r.resolveAll(ctx, q, workspaceID, names)
	if err != nil {
		return nil, err
	}

	out, err := json.Marshal(substituteRefs(v, cache))
	if err != nil {
		return nil, fmt.Errorf("secrets: marshal resolved config: %w", err)
	}
	return out, nil
}

// ResolveStringMap applies the same reference substitution to a flat
// string map (custom_env). Same fail-closed contract as ResolveJSON.
func (r *Resolver) ResolveStringMap(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, m map[string]string) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}

	names := map[string]struct{}{}
	for _, v := range m {
		for _, match := range RefPattern.FindAllStringSubmatch(v, -1) {
			names[match[1]] = struct{}{}
		}
	}
	if len(names) == 0 {
		return m, nil
	}

	cache, err := r.resolveAll(ctx, q, workspaceID, names)
	if err != nil {
		return nil, err
	}

	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = substituteString(v, cache)
	}
	return out, nil
}

// resolveAll decrypts every distinct referenced name once. Returning on
// the first failure (rather than collecting every missing name) keeps the
// resulting task error simple; the set is small in practice (one task's
// MCP config plus custom_env).
func (r *Resolver) resolveAll(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, names map[string]struct{}) (map[string]string, error) {
	cache := make(map[string]string, len(names))
	for name := range names {
		value, err := r.lookup(ctx, q, workspaceID, name)
		if err != nil {
			return nil, err
		}
		cache[name] = value
	}
	return cache, nil
}

// lookup resolves and decrypts a single secret by name. Every error
// message carries only the secret's NAME (not sensitive) — never the
// ciphertext, the plaintext, or low-level crypto diagnostics.
func (r *Resolver) lookup(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, name string) (string, error) {
	row, err := q.GetWorkspaceSecretByName(ctx, db.GetWorkspaceSecretByNameParams{WorkspaceID: workspaceID, Name: name})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("secret %q: not found", name)
		}
		return "", fmt.Errorf("secret %q: lookup failed", name)
	}
	plaintext, err := r.Encryptor.Decrypt(row.ValueEncrypted, row.KeyVersion)
	if err != nil {
		return "", fmt.Errorf("secret %q: could not be decrypted", name)
	}
	return string(plaintext), nil
}

func substituteString(s string, cache map[string]string) string {
	if !RefPattern.MatchString(s) {
		return s
	}
	return RefPattern.ReplaceAllStringFunc(s, func(match string) string {
		sub := RefPattern.FindStringSubmatch(match)
		return cache[sub[1]]
	})
}

// collectRefs walks a decoded JSON tree (map[string]any / []any / string /
// json.Number / bool / nil) and records every referenced secret name.
func collectRefs(v any, names map[string]struct{}) {
	switch t := v.(type) {
	case string:
		for _, match := range RefPattern.FindAllStringSubmatch(t, -1) {
			names[match[1]] = struct{}{}
		}
	case map[string]any:
		for _, vv := range t {
			collectRefs(vv, names)
		}
	case []any:
		for _, vv := range t {
			collectRefs(vv, names)
		}
	}
}

// substituteRefs rebuilds the tree with every string leaf substituted.
// Non-string leaves (json.Number, bool, nil) pass through unchanged.
func substituteRefs(v any, cache map[string]string) any {
	switch t := v.(type) {
	case string:
		return substituteString(t, cache)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = substituteRefs(vv, cache)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = substituteRefs(vv, cache)
		}
		return out
	default:
		return v
	}
}
