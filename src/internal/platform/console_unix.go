//go:build !windows

package platform

import "os/exec"

// HideWindow is a no-op on platforms without a separate console window.
func HideWindow(cmd *exec.Cmd) {}
