# Project-Specific Agent Assignment & Fast Search Plan

> Status: Draft (设计阶段, 未动工)  
> Owner: custom-multica-hpomen-agy  
> Last updated: 2026-07-18  

## TL;DR

- **目标**: 允许将特定的 Agent 关联到特定的 Project（项目），从而在处理项目下的 Issue 时能够快速检索和过滤 Assignee，避免在包含大量 Agent 的 Workspace 中检索缓慢、列表冗长的问题。
- **核心变更**:
  1. **数据库**: 新增 `project_agent` 多对多关联表，支持级联删除与双向索引。
  2. **后端 API**: 
     - 新增 `/api/projects/{projectId}/agents` 端点，支持对项目内的 Agent 进行 `GET` (列表/搜索)、`POST` (新增关联)、`PUT` (覆盖关联) 与 `DELETE` (取消关联)。
     - 增强 `GET /api/agents` 接口，支持可选的 `project_id` 过滤参数，无缝对接现有通用 Agent 列表接口。
  3. **前端 UI/UX**:
     - 在 **Project 详情页** 增加 "Project Agents" 关联管理面板。
     - 在 **Issue 详情页及创建面板** 的 `AssigneePicker` 中，传入当前 Issue 的 `projectId`。若存在，在下拉列表顶部优先展示 "Project Agents" 分组，并折叠或置后非项目关联的 "Other Workspace Agents"。
  4. **CLI**: 扩展 `multica` CLI，支持 `multica project agent add/remove/list` 命令。

---

## 1. 背景与痛点

### 1.1 现状与问题
在 Multica 当前的数据模型中，所有的 Agent 都是直接绑定在 **Workspace（工作空间）** 层级的。
当用户在某个 Project 中创建或编辑 Issue，并尝试为其分配 Assignee（执行智能体）时，前端的 `AssigneePicker` 会拉取整个 Workspace 的全部 Agent：
```typescript
const { data: agents = [] } = useQuery(agentListOptions(wsId));
```
在大型团队或企业级 Workspace 中，可能存在数十甚至上百个不同的 Agent（各自有不同的 role, model 与 skill 组合）。这带来了以下痛点：
1. **检索速度慢**: 大量数据的前端加载与过滤排序在大数据集下体感明显变差。
2. **列表噪音大**: 用户需要从海量无关 Agent 中人肉筛选适合当前项目（例如：前端重构项目只需要 `squirtle-implementer` 与 `frontend-reviewer` 等）的智能体。
3. **协作无序**: 缺乏项目层面的“参与成员/智能体”准入视图，无法直观看出本项目的开发主力是谁。

### 1.2 解决方案
通过建立 **Project ↔ Agent** 的多对多关联关系：
- 项目管理员可以明确指定哪些 Agent 参与该项目。
- 检索 Assignee 时，系统默认且优先推荐这些已分配的项目 Agent。
- 保留“搜索全部 Workspace Agent”的降级/逃生通道，以防临时跨项目调度。

---

## 2. 数据库设计 (Database Schema)

我们需要一个关联表来存储多对多关系。因为 Agent 可能属于多个 Project，而一个 Project 显然包含多个 Agent。

### 2.1 关联表设计 (`project_agent`)
新增迁移文件 `server/migrations/135_project_agents.up.sql`：

```sql
-- Up Migration: Create project_agent relation table
CREATE TABLE project_agent (
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    agent_id     UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, agent_id)
);

-- Indexing for bi-directional queries
-- 1. Fast lookup of all agents assigned to a project (redundant but explicit for PRIMARY KEY order project_id first)
CREATE INDEX idx_project_agent_project ON project_agent(project_id);

-- 2. Fast lookup of all projects an agent is assigned to
CREATE INDEX idx_project_agent_agent ON project_agent(agent_id);
```

对应的回滚迁移 `server/migrations/135_project_agents.down.sql`：

```sql
-- Down Migration: Drop project_agent relation table
DROP TABLE IF EXISTS project_agent;
```

---

## 3. SQLc 查询设计 (SQL Queries)

