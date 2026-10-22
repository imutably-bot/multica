-- Per-agent secret grants (KHI-972 P2 review fix, finding 1).

-- name: CreateAgentSecretGrant :one
INSERT INTO agent_secret (agent_id, secret_id, created_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteAgentSecretGrantByName :execrows
DELETE FROM agent_secret
WHERE agent_id = $1
  AND secret_id = (
    SELECT id FROM workspace_secret WHERE workspace_id = $2 AND name = $3
  );

-- name: ListAgentSecretGrants :many
-- Metadata only (secret name + grant bookkeeping) — never
-- value_encrypted, same discipline as ListWorkspaceSecrets.
SELECT
    agent_secret.id,
    agent_secret.agent_id,
    agent_secret.secret_id,
    workspace_secret.name AS secret_name,
    agent_secret.created_by,
    agent_secret.created_at
FROM agent_secret
JOIN workspace_secret ON workspace_secret.id = agent_secret.secret_id
WHERE agent_secret.agent_id = $1
ORDER BY workspace_secret.name ASC;

-- name: GetGrantedSecretNamesForAgent :many
-- The claim-time resolver's allow-list: every secret name this agent may
-- resolve. Called once per claim (only when the merged config actually
-- contains a ${secret:...} reference), not per-reference.
SELECT workspace_secret.name
FROM agent_secret
JOIN workspace_secret ON workspace_secret.id = agent_secret.secret_id
WHERE agent_secret.agent_id = $1;
