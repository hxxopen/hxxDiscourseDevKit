package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/diagnostics"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/version"
)

type healthResponse struct {
	Status string    `json:"status"`
	Root   string    `json:"root"`
	Time   time.Time `json:"time"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Root:   s.opts.Paths.Root,
		Time:   time.Now().UTC(),
	})
}

type sessionResponse struct {
	CSRFToken string    `json:"csrf_token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	sess := s.sessions.getOrCreate(w, r)
	httpx.WriteJSON(w, http.StatusOK, sessionResponse{
		CSRFToken: sess.CSRF,
		ExpiresAt: sess.ExpiresAt,
	})
}

type storageInfo struct {
	DataRoot        string `json:"data_root"`
	Configured      bool   `json:"configured"`
	Split           bool   `json:"split"`
	Legacy          bool   `json:"legacy"`
	DataRootPending bool   `json:"data_root_pending"`
	Migrating       bool   `json:"migrating"`
	MigrationTaskID string `json:"migration_task_id,omitempty"`
}

type infoResponse struct {
	Version           version.Info    `json:"version"`
	InstanceID        string          `json:"instance_id"`
	Root              string          `json:"root"`
	Storage           storageInfo     `json:"storage"`
	APIAddr           string          `json:"api_addr"`
	StartedAt         time.Time       `json:"started_at"`
	Platform          platform.Info   `json:"platform"`
	ActiveWorkspaceID string          `json:"active_workspace_id"`
	WorkspaceCount    int             `json:"workspace_count"`
	Capabilities      map[string]bool `json:"capabilities"`
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	cfg := s.opts.Config.Get()
	list, err := s.opts.Workspaces.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	apiAddr := s.opts.APIAddr
	if apiAddr == "" {
		apiAddr = fmt.Sprintf("%s:%d", cfg.Network.Host, s.opts.APIPort)
	}
	storage := storageInfo{
		DataRoot:        s.opts.Paths.DataRoot,
		Configured:      strings.TrimSpace(cfg.Storage.DataRoot) != "",
		Split:           s.opts.Paths.UsesSplitDataRoot(),
		Legacy:          s.opts.Paths.LegacyDataRoot,
		DataRootPending: s.opts.Paths.DataRootPending(cfg.Storage.DataRoot),
	}
	if s.opts.StorageMigration != nil {
		storage.Migrating = s.opts.StorageMigration.Migrating()
		storage.MigrationTaskID = s.opts.StorageMigration.CurrentTaskID()
	}
	httpx.WriteJSON(w, http.StatusOK, infoResponse{
		Version:           version.Get(),
		InstanceID:        cfg.InstanceID,
		Root:              s.opts.Paths.Root,
		Storage:           storage,
		APIAddr:           apiAddr,
		StartedAt:         s.startedAt,
		Platform:          platform.Detect(),
		ActiveWorkspaceID: cfg.ActiveWorkspaceID,
		WorkspaceCount:    len(list),
		Capabilities: map[string]bool{
			"workspaces":        true,
			"tasks":             true,
			"runtime":           s.opts.Runtime != nil,
			"runtime_install":   s.opts.Runtime != nil && goruntime.GOOS == "windows",
			"services":          s.opts.Services != nil,
			"project":           s.opts.Project != nil,
			"backups":           s.opts.Backups != nil,
			"audit":             s.opts.Audit != nil,
			"plugins":           s.opts.Extensions != nil,
			"themes":            s.opts.Extensions != nil,
			"theme_preview":     s.opts.ThemePreview != nil,
			"profiles":          s.opts.Profiles != nil,
			"storage_migration": s.opts.StorageMigration != nil,
		},
	})
}

type diagnosticsResponse struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Items       []diagnostics.Check `json:"items"`
	Counts      map[string]int      `json:"counts"`
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	report := s.opts.Diagnostics.Run(r.Context())
	counts := map[string]int{}
	for severity, count := range report.Counts() {
		counts[string(severity)] = count
	}
	httpx.WriteJSON(w, http.StatusOK, diagnosticsResponse{
		GeneratedAt: report.GeneratedAt,
		Items:       report.Items,
		Counts:      counts,
	})
}

type taskListResponse struct {
	Items []task.Task `json:"items"`
	Total int         `json:"total"`
}

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	items := s.opts.Tasks.List(limit)
	if workspaceID := r.URL.Query().Get("workspace_id"); workspaceID != "" {
		filtered := items[:0]
		for _, item := range items {
			if item.WorkspaceID == workspaceID {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	httpx.WriteJSON(w, http.StatusOK, taskListResponse{Items: items, Total: len(items)})
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	item, ok := s.opts.Tasks.Get(r.PathValue("id"))
	if !ok {
		s.fail(w, r, task.ErrNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleCancelTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.opts.Tasks.Cancel(id); err != nil {
		s.fail(w, r, err)
		return
	}
	item, _ := s.opts.Tasks.Get(id)
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, r, httpx.Internal(nil), s.log)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	_, events, unsubscribe := s.opts.Tasks.Subscribe()
	defer unsubscribe()

	if _, err := fmt.Fprint(w, "event: ready\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-events:
			if !open {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: task\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
