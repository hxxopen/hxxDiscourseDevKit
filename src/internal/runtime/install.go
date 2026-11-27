package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
)

const (
	installTokenTTL = 10 * time.Minute
	installLogName  = "runtime-install.log"
)

// Errors reported by the WSL runtime installer.
var (
	ErrWSLUnsupported    = errors.New("当前平台不支持 WSL2 运行时安装")
	ErrWSLUnavailable    = errors.New("WSL2 不可用")
	ErrUnsupportedDistro = errors.New("自动安装仅支持 Debian/Ubuntu 发行版")
	ErrInstallConfirm    = errors.New("安装确认令牌无效或已过期")
)

// InstallComponent is one component that can be installed into WSL.
type InstallComponent struct {
	Name      string `json:"name"`
	Package   string `json:"package"`
	Required  bool   `json:"required"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Execution string `json:"execution,omitempty"`
	Message   string `json:"message,omitempty"`
}

// InstallCommand is one command shown in the preview and executed on install.
type InstallCommand struct {
	Label   string   `json:"label"`
	Preview string   `json:"preview"`
	Args    []string `json:"args,omitempty"`
}

// InstallPlan is the preview of a WSL runtime installation.
type InstallPlan struct {
	Supported     bool               `json:"supported"`
	Available     bool               `json:"available"`
	Distro        string             `json:"distro,omitempty"`
	OSName        string             `json:"os_name,omitempty"`
	OSID          string             `json:"os_id,omitempty"`
	OSVersion     string             `json:"os_version,omitempty"`
	RootAvailable bool               `json:"root_available"`
	Components    []InstallComponent `json:"components"`
	Packages      []string           `json:"packages"`
	Commands      []InstallCommand   `json:"commands"`
	Warnings      []string           `json:"warnings,omitempty"`
	ConfirmToken  string             `json:"confirm_token,omitempty"`
	ExpiresAt     time.Time          `json:"expires_at,omitempty"`
	CheckedAt     time.Time          `json:"checked_at"`
}

type installToken struct {
	distro     string
	packages   []string
	components []string
	expiresAt  time.Time
}

type installComponentSpec struct {
	name       string
	pkg        string
	defaultFor bool
}

var installComponents = []installComponentSpec{
	{name: "postgresql", pkg: "postgresql", defaultFor: true},
	{name: "redis", pkg: "redis-server", defaultFor: true},
	{name: "git", pkg: "git", defaultFor: true},
}

// InstallLogPath is the log file used by the WSL runtime installer.
func (m *Manager) InstallLogPath() string {
	return filepath.Join(m.paths.LogsDir, installLogName)
}

// PreviewInstall builds the apt command plan and issues a confirmation token.
func (m *Manager) PreviewInstall(ctx context.Context, requested []string) (InstallPlan, error) {
	plan := InstallPlan{CheckedAt: time.Now().UTC()}
	if goruntime.GOOS != "windows" {
		return plan, ErrWSLUnsupported
	}
	status := m.WSLStatus(ctx)
	plan.Supported = status.Supported
	plan.Available = status.Available
	plan.Distro = status.Distro
	plan.OSName = status.OSName
	plan.OSID = status.OSID
	plan.OSVersion = status.OSVersion
	plan.RootAvailable = status.RootAvailable
	if !status.Supported {
		return plan, ErrWSLUnsupported
	}
	if !status.Available {
		message := strings.TrimSpace(status.Error)
		if message == "" {
			message = "未检测到可用的 WSL2 发行版"
		}
		return plan, fmt.Errorf("%w: %s", ErrWSLUnavailable, message)
	}
	if !isDebianLike(status) {
		return plan, fmt.Errorf("%w: %s", ErrUnsupportedDistro, strings.TrimSpace(status.OSName+" "+status.OSID))
	}

	names, err := normalizeInstallComponents(requested)
	if err != nil {
		return plan, err
	}
	matrix, _, err := m.Matrix()
	if err != nil {
		return plan, err
	}

	var packages []string
	var missing []string
	for _, name := range names {
		spec := findInstallComponent(name)
		if spec == nil {
			continue
		}
		component := InstallComponent{
			Name:     spec.name,
			Package:  spec.pkg,
			Required: matrix.IsRequired(spec.name),
		}
		if found, err := m.FindComponent(ctx, spec.name); err == nil {
			component.Installed = true
			component.Execution = found.Execution
			component.Version = m.componentVersion(ctx, spec.name, found)
			component.Message = "已可用"
		} else {
			component.Message = "未安装"
			packages = append(packages, spec.pkg)
			missing = append(missing, spec.name)
		}
		plan.Components = append(plan.Components, component)
	}
	plan.Packages = packages

	if !plan.RootAvailable {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"无法在 WSL 发行版 %s 中以 root 执行命令；自动安装依赖需要 root 权限。"+
				"请在 Windows 终端运行 `wsl.exe -d %s -u root -- id -u` 验证，"+
				"也可在 WSL 内用 sudo 手工安装后重新检测", status.Distro, status.Distro))
	}
	if len(packages) == 0 {
		plan.Warnings = append(plan.Warnings, "所选组件均已可用，无需安装")
		return plan, nil
	}

	if _, err := exec.LookPath("wsl.exe"); err != nil {
		return plan, fmt.Errorf("%w: 未找到 wsl.exe", ErrWSLUnavailable)
	}
	plan.Commands = []InstallCommand{
		{
			Label:   "更新软件包索引",
			Preview: wslPreview(status.Distro, "apt-get update"),
			Args:    []string{"-d", status.Distro, "-u", "root", "--", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update"},
		},
		{
			Label:   "安装 " + strings.Join(packages, " "),
			Preview: wslPreview(status.Distro, "apt-get install -y --no-install-recommends "+strings.Join(packages, " ")),
			Args: append([]string{"-d", status.Distro, "-u", "root", "--", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends"},
				packages...),
		},
	}

	token := id.MustNew()
	expires := time.Now().UTC().Add(installTokenTTL)
	m.mu.Lock()
	if m.installTokens == nil {
		m.installTokens = map[string]installToken{}
	}
	for key, value := range m.installTokens {
		if time.Now().After(value.expiresAt) {
			delete(m.installTokens, key)
		}
	}
	m.installTokens[token] = installToken{
		distro:     status.Distro,
		packages:   append([]string(nil), packages...),
		components: missing,
		expiresAt:  expires,
	}
	m.mu.Unlock()

	plan.ConfirmToken = token
	plan.ExpiresAt = expires
	return plan, nil
}

// Install runs the confirmed apt commands and refreshes component discovery.
func (m *Manager) Install(ctx context.Context, confirmToken string, progress func(int, string)) error {
	if progress == nil {
		progress = func(int, string) {}
	}
	m.mu.Lock()
	token, ok := m.installTokens[confirmToken]
	if ok {
		delete(m.installTokens, confirmToken)
	}
	m.mu.Unlock()
	if !ok || token.expiresAt.Before(time.Now()) {
		return ErrInstallConfirm
	}
	if len(token.packages) == 0 {
		return fmt.Errorf("没有需要安装的组件")
	}
	wslPath, err := exec.LookPath("wsl.exe")
	if err != nil {
		return fmt.Errorf("%w: 未找到 wsl.exe", ErrWSLUnavailable)
	}

	logPath := m.InstallLogPath()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("打开安装日志: %w", err)
	}
	writer := newLineLogWriter(file)
	defer func() { _ = writer.Close() }()
	_, _ = fmt.Fprintf(writer.file, "\n[%s] 安装运行时组件: %s (%s)\n",
		time.Now().Format(time.RFC3339), token.distro, strings.Join(token.packages, " "))

	progress(5, "准备安装运行时组件")
	steps := []struct {
		label string
		args  []string
	}{
		{
			label: "更新软件包索引",
			args:  []string{"-d", token.distro, "-u", "root", "--", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update"},
		},
		{
			label: "安装 " + strings.Join(token.packages, " "),
			args: append([]string{"-d", token.distro, "-u", "root", "--", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y", "--no-install-recommends"},
				token.packages...),
		},
	}
	for index, step := range steps {
		progress(15+index*35, step.label)
		_, _ = fmt.Fprintf(writer.file, "[%s] %s\n", time.Now().Format("15:04:05"), step.label)
		cmd := exec.CommandContext(ctx, wslPath, step.args...)
		platform.HideWindow(cmd)
		cmd.Env = append(os.Environ(), "WSL_UTF8=1")
		cmd.Stdout = writer
		cmd.Stderr = writer
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("%s失败: %w（详见日志 %s）", step.label, err, logPath)
		}
	}

	progress(85, "探测组件路径")
	discoverCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	discovery, discoverErr := m.Discover(discoverCtx)
	if discoverErr != nil {
		return discoverErr
	}
	var missing []string
	for _, name := range token.components {
		if _, ok := discovery.Components[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("安装命令已完成，但仍未探测到组件: %s（详见日志 %s）",
			strings.Join(missing, "、"), logPath)
	}
	progress(100, "运行时组件安装完成")
	return nil
}

func (m *Manager) componentVersion(ctx context.Context, name string, found Found) string {
	matrix, _, err := m.Matrix()
	if err != nil {
		return ""
	}
	spec, ok := matrix.Components[name]
	if !ok {
		return ""
	}
	version, err := m.runVersion(ctx, found, spec)
	if err != nil {
		return ""
	}
	return version
}

func normalizeInstallComponents(requested []string) ([]string, error) {
	if len(requested) == 0 {
		names := make([]string, 0, len(installComponents))
		for _, item := range installComponents {
			if item.defaultFor {
				names = append(names, item.name)
			}
		}
		return names, nil
	}
	seen := map[string]bool{}
	var names []string
	for _, raw := range requested {
		name := strings.ToLower(strings.TrimSpace(raw))
		if name == "" {
			continue
		}
		if findInstallComponent(name) == nil {
			return nil, fmt.Errorf("不支持通过 WSL 安装组件 %q", raw)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("至少选择一个组件")
	}
	return names, nil
}

func findInstallComponent(name string) *installComponentSpec {
	for index := range installComponents {
		if installComponents[index].name == name {
			return &installComponents[index]
		}
	}
	return nil
}

func isDebianLike(status WSLStatus) bool {
	id := strings.ToLower(strings.TrimSpace(status.OSID))
	switch id {
	case "debian", "ubuntu", "linuxmint", "pop", "elementary", "zorin":
		return true
	}
	return strings.Contains(strings.ToLower(status.OSLike), "debian") ||
		strings.Contains(strings.ToLower(status.OSLike), "ubuntu")
}

func wslPreview(distro, command string) string {
	return "wsl.exe -d " + distro + " -u root -- env DEBIAN_FRONTEND=noninteractive " + command
}

type lineLogWriter struct {
	mu   sync.Mutex
	file *os.File
	buf  []byte
}

func newLineLogWriter(file *os.File) *lineLogWriter {
	return &lineLogWriter{file: file}
}

func (w *lineLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		index := bytes.IndexByte(w.buf, '\n')
		if index < 0 {
			break
		}
		line := string(w.buf[:index])
		w.buf = w.buf[index+1:]
		w.writeLine(line)
	}
	return len(p), nil
}

func (w *lineLogWriter) writeLine(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	_, _ = fmt.Fprintf(w.file, "[%s] %s\n", time.Now().Format("15:04:05"), logging.Redact(line))
}

func (w *lineLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) > 0 {
		w.writeLine(string(w.buf))
		w.buf = nil
	}
	return w.file.Close()
}
