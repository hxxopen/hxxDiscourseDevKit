package project

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/process"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Options tunes project operations.
type Options struct {
	StartTimeout time.Duration
	StopTimeout  time.Duration
	PollInterval time.Duration
}

// Manager owns source checkout and process lifecycle operations. All
// operations for one workspace are serialized.
type Manager struct {
	paths      *config.Paths
	cfg        *config.Store
	workspaces *workspace.Manager
	services   *service.Manager
	runtime    *runtime.Manager
	processes  *process.Manager
	log        *slog.Logger
	opts       Options

	locks sync.Map // workspace ID -> *sync.Mutex
}

// New creates a project manager.
func New(paths *config.Paths, cfg *config.Store, workspaces *workspace.Manager,
	services *service.Manager, rt *runtime.Manager, processes *process.Manager,
	logger *slog.Logger, opts Options) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.StartTimeout <= 0 {
		opts.StartTimeout = 180 * time.Second
	}
	if opts.StopTimeout <= 0 {
		opts.StopTimeout = 20 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	manager := &Manager{
		paths:      paths,
		cfg:        cfg,
		workspaces: workspaces,
		services:   services,
		runtime:    rt,
		processes:  processes,
		log:        logger,
		opts:       opts,
	}
	processes.SetExitHandler(manager.handleProcessExit)
	return manager
}

// Status returns the live source and process status of a workspace.
func (m *Manager) Status(ctx context.Context, ref string) (*Status, error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	dir := m.paths.WorkspaceDir(ws.WorkspaceID)
	sourceDir := filepath.Join(dir, ws.Source.Path)

	source := SourceStatus{
		Configured:    strings.TrimSpace(ws.Source.RepositoryURL) != "",
		Installed:     ws.Source.Installed,
		Path:          sourceDir,
		RepositoryURL: ws.Source.RepositoryURL,
		Branch:        ws.Source.Branch,
		Tag:           ws.Source.Tag,
		Commit:        ws.Source.Commit,
		HeadCommit:    ws.Source.HeadCommit,
		Shallow:       ws.Source.Shallow,
		Submodules:    ws.Source.Submodules,
		LastSyncAt:    ws.Source.LastSyncAt,
	}
	if info, statErr := os.Stat(sourceDir); statErr == nil && info.IsDir() {
		source.Installed = true
		if info, err := os.Stat(filepath.Join(sourceDir, ".git")); err == nil && info.IsDir() {
			source.IsGit = true
		}
	}

	checks := make([]Check, 0, 6)
	sourceCheck := Check{Name: "source", OK: source.Installed}
	if !source.Installed {
		sourceCheck.Message = "源码尚未检出"
	}
	checks = append(checks, sourceCheck)

	if source.IsGit {
		m.readGitStatus(ctx, ws, &source)
		checks = append(checks, Check{
			Name: "git", OK: source.Error == "",
			Message: source.Error,
		})
		clean := Check{Name: "git_clean", OK: !source.Dirty}
		if source.Dirty {
			clean.Message = fmt.Sprintf("%d 个文件有未提交的修改", source.ChangedFiles)
		}
		checks = append(checks, clean)
	} else if source.Installed {
		checks = append(checks, Check{Name: "git", OK: false, Message: "源码不是 Git 仓库"})
	}

	procAlive := m.processes.Alive(ws.WorkspaceID)
	process := ProcessStatus{
		Initialized:   ws.Project.Initialized,
		InitializedAt: ws.Project.InitializedAt,
		ProcessState:  ws.Project.ProcessState,
		PID:           ws.Project.PID,
		Alive:         procAlive,
		HTTPURL:       fmt.Sprintf("http://%s:%d", ws.Network.Host, ws.Network.HTTPPort),
		EnvFile:       filepath.Join(dir, "config", "discourse.env"),
		LogFile:       filepath.Join(dir, "logs", "project.log"),
		LastStartedAt: ws.Project.LastStartedAt,
		LastError:     ws.LastError,
	}
	initCheck := Check{Name: "initialized", OK: process.Initialized}
	if !process.Initialized {
		initCheck.Message = "依赖或数据库尚未初始化"
	}
	checks = append(checks, initCheck)
	checks = append(checks, Check{
		Name: "database", OK: ws.Database.Provisioned,
		Message: databaseMessage(ws),
	})
	processCheck := Check{Name: "process", OK: procAlive}
	if ws.Project.ProcessState == "RUNNING" && !procAlive {
		processCheck.Message = "记录为运行中，但进程已不存在"
	}
	checks = append(checks, processCheck)

	return &Status{
		WorkspaceID: ws.WorkspaceID,
		Name:        ws.Name,
		Source:      source,
		Process:     process,
		Checks:      checks,
	}, nil
}

