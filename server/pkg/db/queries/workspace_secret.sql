-- Workspace secret store (KHI-972 P2). value_encrypted never appears in any
-- :many / metadata-only query result below — ListWorkspaceSecrets explicitly
-- selects a column allowlist so the API list handler cannot leak a ciphertext
-- blob even by accident. Only GetWorkspaceSecretByName (used solely by the
-- claim-time resolver and by rotate/delete existence checks, never rendered
-- into an API response) selects value_encrypted.

-- name: CreateWorkspaceSecret :one
INSERT INTO workspace_secret (
    workspace_id, name, value_encrypted, key_version, description, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING id, workspace_id, name, key_version, description, created_by, created_at, updated_at, rotated_at;

-- name: GetWorkspaceSecretByName :one
-- Full row including value_encrypted. Only for internal decrypt paths
-- (claim-time resolution, rotate/delete existence checks) — never used to
-- build an API response.
SELECT * FROM workspace_secret
WHERE workspace_id = $1 AND name = $2;

-- name: ListWorkspaceSecrets :many
-- Metadata only, by design: value_encrypted is not in the column list, so
-- this query cannot be the source of a value leak no matter how the handler
-- serializes its result.
SELECT id, workspace_id, name, key_version, description, created_by, created_at, updated_at, rotated_at
FROM workspace_secret
WHERE workspace_id = $1
ORDER BY name ASC;

-- name: RotateWorkspaceSecretValue :one
-- Replaces the envelope and bumps rotated_at/updated_at. Returns metadata
-- only (no value_encrypted) for the same reason as ListWorkspaceSecrets.
UPDATE workspace_secret
SET value_encrypted = $3,
    key_version = $4,
    rotated_at = now(),
    updated_at = now()
WHERE workspace_id = $1 AND name = $2
RETURNING id, workspace_id, name, key_version, description, created_by, created_at, updated_at, rotated_at;

-- name: DeleteWorkspaceSecret :execrows
DELETE FROM workspace_secret
WHERE workspace_id = $1 AND name = $2;
