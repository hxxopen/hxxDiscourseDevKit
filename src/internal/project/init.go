package project

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const maxStepLogBytes = 256 << 10

// Init prepares the checkout: shared services, environment file, Ruby and
// Node.js dependencies, and the development database.
func (m *Manager) Init(ctx context.Context, ref string, opts InitOptions, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()

	if isRunning(ws) || m.processes.Alive(ws.WorkspaceID) {
		return ErrAlreadyRunning
	}
	if err := m.requireSource(ws); err != nil {
		return err
	}
	if _, err := m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.State = workspace.StateInitializing
		current.Project.ProcessState = "STOPPED"
		current.Project.PID = 0
		current.LastError = ""
		return nil
	}); err != nil {
		return err
	}

	initLog := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "init.log")
	m.appendStepLog(initLog, "初始化开始 workspace="+ws.Name, "")

	finish := func(initErr error) error {
		now := time.Now().UTC()
		_, mutateErr := m.workspaces.Mutate(context.WithoutCancel(ctx), ws.WorkspaceID, func(current *workspace.Workspace) error {
			current.Project.ProcessState = "STOPPED"
			current.Project.PID = 0
			if initErr != nil {
				current.State = workspace.StateError
				current.LastError = truncateMessage(initErr.Error(), 500)
				return nil
			}
			current.State = workspace.StateReady
			current.Project.Initialized = true
			current.Project.InitializedAt = &now
			current.LastError = ""
			return nil
		})
		if mutateErr != nil {
			m.appendStepLog(initLog, "无法保存初始化结果: "+mutateErr.Error(), "")
		}
		if initErr != nil {
			m.appendStepLog(initLog, "初始化失败", initErr.Error())
			return initErr
		}
		return mutateErr
	}

	progress(5, "检查共享服务")
	if err := m.ensureServices(ctx, func(percent int, message string) {
		progress(5+percent*20/100, message)
	}); err != nil {
		return finish(err)
	}

	progress(30, "校验 Workspace 数据库")
	if opts.PrepareDatabase {
		if err := m.ensureWorkspaceDatabase(ctx, ws); err != nil {
			return finish(err)
		}
	}

	progress(40, "生成 Discourse 环境文件")
	if _, err := m.ensureEnvFile(ctx, ws); err != nil {
		return finish(err)
	}
	env, err := m.processEnvironment(ws)
	if err != nil {
		return finish(err)
	}
	sourceDir := m.sourcePath(ws)

	if opts.InstallDependencies {
		progress(45, "安装 Ruby 依赖（bundle install）")
		if err := m.installRubyDependencies(ctx, ws, sourceDir, env); err != nil {
			return finish(err)
		}
		progress(75, "安装前端依赖")
		if err := m.installNodeDependencies(ctx, ws, sourceDir, env); err != nil {
			return finish(err)
		}
	}

	if opts.PrepareDatabase {
		progress(88, "初始化数据库（rails db:create db:migrate）")
		if err := m.prepareRailsDatabase(ctx, ws, sourceDir, env); err != nil {
			return finish(err)
		}
	}

	progress(100, "项目初始化完成")
	return finish(nil)
}

// ensureSource verifies that a usable source directory exists.
func (m *Manager) requireSource(ws *workspace.Workspace) error {
	dir := m.sourcePath(ws)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ErrSourceMissing
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return ErrSourceMissing
	}
	if _, err := os.Stat(filepath.Join(dir, "Gemfile")); err != nil {
		return fmt.Errorf("%w: 源码目录中没有 Gemfile，请确认检出的是 Discourse 仓库", ErrValidation)
	}
	return nil
}

// ensureServices starts managed dependencies that are not yet reachable.
func (m *Manager) ensureServices(ctx context.Context, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	for index, name := range []service.Name{service.PostgreSQL, service.Redis} {
		status, err := m.services.Status(ctx, name)
		if err != nil {
			return err
		}
		if status.Mode == "external" {
			if !status.Reachable {
				return fmt.Errorf("外部 %s 不可达: %s", name, status.Message)
			}
			progress((index+1)*50, string(name)+" 可用")
			continue
		}
		if status.State == service.StateRunning && status.Reachable {
			progress((index+1)*50, string(name)+" 正在运行")
			continue
		}
		progress(index*50, "启动 "+string(name))
		if err := m.services.Start(ctx, name, func(percent int, message string) {
			progress(index*50+percent/2, message)
		}); err != nil {
			return fmt.Errorf("启动 %s 失败: %w", name, err)
		}
		progress((index+1)*50, string(name)+" 已启动")
	}
	return nil
}

func (m *Manager) ensureWorkspaceDatabase(ctx context.Context, ws *workspace.Workspace) error {
	updated, err := m.workspaces.Provision(ctx, ws.WorkspaceID)
	if err != nil {
		return fmt.Errorf("创建 Workspace 数据库失败: %w", err)
	}
	if !updated.Database.Provisioned {
		message := updated.Database.ProvisionError
		if message == "" {
			message = "未知原因"
		}
		return fmt.Errorf("Workspace 数据库不可用: %s", message)
	}
	return nil
}

