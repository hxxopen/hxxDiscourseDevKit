package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

type serviceListResponse struct {
	Items []service.Status `json:"items"`
	Total int              `json:"total"`
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	if s.opts.Services == nil {
		s.fail(w, r, errFeatureUnavailable("services"))
		return
	}
	items := s.opts.Services.List(r.Context())
	httpx.WriteJSON(w, http.StatusOK, serviceListResponse{Items: items, Total: len(items)})
}

func (s *Server) handleGetService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	status, err := manager.Status(r.Context(), name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

type serviceActionResponse struct {
	Status *service.Status `json:"status"`
}

func (s *Server) handleStartService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	opCtx, cancel := longServiceContext(r)
	defer cancel()
	if err := manager.Start(opCtx, name, nil); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeServiceStatus(opCtx, w, r, manager, name)
}

func (s *Server) handleStopService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	opCtx, cancel := longServiceContext(r)
	defer cancel()
	if err := manager.Stop(opCtx, name, nil); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeServiceStatus(opCtx, w, r, manager, name)
}

func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	opCtx, cancel := longServiceContext(r)
	defer cancel()
	if err := manager.Restart(opCtx, name, nil); err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeServiceStatus(opCtx, w, r, manager, name)
}

func (s *Server) writeServiceStatus(ctx context.Context, w http.ResponseWriter, r *http.Request,
	manager *service.Manager, name service.Name) {
	status, err := manager.Status(ctx, name)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, serviceActionResponse{Status: &status})
}

// longServiceContext keeps an initdb/start sequence running even if the CLI
// disconnects, while still bounding it.
func longServiceContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Minute)
}

func (s *Server) handleTestService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	var override *service.ConfigInput
	var input service.ConfigInput
	if err := decodeOptionalJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if input.Mode != "" || input.Host != "" || input.Port != 0 || input.Password != "" ||
		input.Username != "" || input.Database != "" || input.AdminUser != "" || input.TLS {
		override = &input
	}
	result, err := manager.Test(r.Context(), name, override)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) handleUpdateService(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	var input service.ConfigInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	status, err := manager.Update(r.Context(), name, input)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

type serviceLogsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path,omitempty"`
	Total int      `json:"total"`
}

func (s *Server) handleServiceLogs(w http.ResponseWriter, r *http.Request) {
	manager, name, ok := s.serviceTarget(w, r)
	if !ok {
		return
	}
	tail := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("tail")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			tail = parsed
		}
	}
	lines, path, err := manager.Logs(r.Context(), name, tail)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, serviceLogsResponse{Lines: lines, Path: path, Total: len(lines)})
}

type databasesResponse struct {
	Items []string `json:"items"`
	Total int      `json:"total"`
}

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	if s.opts.Services == nil {
		s.fail(w, r, errFeatureUnavailable("services"))
		return
	}
	items, err := s.opts.Services.Databases(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, databasesResponse{Items: items, Total: len(items)})
}

func (s *Server) serviceTarget(w http.ResponseWriter, r *http.Request) (*service.Manager, service.Name, bool) {
	if s.opts.Services == nil {
		s.fail(w, r, errFeatureUnavailable("services"))
		return nil, "", false
	}
	name, err := service.ParseName(r.PathValue("name"))
	if err != nil {
		s.fail(w, r, err)
		return nil, "", false
	}
	return s.opts.Services, name, true
}

func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return httpx.DecodeJSON(w, r, dst)
}
