package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const maxGitLogBytes = 64 << 10

var gitEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_PAGER=cat",
	"LC_ALL=C",
}

// Clone checks out the workspace source into its empty source directory.
func (m *Manager) Clone(ctx context.Context, ref string, input SourceInput, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return err
	}
	unlock := m.lock(ws.WorkspaceID)
	defer unlock()

	ws, err = m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		return err
	}
	if isRunning(ws) || m.processes.Alive(ws.WorkspaceID) {
		return ErrAlreadyRunning
	}
	if ws.Source.Installed {
		return fmt.Errorf("%w: 已检出源码；请使用 fetch、pull 或 checkout", ErrSourceInstalled)
	}

	next, _, _, err := applySourceInput(ws.Source, input)
	if err != nil {
		return err
	}
	sourceDir := m.sourcePath(ws)
	empty, err := isEmptyDir(sourceDir)
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("%w: %s", ErrSourceNotEmpty, sourceDir)
	}

	git, err := m.runtime.FindBinary(ctx, "git")
	if err != nil {
		return fmt.Errorf("未找到 git（Windows 上可在 WSL2 中安装）: %w", err)
	}

	progress(5, "准备克隆 "+next.RepositoryURL)
	tmpRoot := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return err
	}
	tmpDir := filepath.Join(tmpRoot, "clone-"+id.MustNew())
	_ = os.RemoveAll(tmpDir)
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	execTmp, err := m.runtime.TranslatePath(git.Execution, tmpDir)
	if err != nil {
		return err
	}
	args := []string{"clone"}
	switch {
	case next.Commit != "":
		// A specific commit cannot be combined with --branch/--depth.
	case next.Tag != "":
		args = append(args, "--branch", next.Tag)
	case next.Branch != "":
		args = append(args, "--branch", next.Branch)
	}
	if next.Shallow && next.Commit == "" {
		args = append(args, "--depth", "1")
	}
	if next.Submodules {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, "--", next.RepositoryURL, execTmp)

	progress(15, "正在克隆仓库（可能需要几分钟）")
	run, runErr := m.runtime.RunFound(ctx, git, runtime.RunOptions{Args: args, Env: gitEnv})
	m.appendGitLog(ws, append([]string{"git"}, args...), run.Stdout+"\n"+run.Stderr)
	if runErr != nil || run.ExitCode != 0 {
		return gitCommandError(args, run, runErr)
	}

	if next.Commit != "" {
		progress(55, "检出提交 "+shortRef(next.Commit))
		run, runErr = m.runGitAt(ctx, git, tmpDir, "checkout", "--detach", next.Commit)
		m.appendGitLog(ws, []string{"git", "checkout", "--detach", next.Commit}, run.Stdout+"\n"+run.Stderr)
		if runErr != nil || run.ExitCode != 0 {
			return gitCommandError([]string{"checkout", next.Commit}, run, runErr)
		}
	}

	head, headErr := m.gitOutputAt(ctx, git, tmpDir, "rev-parse", "HEAD")
	if headErr != nil {
		return fmt.Errorf("读取克隆结果失败: %w", headErr)
	}
	currentBranch, _ := m.gitOutputAt(ctx, git, tmpDir, "symbolic-ref", "--short", "-q", "HEAD")

	progress(85, "正在登记 Workspace 源码")
	if err := os.Remove(sourceDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("源码目录不是空目录: %w", err)
	}
	if err := os.Rename(tmpDir, sourceDir); err != nil {
		return fmt.Errorf("移动克隆结果: %w", err)
	}

	now := time.Now().UTC()
	_, err = m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.Source = next
		current.Source.Installed = true
		current.Source.HeadCommit = head
		current.Source.LastSyncAt = &now
		if current.Source.Branch == "" && current.Source.Tag == "" && current.Source.Commit == "" && currentBranch != "" {
			current.Source.Branch = currentBranch
		}
		current.State = workspace.StateEmpty
		current.LastError = ""
		return nil
	})
	if err != nil {
		return err
	}
	progress(100, "源码克隆完成")
	m.log.Info("workspace source cloned", "workspace_id", ws.WorkspaceID,
		"repository", logging.Redact(next.RepositoryURL), "commit", head)
	return nil
}

// Fetch updates remote-tracking refs without changing the working tree.
func (m *Manager) Fetch(ctx context.Context, ref string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	if err := m.requireGitSource(ws); err != nil {
		return err
	}
	git, err := m.runtime.FindBinary(ctx, "git")
	if err != nil {
		return fmt.Errorf("未找到 git: %w", err)
	}
	progress(20, "正在获取远程更新")
	run, runErr := m.runGitAt(ctx, git, m.sourcePath(ws), "fetch", "--all", "--prune", "--tags")
	m.appendGitLog(ws, []string{"git", "fetch", "--all", "--prune", "--tags"}, run.Stdout+"\n"+run.Stderr)
	if runErr != nil || run.ExitCode != 0 {
		return gitCommandError([]string{"fetch"}, run, runErr)
	}
	now := time.Now().UTC()
	_, err = m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.Source.LastSyncAt = &now
		return nil
	})
	progress(100, "远程引用已更新")
	return err
}

