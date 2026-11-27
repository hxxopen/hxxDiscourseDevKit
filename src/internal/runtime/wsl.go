package runtime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	goruntime "runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
)

// WSLStatus describes the Windows Subsystem for Linux environment used by
// DevKit to execute runtime components and managed services.
type WSLStatus struct {
	Supported     bool      `json:"supported"`
	Available     bool      `json:"available"`
	Distro        string    `json:"distro,omitempty"`
	Distros       []string  `json:"distros,omitempty"`
	Kernel        string    `json:"kernel,omitempty"`
	OSName        string    `json:"os_name,omitempty"`
	OSID          string    `json:"os_id,omitempty"`
	OSLike        string    `json:"os_like,omitempty"`
	OSVersion     string    `json:"os_version,omitempty"`
	User          string    `json:"user,omitempty"`
	Home          string    `json:"home,omitempty"`
	RootAvailable bool      `json:"root_available"`
	Error         string    `json:"error,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

const (
	wslListTimeout  = 10 * time.Second
	wslProbeTimeout = 12 * time.Second
)

// WSLStatus probes the configured (or first) WSL distribution. Results are
// cached briefly so the UI can poll without repeatedly starting WSL.
func (m *Manager) WSLStatus(ctx context.Context) WSLStatus {
	m.mu.Lock()
	age := time.Since(m.wslStatusAt)
	cached := m.wslStatus
	m.mu.Unlock()
	if !cached.CheckedAt.IsZero() {
		ttl := 30 * time.Second
		if !cached.Available {
			ttl = 5 * time.Second
		}
		if age < ttl {
			return cached
		}
	}

	status := m.probeWSLStatus(ctx)

	m.mu.Lock()
	m.wslStatus = status
	m.wslStatusAt = time.Now()
	m.mu.Unlock()
	return status
}

func (m *Manager) probeWSLStatus(ctx context.Context) WSLStatus {
	status := WSLStatus{CheckedAt: time.Now().UTC()}
	if goruntime.GOOS != "windows" {
		status.Error = "当前平台不使用 WSL2"
		return status
	}
	status.Supported = true
	if _, err := exec.LookPath("wsl.exe"); err != nil {
		status.Error = "未找到 wsl.exe"
		return status
	}

	distro := strings.TrimSpace(m.cfg.Get().Runtime.WSLDistro)
	if distro == "" {
		distros, err := listWSLDistrosDetailed(ctx)
		status.Distros = distros
		if err != nil {
			status.Error = "无法读取 WSL 发行版列表: " + err.Error()
			return status
		}
		if len(distros) == 0 {
			status.Error = "未检测到已安装的 WSL2 发行版"
			return status
		}
		distro = distros[0]
	}
	status.Distro = distro

	kernel, err := runWSL(ctx, distro, false, "uname", "-r")
	if err != nil {
		status.Error = "WSL 发行版不可用: " + wslErrorText(err)
		return status
	}
	status.Kernel = wslScalar(kernel)

	if osRelease, err := runWSL(ctx, distro, false, "cat", "/etc/os-release"); err == nil {
		fields := parseOSRelease(osRelease)
		status.OSName = fields["PRETTY_NAME"]
		status.OSID = strings.ToLower(fields["ID"])
		status.OSLike = strings.ToLower(fields["ID_LIKE"])
		status.OSVersion = fields["VERSION_ID"]
	}
	if user, err := runWSL(ctx, distro, false, "id", "-un"); err == nil {
		status.User = wslScalar(user)
	}
	if home, err := runWSL(ctx, distro, false, "printenv", "HOME"); err == nil {
		status.Home = wslScalar(home)
	}
	if uid, err := runWSL(ctx, distro, true, "id", "-u"); err == nil {
		status.RootAvailable = wslScalar(uid) == "0"
	}
	status.Available = true
	return status
}

// DefaultDataRoot returns a Windows-accessible UNC path inside the configured
// WSL distribution for DevKit workspace and service data. Data lives in a
// visible "hxxwork" directory under the WSL user's home rather than a hidden
// XDG-style path, so users can find and manage it directly.
func (m *Manager) DefaultDataRoot(ctx context.Context) (string, error) {
	status := m.WSLStatus(ctx)
	if !status.Available {
		if status.Error != "" {
			return "", fmt.Errorf("%s", status.Error)
		}
		return "", fmt.Errorf("WSL2 不可用")
	}
	linuxPath := WSLDataRoot(status.Home, status.User)
	if linuxPath == "" {
		return "", fmt.Errorf("无法确定 WSL 用户主目录")
	}
	return WSLUNCPath(status.Distro, linuxPath), nil
}

// WSLDataRoot returns the Linux path used for DevKit data inside WSL. It is
// kept separate from DefaultDataRoot so the layout can be unit tested without
// probing a live distribution.
func WSLDataRoot(home, user string) string {
	home = strings.TrimRight(strings.TrimSpace(home), "/")
	if home == "" && strings.TrimSpace(user) != "" {
		home = "/home/" + strings.TrimSpace(user)
	}
	if home == "" || home == "/home" {
		return ""
	}
	// A contaminated probe (for example a wsl.exe diagnostic line mixed into
	// the output) must never become a data path.
	if !strings.HasPrefix(home, "/") || strings.ContainsAny(home, "\x00\r\n") {
		return ""
	}
	return home + "/hxxwork"
}

// WSLUNCPath maps a Linux absolute path inside distro to the Windows
// \\wsl.localhost UNC form. The existing ToWSLPath helper maps it back.
func WSLUNCPath(distro, linuxPath string) string {
	cleaned := "/" + strings.TrimLeft(strings.ReplaceAll(linuxPath, "\\", "/"), "/")
	return `\\wsl.localhost\` + distro + `\` + strings.ReplaceAll(strings.TrimPrefix(cleaned, "/"), "/", `\`)
}

func listWSLDistrosDetailed(ctx context.Context) ([]string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, wslListTimeout)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "wsl.exe", "-l", "-q")
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	output, err := cmd.Output()
	if err != nil {
		if probeCtx.Err() != nil {
			return nil, fmt.Errorf("wsl.exe 在 %s 内未响应", wslListTimeout)
		}
		text := trimWSLOutput(string(output))
		if text != "" {
			return nil, fmt.Errorf("%v: %s", err, text)
		}
		return nil, err
	}
	return parseDistroList(trimWSLOutput(string(output))), nil
}

func parseDistroList(output string) []string {
	seen := map[string]bool{}
	var distros []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "*"))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "wsl:") ||
			strings.Contains(lower, "windows subsystem for linux") {
			continue
		}
		if seen[line] {
			continue
		}
		seen[line] = true
		distros = append(distros, line)
	}
	return distros
}

func parseOSRelease(output string) map[string]string {
	fields := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' ||
			value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		fields[key] = value
	}
	return fields
}

func runWSL(ctx context.Context, distro string, root bool, args ...string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, wslProbeTimeout)
	defer cancel()
	base := []string{"-d", distro}
	if root {
		base = append(base, "-u", "root")
	}
	base = append(base, "--")
	base = append(base, args...)
	cmd := exec.CommandContext(probeCtx, "wsl.exe", base...)
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	// Keep the streams separate: wsl.exe emits its own diagnostics (such as
	// the NAT localhost-proxy notice) on stderr, and merging them into stdout
	// made HOME/User/UID probes return the warning text as their value.
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	text := trimWSLOutput(stdout.String())
	if err != nil {
		if probeCtx.Err() != nil {
			return text, fmt.Errorf("wsl.exe 在 %s 内未响应", wslProbeTimeout)
		}
		detail := stripWSLProxyNoise(stderr.String())
		if detail == "" {
			detail = text
		}
		if detail != "" {
			return text, fmt.Errorf("%v: %s", err, detail)
		}
		return text, err
	}
	return text, nil
}

// wslScalar returns the last non-empty line of single-value command output.
// wsl.exe diagnostics are filtered before this point; taking the last line
// also tolerates any banner printed ahead of the actual value.
func wslScalar(value string) string {
	lines := strings.Split(value, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}

func wslErrorText(err error) string {
	text := strings.TrimSpace(err.Error())
	if text == "" {
		return "未知错误"
	}
	return text
}

// sortPostgresCandidates orders pg_ctl paths so the highest major version is
// preferred. Paths typically look like /usr/lib/postgresql/16/bin/pg_ctl.
func sortPostgresCandidates(paths []string) []string {
	sorted := append([]string(nil), paths...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return postgresPathVersion(sorted[i]) > postgresPathVersion(sorted[j])
	})
	return sorted
}

func postgresPathVersion(value string) int {
	for _, part := range strings.Split(value, "/") {
		if part == "" || strings.Contains(part, ".") {
			continue
		}
		if number, err := strconv.Atoi(part); err == nil {
			return number
		}
	}
	return 0
}
