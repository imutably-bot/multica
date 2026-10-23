// KHI-972 P2 review fix (finding 1, HIGH): per-agent secret grants. Closes
// the gap where any agent owner — who can be a plain workspace member via
// canManageAgent — could reference ${secret:ANY_WORKSPACE_SECRET} in their
// own agent's config and have the claim-time resolver decrypt and serve
// it unconditionally. Grant/revoke/list here are owner/admin only, same
// contract as the workspace_secret CRUD in secret.go; the resolver
// (server/internal/secrets.Resolver) enforces the grant at claim time.
package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	agentSecretActivityGranted = "agent_secret_granted"
	agentSecretActivityRevoked = "agent_secret_revoked"
)

// AgentSecretGrantResponse is the metadata-only wire shape for a grant
// row. Like SecretResponse, there is no value field anywhere here.
type AgentSecretGrantResponse struct {
	ID         string  `json:"id"`
	AgentID    string  `json:"agent_id"`
	SecretName string  `json:"secret_name"`
	CreatedBy  *string `json:"created_by,omitempty"`
	CreatedAt  string  `json:"created_at"`
}

// GrantAgentSecretRequest is the wire shape for POST
// /api/agents/{id}/secrets. Grants are addressed by the secret's NAME
// (consistent with /api/secrets itself being name-keyed), not its id.
type GrantAgentSecretRequest struct {
	SecretName string `json:"secret_name"`
}

// authorizeAgentSecretGrantAccess enforces the per-request auth contract
// shared by every grant endpoint — the same three checks as
// authorizeSecretsAccess, plus loading and workspace-scoping the target
// agent:
//
//  1. The secrets store must be configured and enabled for the agent's
//     workspace (workspace-scoped EvalContext, same as authorizeSecretsAccess
//     and the claim path — KHI-972 P2 review finding 2).
//  2. The actor must resolve to a member (human); an agent actor is
//     rejected outright, even one managing itself.
//  3. The member must be a workspace owner or admin. Deliberately NOT
//     "owner/admin OR agent owner" (canManageAgent's rule) — granting or
//     revoking a secret is an admin action regardless of who owns the
//     agent, otherwise a plain member could grant their own agent access
//     to any workspace secret they can guess the name of.
func (h *Handler) authorizeAgentSecretGrantAccess(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	workspaceID := uuidToString(agent.WorkspaceID)

	if h.Secrets == nil {
		writeError(w, http.StatusServiceUnavailable, "secrets store is not configured on this server")
		return db.Agent{}, db.Member{}, false
	}
	if !secretsStoreEnabledForWorkspace(r.Context(), h.FeatureFlags, workspaceID) {
		writeError(w, http.StatusServiceUnavailable, "secrets store is not enabled for this workspace")
		return db.Agent{}, db.Member{}, false
	}

	userID := requestUserID(r)
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not manage secret grants")
		return db.Agent{}, db.Member{}, false
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	return agent, member, true
}

// GrantAgentSecret handles POST /api/agents/{id}/secrets. Persist + audit
// run in one transaction, same fail-closed-on-audit-failure contract as
// CreateSecret.
func (h *Handler) GrantAgentSecret(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentSecretGrantAccess(w, r)
	if !ok {
		return
	}

	var req GrantAgentSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.SecretName == "" {
		writeError(w, http.StatusBadRequest, "secret_name is required")
		return
	}

	secret, err := h.Queries.GetWorkspaceSecretByName(r.Context(), db.GetWorkspaceSecretByNameParams{
		WorkspaceID: agent.WorkspaceID,
		Name:        req.SecretName,
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "secret not found")
			return
		}
		slog.Warn("grant agent secret: lookup secret failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to grant secret")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("grant agent secret: begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to grant secret")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	created, err := qtx.CreateAgentSecretGrant(r.Context(), db.CreateAgentSecretGrantParams{
		AgentID:   agent.ID,
		SecretID:  secret.ID,
		CreatedBy: member.UserID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "this secret is already granted to this agent")
			return
		}
		slog.Warn("grant agent secret: insert failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to grant secret")
		return
	}

	details, _ := json.Marshal(map[string]any{
		"agent_id":    uuidToString(agent.ID),
		"secret_name": req.SecretName,
		"secret_id":   uuidToString(secret.ID),
	})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      agentSecretActivityGranted,
		Details:     details,
	}); err != nil {
		slog.Error("agent_secret_granted audit write failed; rolling back grant",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID), "secret_name", req.SecretName)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; grant rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("grant agent secret: tx commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to grant secret")
		return
	}

	writeJSON(w, http.StatusCreated, AgentSecretGrantResponse{
		ID:         uuidToString(created.ID),
		AgentID:    uuidToString(created.AgentID),
		SecretName: req.SecretName,
		CreatedBy:  uuidToPtr(created.CreatedBy),
		CreatedAt:  timestampToString(created.CreatedAt),
	})
}

// ListAgentSecretGrants handles GET /api/agents/{id}/secrets.
func (h *Handler) ListAgentSecretGrants(w http.ResponseWriter, r *http.Request) {
	agent, _, ok := h.authorizeAgentSecretGrantAccess(w, r)
	if !ok {
		return
	}

	rows, err := h.Queries.ListAgentSecretGrants(r.Context(), agent.ID)
	if err != nil {
		slog.Warn("list agent secret grants failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list secret grants")
		return
	}

	out := make([]AgentSecretGrantResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, AgentSecretGrantResponse{
			ID:         uuidToString(row.ID),
			AgentID:    uuidToString(row.AgentID),
			SecretName: row.SecretName,
			CreatedBy:  uuidToPtr(row.CreatedBy),
			CreatedAt:  timestampToString(row.CreatedAt),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"grants": out})
}

// RevokeAgentSecret handles DELETE /api/agents/{id}/secrets/{name}.
func (h *Handler) RevokeAgentSecret(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentSecretGrantAccess(w, r)
	if !ok {
		return
	}
	name := chi.URLParam(r, "name")

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("revoke agent secret: begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to revoke secret")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	rowsAffected, err := qtx.DeleteAgentSecretGrantByName(r.Context(), db.DeleteAgentSecretGrantByNameParams{
		AgentID:     agent.ID,
		WorkspaceID: agent.WorkspaceID,
		Name:        name,
	})
	if err != nil {
		slog.Warn("revoke agent secret: delete failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to revoke secret")
		return
	}
	if rowsAffected == 0 {
		writeError(w, http.StatusNotFound, "grant not found")
		return
	}

	details, _ := json.Marshal(map[string]any{"agent_id": uuidToString(agent.ID), "secret_name": name})
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     member.UserID,
		Action:      agentSecretActivityRevoked,
		Details:     details,
	}); err != nil {
		slog.Error("agent_secret_revoked audit write failed; rolling back revoke",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID), "secret_name", name)...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; revoke rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("revoke agent secret: tx commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to revoke secret")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