// Pull fast-forwards the current branch. A dirty worktree is refused unless
// force is set.
func (m *Manager) Pull(ctx context.Context, ref string, force bool, progress func(int, string)) error {
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
	if err := m.requireGitSource(ws); err != nil {
		return err
	}
	if err := m.ensureClean(ctx, ws, force); err != nil {
		return err
	}
	git, err := m.runtime.FindBinary(ctx, "git")
	if err != nil {
		return fmt.Errorf("未找到 git: %w", err)
	}
	branch, err := m.gitOutputAt(ctx, git, m.sourcePath(ws), "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil || branch == "" {
		return fmt.Errorf("%w: 当前处于分离 HEAD，请先 checkout 一个分支", ErrInvalidRef)
	}
	progress(20, "正在拉取 "+branch)
	run, runErr := m.runGitAt(ctx, git, m.sourcePath(ws), "pull", "--ff-only")
	m.appendGitLog(ws, []string{"git", "pull", "--ff-only"}, run.Stdout+"\n"+run.Stderr)
	if runErr != nil || run.ExitCode != 0 {
		return gitCommandError([]string{"pull"}, run, runErr)
	}
	head, _ := m.gitOutputAt(ctx, git, m.sourcePath(ws), "rev-parse", "HEAD")
	now := time.Now().UTC()
	_, err = m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.Source.Branch = branch
		current.Source.Tag = ""
		current.Source.Commit = ""
		current.Source.HeadCommit = head
		current.Source.LastSyncAt = &now
		current.LastError = ""
		return nil
	})
	progress(100, "已更新到 "+shortRef(head))
	return err
}

// Checkout switches to a branch, tag, or commit. Force discards local changes
// and must be confirmed by the caller.
func (m *Manager) Checkout(ctx context.Context, ref string, input CheckoutInput, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	ref = strings.TrimSpace(ref)
	ws, unlock, err := m.lockedWorkspace(ctx, ref)
	if err != nil {
		return err
	}
	defer unlock()
	if isRunning(ws) || m.processes.Alive(ws.WorkspaceID) {
		return ErrAlreadyRunning
	}
	if err := m.requireGitSource(ws); err != nil {
		return err
	}
	target := strings.TrimSpace(input.Ref)
	if err := validateRefName(target); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidRef, target)
	}
	if err := m.ensureClean(ctx, ws, input.Force); err != nil {
		return err
	}
	git, err := m.runtime.FindBinary(ctx, "git")
	if err != nil {
		return fmt.Errorf("未找到 git: %w", err)
	}
	dir := m.sourcePath(ws)

	kind := "commit"
	resolved := target
	if !hexRefPattern.MatchString(target) {
		kind = ""
		if m.gitRevParse(ctx, git, dir, "refs/heads/"+target) {
			kind = "branch"
		} else if m.gitRevParse(ctx, git, dir, "refs/tags/"+target) {
			kind = "tag"
		} else if m.gitRevParse(ctx, git, dir, "refs/remotes/origin/"+target) {
			kind = "remote-branch"
		} else if m.gitRevParse(ctx, git, dir, target+"^{commit}") {
			kind = "commit"
		}
	} else if m.gitRevParse(ctx, git, dir, target+"^{commit}") {
		kind = "commit"
	} else {
		kind = ""
	}
	if kind == "" {
		return fmt.Errorf("%w: 找不到分支、标签或提交 %q", ErrInvalidRef, target)
	}

	args := []string{"checkout"}
	if input.Force {
		args = append(args, "--force")
	}
	switch kind {
	case "branch":
		args = append(args, target)
	case "remote-branch":
		args = append(args, "-b", target, "--track", "origin/"+target)
	case "tag":
		args = append(args, "--detach", "refs/tags/"+target)
	case "commit":
		full, err := m.gitOutput(ctx, ws, dir, "rev-parse", target+"^{commit}")
		if err == nil && full != "" {
			resolved = full
		}
		args = append(args, "--detach", resolved)
	}
	progress(30, "正在切换到 "+target)
	run, runErr := m.runGitAt(ctx, git, dir, args...)
	m.appendGitLog(ws, append([]string{"git"}, args...), run.Stdout+"\n"+run.Stderr)
	if runErr != nil || run.ExitCode != 0 {
		return gitCommandError(args, run, runErr)
	}

	head, _ := m.gitOutputAt(ctx, git, dir, "rev-parse", "HEAD")
	now := time.Now().UTC()
	_, err = m.workspaces.Mutate(ctx, ws.WorkspaceID, func(current *workspace.Workspace) error {
		switch kind {
		case "branch", "remote-branch":
			current.Source.Branch = target
			current.Source.Tag = ""
			current.Source.Commit = ""
		case "tag":
			current.Source.Branch = ""
			current.Source.Tag = target
			current.Source.Commit = ""
		default:
			current.Source.Branch = ""
			current.Source.Tag = ""
			current.Source.Commit = resolved
		}
		current.Source.HeadCommit = head
		current.Source.LastSyncAt = &now
		current.LastError = ""
		return nil
	})
	progress(100, "已切换到 "+target)
	return err
}