在 `server/pkg/db/queries/project.sql` (或新增 `project_agent.sql`) 中定义以下 sqlc 查询，用于生成 Go 语言的数据库访问层代码。

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
-- Fast existence check for authorization or validation.
SELECT EXISTS(
    SELECT 1 FROM project_agent 
    WHERE project_id = $1 AND agent_id = $2
);
```

---

## 4. 后端 API 接口设计 (Go API Endpoints)

### 4.1 端点路由定义 (`server/cmd/server/router.go`)
在项目路由组内新增 `/agents` 子路由：

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

### 4.2 控制器逻辑与权限校验 (`server/internal/handler/project_agent.go`)
需要实现如下处理函数。所有写操作必须通过权限守卫（确保当前用户是项目负责人或拥有 Workspace 管理/拥有者权限）。

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
	// Re-uses standard agent response mapper to avoid payload drift
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
    // 逻辑类似于 AddAgentsToProject，但在一个事务中首先调用 q.ClearProjectAgents(ctx, projUUID)，
    // 然后对传入的 req.AgentIDs 进行逐个添加，实现全量覆盖/同步。
}

// DELETE /api/projects/{id}/agents (Batch Remove)
func (h *Handler) RemoveAgentsFromProject(w http.ResponseWriter, r *http.Request) {
    // 逻辑与 Add 类似，通过解析传入的 req.AgentIDs，循环调用 q.RemoveAgentFromProject 实现批量解绑。
}
```

### 4.3 增强现有的通用 `GET /api/agents` 检索
为了让前端可以用统一的 `listAgents` API 并通过 Query 过滤：
修改 `server/internal/handler/agent.go` 中的 `ListAgents`：
```go
projectID := r.URL.Query().Get("project_id")
if projectID != "" {
    projUUID, ok := parseUUIDOrBadRequest(w, projectID, "project id")
    if ok {
        agents, err = h.Queries.ListAgentsInProject(r.Context(), projUUID)
        // ... 继续后续的 skills 批处理加载 ...
    }
    return
}
```

---

## 5. 前端 API 客户端与状态管理 (Frontend Changes)

### 5.1 客户端接口扩展 (`packages/core/api/client.ts`)
```typescript
// 扩展 listAgents 参数结构
async listAgents(params?: { 
  workspace_id?: string; 
  project_id?: string; // 新增可选的项目关联过滤
  include_archived?: boolean; 
}): Promise<Agent[]> {
  const search = new URLSearchParams();
  if (params?.workspace_id) search.set("workspace_id", params.workspace_id);
  if (params?.project_id) search.set("project_id", params.project_id);
  if (params?.include_archived) search.set("include_archived", "true");
  return this.fetch(`/api/agents?${search}`);
}

// 新增专用的项目-智能体关系写入方法
async setProjectAgents(projectId: string, agentIds: string[]): Promise<void> {
  return this.fetch(`/api/projects/${projectId}/agents`, {
    method: "PUT",
    body: JSON.stringify({ agent_ids: agentIds }),
  });
}
```

### 5.2 React Query 选项扩展 (`packages/core/workspace/queries.ts`)
```typescript
export function projectAgentListOptions(wsId: string, projectId: string) {
  return queryOptions({
    // 确保与 workspaceKeys.agents 在 query key 上区分开
    queryKey: [...workspaceKeys.agents(wsId), "project", projectId],
    queryFn: () =>
      api.listAgents({ workspace_id: wsId, project_id: projectId, include_archived: false }),
  });
}
```

---

## 6. 前端 UI/UX 设计 (User Interface)

主要改造两个场景：**关系管理（项目详情页）** 与 **智能体检索（Assignee Picker）**。

### 6.1 项目详情页的 Agent 管理面板 (`project-detail.tsx`)
在 `project-detail.tsx` 侧边栏或主内容区中，紧接在 "Resources" (关联代码库/文档) 之后，增加 **"Project Agents"** 管理面板：
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
- 点击 **`[ Manage ]`** 打开一个 Multi-select Modal，展示当前 Workspace 中的所有可用 Agent。
- 用户通过复选框增删关联，点击保存时调用 `api.setProjectAgents(projectId, selectedIds)`。

