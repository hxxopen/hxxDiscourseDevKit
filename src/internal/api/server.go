// Package api implements the loopback-only management HTTP API and UI host.
package api

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/diagnostics"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Options supplies the API server's dependencies.
type Options struct {
	Paths       *config.Paths
	Config      *config.Store
	Secrets     *secret.Store
	Workspaces  *workspace.Manager
	Runtime     *runtime.Manager
	Services    *service.Manager
	Project     *project.Manager
	Backups     *backup.Manager
	Tasks       *task.Manager
	Diagnostics *diagnostics.Service
	UI          fs.FS
	Logger      *slog.Logger
	Dev         bool
	APIPort     int
	APIAddr     string
	StartedAt   time.Time
	SetLogLevel func(level string)
}

// Server handles HTTP management requests.
type Server struct {
	opts           Options
	log            *slog.Logger
	sessions       *sessionStore
	allowedOrigins map[string]bool
	startedAt      time.Time
	// startupConfig records the configuration the daemon booted with so
	// settings that need a restart can be reported to the UI.
	startupConfig config.Config
}

// New builds an API server.
func New(opts Options) *Server {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.StartedAt.IsZero() {
		opts.StartedAt = time.Now()
	}
	if opts.APIPort == 0 {
		opts.APIPort = opts.Config.Get().Network.APIPort
	}
	server := &Server{
		opts:           opts,
		log:            opts.Logger.With("module", "api"),
		sessions:       newSessionStore(),
		allowedOrigins: buildAllowedOrigins(opts.APIPort, opts.Dev),
		startedAt:      opts.StartedAt,
		startupConfig:  opts.Config.Get(),
	}
	return server
}

