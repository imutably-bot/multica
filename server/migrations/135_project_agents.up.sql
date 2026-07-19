-- Up Migration: Create project_agent relation table
CREATE TABLE project_agent (
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, agent_id)
);

-- Indexing for bi-directional queries
-- 1. Fast lookup of all agents assigned to a project
CREATE INDEX idx_project_agent_project ON project_agent(project_id);

-- 2. Fast lookup of all projects an agent is assigned to
CREATE INDEX idx_project_agent_agent ON project_agent(agent_id);
