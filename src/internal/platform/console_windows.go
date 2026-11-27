//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// HideWindow prevents a console window from appearing when DevKit starts a
// console child process. The daemon itself is detached (DETACHED_PROCESS), so
// without this flag every wsl.exe probe allocates a new visible console.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
