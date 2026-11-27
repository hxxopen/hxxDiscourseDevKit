//go:build windows

package cli

import (
	"os"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}
