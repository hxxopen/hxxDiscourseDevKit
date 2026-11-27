//go:build windows

package platform

import (
	"os/exec"
	"syscall"
	"testing"
)

func TestHideWindowSetsConsoleFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	HideWindow(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow = false, want true")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x, missing CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
}

func TestHideWindowPreservesExistingFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	HideWindow(cmd)
	if cmd.SysProcAttr.CreationFlags&0x00000200 == 0 {
		t.Error("existing CREATE_NEW_PROCESS_GROUP flag was cleared")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Error("CREATE_NO_WINDOW was not added")
	}
}
