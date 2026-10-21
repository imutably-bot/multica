-- KHI-972 P2: encrypted workspace secret store.
--
-- One row per named secret within a workspace. Values are never stored in
-- plaintext: value_encrypted holds an envelope (a random per-secret AES-256-GCM
-- data key, itself wrapped by the current master/KMS key) built and opened by
-- the server/internal/secrets package. key_version records which wrapping key
-- produced the envelope, so a future key rotation can decrypt old rows with
-- the old key while new writes use the new one.
--
-- Naming: this migration uses the khi972_<NNN>_<description> scheme decided
-- in KHI-972 P0 instead of the upstream numeric scheme. Filenames sort
-- lexically (migrations.Files), and any letter-prefixed name sorts after every
-- purely-numeric upstream migration regardless of how high upstream's counter
-- climbs — so fork-only migrations never collide with, or get interleaved
-- with, migrations pulled in from a future upstream sync.
CREATE TABLE workspace_secret (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    value_encrypted BYTEA NOT NULL,
    key_version     INTEGER NOT NULL,
    description     TEXT,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    rotated_at      TIMESTAMPTZ,
    UNIQUE (workspace_id, name)
);

COMMENT ON TABLE workspace_secret IS
    'KHI-972 P2: encrypted secret values referenced as ${secret:NAME}, resolved at task claim time. value_encrypted is an envelope, never plaintext.';
COMMENT ON COLUMN workspace_secret.value_encrypted IS
    'Envelope: a random per-secret AES-256-GCM data key wrapped by the key_version master/KMS key, plus the value sealed under that data key. Never returned by any API.';
COMMENT ON COLUMN workspace_secret.key_version IS
    'Identifies which wrapping key produced this envelope (see secrets.KeyProvider.KeyVersion). Lets a future key rotation decrypt old rows with the old key while new writes use the new one.';