### 6.2 快速智能体检索与过滤 (`assignee-picker.tsx`)
当用户点击 Issue 的 Assignee 下拉框时，我们需要向 `AssigneePicker` 传入 `projectId`：
```typescript
// packages/views/issues/components/pickers/assignee-picker.tsx
export function AssigneePicker({
  assigneeType,
  assigneeId,
  projectId, // 新增：当前 Issue 关联的 Project ID
  // ... 其他属性
}: {
  projectId?: string;
  // ...
})
```

在组件内部，如果 `projectId` 存在，**同时加载**项目 Agent 和全部 Agent：
```typescript
const wsId = useWorkspaceId();
const { data: allAgents = [] } = useQuery(agentListOptions(wsId));
const { data: projectAgents = [] } = useQuery(
  projectId ? projectAgentListOptions(wsId, projectId) : { enabled: false }
);
```

#### 检索列表分区渲染：
在下拉弹出框中，采用分组渲染逻辑，避免用户眼花缭乱：
1. **若提供了 `projectId`**:
   - 顶部首个分组渲染为: `Project Agents (${projectAgents.length})`。该分组只包含绑定到当前项目的 Agent。
   - 第二个分组渲染为: `Other Workspace Agents`。该分组包含当前 Workspace 中其余未绑定该项目的 Agent，可默认折叠展示，点击展开。
   - 当用户在输入框 `filter` 键入关键字搜索时，双向匹配两个分组并在其内实时过滤。
2. **若未提供 `projectId` (如在 workspace 全局 Kanban 视图中筛选)**:
   - 维持现状，直接展示所有的 Workspace Agents。

---

## 7. 命令行工具集成 (CLI Integration)

需要在 `multica` CLI 的 `project` 命令组中扩充子命令：

```bash
# 1. 查看某个项目关联的所有 Agent
multica project agent list <project-id> [--output json]

# 2. 将一个或多个 Agent 关联到项目
multica project agent add <project-id> --agent <agent-id> [--agent <agent-id-2> ...]

# 3. 将 Agent 从项目关联中移除
multica project agent remove <project-id> --agent <agent-id>
```

---

## 8. 实施阶段划分 (Implementation Plan)

### Phase 1: 数据库与数据层 (1-2 days)
1. 创建数据库迁移文件 `135_project_agents.up.sql` 及 `down.sql`。
2. 运行 `make db-up` / `go run ./cmd/migrate up` 应用更改。
3. 在 `server/pkg/db/queries/` 中增加对应的 sqlc 查询配置。
4. 运行 `make sqlc` 重新生成 Go DB 数据访问层代码。

### Phase 2: 后端控制器及路由验证 (2 days)
1. 在 `server/internal/handler/` 下实现 `project_agent.go` 中各处理函数。
2. 在 `server/cmd/server/router.go` 挂载对应 API 路径。
3. 扩展 `ListAgents` 原接口支持 `project_id` 过滤参数。
4. 编写后端集成测试（例如在 `project_resource_test.go` 同级目录下增加 `project_agent_test.go`），覆盖各种权限边界 (Workspace Admin/Owner, Project Lead, Ordinary Member) 和跨租户安全性检查。

### Phase 3: 前端数据流与 UI/UX 改造 (2-3 days)
1. 在 `packages/core/api/client.ts` 补充新的客户端请求接口。
2. 在 `packages/core/workspace/queries.ts` 新增 `projectAgentListOptions` query config。
3. 修改 `project-detail.tsx` 侧边面板，增添 Project Agents 部分与管理对话框。
4. 修改 `AssigneePicker` (位于 `assignee-picker.tsx`)，支持 `projectId` 传参，并根据分组划分渲染逻辑（Project Agents 置顶，Other Workspace Agents 区分并支持折叠）。
5. 在 Issue 列表/详情等页面使用 `AssigneePicker` 处传递 `projectId`。

### Phase 4: CLI 与发布测试 (1 day)
1. 在 `multica` CLI 源码中增加 `project agent` 命令及其子命令解析。
2. 运行端到端 (E2E) 测试，验证极速检索功能是否达成体验优化指标。
