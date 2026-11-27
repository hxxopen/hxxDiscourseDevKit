package runtime

import (
	"context"
	"fmt"
	"os"
)

// EnsureDir creates hostPath in the execution environment with the requested
// permissions. Windows directories created through \\wsl.localhost do not
// preserve Unix modes, so WSL directories are created and chmod-ed inside the
// distribution instead.
func (m *Manager) EnsureDir(ctx context.Context, execution, hostPath string, mode os.FileMode) error {
	if execution == "" || execution == ExecutionNative {
		return os.MkdirAll(hostPath, mode)
	}
	execPath, err := m.TranslatePath(execution, hostPath)
	if err != nil {
		return err
	}
	perm := fmt.Sprintf("%04o", mode.Perm())
	mkdir := Found{Binary: "mkdir", Path: "mkdir", Execution: execution}
	if _, err := m.RunFound(ctx, mkdir, RunOptions{Args: []string{"-p", "-m", perm, execPath}}); err != nil {
		return fmt.Errorf("create directory %s: %w", execPath, err)
	}
	chmod := Found{Binary: "chmod", Path: "chmod", Execution: execution}
	if _, err := m.RunFound(ctx, chmod, RunOptions{Args: []string{perm, execPath}}); err != nil {
		return fmt.Errorf("set permissions on %s: %w", execPath, err)
	}
	return nil
}
