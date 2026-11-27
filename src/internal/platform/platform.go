// Package platform exposes host detection and process execution primitives.
package platform

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Info describes the host platform.
type Info struct {
	OS            string `json:"os"`
	Arch          string `json:"arch"`
	ArchFamily    string `json:"arch_family"`
	Distro        string `json:"distro,omitempty"`
	DistroVersion string `json:"distro_version,omitempty"`
	IsWSL         bool   `json:"is_wsl"`
}

// Detect reports basic platform details.
func Detect() Info {
	info := Info{
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		ArchFamily: archFamily(runtime.GOARCH),
	}
	if runtime.GOOS == "linux" {
		info.Distro, info.DistroVersion = readOSRelease()
		if _, ok := os.LookupEnv("WSL_DISTRO_NAME"); ok {
			info.IsWSL = true
		} else if data, err := os.ReadFile("/proc/version"); err == nil {
			info.IsWSL = strings.Contains(strings.ToLower(string(data)), "microsoft")
		}
	}
	return info
}

// ExecSpec is one process invocation. Commands are always passed as argv;
// DevKit never invokes a shell for documented operations.
type ExecSpec struct {
	Path   string
	Args   []string
	Dir    string
	Env    []string
	Stdin  []byte
	Stdout *bytes.Buffer
	Stderr *bytes.Buffer
}

// ExecResult is a completed process result.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Executor runs host commands.
type Executor struct{}

// Run executes a command and waits for it to finish.
func (Executor) Run(ctx context.Context, spec ExecSpec) (ExecResult, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	HideWindow(cmd)
	cmd.Dir = spec.Dir
	if spec.Env != nil {
		cmd.Env = spec.Env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if spec.Stdout != nil {
		cmd.Stdout = spec.Stdout
	}
	if spec.Stderr != nil {
		cmd.Stderr = spec.Stderr
	}
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}
	err := cmd.Run()
	result := ExecResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return result, fmt.Errorf("run %s: %w", spec.Path, err)
	}
	return result, nil
}

// Which resolves an executable using the current PATH.
func (Executor) Which(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found in PATH: %w", name, err)
	}
	return path, nil
}

// OpenBrowser opens url with the platform's default handler.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func archFamily(arch string) string {
	switch arch {
	case "amd64", "386":
		return "x86"
	case "arm64", "arm":
		return "arm"
	case "loong64":
		return "loongarch"
	case "riscv64":
		return "riscv"
	case "mips64", "mips64le", "mips", "mipsle":
		return "mips"
	default:
		return arch
	}
}

func readOSRelease() (string, string) {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "", ""
	}
	var name, version string
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "NAME":
			name = value
		case "VERSION_ID":
			version = value
		}
	}
	return name, version
}
