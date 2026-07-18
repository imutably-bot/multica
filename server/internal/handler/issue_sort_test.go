package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListIssuesSortByUpdatedAt(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Sort Updated At %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(title string, updatedAt time.Time) string {
		t.Helper()
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}

		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, project_id
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5)
			RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE issue SET updated_at = $1 WHERE id = $2`, updatedAt, id); err != nil {
			t.Fatalf("set updated_at for %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	oldest := insertIssue(fmt.Sprintf("updated-old-%d", suffix), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newest := insertIssue(fmt.Sprintf("updated-new-%d", suffix), time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))

	w := httptest.NewRecorder()
	path := fmt.Sprintf("/api/issues?workspace_id=%s&project_id=%s&sort=updated_at&direction=desc&limit=20", testWorkspaceID, projectID)
	testHandler.ListIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Issues []IssueResponse `json:"issues"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(resp.Issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(resp.Issues))
	}
	if resp.Issues[0].ID != newest || resp.Issues[1].ID != oldest {
		t.Fatalf("updated_at sort mismatch: %#v", resp.Issues)
	}
}

func TestListGroupedIssuesSortByUpdatedAt(t *testing.T) {
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var projectID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO project (workspace_id, title) VALUES ($1, $2) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("Grouped Sort Updated At %d", suffix)).Scan(&projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE project_id = $1`, projectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id = $1`, projectID)
	})

	insertIssue := func(title string, updatedAt time.Time) string {
		t.Helper()
		var number int
		if err := testPool.QueryRow(ctx, `
			UPDATE workspace
			SET issue_counter = GREATEST(issue_counter, (SELECT COALESCE(MAX(number), 0) FROM issue WHERE workspace_id = $1)) + 1
			WHERE id = $1 RETURNING issue_counter
		`, testWorkspaceID).Scan(&number); err != nil {
			t.Fatalf("next issue number: %v", err)
		}

		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO issue (
				workspace_id, title, status, priority, creator_type, creator_id,
				position, number, project_id
			)
			VALUES ($1, $2, 'todo', 'none', 'member', $3, 0, $4, $5)
			RETURNING id
		`, testWorkspaceID, title, testUserID, number, projectID).Scan(&id); err != nil {
			t.Fatalf("create issue %q: %v", title, err)
		}
		if _, err := testPool.Exec(ctx, `UPDATE issue SET updated_at = $1 WHERE id = $2`, updatedAt, id); err != nil {
			t.Fatalf("set updated_at for %q: %v", title, err)
		}
		t.Cleanup(func() {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, id)
		})
		return id
	}

	oldest := insertIssue(fmt.Sprintf("grouped-updated-old-%d", suffix), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	newest := insertIssue(fmt.Sprintf("grouped-updated-new-%d", suffix), time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC))

	w := httptest.NewRecorder()
	path := fmt.Sprintf("/api/issues/grouped?workspace_id=%s&group_by=assignee&project_id=%s&sort=updated_at&direction=desc&limit=20", testWorkspaceID, projectID)
	testHandler.ListGroupedIssues(w, newRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("ListGroupedIssues: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp GroupedIssuesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode grouped response: %v", err)
	}
	if len(resp.Groups) != 1 {
		t.Fatalf("expected 1 group, got %#v", resp.Groups)
	}
	if len(resp.Groups[0].Issues) != 2 {
		t.Fatalf("expected 2 issues in group, got %#v", resp.Groups[0].Issues)
	}
	if resp.Groups[0].Issues[0].ID != newest || resp.Groups[0].Issues[1].ID != oldest {
		t.Fatalf("grouped updated_at sort mismatch: %#v", resp.Groups[0].Issues)
	}
}
