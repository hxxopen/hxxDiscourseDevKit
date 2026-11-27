//go:build windows

package secret

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
	"golang.org/x/sys/windows"
)

const wslChmodTimeout = 5 * time.Second

// protectFile keeps a secrets file readable only by the current user. Files
// under a WSL distribution live on DrvFs, which does not implement Windows
// security descriptors (SetNamedSecurityInfo returns ERROR_INVALID_FUNCTION),
// so those files are protected with a Unix mode inside the distribution
// instead.
func protectFile(path string) error {
	if distro, linuxPath, ok := wslHostPath(path); ok {
		return chmodWSLFile(distro, linuxPath)
	}
	token, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return fmt.Errorf("open process token: %w", err)
	}
	defer token.Close()

	user, err := token.GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}
	if user.User.Sid == nil {
		return fmt.Errorf("current user has no SID")
	}

	sddl := fmt.Sprintf("D:P(A;;FA;;;%s)", user.User.Sid.String())
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build security descriptor: %w", err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read security descriptor DACL: %w", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("set file DACL: %w", err)
	}
	return nil
}

// wslHostPath recognizes \\wsl.localhost\<distro>\... and \\wsl$\<distro>\...
// paths and returns the Linux path inside that distribution.
func wslHostPath(hostPath string) (distro, linuxPath string, ok bool) {
	cleaned := filepath.Clean(hostPath)
	lower := strings.ToLower(cleaned)
	for _, prefix := range []string{`\\wsl.localhost\`, `\\wsl$\`} {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		rest := cleaned[len(prefix):]
		parts := strings.SplitN(rest, `\`, 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", false
		}
		return parts[0], "/" + strings.ReplaceAll(parts[1], `\`, "/"), true
	}
	return "", "", false
}

// chmodWSLFile applies the equivalent of mode 0600 to a file inside a WSL
// distribution. It is used because Windows DACLs cannot be set on DrvFs.
func chmodWSLFile(distro, linuxPath string) error {
	ctx, cancel := context.WithTimeout(context.Background(), wslChmodTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "chmod", "600", linuxPath)
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(strings.ReplaceAll(string(output), "\x00", ""))
		if detail != "" {
			return fmt.Errorf("在 WSL(%s) 中设置文件权限 %s: %w: %s", distro, linuxPath, err, detail)
		}
		return fmt.Errorf("在 WSL(%s) 中设置文件权限 %s: %w", distro, linuxPath, err)
	}
	return nil
}
