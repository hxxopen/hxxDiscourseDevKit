package app

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

var gitCommitPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

var profileGitEnv = []string{
	"GIT_TERMINAL_PROMPT=0",
	"GIT_PAGER=cat",
	"LC_ALL=C",
}

// profileGitFetcher clones a template source repository with the same Runtime
// resolution (host or WSL) used by project source operations. It only fetches
// data; DevKit never executes scripts from a profile repository.
type profileGitFetcher struct {
	runtime *runtime.Manager
}

func (f profileGitFetcher) Fetch(ctx context.Context, repoURL, branch, destDir string) (string, error) {
	git, err := f.runtime.FindBinary(ctx, "git")
	if err != nil {
		return "", fmt.Errorf("未找到 git: %w", err)
	}
	execDest, err := f.runtime.TranslatePath(git.Execution, destDir)
	if err != nil {
		return "", err
	}

	args := []string{"clone", "--depth", "1", "--single-branch"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "--branch", branch)
	}
	args = append(args, "--", repoURL, execDest)
	run, runErr := f.runtime.RunFound(ctx, git, runtime.RunOptions{Args: args, Env: profileGitEnv})
	if runErr != nil || run.ExitCode != 0 {
		detail := strings.TrimSpace(run.Stderr)
		if detail == "" {
			detail = strings.TrimSpace(run.Stdout)
		}
		if len(detail) > 4096 {
			detail = detail[:4096]
		}
		if detail == "" {
			detail = "git clone 失败"
		}
		if runErr != nil {
			return "", fmt.Errorf("%s: %w", detail, runErr)
		}
		return "", fmt.Errorf("%s", detail)
	}

	run, runErr = f.runtime.RunFound(ctx, git, runtime.RunOptions{
		Args: []string{"rev-parse", "HEAD"},
		Env:  profileGitEnv,
		Dir:  destDir,
	})
	if runErr != nil || run.ExitCode != 0 {
		return "", fmt.Errorf("读取来源提交失败: %s", strings.TrimSpace(run.Stderr))
	}
	commit := strings.TrimSpace(run.Stdout)
	if !gitCommitPattern.MatchString(commit) {
		return "", fmt.Errorf("来源提交 %q 无效", commit)
	}
	return commit, nil
}