// SaveSource validates and persists the desired source selection.
func (m *Manager) SaveSource(ctx context.Context, ref string, input SourceInput) (*workspace.Workspace, error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	unlock := m.lock(ws.WorkspaceID)
	defer unlock()

	ws, err = m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if isRunning(ws) || m.processes.Alive(ws.WorkspaceID) {
		return nil, ErrAlreadyRunning
	}

	next, urlChanged, selectionChanged, err := applySourceInput(ws.Source, input)
	if err != nil {
		return nil, err
	}
	if ws.Source.Installed && urlChanged {
		return nil, fmt.Errorf("%w: 源码已检出，如需更换仓库请先清空源码目录", ErrSourceInstalled)
	}
	if selectionChanged {
		next.HeadCommit = ""
	}
	return m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.Source = next
		current.LastError = ""
		return nil
	})
}

// Logs returns redacted log lines for a workspace. source is project, init, or
// git.
func (m *Manager) Logs(ctx context.Context, ref, source string, tail int) ([]string, string, error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	if tail <= 0 {
		tail = 200
	}
	if tail > 2000 {
		tail = 2000
	}
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "project":
		if lines, ok := m.processes.Logs(ws.WorkspaceID, tail); ok {
			return lines, filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "project.log"), nil
		}
		path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "project.log")
		lines, err := process.TailFile(path, tail)
		return lines, path, err
	case "init":
		path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "init.log")
		lines, err := process.TailFile(path, tail)
		return lines, path, err
	case "git":
		path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "git.log")
		lines, err := process.TailFile(path, tail)
		return lines, path, err
	default:
		return nil, "", fmt.Errorf("%w: 未知日志来源 %q", ErrValidation, source)
	}
}

// applySourceInput merges an input into a persisted source configuration.
func applySourceInput(current workspace.SourceConfig, input SourceInput) (workspace.SourceConfig, bool, bool, error) {
	next := current
	if strings.TrimSpace(input.RepositoryURL) != "" {
		next.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	}
	urlChanged := next.RepositoryURL != current.RepositoryURL
	selectionChanged := false
	switch {
	case strings.TrimSpace(input.Commit) != "":
		next.Commit = strings.TrimSpace(input.Commit)
		next.Branch = ""
		next.Tag = ""
		selectionChanged = true
	case strings.TrimSpace(input.Tag) != "":
		next.Tag = strings.TrimSpace(input.Tag)
		next.Branch = ""
		next.Commit = ""
		selectionChanged = true
	case strings.TrimSpace(input.Branch) != "":
		next.Branch = strings.TrimSpace(input.Branch)
		next.Tag = ""
		next.Commit = ""
		selectionChanged = true
	}
	if input.Shallow != nil {
		next.Shallow = *input.Shallow
	}
	if input.Submodules != nil {
		next.Submodules = *input.Submodules
	}
	if next.Branch != current.Branch || next.Tag != current.Tag || next.Commit != current.Commit {
		selectionChanged = true
	}
	if err := validateSource(next.RepositoryURL, next.Branch, next.Tag, next.Commit); err != nil {
		return current, false, false, err
	}
	return next, urlChanged, selectionChanged, nil
}

// --- helpers ----------------------------------------------------------------

func (m *Manager) lock(workspaceID string) func() {
	value, _ := m.locks.LoadOrStore(workspaceID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) sourcePath(ws *workspace.Workspace) string {
	path := ws.Source.Path
	if path == "" {
		path = "source"
	}
	return filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), path)
}

func isRunning(ws *workspace.Workspace) bool {
	return ws.State == workspace.StateRunning || ws.State == workspace.StateStarting ||
		ws.Project.ProcessState == "RUNNING" || ws.Project.ProcessState == "STARTING"
}

