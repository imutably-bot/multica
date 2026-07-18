-- Explicit project membership assignments: lets workspace owners/admins add
-- workspace members to a project in the UI without conflating it with project
-- access control. A project member is just an assignment record; the
-- application layer enforces membership and permissions.
--
-- No foreign keys or cascades — the repo convention keeps lifecycle checks in
-- handlers, and project delete cleans these rows explicitly.
CREATE TABLE project_member (
    project_id UUID NOT NULL,
    user_id    UUID NOT NULL,
    added_by   UUID NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE INDEX idx_project_member_user
    ON project_member (user_id);
