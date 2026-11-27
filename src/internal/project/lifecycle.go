package project

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/process"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const (
	processStateStopped = "STOPPED"
	processStateRunning = "RUNNING"
)

// Start launches the development server for a workspace.
func (m *Manager) Start(ctx context.Context, ref string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	return m.startLocked(ctx, ws, progress)
}

func (m *Manager) startLocked(ctx context.Context, ws *workspace.Workspace, progress func(int, string)) error {
	if m.processes.Alive(ws.WorkspaceID) || isRunning(ws) {
		return ErrAlreadyRunning
	}
	if err := m.requireSource(ws); err != nil {
		return err
	}
	if !ws.Project.Initialized {
		return ErrNotInitialized
	}
	host := ws.Network.Host
	if host == "" {
		host = "127.0.0.1"
	}
	if ws.Network.HTTPPort < 1 {
		return fmt.Errorf("%w: Workspace 端口无效", ErrValidation)
	}
	if !portAvailable(host, ws.Network.HTTPPort) {
		suggestion := suggestPort(host, ws.Network.HTTPPort)
		if suggestion > 0 {
			return fmt.Errorf("端口 %d 已被占用，可在 workspace.json 中把 http_port 改为空闲端口（例如 %d）",
				ws.Network.HTTPPort, suggestion)
		}
		return fmt.Errorf("端口 %d 已被占用，且附近没有可用端口", ws.Network.HTTPPort)
	}

	progress(5, "检查共享服务")
	if err := m.ensureServices(ctx, func(percent int, message string) {
		progress(5+percent*15/100, message)
	}); err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}
	progress(22, "校验 Workspace 数据库")
	if err := m.ensureWorkspaceDatabase(ctx, ws); err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}
	progress(30, "生成 Discourse 环境文件")
	envValues, err := m.WriteEnvFile(ctx, ws)
	if err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}
	envSecretValues(envValues)
	env, err := m.processEnvironment(ws)
	if err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}

	bundle, prefix, err := m.bundleCommand(ctx)
	if err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}
	args := append(prefix, "exec", "rails", "server",
		"-b", host, "-p", strconv.Itoa(ws.Network.HTTPPort))
	progress(35, "启动 Rails 服务")
	command, err := m.runtime.CommandFoundInDir(ctx, bundle, args, env, m.sourcePath(ws))
	if err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}

	if _, err := m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.State = workspace.StateStarting
		current.Project.ProcessState = "STARTING"
		current.Project.PID = 0
		current.LastError = ""
		return nil
	}); err != nil {
		return err
	}

	logPath := m.stepLogPath(ws, "project")
	proc, err := m.processes.Start(process.Spec{
		Key:     ws.WorkspaceID,
		Command: command.Path,
		Args:    command.Args,
		Env:     command.Env,
		Dir:     command.Dir,
		LogPath: logPath,
	})
	if err != nil {
		return m.recordProcessError(ctx, ws.WorkspaceID, fmt.Errorf("启动 Rails 进程失败: %w", err))
	}

	progress(45, "等待服务响应")
	if err := m.waitForServer(ctx, ws, proc); err != nil {
		_ = m.processes.Stop(context.WithoutCancel(ctx), ws.WorkspaceID, m.opts.StopTimeout)
		return m.recordProcessError(ctx, ws.WorkspaceID, err)
	}

	now := time.Now().UTC()
	if _, err := m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		if current.Project.PID != 0 && current.Project.PID != proc.PID {
			return nil
		}
		current.State = workspace.StateRunning
		current.Project.ProcessState = processStateRunning
		current.Project.PID = proc.PID
		current.Project.LastStartedAt = &now
		current.LastError = ""
		return nil
	}); err != nil {
		_ = m.processes.Stop(context.WithoutCancel(ctx), ws.WorkspaceID, m.opts.StopTimeout)
		return err
	}
	progress(100, fmt.Sprintf("服务已启动：http://%s:%d", host, ws.Network.HTTPPort))
	m.log.Info("workspace started", "workspace_id", ws.WorkspaceID,
		"pid", proc.PID, "url", fmt.Sprintf("http://%s:%d", host, ws.Network.HTTPPort))
	return nil
}

// Stop terminates the workspace's development process.
func (m *Manager) Stop(ctx context.Context, ref string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	return m.stopLocked(ctx, ws, progress)
}

func (m *Manager) stopLocked(ctx context.Context, ws *workspace.Workspace, progress func(int, string)) error {
	progress(10, "停止 Rails 服务")
	if _, err := m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		if m.processes.Alive(current.WorkspaceID) {
			current.State = workspace.StateStopping
			current.Project.ProcessState = "STOPPING"
		}
		return nil
	}); err != nil {
		return err
	}
	stopErr := m.processes.Stop(ctx, ws.WorkspaceID, m.opts.StopTimeout)
	nowState := processStateStopped
	state := workspace.StateStopped
	message := ""
	if stopErr != nil {
		state = workspace.StateError
		message = truncateMessage(stopErr.Error(), 500)
	}
	if _, err := m.workspaces.Mutate(context.WithoutCancel(ctx), ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.State = state
		current.Project.ProcessState = nowState
		current.Project.PID = 0
		current.LastError = message
		return nil
	}); err != nil {
		return err
	}
	if stopErr != nil {
		return stopErr
	}
	progress(100, "服务已停止")
	m.log.Info("workspace stopped", "workspace_id", ws.WorkspaceID)
	return nil
}

