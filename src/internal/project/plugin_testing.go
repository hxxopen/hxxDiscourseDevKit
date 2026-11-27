package project

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

const maxTestLogBytes = 8 << 20

// RunPluginTest executes a plugin's RSpec suite with LOAD_PLUGINS=1 against an
// isolated per-workspace test database. Output is streamed to logs/test.log so
// the UI can follow long-running tests.
func (m *Manager) RunPluginTest(ctx context.Context, plan extension.PluginTestPlan, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, plan.WorkspaceID)
	if err != nil {
		return err
	}
	defer unlock()

	if !ws.Source.Installed {
		return ErrSourceMissing
	}
	if !ws.Project.Initialized {
		return fmt.Errorf("%w: 请先在项目页初始化 Ruby 依赖与数据库", ErrNotInitialized)
	}
	slug := strings.ToLower(strings.TrimSpace(plan.Slug))
	if !validPluginSlug(slug) {
		return fmt.Errorf("%w: 插件目录名不合法", ErrValidation)
	}
	sourceDir := m.sourcePath(ws)
	pluginDir := filepath.Join(sourceDir, "plugins", slug)
	info, statErr := os.Lstat(pluginDir)
	switch {
	case statErr != nil:
		return fmt.Errorf("插件目录 %s 不可访问: %w", pluginDir, statErr)
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%w: 插件目录是符号链接，DevKit 不会执行链接目标", ErrValidation)
	case !info.IsDir():
		return fmt.Errorf("%w: %s 不是插件目录", ErrValidation, slug)
	}
	specDir := filepath.Join(pluginDir, "spec")
	specInfo, specErr := os.Lstat(specDir)
	switch {
	case specErr != nil:
		return fmt.Errorf("%w: 插件缺少 spec/ 目录", ErrValidation)
	case specInfo.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%w: spec/ 目录是符号链接，DevKit 不会执行链接目标", ErrValidation)
	case !specInfo.IsDir():
		return fmt.Errorf("%w: spec/ 不是目录", ErrValidation)
	}

	database := ws.TestDatabaseName()
	if database == "" {
		return fmt.Errorf("%w: Workspace 没有可用于测试的数据库名", ErrValidation)
	}

	progress(5, "检查测试服务")
	if err := m.ensureServices(ctx, func(percent int, message string) {
		progress(5+percent*10/100, message)
	}); err != nil {
		return err
	}
	progress(16, "准备测试数据库 "+database)
	if _, err := m.services.EnsureWorkspaceTestDatabase(ctx, ws); err != nil {
		return fmt.Errorf("准备测试数据库失败: %w", err)
	}
	env, err := m.processEnvironment(ws, pluginTestEnvironment(database)...)
	if err != nil {
		return err
	}
	bundle, prefix, err := m.bundleCommand(ctx)
	if err != nil {
		return err
	}

	logPath := m.stepLogPath(ws, "test")
	rotateLog(logPath, maxTestLogBytes)
	m.appendStepLog(logPath,
		fmt.Sprintf("插件测试开始 workspace=%s plugin=%s database=%s", ws.Name, slug, database), "")
	fail := func(testErr error) error {
		m.appendStepLog(logPath, "插件测试失败", testErr.Error())
		return testErr
	}

	migrateArgs, rspecArgs := pluginTestCommands(prefix, slug)
	progress(30, "创建并迁移测试数据库")
	if err := m.runStreamingStep(ctx, logPath,
		"bundle exec rake db:create db:migrate", bundle, migrateArgs, sourceDir, env); err != nil {
		return fail(err)
	}
	progress(55, "运行插件 RSpec 测试")
	if err := m.runStreamingStep(ctx, logPath,
		"bundle exec rspec plugins/"+slug+"/spec", bundle, rspecArgs, sourceDir, env); err != nil {
		return fail(err)
	}
	progress(100, "插件测试通过")
	m.appendStepLog(logPath, fmt.Sprintf("插件测试通过 workspace=%s plugin=%s", ws.Name, slug), "")
	m.log.Info("plugin tests passed", "workspace_id", ws.WorkspaceID, "plugin", slug)
	return nil
}

