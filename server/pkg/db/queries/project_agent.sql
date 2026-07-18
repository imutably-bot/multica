-- name: AddAgentToProject :exec
INSERT INTO project_agent (project_id, agent_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveAgentFromProject :exec
DELETE FROM project_agent
WHERE project_id = $1 AND agent_id = $2;

-- name: ClearProjectAgents :exec
DELETE FROM project_agent
WHERE project_id = $1;

-- name: ListAgentsInProject :many
-- Returns all active (non-archived) agents associated with a project.
SELECT a.* FROM agent a
JOIN project_agent pa ON a.id = pa.agent_id
WHERE pa.project_id = $1 AND a.archived_at IS NULL
ORDER BY a.name ASC;

-- name: ListProjectsForAgent :many
-- Returns all projects associated with a specific agent.
SELECT p.* FROM project p
JOIN project_agent pa ON p.id = pa.project_id
WHERE pa.agent_id = $1
ORDER BY p.title ASC;

-- name: IsAgentAssignedToProject :one
-- Fast check if an agent is associated with a project.
SELECT EXISTS(
    SELECT 1 FROM project_agent 
    WHERE project_id = $1 AND agent_id = $2
);