func (m *Manager) installRubyDependencies(ctx context.Context, ws *workspace.Workspace, dir string, env []string) error {
	bundle, prefix, err := m.bundleCommand(ctx)
	if err != nil {
		return err
	}
	if _, runErr := m.runStep(ctx, ws, "init", "bundle config", bundle, append(prefix, "config", "set", "--local", "path", "vendor/bundle"), dir, env); runErr != nil {
		return runErr
	}
	_, err = m.runStep(ctx, ws, "init", "bundle install", bundle, append(prefix, "install"), dir, env)
	return err
}

func (m *Manager) installNodeDependencies(ctx context.Context, ws *workspace.Workspace, dir string, env []string) error {
	packageJSON := filepath.Join(dir, "package.json")
	if _, err := os.Stat(packageJSON); err != nil {
		m.appendStepLog(filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "init.log"),
			"跳过前端依赖：未找到 package.json", "")
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, "yarn.lock")); err == nil {
		if yarn, findErr := m.runtime.FindBinary(ctx, "yarn"); findErr == nil {
			_, err := m.runStep(ctx, ws, "init", "yarn install", yarn, []string{"install"}, dir, env)
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules")); err == nil {
		m.appendStepLog(filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "init.log"),
			"未找到 yarn，且 node_modules 已存在；跳过前端依赖安装", "")
		return nil
	}
	npm, err := m.runtime.FindBinary(ctx, "npm")
	if err != nil {
		return fmt.Errorf("未找到 yarn 或 npm，无法安装前端依赖")
	}
	args := []string{"install"}
	if _, lockErr := os.Stat(filepath.Join(dir, "package-lock.json")); lockErr == nil {
		args = append(args, "--no-audit", "--no-fund")
	}
	_, err = m.runStep(ctx, ws, "init", "npm install", npm, args, dir, env)
	return err
}

func (m *Manager) prepareRailsDatabase(ctx context.Context, ws *workspace.Workspace, dir string, env []string) error {
	bundle, prefix, err := m.bundleCommand(ctx)
	if err != nil {
		return err
	}
	args := append(prefix, "exec", "rails", "db:create", "db:migrate")
	_, err = m.runStep(ctx, ws, "init", "rails db:create db:migrate", bundle, args, dir, env)
	return err
}

// bundleCommand resolves bundler, falling back to `ruby -S bundle`.
func (m *Manager) bundleCommand(ctx context.Context) (runtime.Found, []string, error) {
	if found, err := m.runtime.FindBinary(ctx, "bundle"); err == nil {
		return found, nil, nil
	}
	ruby, err := m.runtime.FindBinary(ctx, "ruby")
	if err != nil {
		return runtime.Found{}, nil, fmt.Errorf("未找到 bundle 或 ruby；请安装 Ruby 与 Bundler（Windows 可在 WSL2 中安装）")
	}
	return ruby, []string{"-S", "bundle"}, nil
}

// runStep executes one initialization command with logging and a clear error.
func (m *Manager) runStep(ctx context.Context, ws *workspace.Workspace, logName, label string,
	found runtime.Found, args []string, dir string, env []string) (runtime.RunResult, error) {
	if label == "" {
		label = found.Binary
	}
	display := logging.Redact("$ " + label)
	m.appendStepLog(m.stepLogPath(ws, logName), "运行 "+display, "")
	start := time.Now()
	run, err := m.runtime.RunFound(ctx, found, runtime.RunOptions{
		Args: args,
		Dir:  dir,
		Env:  env,
	})
	output := run.Stdout
	if run.Stderr != "" {
		output += "\n" + run.Stderr
	}
	m.appendStepLog(m.stepLogPath(ws, logName), display+" 用时 "+time.Since(start).Round(time.Second).String(), output)
	if err != nil || run.ExitCode != 0 {
		return run, fmt.Errorf("%s 失败: %s", label, commandFailure(run, err))
	}
	return run, nil
}

func (m *Manager) stepLogPath(ws *workspace.Workspace, name string) string {
	if name == "" {
		name = "project"
	}
	return filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", name+".log")
}

func (m *Manager) appendStepLog(path, header, output string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	header = logging.Redact(header)
	output = logging.Redact(output)
	if len(output) > maxStepLogBytes {
		output = output[len(output)-maxStepLogBytes:]
	}
	_, _ = fmt.Fprintf(file, "[%s] %s\n", time.Now().UTC().Format(time.RFC3339), header)
	if strings.TrimSpace(output) != "" {
		_, _ = fmt.Fprintln(file, strings.TrimRight(output, "\r\n"))
	}
}

func commandFailure(run runtime.RunResult, err error) string {
	message := strings.TrimSpace(run.Stderr)
	if message == "" {
		message = strings.TrimSpace(run.Stdout)
	}
	if message == "" && err != nil {
		message = err.Error()
	}
	message = logging.Redact(message)
	if len(message) > 1500 {
		message = message[len(message)-1500:]
	}
	if message == "" {
		message = fmt.Sprintf("退出码 %d", run.ExitCode)
	}
	return message
}

func truncateMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