func pluginTestEnvironment(database string) []envVar {
	return []envVar{
		{"RAILS_ENV", "test"},
		{"DISCOURSE_ENV", "test"},
		{"LOAD_PLUGINS", "1"},
		{"RAILS_DB", database},
		{"DISCOURSE_DB_NAME", database},
	}
}

func pluginTestCommands(prefix []string, slug string) (migrate, rspec []string) {
	base := append([]string(nil), prefix...)
	migrate = append(append([]string(nil), base...), "exec", "rake", "db:create", "db:migrate")
	rspec = append(append([]string(nil), base...), "exec", "rspec",
		filepath.ToSlash(filepath.Join("plugins", slug, "spec")))
	return migrate, rspec
}

func validPluginSlug(slug string) bool {
	if slug == "" || slug == "." || slug == ".." || len(slug) > 64 {
		return false
	}
	for index, r := range slug {
		alphanumeric := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if index == 0 && !alphanumeric {
			return false
		}
		if !alphanumeric && r != '.' && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func rotateLog(path string, maxBytes int64) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxBytes {
		return
	}
	rotated := path + ".1"
	_ = os.Remove(rotated)
	_ = os.Rename(path, rotated)
}

// runStreamingStep runs one long-lived command while writing redacted output to
// both the workspace log and a bounded tail used for error messages.
func (m *Manager) runStreamingStep(ctx context.Context, logPath, label string,
	found runtime.Found, args []string, dir string, env []string) error {
	display := logging.Redact("$ " + label)
	m.appendStepLog(logPath, "运行 "+display, "")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开测试日志: %w", err)
	}
	defer func() { _ = file.Close() }()

	command, err := m.runtime.CommandFoundInDir(ctx, found, args, env, dir)
	if err != nil {
		return err
	}
	writer := &lineLogWriter{file: file, limit: 64 << 10}
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	platform.HideWindow(cmd)
	cmd.Env = command.Env
	cmd.Dir = command.Dir
	cmd.Stdout = writer
	cmd.Stderr = writer

	start := time.Now()
	runErr := cmd.Run()
	writer.Flush()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	summary := fmt.Sprintf("退出码 %d，用时 %s", exitCode, time.Since(start).Round(time.Second))
	m.appendStepLog(logPath, display+" 完成", summary)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if runErr != nil || exitCode != 0 {
		message := strings.TrimSpace(writer.Tail())
		if message == "" && runErr != nil {
			message = runErr.Error()
		}
		message = logging.Redact(message)
		if message == "" {
			message = summary
		}
		return fmt.Errorf("%s 失败: %s", label, truncateMessage(message, 1500))
	}
	return nil
}

// lineLogWriter redacts secrets per complete line and keeps a bounded tail.
type lineLogWriter struct {
	mu      sync.Mutex
	file    *os.File
	pending []byte
	tail    []byte
	limit   int
}

func (w *lineLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, data...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := append([]byte(nil), w.pending[:index+1]...)
		w.pending = w.pending[index+1:]
		w.writeLine(line)
	}
	if len(w.pending) > 256<<10 {
		w.writeLine(w.pending)
		w.pending = w.pending[:0]
	}
	return len(data), nil
}

func (w *lineLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) > 0 {
		w.writeLine(w.pending)
		w.pending = w.pending[:0]
	}
}

func (w *lineLogWriter) Tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return string(w.tail)
}

func (w *lineLogWriter) writeLine(data []byte) {
	text := logging.Redact(string(data))
	_, _ = io.WriteString(w.file, text)
	if len(text) >= w.limit {
		w.tail = append(w.tail[:0], text[len(text)-w.limit:]...)
		return
	}
	w.tail = append(w.tail, text...)
	if len(w.tail) > w.limit {
		w.tail = append([]byte(nil), w.tail[len(w.tail)-w.limit:]...)
	}
}
