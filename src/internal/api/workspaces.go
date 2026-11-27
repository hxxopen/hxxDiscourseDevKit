package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type workspaceListResponse struct {
	Items             []*workspace.Workspace `json:"items"`
	Total             int                    `json:"total"`
	ActiveWorkspaceID string                 `json:"active_workspace_id"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	items, err := s.opts.Workspaces.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, workspaceListResponse{
		Items:             items,
		Total:             len(items),
		ActiveWorkspaceID: s.opts.Config.Get().ActiveWorkspaceID,
	})
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req workspace.CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	created, err := s.opts.Workspaces.Create(r.Context(), req)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/workspaces/"+created.WorkspaceID)
	httpx.WriteJSON(w, http.StatusCreated, created)
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	item, err := s.opts.Workspaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleProvisionWorkspace(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	item, err := s.opts.Workspaces.Provision(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	var req workspace.UpdateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	updated, err := s.opts.Workspaces.Update(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

type activateResponse struct {
	ActiveWorkspaceID string               `json:"active_workspace_id"`
	Workspace         *workspace.Workspace `json:"workspace"`
}

func (s *Server) handleActivateWorkspace(w http.ResponseWriter, r *http.Request) {
	item, err := s.opts.Workspaces.SetActive(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, activateResponse{
		ActiveWorkspaceID: item.WorkspaceID,
		Workspace:         item,
	})
}

type workspaceStatusResponse struct {
	Workspace *workspace.Workspace `json:"workspace"`
	Status    *workspace.Status    `json:"status"`
}

func (s *Server) handleWorkspaceStatus(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("id")
	item, err := s.opts.Workspaces.Get(r.Context(), ref)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status, err := s.opts.Workspaces.Status(r.Context(), item.WorkspaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, workspaceStatusResponse{Workspace: item, Status: status})
}

func (s *Server) handlePreviewDelete(w http.ResponseWriter, r *http.Request) {
	plan, err := s.opts.Workspaces.PreviewDelete(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
}

type deleteAcceptedResponse struct {
	TaskID    string      `json:"task_id"`
	StatusURL string      `json:"status_url"`
	Task      interface{} `json:"task"`
}

func (s *Server) handleDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	ref := r.PathValue("id")
	item, err := s.opts.Workspaces.Get(r.Context(), ref)
	if err != nil {
		s.fail(w, r, err)
		return
	}

	query := r.URL.Query()
	token := query.Get("confirm_token")
	if err := s.opts.Workspaces.ValidateDeleteToken(item.WorkspaceID, token); err != nil {
		s.fail(w, r, err)
		return
	}

	status, err := s.opts.Workspaces.Status(r.Context(), item.WorkspaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if status.Running {
		s.fail(w, r, workspace.ErrRunning)
		return
	}

	opts := workspace.DeleteOptions{ConfirmToken: token}
	switch strings.ToLower(query.Get("mode")) {
	case "", "record":
	case "purge":
		opts.DeleteConfig = true
		opts.DeleteSource = true
		opts.DeleteData = true
		opts.DeleteDatabase = true
		opts.DeleteRedis = true
	case "custom":
	default:
		httpx.WriteError(w, r,
			httpx.BadRequest("VALIDATION_FAILED", "mode 必须是 record、custom 或 purge"), s.log)
		return
	}
	if boolQuery(query, "delete_config") {
		opts.DeleteConfig = true
	}
	if boolQuery(query, "delete_source") {
		opts.DeleteSource = true
	}
	if boolQuery(query, "delete_data") {
		opts.DeleteData = true
	}
	if boolQuery(query, "delete_database") {
		opts.DeleteDatabase = true
	}
	if boolQuery(query, "delete_redis") {
		opts.DeleteRedis = true
	}

	workspaceID := item.WorkspaceID
	submitted := s.opts.Tasks.Submit("workspace.delete", workspaceID, true,
		func(ctx context.Context, progress func(int, string)) error {
			progress(10, "正在删除 Workspace 资源")
			result, err := s.opts.Workspaces.Delete(ctx, workspaceID, opts)
			if err != nil {
				return err
			}
			progress(100, fmt.Sprintf("已删除 %d 项资源，保留 %d 项", len(result.Removed), len(result.Kept)))
			return nil
		})

	httpx.WriteJSON(w, http.StatusAccepted, deleteAcceptedResponse{
		TaskID:    submitted.TaskID,
		StatusURL: "/api/v1/tasks/" + submitted.TaskID,
		Task:      submitted,
	})
}

func boolQuery(query url.Values, name string) bool {
	switch strings.ToLower(query.Get(name)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
