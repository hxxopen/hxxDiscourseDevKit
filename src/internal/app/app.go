// Package app wires the DevKit daemon together.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/api"
	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/diagnostics"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
	"github.com/hxx/hxxDiscourseDevKit/internal/process"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/version"
	"github.com/hxx/hxxDiscourseDevKit/internal/webembed"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Options configures one daemon run.
type Options struct {
	Root        string
	Port        int
	Dev         bool
	OpenBrowser bool
	Console     bool
}

// RunFile is the discovery record for the running daemon.
type RunFile struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	Root      string    `json:"root"`
	StartedAt time.Time `json:"started_at"`
}

// Run starts the daemon and blocks until ctx is cancelled or the server fails.
func Run(ctx context.Context, opts Options) error {
	root, err := config.ResolveRoot(opts.Root)
	if err != nil {
		return err
	}
	paths := config.NewPaths(root)
	if err := paths.Ensure(); err != nil {
		return err
	}

	lock, err := store.AcquireLock(paths.LockFile())
	if errors.Is(err, store.ErrLocked) {
		return fmt.Errorf("另一个 DevKit 实例正在使用根目录 %s", root)
	}
	if err != nil {
		return fmt.Errorf("获取实例锁: %w", err)
	}
	defer func() { _ = lock.Release() }()

	cfgStore, err := config.Open(paths)
	if err != nil {
		return err
	}
	cfg := cfgStore.Get()

	logProvider, err := logging.New(logging.Options{
		Level:         cfg.Logging.Level,
		Dir:           paths.LogsDir,
		Console:       opts.Console,
		MaxFileMB:     cfg.Logging.MaxFileMB,
		RetentionDays: cfg.Logging.RetentionDays,
	})
	if err != nil {
		return err
	}
	defer func() { _ = logProvider.Close() }()

	logger := logProvider.Logger().With("module", "app")
	logger.Info("devkit starting",
		"version", version.Version,
		"root", root,
		"api_port", cfg.Network.APIPort,
	)

	secrets, err := secret.Open(paths.SecretsFile())
	if err != nil {
		return err
	}

	tasks := task.New(filepath.Join(paths.LogsDir, "tasks.jsonl"),
		logProvider.Logger().With("module", "task"))
	defer tasks.Close()

	workspaces := workspace.New(paths, cfgStore, secrets,
		logProvider.Logger().With("module", "workspace"),
		workspace.Options{PortStart: 3000, BindHost: cfg.Network.Host})
	runtimeManager := runtime.New(paths, cfgStore,
		logProvider.Logger().With("module", "runtime"))
	serviceManager := service.New(paths, cfgStore, secrets, runtimeManager,
		logProvider.Logger().With("module", "service"), service.Options{
			Dependencies: workspaces.RunningNames,
		})
	serviceManager.SetWorkspaces(workspaces)
	workspaces.SetProvisioner(serviceManager)

	processManager := process.New(logProvider.Logger().With("module", "process"))
	projectManager := project.New(paths, cfgStore, workspaces, serviceManager,
		runtimeManager, processManager,
		logProvider.Logger().With("module", "project"), project.Options{
			StartTimeout: time.Duration(cfg.Project.StartTimeoutSeconds) * time.Second,
			StopTimeout:  time.Duration(cfg.Project.StopTimeoutSeconds) * time.Second,
		})
	backupManager := backup.New(paths, workspaces, serviceManager,
		logProvider.Logger().With("module", "backup"))
	extensionManager := extension.New(paths, workspaces,
		logProvider.Logger().With("module", "extension"))

	if err := workspaces.Reconcile(ctx); err != nil {
		logger.Warn("workspace reconcile reported an error", "error", err)
	}
	defaultWorkspace, defaultCreated, err := workspaces.EnsureDefault(ctx)
	if err != nil {
		return fmt.Errorf("初始化 Default Workspace: %w", err)
	}
	if defaultCreated && defaultWorkspace != nil {
		logger.Info("default workspace created", "workspace_id", defaultWorkspace.WorkspaceID)
	}

	diag := diagnostics.New(paths, cfgStore, workspaces,
		logProvider.Logger().With("module", "diagnostics"),
		diagnostics.Options{Runtime: runtimeManager, Services: serviceManager})

	port := opts.Port
	if port == 0 {
		port = cfg.Network.APIPort
	}
	listenAddr := net.JoinHostPort(cfg.Network.Host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败（端口可能被占用，可使用 --port 指定其他端口）: %w", listenAddr, err)
	}

	apiServer := api.New(api.Options{
		Paths:       paths,
		Config:      cfgStore,
		Secrets:     secrets,
		Workspaces:  workspaces,
		Runtime:     runtimeManager,
		Services:    serviceManager,
		Project:     projectManager,
		Backups:     backupManager,
		Extensions:  extensionManager,
		Tasks:       tasks,
		Diagnostics: diag,
		UI:          webembed.FS(),
		Logger:      logProvider.Logger(),
		Dev:         opts.Dev,
		APIPort:     port,
		APIAddr:     listener.Addr().String(),
		StartedAt:   time.Now(),
		SetLogLevel: logProvider.SetLevel,
	})

	httpServer := &http.Server{
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	runFile := RunFile{
		PID:       os.Getpid(),
		Addr:      listener.Addr().String(),
		Root:      root,
		StartedAt: time.Now().UTC(),
	}
	if err := store.WriteJSON(paths.RunFile(), runFile, 0o600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("写入运行状态: %w", err)
	}
	defer func() { _ = os.Remove(paths.RunFile()) }()

	serveErr := make(chan error, 1)
	go func() { serveErr <- httpServer.Serve(listener) }()

	apiURL := "http://" + listener.Addr().String()
	logger.Info("devkit listening", "url", apiURL, "pid", runFile.PID)
	if opts.OpenBrowser {
		go func() {
			if err := platform.OpenBrowser(apiURL); err != nil {
				logger.Warn("could not open browser", "error", err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		logger.Info("shutdown requested")
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务异常退出: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http shutdown", "error", err)
	}
	projectManager.Shutdown(shutdownCtx)
	tasks.Close()
	logger.Info("devkit stopped")
	return nil
}

// DiscardLogger is a helper for tests or commands that need a no-op logger.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
