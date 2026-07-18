package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type ProjectMemberResponse struct {
	UserID    string `json:"user_id"`
	AddedBy   string `json:"added_by"`
	CreatedAt string `json:"created_at"`
}

type ProjectMembersResponse struct {
	Members []ProjectMemberResponse `json:"members"`
	Total   int                     `json:"total"`
}

type AddProjectMemberRequest struct {
	UserID string `json:"user_id"`
}

type projectMemberRow struct {
	ProjectID pgtype.UUID
	UserID    pgtype.UUID
	AddedBy   pgtype.UUID
	CreatedAt pgtype.Timestamptz
}

func projectMemberRowToResponse(row projectMemberRow) ProjectMemberResponse {
	return ProjectMemberResponse{
		UserID:    uuidToString(row.UserID),
		AddedBy:   uuidToString(row.AddedBy),
		CreatedAt: timestampToString(row.CreatedAt),
	}
}

func (h *Handler) listProjectMembers(ctx context.Context, projectID pgtype.UUID) ([]projectMemberRow, error) {
	if h.DB == nil {
		return nil, errors.New("database executor not configured")
	}
	rows, err := h.DB.Query(ctx, `
		SELECT project_id, user_id, added_by, created_at
		FROM project_member
		WHERE project_id = $1
		ORDER BY created_at ASC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []projectMemberRow{}
	for rows.Next() {
		var row projectMemberRow
		if err := rows.Scan(&row.ProjectID, &row.UserID, &row.AddedBy, &row.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (h *Handler) writeProjectMembersResponse(w http.ResponseWriter, r *http.Request, projectID pgtype.UUID) {
	rows, err := h.listProjectMembers(r.Context(), projectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list project members")
		return
	}
	resp := make([]ProjectMemberResponse, len(rows))
	for i, row := range rows {
		resp[i] = projectMemberRowToResponse(row)
	}
	writeJSON(w, http.StatusOK, ProjectMembersResponse{
		Members: resp,
		Total:   len(resp),
	})
}

// ListProjectMembers returns the explicit member assignments for a project.
func (h *Handler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	h.writeProjectMembersResponse(w, r, project.ID)
}

// AddProjectMember assigns a workspace member to a project.
func (h *Handler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	requester, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin")
	if !ok {
		return
	}

	var req AddProjectMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.UserID = strings.TrimSpace(req.UserID)
	if req.UserID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}
	target, err := h.getWorkspaceMember(r.Context(), req.UserID, uuidToString(project.WorkspaceID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace member not found")
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database executor not configured")
		return
	}
	var created projectMemberRow
	err = h.DB.QueryRow(r.Context(), `
		INSERT INTO project_member (project_id, user_id, added_by)
		VALUES ($1, $2, $3)
		RETURNING project_id, user_id, added_by, created_at
	`, project.ID, target.UserID, requester.UserID).Scan(&created.ProjectID, &created.UserID, &created.AddedBy, &created.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "project member already exists")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to add project member")
		return
	}
	h.writeProjectMembersResponse(w, r, project.ID)
}

// RemoveProjectMember removes an explicit project assignment.
func (h *Handler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, uuidToString(project.WorkspaceID), "project not found", "owner", "admin"); !ok {
		return
	}
	userID := chi.URLParam(r, "userId")
	userID = strings.TrimSpace(userID)
	memberUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	if h.DB == nil {
		writeError(w, http.StatusInternalServerError, "database executor not configured")
		return
	}
	var deleted projectMemberRow
	err := h.DB.QueryRow(r.Context(), `
		DELETE FROM project_member
		WHERE project_id = $1 AND user_id = $2
		RETURNING project_id, user_id, added_by, created_at
	`, project.ID, memberUUID).Scan(&deleted.ProjectID, &deleted.UserID, &deleted.AddedBy, &deleted.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "project member not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to remove project member")
		return
	}
	h.writeProjectMembersResponse(w, r, project.ID)
}
