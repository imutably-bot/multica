# Project-Specific Agent Assignment & Fast Search Plan

> Status: Draft (Design Phase, Not Started)  
> Owner: custom-multica-hpomen-agy  
> Last updated: 2026-07-18  

## TL;DR

- **Goal**: Allow associating specific Agents with specific Projects so that when working on Issues under a project, users can quickly search and filter the Assignee dropdown. This avoids slow search performance and overly cluttered lists in Workspaces that contain a large number of Agents.
- **Key Changes**:
  1. **Database**: Add a new `project_agent` many-to-many junction table with cascade delete rules and bi-directional indexes.
  2. **Backend API**:
     - Add `/api/projects/{projectId}/agents` endpoints supporting `GET` (list/search), `POST` (add associations), `PUT` (overwrite/sync associations), and `DELETE` (remove associations).
     - Enhance the existing `GET /api/agents` endpoint with an optional `project_id` query parameter for backward compatibility.
  3. **Frontend UI/UX**:
     - Add a "Project Agents" management section in the **Project Detail** page.
     - Pass the current `projectId` to the `AssigneePicker` inside the **Issue Detail** and **Issue Creation** views. Prioritize a "Project Agents" section at the top of the picker dropdown, with other workspace agents listed in a secondary fold or collapsible group below.
  4. **CLI**: Extend the `multica` CLI with `multica project agent add/remove/list` subcommands.

---

## 1. Background & Pain Points

### 1.1 Current Status & Issues
In Multica's current data model, all Agents are directly attached at the **Workspace** level.
When a user creates or edits an Issue under a specific Project and tries to assign it to an Agent, the frontend `AssigneePicker` retrieves all Agents in the Workspace:
```typescript
const { data: agents = [] } = useQuery(agentListOptions(wsId));
```
In large enterprise workspaces, there could be dozens or hundreds of different Agents (each with unique roles, models, and skill combinations). This leads to several issues:
1. **Slow Search**: Loading and filtering a massive list of agents client-side feels laggy and degrades the UX.
2. **Heavy Noise**: Users must manually scroll through completely irrelevant agents to find the few that actually work on the current project (e.g., a frontend refactor project only needs a few frontend implementation or review agents).
3. **No Project Boundaries**: There is no direct way to view who the active/involved agents are for a given project.

### 1.2 Proposed Solution
By establishing a **Project ↔ Agent** many-to-many relationship:
- Project leads/admins can explicitly define which Agents belong to the project team.
- When selecting an assignee, the system prioritizes and defaults to showing these project-assigned agents first.
- A fallback "Search all Workspace Agents" is preserved to allow cross-project assignment when necessary.

---

## 2. Database Design (Database Schema)

We need a join table to store the many-to-many relationship since an Agent can work on multiple projects, and a Project can have multiple agents.

### 2.1 Table Schema (`project_agent`)
New migration files: `server/migrations/135_project_agents.up.sql`:

```sql
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
```

Corresponding rollback file: `server/migrations/135_project_agents.down.sql`:

```sql
-- Down Migration: Drop project_agent relation table
DROP TABLE IF EXISTS project_agent;
```

---

## 3. SQLc Query Design (SQL Queries)

Define these sqlc queries in `server/pkg/db/queries/project.sql` (or a new file) to generate Go DB-access structures:

```sql
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
```

---

## 4. Backend API Design (Go API Endpoints)

### 4.1 Route Definition (`server/cmd/server/router.go`)
Add the `/agents` sub-resources under the project resource group:

```diff
 			// Projects
 			r.Route("/api/projects", func(r chi.Router) {
 				r.Get("/search", h.SearchProjects)
 				r.Get("/", h.ListProjects)
 				r.Post("/", h.CreateProject)
 				r.Route("/{id}", func(r chi.Router) {
 					r.Get("/", h.GetProject)
 					r.Put("/", h.UpdateProject)
 					r.Delete("/", h.DeleteProject)
 					r.Get("/resources", h.ListProjectResources)
 					r.Post("/resources", h.CreateProjectResource)
 					r.Put("/resources/{resourceId}", h.UpdateProjectResource)
 					r.Delete("/resources/{resourceId}", h.DeleteProjectResource)
+					
+					// Project Agents Relation Management
+					r.Get("/agents", h.ListProjectAgents)
+					r.Post("/agents", h.AddAgentsToProject)
+					r.Put("/agents", h.SetProjectAgents)
+					r.Delete("/agents", h.RemoveAgentsFromProject)
 				})
 			})
```