func databaseMessage(ws *workspace.Workspace) string {
	if ws.Database.Provisioned {
		return ""
	}
	if ws.Database.ProvisionError != "" {
		return ws.Database.ProvisionError
	}
	return "数据库尚未创建"
}

func (m *Manager) readGitStatus(ctx context.Context, ws *workspace.Workspace, source *SourceStatus) {
	statusCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	dir := m.sourcePath(ws)

	head, err := m.gitOutput(statusCtx, ws, dir, "rev-parse", "HEAD")
	if err != nil {
		source.Error = err.Error()
		return
	}
	source.HeadCommit = head
	if branch, err := m.gitOutput(statusCtx, ws, dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		source.CurrentBranch = branch
	}
	raw, err := m.gitOutput(statusCtx, ws, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		source.Error = err.Error()
		return
	}
	dirty, changed, ahead, behind := parsePorcelainV2(raw)
	source.Dirty = dirty
	source.ChangedFiles = changed
	source.Ahead = ahead
	source.Behind = behind
}

func parsePorcelainV2(raw string) (dirty bool, changed, ahead, behind int) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "# branch.ab "):
			fields := strings.Fields(strings.TrimPrefix(line, "# branch.ab "))
			for _, field := range fields {
				if strings.HasPrefix(field, "+") {
					ahead, _ = strconv.Atoi(strings.TrimPrefix(field, "+"))
				}
				if strings.HasPrefix(field, "-") {
					behind, _ = strconv.Atoi(strings.TrimPrefix(field, "-"))
				}
			}
		case strings.HasPrefix(line, "# "):
		case strings.HasPrefix(line, "1 "), strings.HasPrefix(line, "2 "),
			strings.HasPrefix(line, "u "), strings.HasPrefix(line, "? "):
			changed++
		}
	}
	return changed > 0, changed, ahead, behind
}

func portAvailable(host string, port int) bool {
	if port <= 0 {
		return false
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}

var hexRefPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)
var scpURLPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:`)

func validateSource(repositoryURL, branch, tag, commit string) error {
	repositoryURL = strings.TrimSpace(repositoryURL)
	if repositoryURL == "" {
		return fmt.Errorf("%w: 仓库地址不能为空", ErrSourceNotConfigured)
	}
	if strings.HasPrefix(repositoryURL, "-") || strings.ContainsAny(repositoryURL, "\x00\r\n ") {
		return fmt.Errorf("%w: 仓库地址无效", ErrInvalidSource)
	}
	lower := strings.ToLower(repositoryURL)
	allowed := strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "ssh://") || strings.HasPrefix(lower, "git://") ||
		strings.HasPrefix(lower, "file://") || scpURLPattern.MatchString(repositoryURL)
	if !allowed {
		return fmt.Errorf("%w: 仅支持 https、ssh、git、file 或 scp 形式的仓库地址", ErrInvalidSource)
	}
	if strings.Contains(repositoryURL, "://") {
		parts := strings.SplitN(repositoryURL, "://", 2)
		authority := parts[1]
		if index := strings.IndexAny(authority, "/"); index >= 0 {
			authority = authority[:index]
		}
		if strings.Contains(authority, "@") {
			return fmt.Errorf("%w: 仓库地址不能包含用户名或密码，请使用 SSH Agent 或 Git 凭据助手", ErrInvalidSource)
		}
	}
	for name, value := range map[string]string{"branch": branch, "tag": tag, "commit": commit} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		if err := validateRefName(value); err != nil {
			return fmt.Errorf("%w: %s %q 无效", ErrInvalidRef, name, value)
		}
	}
	if commit != "" && !hexRefPattern.MatchString(commit) {
		return fmt.Errorf("%w: commit 必须是 4-64 位十六进制对象名", ErrInvalidRef)
	}
	return nil
}

func validateRefName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return ErrInvalidRef
	}
	if strings.HasPrefix(value, "-") || strings.ContainsAny(value, "\x00\r\n \t~^:?*[\\") ||
		strings.Contains(value, "..") || strings.HasSuffix(value, ".") || strings.HasSuffix(value, "/") {
		return ErrInvalidRef
	}
	return nil
}

func isEmptyDir(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}
