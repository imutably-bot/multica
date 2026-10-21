// KHI-972 P2: encrypted workspace secret store API. Every handler here
// enforces the same contract: owners/admins only (never agents, even an
// agent whose host member is an owner — mirrors the MUL-2600 agent_env
// precedent), values are never read back once written, and every
// create/rotate/delete writes an activity_log row. See secretActivity*
// constants and authorizeSecretsAccess below.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/featureflags"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/secrets"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// secretActivityCreated/Rotated/Deleted are the activity_log `action`
// constants for the three mutating secret endpoints. Rows are written
// with issue_id NULL (secret management is not tied to any issue), same
// convention as agent_env_revealed/agent_env_updated.
const (
	secretActivityCreated = "workspace_secret_created"
	secretActivityRotated = "workspace_secret_rotated"
	secretActivityDeleted = "workspace_secret_deleted"
)

// SecretResponse is the metadata-only wire shape for every secret
// endpoint. There is intentionally no Value field anywhere in this
// file — a secret's plaintext is written once (create) or replaced
// (rotate) and never read back by any API response.
type SecretResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	KeyVersion  int32   `json:"key_version"`
	Description *string `json:"description,omitempty"`
	CreatedBy   *string `json:"created_by,omitempty"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	RotatedAt   *string `json:"rotated_at,omitempty"`
}

// CreateSecretRequest is the wire shape for POST /api/secrets. Value is
// the one place a secret's plaintext appears in an API request; it must
// never be echoed back or logged.
type CreateSecretRequest struct {
	Name        string  `json:"name"`
	Value       string  `json:"value"`
	Description *string `json:"description,omitempty"`
}

// RotateSecretRequest is the wire shape for POST /api/secrets/{name}/rotate.
type RotateSecretRequest struct {
	Value string `json:"value"`
}

// authorizeSecretsAccess enforces the per-request auth contract shared by
// every secret endpoint:
//
//  1. The secrets store must be configured (h.Secrets != nil, i.e.
//     MULTICA_SECRETS_KEY is set and valid) and the secrets_store feature
//     flag must be enabled for this workspace. Either gap returns 503 —
//     scope item 3/the P0 plan's "feature stays disabled with a clear log
//     line" contract, surfaced to API callers as a clear error rather
//     than a silent 404.
//  2. The actor must resolve to a member (human). Any request authored by
//     an agent token is rejected, even one whose backing member is a
//     workspace owner — same lateral-movement guard as agent_env
//     (MUL-2600): an agent must never be able to read or rotate secrets
//     via its host's owner credentials.
//  3. The member must be a workspace owner or admin.
func (h *Handler) authorizeSecretsAccess(w http.ResponseWriter, r *http.Request) (db.Member, bool) {
	if h.Secrets == nil {
		writeError(w, http.StatusServiceUnavailable, "secrets store is not configured on this server")
		return db.Member{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	if !featureflags.SecretsStoreEnabled(r.Context(), h.FeatureFlags) {
		writeError(w, http.StatusServiceUnavailable, "secrets store is not enabled for this workspace")
		return db.Member{}, false
	}

	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access the secrets API")
		return db.Member{}, false
	}

	return h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin")
}

func secretToResponse(s db.WorkspaceSecret) SecretResponse {
	return SecretResponse{
		ID:          uuidToString(s.ID),
		Name:        s.Name,
		KeyVersion:  s.KeyVersion,
		Description: textToPtr(s.Description),
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
		RotatedAt:   timestampToPtr(s.RotatedAt),
	}
}

func listedSecretToResponse(s db.ListWorkspaceSecretsRow) SecretResponse {
	return SecretResponse{
		ID:          uuidToString(s.ID),
		Name:        s.Name,
		KeyVersion:  s.KeyVersion,
		Description: textToPtr(s.Description),
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
		RotatedAt:   timestampToPtr(s.RotatedAt),
	}
}

func rotatedSecretToResponse(s db.RotateWorkspaceSecretValueRow) SecretResponse {
	return SecretResponse{
		ID:          uuidToString(s.ID),
		Name:        s.Name,
		KeyVersion:  s.KeyVersion,
		Description: textToPtr(s.Description),
		CreatedBy:   uuidToPtr(s.CreatedBy),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
		RotatedAt:   timestampToPtr(s.RotatedAt),
	}
}

// CreateSecret handles POST /api/secrets. Encrypt-then-persist-then-audit
// all happen inside one transaction (mirrors UpdateAgentEnv): an audit
// write failure rolls back the insert, so a secret can never exist
// without a corresponding activity_log row.
func (h *Handler) CreateSecret(w http.ResponseWriter, r *http.Request) {
	member, ok := h.authorizeSecretsAccess(w, r)
	if !ok {
		return
	}

	var req CreateSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !secrets.ValidName(req.Name) {
		writeError(w, http.StatusBadRequest, "name must match "+secrets.NamePattern.String())
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	envelope, keyVersion, err := h.Secrets.Encrypt([]byte(req.Value))
	if err != nil {
		slog.Error("create secret: encrypt failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to encrypt secret value")
		return
	}

	var description pgtype.Text
	if req.Description != nil {
		description = pgtype.Text{String: *req.Description, Valid: true}
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("create secret: begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create secret")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	created, err := qtx.CreateWorkspaceSecret(r.Context(), db.CreateWorkspaceSecretParams{
		WorkspaceID:    member.WorkspaceID,
		Name:           req.Name,
		ValueEncrypted: envelope,
		KeyVersion:     keyVersion,
		Description:    description,
		CreatedBy:      member.UserID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "a secret with this name already exists")
			return
		}
		slog.Warn("create secret: insert failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create secret")
		return
	}

	details, _ := json.Marshal(map[string]any{"secret_name": req.Name, "secret_id": uuidToString(created.ID)})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: member.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      secretActivityCreated,
		Details:     details,
	}); err != nil {
		slog.Error("workspace_secret_created audit write failed; rolling back create",
			append(logger.RequestAttrs(r), "error", err, "secret_name", req.Name)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; secret creation rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("create secret: tx commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create secret")
		return
	}

	writeJSON(w, http.StatusCreated, SecretResponse{
		ID:          uuidToString(created.ID),
		Name:        created.Name,
		KeyVersion:  created.KeyVersion,
		Description: textToPtr(created.Description),
		CreatedBy:   uuidToPtr(created.CreatedBy),
		CreatedAt:   timestampToString(created.CreatedAt),
		UpdatedAt:   timestampToString(created.UpdatedAt),
		RotatedAt:   timestampToPtr(created.RotatedAt),
	})
}

// ListSecrets handles GET /api/secrets. Backed by ListWorkspaceSecrets,
// which does not select value_encrypted at the SQL level — there is no
// code path in this handler that could leak a value even by accident.
func (h *Handler) ListSecrets(w http.ResponseWriter, r *http.Request) {
	member, ok := h.authorizeSecretsAccess(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListWorkspaceSecrets(r.Context(), member.WorkspaceID)
	if err != nil {
		slog.Warn("list secrets failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}

	out := make([]SecretResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, listedSecretToResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

// RotateSecret handles POST /api/secrets/{name}/rotate: replaces the
// stored value under a freshly generated data key (and the provider's
// current wrapping key_version), leaving name/description/created_by
// untouched. Same transactional persist+audit contract as CreateSecret.
func (h *Handler) RotateSecret(w http.ResponseWriter, r *http.Request) {
	member, ok := h.authorizeSecretsAccess(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")

	var req RotateSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Value == "" {
		writeError(w, http.StatusBadRequest, "value is required")
		return
	}

	envelope, keyVersion, err := h.Secrets.Encrypt([]byte(req.Value))
	if err != nil {
		slog.Error("rotate secret: encrypt failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to encrypt secret value")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("rotate secret: begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to rotate secret")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	rotated, err := qtx.RotateWorkspaceSecretValue(r.Context(), db.RotateWorkspaceSecretValueParams{
		WorkspaceID:    member.WorkspaceID,
		Name:           name,
		ValueEncrypted: envelope,
		KeyVersion:     keyVersion,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		slog.Warn("rotate secret: update failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to rotate secret")
		return
	}

	details, _ := json.Marshal(map[string]any{"secret_name": name, "secret_id": uuidToString(rotated.ID)})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: member.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      secretActivityRotated,
		Details:     details,
	}); err != nil {
		slog.Error("workspace_secret_rotated audit write failed; rolling back rotate",
			append(logger.RequestAttrs(r), "error", err, "secret_name", name)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; secret rotation rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("rotate secret: tx commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to rotate secret")
		return
	}

	writeJSON(w, http.StatusOK, rotatedSecretToResponse(rotated))
}

// DeleteSecret handles DELETE /api/secrets/{name}. Same transactional
// delete+audit contract: an audit write failure rolls back the delete, so
// a secret is never removed without a trace.
func (h *Handler) DeleteSecret(w http.ResponseWriter, r *http.Request) {
	member, ok := h.authorizeSecretsAccess(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("delete secret: begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	rowsAffected, err := qtx.DeleteWorkspaceSecret(r.Context(), db.DeleteWorkspaceSecretParams{
		WorkspaceID: member.WorkspaceID,
		Name:        name,
	})
	if err != nil {
		slog.Warn("delete secret: delete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "secret not found")
		return
	}

	details, _ := json.Marshal(map[string]any{"secret_name": name})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: member.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      secretActivityDeleted,
		Details:     details,
	}); err != nil {
		slog.Error("workspace_secret_deleted audit write failed; rolling back delete",
			append(logger.RequestAttrs(r), "error", err, "secret_name", name)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; secret deletion rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("delete secret: tx commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
