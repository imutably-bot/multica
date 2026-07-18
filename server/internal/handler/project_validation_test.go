package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// An unknown project status must fail fast with a 400 and the valid list, not
// surface the DB CHECK violation as a 500 (#3925: `--status active`).
func TestCreateProjectInvalidStatusReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "invalid status project",
		"status": "active",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid status, got %d: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "planned") {
		t.Errorf("expected error to list valid statuses, got: %s", body)
	}
}

func TestCreateProjectInvalidPriorityReturns400(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":    "invalid priority project",
		"priority": "critical",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid priority, got %d: %s", w.Code, w.Body.String())
	}
}

// A valid status still creates the project (the validation does not over-reject).
func TestCreateProjectValidStatusReturns201(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title":  "valid status project",
		"status": "in_progress",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid status, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})
	if project.Status != "in_progress" {
		t.Errorf("expected status in_progress, got %q", project.Status)
	}
}

// Updating to an unknown status is a 400, not a 500.
func TestUpdateProjectInvalidStatusReturns400(t *testing.T) {
	// Seed a project to update.
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": "update validation project",
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("seed CreateProject: %d %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		req := newRequest("DELETE", "/api/projects/"+project.ID, nil)
		req = withURLParam(req, "id", project.ID)
		testHandler.DeleteProject(httptest.NewRecorder(), req)
	})

	w = httptest.NewRecorder()
	req = newRequest("PUT", "/api/projects/"+project.ID, map[string]any{"status": "active"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid update status, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateProjectRequiresAdminOrOwner(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "update permission project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "PUT", "/api/projects/"+project.ID, map[string]any{"title": "member edit"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project update, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "PUT", "/api/projects/"+project.ID, map[string]any{"title": "admin edit"})
	req = withURLParam(req, "id", project.ID)
	testHandler.UpdateProject(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin project update, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDeleteProjectRequiresAdminOrOwner(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	project := createProjectPermissionTestProject(t, "delete permission denied project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project exists: %v", err)
	}
	if !exists {
		t.Fatal("project was deleted despite plain member request")
	}
}

func TestDeleteProjectAllowsAdmin(t *testing.T) {
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "delete permission admin project")

	w := httptest.NewRecorder()
	req := newRequestAs(adminUserID, "DELETE", "/api/projects/"+project.ID, nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.DeleteProject(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin project delete, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM project WHERE id = $1)`, project.ID).Scan(&exists); err != nil {
		t.Fatalf("verify project deleted: %v", err)
	}
	if exists {
		t.Fatal("project still exists after admin delete")
	}
}

func TestProjectResourceMutationsRequireAdminOrOwner(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "resource permission project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref": map[string]any{
			"url": "https://github.com/multica-ai/multica",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project resource create, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "POST", "/api/projects/"+project.ID+"/resources", map[string]any{
		"resource_type": "github_repo",
		"resource_ref": map[string]any{
			"url": "https://github.com/multica-ai/multica",
		},
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.CreateProjectResource(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 for admin project resource create, got %d: %s", w.Code, w.Body.String())
	}
	var resource ProjectResourceResponse
	if err := json.NewDecoder(w.Body).Decode(&resource); err != nil {
		t.Fatalf("decode CreateProjectResource: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id = $1`, resource.ID)
	})

	w = httptest.NewRecorder()
	req = newRequestAs(memberUserID, "PUT", "/api/projects/"+project.ID+"/resources/"+resource.ID, map[string]any{
		"label": "member edit",
	})
	req = withURLParams(req, "id", project.ID, "resourceId", resource.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project resource update, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "PUT", "/api/projects/"+project.ID+"/resources/"+resource.ID, map[string]any{
		"label": "admin edit",
	})
	req = withURLParams(req, "id", project.ID, "resourceId", resource.ID)
	testHandler.UpdateProjectResource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin project resource update, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(memberUserID, "DELETE", "/api/projects/"+project.ID+"/resources/"+resource.ID, nil)
	req = withURLParams(req, "id", project.ID, "resourceId", resource.ID)
	testHandler.DeleteProjectResource(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project resource delete, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "DELETE", "/api/projects/"+project.ID+"/resources/"+resource.ID, nil)
	req = withURLParams(req, "id", project.ID, "resourceId", resource.ID)
	testHandler.DeleteProjectResource(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for admin project resource delete, got %d: %s", w.Code, w.Body.String())
	}
}