// Restart stops and starts the workspace in one serialized operation.
func (m *Manager) Restart(ctx context.Context, ref string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	if err := m.stopLocked(ctx, ws, func(percent int, message string) {
		progress(percent*20/100, message)
	}); err != nil {
		return err
	}
	fresh, err := m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		return err
	}
	return m.startLocked(ctx, fresh, func(percent int, message string) {
		progress(20+percent*80/100, message)
	})
}

// Shutdown stops every supervised workspace process during daemon shutdown.
func (m *Manager) Shutdown(ctx context.Context) {
	list, err := m.workspaces.List(ctx)
	if err != nil {
		return
	}
	for _, ws := range list {
		if !m.processes.Alive(ws.WorkspaceID) {
			continue
		}
		if err := m.processes.Stop(ctx, ws.WorkspaceID, m.opts.StopTimeout); err != nil {
			m.log.Warn("could not stop workspace during shutdown",
				"workspace_id", ws.WorkspaceID, "error", err)
		}
	}
}

func (m *Manager) waitForServer(ctx context.Context, ws *workspace.Workspace, proc *process.Process) error {
	deadline := time.Now().Add(m.opts.StartTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.opts.PollInterval):
		}
		if !proc.Alive() {
			code, _ := proc.ExitCode()
			tail, _ := m.processes.Logs(ws.WorkspaceID, 30)
			message := fmt.Sprintf("Rails 进程在健康检查前退出（退出码 %d）", code)
			if len(tail) > 0 {
				message += "：" + strings.Join(tail, " | ")
			}
			return fmt.Errorf("%s", truncateMessage(message, 1500))
		}
		if portReachable(ws.Network.Host, ws.Network.HTTPPort) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待端口 %d 响应超时（%s）",
				ws.Network.HTTPPort, m.opts.StartTimeout.Round(time.Second))
		}
	}
}

func portReachable(host string, port int) bool {
	if port < 1 {
		return false
	}
	if host == "" {
		host = "127.0.0.1"
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 700*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func suggestPort(host string, start int) int {
	for port := start + 1; port < start+50 && port <= 65535; port++ {
		if portAvailable(host, port) {
			return port
		}
	}
	return 0
}

func (m *Manager) recordProcessError(ctx context.Context, workspaceID string, cause error) error {
	if _, err := m.workspaces.Mutate(context.WithoutCancel(ctx), workspaceID, func(current *workspace.Workspace) error {
		current.State = workspace.StateError
		current.Project.ProcessState = processStateStopped
		current.Project.PID = 0
		current.LastError = truncateMessage(cause.Error(), 500)
		return nil
	}); err != nil {
		m.log.Warn("could not record project error", "workspace_id", workspaceID, "error", err)
	}
	return cause
}

// handleProcessExit reconciles persisted state when a supervised process exits.
func (m *Manager) handleProcessExit(key string, pid, exitCode int, requestedStop bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, err := m.workspaces.Get(ctx, key)
	if err != nil {
		return
	}
	if ws.Project.PID != 0 && ws.Project.PID != pid {
		// A newer process replaced this one.
		return
	}
	state := workspace.StateStopped
	processState := processStateStopped
	message := ""
	if !requestedStop && exitCode != 0 {
		state = workspace.StateError
		message = fmt.Sprintf("Rails 进程意外退出（退出码 %d）", exitCode)
	}
	if _, err := m.workspaces.Mutate(ctx, key, func(current *workspace.Workspace) error {
		if current.Project.PID != 0 && current.Project.PID != pid {
			return nil
		}
		current.State = state
		current.Project.ProcessState = processState
		current.Project.PID = 0
		current.LastError = message
		return nil
	}); err != nil {
		m.log.Warn("could not reconcile process exit", "workspace_id", key, "error", err)
	}
}

// LiveProcesses returns the IDs of workspaces with an alive supervised process.
func (m *Manager) LiveProcesses() []string {
	list, err := m.workspaces.List(context.Background())
	if err != nil {
		return nil
	}
	var result []string
	for _, ws := range list {
		if m.processes.Alive(ws.WorkspaceID) {
			result = append(result, ws.WorkspaceID)
		}
	}
	return result
}

// ServiceStatus is a small helper used by the API layer to surface dependency
// health without duplicating service probing rules.
func (m *Manager) ServiceStatus(ctx context.Context) []service.Status {
	return m.services.List(ctx)
}

// Runtime is exposed for callers that need to resolve the same executables.
func (m *Manager) Runtime() *runtime.Manager { return m.runtime }
