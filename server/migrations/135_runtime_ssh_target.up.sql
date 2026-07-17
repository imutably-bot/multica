-- Opt-in SSH target for a runtime (KHI-677, Tier B of the copy-command
-- plan). The daemon only holds an outbound websocket to the server, so
-- the server has no way to infer whether — or how — a runtime is
-- reachable for inbound SSH. This column is never set automatically; an
-- operator who knows the machine is reachable sets it explicitly via
-- `multica runtime set-ssh-target`.
ALTER TABLE agent_runtime
    ADD COLUMN ssh_target TEXT;