### 4.2 Controller Logic & Authorization (`server/internal/handler/project_agent.go`)
Implement the project agent relationship handler actions with tenancy checks and project permissions verification (allowing Workspace Owner/Admins or the Project Lead to mutate relationships).

```go
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type ProjectAgentsRequest struct {
	AgentIDs []string `json:"agent_ids"`
}

// GET /api/projects/{id}/agents
func (h *Handler) ListProjectAgents(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Ensure project exists and lies in workspace tenant boundary
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	_, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// 2. Fetch assigned agents
	agents, err := h.Queries.ListAgentsInProject(r.Context(), projUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project agents")
		return
	}

	// 3. Serialize output
	resp := make([]AgentResponse, len(agents))
	for i, a := range agents {
		resp[i] = agentToResponse(a)
	}

	writeJSON(w, http.StatusOK, map[string]any{"agents": resp, "total": len(resp)})
}

// POST /api/projects/{id}/agents (Batch Add)
func (h *Handler) AddAgentsToProject(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
	if !ok {
		return
	}

	// 1. Tenant Check & Project Permission Verification
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID := parseUUID(workspaceID)
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}

	// Guard: Only workspace owners/admins, or project lead can assign agents
	if !h.isWorkspaceOwnerOrAdmin(r, wsUUID) && !isProjectLead(r, project) {
		writeError(w, http.StatusForbidden, "insufficient permission to manage project agents")
		return
	}

	var req ProjectAgentsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// 2. Insert relations within a transaction
	txErr := h.withTransaction(r.Context(), func(q *db.Queries) error {
		for _, idStr := range req.AgentIDs {
			agentUUID, err := parseUUIDOrErr(idStr)
			if err != nil {
				return err
			}
			// Verify agent belongs to the same workspace to prevent cross-tenant mapping
			agent, err := q.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{
				ID: agentUUID, WorkspaceID: wsUUID,
			})
			if err != nil {
				return fmt.Errorf("agent %s not found in workspace", idStr)
			}

			if err := q.AddAgentToProject(r.Context(), db.AddAgentToProjectParams{
				ProjectID: projUUID,
				AgentID:   agent.ID,
			}); err != nil {
				return err
			}
		}
		return nil
	})

	if txErr != nil {
		writeError(w, http.StatusInternalServerError, txErr.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// PUT /api/projects/{id}/agents (Sync / Overwrite)
func (h *Handler) SetProjectAgents(w http.ResponseWriter, r *http.Request) {
    // Similar to AddAgentsToProject, but clears previous agents inside the tx first:
    // q.ClearProjectAgents(ctx, projUUID) and then loops through input IDs to re-add.
}

// DELETE /api/projects/{id}/agents (Batch Remove)
func (h *Handler) RemoveAgentsFromProject(w http.ResponseWriter, r *http.Request) {
    // Loops through req.AgentIDs and calls q.RemoveAgentFromProject in a tx.
}
```

### 4.3 Enhanced `GET /api/agents` Filter
Optionally filter agents directly through the main endpoint:
Modify `server/internal/handler/agent.go` `ListAgents`:
```go
projectID := r.URL.Query().Get("project_id")
if projectID != "" {
    projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
    if ok {
        agents, err = h.Queries.ListAgentsInProject(r.Context(), projUUID)
        // ... continue processing skill summaries batch-load ...
    }
    return
}
```

---

## 5. Frontend API & Query State (Frontend Changes)