func TestProjectMembersRequireAdminOrOwnerForMutations(t *testing.T) {
	memberUserID := createProjectPermissionTestMember(t, "member")
	adminUserID := createProjectPermissionTestMember(t, "admin")
	project := createProjectPermissionTestProject(t, "member permission project")

	w := httptest.NewRecorder()
	req := newRequestAs(memberUserID, "GET", "/api/projects/"+project.ID+"/members", nil)
	req = withURLParam(req, "id", project.ID)
	testHandler.ListProjectMembers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for project member list, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(memberUserID, "POST", "/api/projects/"+project.ID+"/members", map[string]any{
		"user_id": adminUserID,
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.AddProjectMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project member add, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "POST", "/api/projects/"+project.ID+"/members", map[string]any{
		"user_id": memberUserID,
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.AddProjectMember(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin project member add, got %d: %s", w.Code, w.Body.String())
	}
	var addResp ProjectMembersResponse
	if err := json.NewDecoder(w.Body).Decode(&addResp); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if addResp.Total != 1 || len(addResp.Members) != 1 {
		t.Fatalf("expected 1 assigned member after add, got %+v", addResp)
	}
	if addResp.Members[0].UserID != memberUserID {
		t.Fatalf("expected added member %q, got %q", memberUserID, addResp.Members[0].UserID)
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "POST", "/api/projects/"+project.ID+"/members", map[string]any{
		"user_id": memberUserID,
	})
	req = withURLParam(req, "id", project.ID)
	testHandler.AddProjectMember(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate project member add, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(memberUserID, "DELETE", "/api/projects/"+project.ID+"/members/"+memberUserID, nil)
	req = withURLParams(req, "id", project.ID, "userId", memberUserID)
	testHandler.RemoveProjectMember(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for plain member project member delete, got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequestAs(adminUserID, "DELETE", "/api/projects/"+project.ID+"/members/"+memberUserID, nil)
	req = withURLParams(req, "id", project.ID, "userId", memberUserID)
	testHandler.RemoveProjectMember(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin project member delete, got %d: %s", w.Code, w.Body.String())
	}
	var deleteResp ProjectMembersResponse
	if err := json.NewDecoder(w.Body).Decode(&deleteResp); err != nil {
		t.Fatalf("decode delete response: %v", err)
	}
	if deleteResp.Total != 0 || len(deleteResp.Members) != 0 {
		t.Fatalf("expected empty project member list after delete, got %+v", deleteResp)
	}
}

func createProjectPermissionTestMember(t *testing.T, role string) string {
	t.Helper()

	ctx := context.Background()
	email := "project-delete-" + role + "@multica.test"
	// The schema uses no foreign keys or cascades, so a leftover member from a
	// prior run won't disappear when its user is deleted. Drop the member first.
	_, _ = testPool.Exec(ctx, `DELETE FROM member WHERE user_id IN (SELECT id FROM "user" WHERE email = $1)`, email)
	_, _ = testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)

	var userID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email)
VALUES ($1, $2)
RETURNING id
`, "Project Delete "+role, email).Scan(&userID); err != nil {
		t.Fatalf("create %s user: %v", role, err)
	}
	t.Cleanup(func() {
		// No cascade in the schema: remove the member row before its user so the
		// shared test workspace isn't left with an orphaned member record.
		_, _ = testPool.Exec(context.Background(), `DELETE FROM member WHERE user_id = $1`, userID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, userID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, $3)
`, testWorkspaceID, userID, role); err != nil {
		t.Fatalf("create %s member: %v", role, err)
	}

	return userID
}

func createProjectPermissionTestProject(t *testing.T, title string) ProjectResponse {
	t.Helper()

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/projects?workspace_id="+testWorkspaceID, map[string]any{
		"title": title,
	})
	testHandler.CreateProject(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var project ProjectResponse
	if err := json.NewDecoder(w.Body).Decode(&project); err != nil {
		t.Fatalf("decode CreateProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, project.ID)
	})
	return project
}