// --- git helpers ------------------------------------------------------------

func (m *Manager) lockedWorkspace(ctx context.Context, ref string) (*workspace.Workspace, func(), error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	unlock := m.lock(ws.WorkspaceID)
	ws, err = m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		unlock()
		return nil, nil, err
	}
	return ws, unlock, nil
}

func (m *Manager) requireGitSource(ws *workspace.Workspace) error {
	if !ws.Source.Installed {
		return ErrSourceMissing
	}
	dir := m.sourcePath(ws)
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return ErrSourceMissing
	}
	gitDir, err := os.Stat(filepath.Join(dir, ".git"))
	if err != nil || !gitDir.IsDir() {
		return ErrNotGitRepository
	}
	return nil
}

func (m *Manager) ensureClean(ctx context.Context, ws *workspace.Workspace, force bool) error {
	if force {
		return nil
	}
	raw, err := m.gitOutput(ctx, ws, m.sourcePath(ws), "status", "--porcelain=v2", "--branch")
	if err != nil {
		return err
	}
	dirty, changed, _, _ := parsePorcelainV2(raw)
	if dirty {
		return fmt.Errorf("%w: %d 个文件有修改；请提交、暂存或使用强制操作", ErrDirtyWorktree, changed)
	}
	return nil
}

func (m *Manager) runGitAt(ctx context.Context, git runtime.Found, hostDir string, args ...string) (runtime.RunResult, error) {
	execDir, err := m.runtime.TranslatePath(git.Execution, hostDir)
	if err != nil {
		return runtime.RunResult{}, err
	}
	fullArgs := append([]string{"-C", execDir}, args...)
	return m.runtime.RunFound(ctx, git, runtime.RunOptions{Args: fullArgs, Env: gitEnv})
}

func (m *Manager) gitOutput(ctx context.Context, ws *workspace.Workspace, hostDir string, args ...string) (string, error) {
	git, err := m.runtime.FindBinary(ctx, "git")
	if err != nil {
		return "", fmt.Errorf("未找到 git: %w", err)
	}
	return m.gitOutputWith(ctx, ws, git, hostDir, args...)
}

func (m *Manager) gitOutputAt(ctx context.Context, git runtime.Found, hostDir string, args ...string) (string, error) {
	return m.gitOutputWith(ctx, nil, git, hostDir, args...)
}

func (m *Manager) gitOutputWith(ctx context.Context, ws *workspace.Workspace, git runtime.Found, hostDir string, args ...string) (string, error) {
	run, err := m.runGitAt(ctx, git, hostDir, args...)
	if ws != nil {
		m.appendGitLog(ws, append([]string{"git"}, args...), run.Stdout+"\n"+run.Stderr)
	}
	if err != nil || run.ExitCode != 0 {
		return "", gitCommandError(args, run, err)
	}
	return strings.TrimSpace(run.Stdout), nil
}

func (m *Manager) gitRevParse(ctx context.Context, git runtime.Found, hostDir, ref string) bool {
	run, err := m.runGitAt(ctx, git, hostDir, "rev-parse", "--verify", "--quiet", ref)
	return err == nil && run.ExitCode == 0 && strings.TrimSpace(run.Stdout) != ""
}

func gitCommandError(args []string, run runtime.RunResult, err error) error {
	message := strings.TrimSpace(run.Stderr)
	if message == "" {
		message = strings.TrimSpace(run.Stdout)
	}
	if message == "" && err != nil {
		message = err.Error()
	}
	message = logging.Redact(message)
	if len(message) > 2000 {
		message = message[len(message)-2000:]
	}
	if message == "" {
		message = fmt.Sprintf("退出码 %d", run.ExitCode)
	}
	return fmt.Errorf("git %s 失败: %s", strings.Join(args, " "), message)
}

func (m *Manager) appendGitLog(ws *workspace.Workspace, args []string, output string) {
	if ws == nil {
		return
	}
	path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "logs", "git.log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = file.Close() }()
	command := logging.Redact("$ git " + strings.Join(args, " "))
	output = logging.Redact(output)
	if len(output) > maxGitLogBytes {
		output = output[len(output)-maxGitLogBytes:]
	}
	_, _ = fmt.Fprintf(file, "[%s] %s\n%s\n", time.Now().UTC().Format(time.RFC3339), command, output)
}

func shortRef(value string) string {
	if len(value) > 12 {
		return value[:12]
	}
	return value
}