### 5.1 Client API Extension (`packages/core/api/client.ts`)
```typescript
// Extend listAgents query params
async listAgents(params?: { 
  workspace_id?: string; 
  project_id?: string; // New optional project filter
  include_archived?: boolean; 
}): Promise<Agent[]> {
  const search = new URLSearchParams();
  if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
  if (params?.project_id) search.set("project_id", params.project_id);
  if (params?.include_archived) search.set("include_archived", "true");
  return this.fetch(`/api/agents?${search}`);
}

// Add PUT client helper
async setProjectAgents(projectId: string, agentIds: string[]): Promise<void> {
  return this.fetch(`/api/projects/${projectId}/agents`, {
    method: "PUT",
    body: JSON.stringify({ agent_ids: agentIds }),
  });
}
```

### 5.2 React Query Configuration (`packages/core/workspace/queries.ts`)
```typescript
export function projectAgentListOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
    queryFn: () =>
      api.listAgents({ workspace_id: wsId, project_id: projectId, include_archived: false }),
  });
}
```

---

## 6. Frontend UI/UX Design (User Interface)

### 6.1 Project Details Management Panel (`project-detail.tsx`)
In `project-detail.tsx`, adjacent to the "Resources" list, add the **"Project Agents"** sidebar panel:
```
+------------------------------------------------------+
| Project: Frontend Redesign                           |
+------------------------------------------------------+
| > Lead: @khiemfle                                    |
| > Status: In Progress                                |
|                                                      |
| > Project Agents (4)                        [ Manage ]|
|   [🤖] squirtle-implementer (GPT-4o)                 |
|   [🤖] frontend-reviewer (Claude 3.5 Sonnet)         |
|   [🤖] css-wizard (Gemini 1.5 Pro)                   |
|   [🤖] unit-test-bot (Claude 3.5 Haiku)              |
|                                                      |
| > Resources (2)                                      |
|   - github_repo: imutably-bot/multica                |
+------------------------------------------------------+
```
- Clicking **`[ Manage ]`** opens a workspace-wide multi-select modal containing all available agents.
- Saving updates triggers `api.setProjectAgents(projectId, selectedIds)`.

### 6.2 Assignee Picker Filtering (`assignee-picker.tsx`)
Pass `projectId` down to the Assignee dropdown:
```typescript
// packages/views/issues/components/pickers/assignee-picker.tsx
export function AssigneePicker({
  assigneeType,
  assigneeId,
  projectId, // Current issue's project context
  // ...
})
```

Inside the component, load both project-specific and workspace-wide agents:
```typescript
const wsId = useWorkspaceId();
const { data: allAgents = [] } = useQuery(agentListOptions(wsId));
const { data: projectAgents = [] } = useQuery(
  projectId ? projectAgentListOptions(wsId, projectId) : { enabled: false }
);
```

#### Dropdown Grouping Logic:
1. **If `projectId` is provided**:
   - Primary top section: `Project Agents (${projectAgents.length})`.
   - Secondary collapsible fold: `Other Workspace Agents`.
   - Quick search filters matches dynamically across both sections.
2. **If no `projectId` context**:
   - Fall back to standard flat workspace agents lists.

---

## 7. CLI Integration

Extend the `multica` CLI project commands:

```bash
# List agents in a project
multica project agent list <project-id> [--output json]

# Add agent(s) to project
multica project agent add <project-id> --agent <agent-id> [--agent <agent-id-2> ...]

# Remove agent from project
multica project agent remove <project-id> --agent <agent-id>
```

---

## 8. Phases of Implementation

### Phase 1: Database Migration (1-2 days)
1. Add migration `135_project_agents.up.sql` / `down.sql`.
2. Apply changes via `make db-up` / `go run ./cmd/migrate up`.
3. Add queries to `server/pkg/db/queries/project.sql` and run `make sqlc`.

### Phase 2: Go Backend API & Testing (2 days)
1. Implement route handlers in `server/internal/handler/project_agent.go`.
2. Update router and `GET /api/agents` implementation.
3. Write Go unit and integration tests.

### Phase 3: Frontend Views & Components (2-3 days)
1. Update API client schema definition and queries hooks.
2. Add project detail management panel.
3. Enhance `AssigneePicker` component and pass `projectId`.

### Phase 4: CLI Tooling (1 day)
1. Add CLI CLI command definitions and logic under project client.
