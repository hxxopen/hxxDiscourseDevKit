//go:build !windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// startDetached starts a long-lived child in its own session so it survives
// the daemon process.
func startDetached(command string, args, env []string, logPath string) (int, error) {
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, fmt.Errorf("打开服务日志: %w", err)
	}
	defer func() { _ = logFile.Close() }()
	cmd := exec.Command(command, args...)
	cmd.Env = env
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Release(); err != nil {
		return pid, err
	}
	return pid, nil
}