// Handler returns the fully wrapped HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	mux.HandleFunc("/", s.handleUI)

	return httpx.Chain(mux,
		httpx.Recoverer(s.log),
		httpx.RequestID,
		httpx.SecurityHeaders,
		httpx.AccessLog(s.log),
		s.hostGuard,
	)
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/system/health", s.handleHealth)
	mux.HandleFunc("GET /api/v1/system/session", s.handleSession)

	auth := func(pattern string, handler http.HandlerFunc) {
		mux.Handle(pattern, s.auth(handler))
	}

	auth("GET /api/v1/system/info", s.handleInfo)
	auth("GET /api/v1/diagnostics", s.handleDiagnostics)
	auth("GET /api/v1/settings", s.handleGetSettings)
	auth("PATCH /api/v1/settings", s.handleUpdateSettings)

	auth("GET /api/v1/logs/sources", s.handleListLogSources)
	auth("GET /api/v1/logs", s.handleReadLog)

	auth("GET /api/v1/events", s.handleEvents)
	auth("GET /api/v1/tasks", s.handleListTasks)
	auth("GET /api/v1/tasks/{id}", s.handleGetTask)
	auth("POST /api/v1/tasks/{id}/cancel", s.handleCancelTask)

	auth("GET /api/v1/workspaces", s.handleListWorkspaces)
	auth("POST /api/v1/workspaces", s.handleCreateWorkspace)
	auth("GET /api/v1/workspaces/{id}", s.handleGetWorkspace)
	auth("PATCH /api/v1/workspaces/{id}", s.handleUpdateWorkspace)
	auth("POST /api/v1/workspaces/{id}/activate", s.handleActivateWorkspace)
	auth("GET /api/v1/workspaces/{id}/status", s.handleWorkspaceStatus)
	auth("POST /api/v1/workspaces/{id}/provision", s.handleProvisionWorkspace)
	auth("DELETE /api/v1/workspaces/{id}/preview", s.handlePreviewDelete)
	auth("DELETE /api/v1/workspaces/{id}", s.handleDeleteWorkspace)
	auth("POST /api/v1/workspaces/{id}/start", s.handleStartProject)
	auth("POST /api/v1/workspaces/{id}/stop", s.handleStopProject)
	auth("POST /api/v1/workspaces/{id}/restart", s.handleRestartProject)
	auth("GET /api/v1/workspaces/{id}/logs", s.handleProjectLogs)

	auth("GET /api/v1/workspaces/{id}/project", s.handleGetProject)
	auth("PUT /api/v1/workspaces/{id}/project/source", s.handleUpdateProjectSource)
	auth("POST /api/v1/workspaces/{id}/project/clone", s.handleCloneProject)
	auth("POST /api/v1/workspaces/{id}/project/fetch", s.handleFetchProject)
	auth("POST /api/v1/workspaces/{id}/project/pull", s.handlePullProject)
	auth("POST /api/v1/workspaces/{id}/project/checkout", s.handleCheckoutProject)
	auth("POST /api/v1/workspaces/{id}/project/init", s.handleInitProject)
	auth("GET /api/v1/workspaces/{id}/project/status", s.handleProjectStatus)

	auth("GET /api/v1/workspaces/{id}/backups", s.handleListBackups)
	auth("POST /api/v1/workspaces/{id}/backups", s.handleCreateBackup)
	auth("GET /api/v1/workspaces/{id}/backups/{backupID}", s.handleGetBackup)
	auth("DELETE /api/v1/workspaces/{id}/backups/{backupID}", s.handleDeleteBackup)
	auth("POST /api/v1/workspaces/{id}/backups/{backupID}/restore-preview", s.handlePreviewRestore)
	auth("POST /api/v1/workspaces/{id}/backups/{backupID}/restore", s.handleRestoreBackup)

	auth("GET /api/v1/runtime/matrix", s.handleRuntimeMatrix)
	auth("GET /api/v1/runtime/components", s.handleListRuntimeComponents)
	auth("GET /api/v1/runtime/components/{name}", s.handleGetRuntimeComponent)
	auth("POST /api/v1/runtime/verify", s.handleVerifyRuntime)

	auth("GET /api/v1/services", s.handleListServices)
	auth("GET /api/v1/services/{name}", s.handleGetService)
	auth("PATCH /api/v1/services/{name}", s.handleUpdateService)
	auth("GET /api/v1/services/{name}/logs", s.handleServiceLogs)
	auth("POST /api/v1/services/{name}/test", s.handleTestService)
	auth("POST /api/v1/services/{name}/start", s.handleStartService)
	auth("POST /api/v1/services/{name}/stop", s.handleStopService)
	auth("POST /api/v1/services/{name}/restart", s.handleRestartService)
	auth("GET /api/v1/services/postgresql/databases", s.handleListDatabases)
}

// handleNotImplemented returns a stable 501 for routes reserved by later
// milestones.
func (s *Server) handleNotImplemented(message string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, r,
			httpx.New(http.StatusNotImplemented, "NOT_IMPLEMENTED", message), s.log)
	}
}

// hostGuard rejects requests whose Host is not a loopback name. It allows any
// port so tests and development proxies can reach the daemon.
func (s *Server) hostGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.ToLower(strings.Trim(host, "[]"))
		switch host {
		case "127.0.0.1", "::1", "localhost":
			next.ServeHTTP(w, r)
		default:
			httpx.WriteError(w, r,
				httpx.Forbidden("FORBIDDEN_HOST", "management API only accepts loopback hosts"),
				s.log)
		}
	})
}

func buildAllowedOrigins(apiPort int, dev bool) map[string]bool {
	origins := map[string]bool{
		fmt.Sprintf("http://127.0.0.1:%d", apiPort): true,
		fmt.Sprintf("http://localhost:%d", apiPort): true,
		fmt.Sprintf("http://[::1]:%d", apiPort):     true,
	}
	if dev {
		origins["http://127.0.0.1:5173"] = true
		origins["http://localhost:5173"] = true
	}
	return origins
}

// fail maps domain errors to API responses.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	httpx.WriteError(w, r, mapError(err), s.log)
}
