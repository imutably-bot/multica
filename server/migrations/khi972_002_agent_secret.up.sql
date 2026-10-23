-- KHI-972 P2 review fix (finding 1, HIGH): per-agent secret grants.
--
-- workspace_secret CRUD is owner/admin only, but nothing previously
-- checked who may USE a secret: any agent owner (who can be a plain
-- workspace member via canManageAgent) could reference ${secret:NAME}
-- for ANY workspace secret in their own agent's mcp_config/custom_env,
-- and the claim-time resolver would decrypt and serve it unconditionally.
--
-- agent_secret is the allow-list that closes this gap: a secret is only
-- resolved for an agent whose grant row exists here. Real foreign keys
-- with ON DELETE CASCADE (unlike the no-FK convention used by some other
-- binding tables in this schema) so deleting either the agent or the
-- secret automatically retires the grant instead of leaving an orphaned
-- row that could later point at a reused id.
CREATE TABLE agent_secret (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id   UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    secret_id  UUID NOT NULL REFERENCES workspace_secret(id) ON DELETE CASCADE,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (agent_id, secret_id)
);

COMMENT ON TABLE agent_secret IS
    'KHI-972 P2 review fix: per-agent grant allow-list for workspace_secret. The claim-time resolver only decrypts a ${secret:NAME} reference for an agent when a row exists here; an ungranted reference fails the task closed.';

-- Claim-time hot path: "which secrets is this agent granted" (one query
-- per claim, only when the merged config contains any reference).
CREATE INDEX agent_secret_agent_id_idx ON agent_secret(agent_id);

-- Admin-side reverse lookup ("which agents hold this secret") and the
-- CASCADE from workspace_secret delete both benefit from this index.
CREATE INDEX agent_secret_secret_id_idx ON agent_secret(secret_id);
