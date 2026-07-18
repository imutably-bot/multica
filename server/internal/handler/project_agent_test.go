package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProjectAgentsEndToEnd(t *testing.T) {
	// 1. Seed a project
	project := createProjectPermissionTestProject(t, "project agent E2E test project")

	// 2. Seed two agents in the workspace
	agent1ID := createProjectAgentTestAgent(t, "agent-1")
	agent2ID := createProjectAgentTestAgent(t, "agent-2")

	// 3. Add agents to project
	{
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, map[string]any{
			"agent_ids": []string{agent1ID, agent2ID},
		})
		req = withURLParam(req, "id", project.ID)
		testHandler.AddAgentsToProject(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for AddAgentsToProject, got %d: %s", w.Code, w.Body.String())
		}
	}

	// 4. List agents in project
	{
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.ListProjectAgents(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for ListProjectAgents, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Agents []AgentResponse `json:"agents"`
			Total  int             `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode ListProjectAgents: %v", err)
		}

		if resp.Total != 2 {
			t.Errorf("expected 2 project agents, got %d", resp.Total)
		}
		if resp.Agents[0].ID != agent1ID && resp.Agents[1].ID != agent1ID {
			t.Errorf("expected agent-1 in project agents")
		}
	}

	// 5. Test ListAgents with project_id filter query param
	{
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/agents?workspace_id="+testWorkspaceID+"&project_id="+project.ID, nil)
		testHandler.ListAgents(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for ListAgents with project filter, got %d: %s", w.Code, w.Body.String())
		}

		var agents []AgentResponse
		if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
			t.Fatalf("decode ListAgents with project filter: %v", err)
		}

		// ListAgents returns a slice directly
		if len(agents) != 2 {
			t.Errorf("expected 2 filtered agents, got %d", len(agents))
		}
	}

	// 5.5 Test ListAgentProjects
	{
		w := httptest.NewRecorder()
		req := newRequest("GET", "/api/agents/"+agent1ID+"/projects?workspace_id="+testWorkspaceID, nil)
		req = withURLParam(req, "id", agent1ID)
		testHandler.ListAgentProjects(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 for ListAgentProjects, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Projects []ProjectResponse `json:"projects"`
			Total    int               `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("decode ListAgentProjects: %v", err)
		}

		if resp.Total != 1 {
			t.Errorf("expected 1 project for agent-1, got %d", resp.Total)
		}
		if resp.Projects[0].ID != project.ID {
			t.Errorf("expected project ID %s, got %s", project.ID, resp.Projects[0].ID)
		}
	}

	// 6. Set (sync/overwrite) agents in project (overwrite with only agent-2)
	{
		w := httptest.NewRecorder()
		req := newRequest("PUT", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, map[string]any{
			"agent_ids": []string{agent2ID},
		})
		req = withURLParam(req, "id", project.ID)
		testHandler.SetProjectAgents(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for SetProjectAgents, got %d: %s", w.Code, w.Body.String())
		}

		// Verify ListProjectAgents returns only agent-2
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.ListProjectAgents(w, req)

		var resp struct {
			Agents []AgentResponse `json:"agents"`
			Total  int             `json:"total"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		if resp.Total != 1 {
			t.Errorf("expected 1 project agent after sync, got %d", resp.Total)
		}
		if resp.Agents[0].ID != agent2ID {
			t.Errorf("expected agent-2 only, got %s", resp.Agents[0].ID)
		}
	}

	// 7. Remove agent from project
	{
		w := httptest.NewRecorder()
		req := newRequest("DELETE", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, map[string]any{
			"agent_ids": []string{agent2ID},
		})
		req = withURLParam(req, "id", project.ID)
		testHandler.RemoveAgentsFromProject(w, req)

		if w.Code != http.StatusNoContent {
			t.Fatalf("expected 204 for RemoveAgentsFromProject, got %d: %s", w.Code, w.Body.String())
		}

		// Verify list is empty
		w = httptest.NewRecorder()
		req = newRequest("GET", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.ListProjectAgents(w, req)

		var resp struct {
			Agents []AgentResponse `json:"agents"`
			Total  int             `json:"total"`
		}
		json.NewDecoder(w.Body).Decode(&resp)

		if resp.Total != 0 {
			t.Errorf("expected 0 project agents, got %d", resp.Total)
		}
	}
}

func TestProjectAgentsPermissions(t *testing.T) {
	project := createProjectPermissionTestProject(t, "permissions test project")
	agentID := createProjectAgentTestAgent(t, "permission-agent")

	// Plain member should be denied AddAgentsToProject
	memberUserID := createProjectPermissionTestMember(t, "member")
	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "POST", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, map[string]any{
		"agent_ids": []string{agentID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.AddAgentsToProject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member AddAgentsToProject, got %d: %s", w.Code, w.Body.String())
	}

	// Admin member should be allowed AddAgentsToProject
	adminUserID := createProjectPermissionTestMember(t, "admin")
	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "POST", "/api/projects/"+project.ID+"/agents?workspace_id="+testWorkspaceID, map[string]any{
		"agent_ids": []string{agentID},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.AddAgentsToProject(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin AddAgentsToProject, got %d: %s", w.Code, w.Body.String())
	}
}

func createProjectAgentTestAgent(t *testing.T, name string) string {
	t.Helper()

	ctx := context.Background()
	var agentID string
	// Check/clean any leftover test agents
	_, _ = testPool.Exec(ctx, `DELETE FROM agent WHERE workspace_id = $1 AND name = $2`, testWorkspaceID, name)

	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (workspace_id, name, runtime_mode, status, runtime_id)
VALUES ($1, $2, 'cloud', 'idle', $3)
RETURNING id
`, testWorkspaceID, name, testRuntimeID).Scan(&agentID); err != nil {
		t.Fatalf("create test agent %s: %v", name, err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, agentID)
	})

	return agentID
}
