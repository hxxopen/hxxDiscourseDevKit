package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type projectResponse struct {
	Workspace *workspace.Workspace `json:"workspace"`
	Project   *project.Status      `json:"project"`
}

func (s *Server) projectReady(w http.ResponseWriter, r *http.Request) bool {
	if s.opts.Project == nil {
		s.fail(w, r, errFeatureUnavailable("project"))
		return false
	}
	return true
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	item, err := s.opts.Workspaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status, err := s.opts.Project.Status(r.Context(), item.WorkspaceID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectResponse{Workspace: item, Project: status})
}

func (s *Server) handleProjectStatus(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	status, err := s.opts.Project.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

func (s *Server) handleUpdateProjectSource(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	var input project.SourceInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	updated, err := s.opts.Project.SaveSource(r.Context(), r.PathValue("id"), input)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
}

func (s *Server) handleCloneProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	var input project.SourceInput
	if err := decodeOptionalBody(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	s.submitWorkspaceTask(w, r, "project.clone", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Clone(ctx, workspaceID, input, progress)
		})
}

func (s *Server) handleFetchProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	s.submitWorkspaceTask(w, r, "project.fetch", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Fetch(ctx, workspaceID, progress)
		})
}

func (s *Server) handlePullProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	var input struct {
		Force bool `json:"force"`
	}
	if err := decodeOptionalBody(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if boolQuery(r.URL.Query(), "force") {
		input.Force = true
	}
	s.submitWorkspaceTask(w, r, "project.pull", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Pull(ctx, workspaceID, input.Force, progress)
		})
}

func (s *Server) handleCheckoutProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	var input project.CheckoutInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if boolQuery(r.URL.Query(), "force") {
		input.Force = true
	}
	s.submitWorkspaceTask(w, r, "project.checkout", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Checkout(ctx, workspaceID, input, progress)
		})
}

func (s *Server) handleInitProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	opts := project.DefaultInitOptions()
	var input struct {
		InstallDependencies *bool `json:"install_dependencies"`
		PrepareDatabase     *bool `json:"prepare_database"`
	}
	if err := decodeOptionalBody(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if input.InstallDependencies != nil {
		opts.InstallDependencies = *input.InstallDependencies
	}
	if input.PrepareDatabase != nil {
		opts.PrepareDatabase = *input.PrepareDatabase
	}
	s.submitWorkspaceTask(w, r, "project.init", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Init(ctx, workspaceID, opts, progress)
		})
}

func (s *Server) handleStartProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	s.submitWorkspaceTask(w, r, "project.start", false,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Start(ctx, workspaceID, progress)
		})
}

func (s *Server) handleStopProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	s.submitWorkspaceTask(w, r, "project.stop", false,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Stop(ctx, workspaceID, progress)
		})
}

func (s *Server) handleRestartProject(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	s.submitWorkspaceTask(w, r, "project.restart", false,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			return s.opts.Project.Restart(ctx, workspaceID, progress)
		})
}

func (s *Server) submitWorkspaceTask(w http.ResponseWriter, r *http.Request, taskType string,
	cancelable bool, fn func(context.Context, string, func(int, string)) error) {
	item, err := s.opts.Workspaces.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	workspaceID := item.WorkspaceID
	submitted := s.opts.Tasks.Submit(taskType, workspaceID, cancelable,
		func(ctx context.Context, progress func(int, string)) error {
			return fn(ctx, workspaceID, progress)
		})
	httpx.WriteJSON(w, http.StatusAccepted, deleteAcceptedResponse{
		TaskID:    submitted.TaskID,
		StatusURL: "/api/v1/tasks/" + submitted.TaskID,
		Task:      submitted,
	})
}

type projectLogsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path,omitempty"`
	Total int      `json:"total"`
}

func (s *Server) handleProjectLogs(w http.ResponseWriter, r *http.Request) {
	if !s.projectReady(w, r) {
		return
	}
	query := r.URL.Query()
	tail := 200
	if raw := strings.TrimSpace(query.Get("tail")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			tail = parsed
		}
	}
	lines, path, err := s.opts.Project.Logs(r.Context(), r.PathValue("id"),
		query.Get("source"), tail)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectLogsResponse{Lines: lines, Path: path, Total: len(lines)})
}

// decodeOptionalBody accepts an absent or empty request body. Chunked bodies
// without a known length go through the normal decoder.
func decodeOptionalBody(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength <= 0 {
		return nil
	}
	return httpx.DecodeJSON(w, r, dst)
}
