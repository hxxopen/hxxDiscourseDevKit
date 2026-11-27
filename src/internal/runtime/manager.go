package runtime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
)

const (
	ExecutionNative = "native"
	executionWSL    = "wsl:"
)

// IsWSL reports whether an execution target runs inside a WSL distribution.
func IsWSL(execution string) bool {
	return strings.HasPrefix(execution, executionWSL)
}

// WSLExecution returns the execution identifier for a WSL distribution.
func WSLExecution(distro string) string {
	return executionWSL + strings.TrimSpace(distro)
}

// ErrUnknownComponent is returned when a component is not in the matrix.
var ErrUnknownComponent = errors.New("runtime component not found")

// Found describes a resolved executable.
type Found struct {
	Binary    string `json:"binary"`
	Path      string `json:"path"`
	Execution string `json:"execution"`
}

// ComponentStatus is the verification result for one component.
type ComponentStatus struct {
	Name              string    `json:"name"`
	Required          bool      `json:"required"`
	ExpectedVersion   string    `json:"expected_version"`
	Source            string    `json:"source"`
	ActualVersion     string    `json:"actual_version,omitempty"`
	Path              string    `json:"path,omitempty"`
	Execution         string    `json:"execution,omitempty"`
	Available         bool      `json:"available"`
	VersionCompatible bool      `json:"version_compatible"`
	SHA256            string    `json:"sha256,omitempty"`
	HashVerified      bool      `json:"hash_verified"`
	Verified          bool      `json:"verified"`
	Message           string    `json:"message,omitempty"`
	CheckedAt         time.Time `json:"checked_at"`
}

// RunOptions controls one command invocation. Dir is a host path; it is
// translated automatically when the command executes through WSL.
type RunOptions struct {
	Args  []string
	Env   []string
	Stdin []byte
	Dir   string
}

// RunResult is the outcome of one invocation. ExitCode is meaningful even
// when err reports a non-zero exit.
type RunResult struct {
	Found    Found
	Stdout   string
	Stderr   string
	ExitCode int
}

// Command is a fully resolved invocation, including the WSL bridge prefix.
// Dir is set for native commands only; WSL commands carry their working
// directory in Args (--cd).
type Command struct {
	Found Found
	Path  string
	Args  []string
	Env   []string
	Dir   string
}

// Manager loads the matrix and resolves host/WSL executables.
type Manager struct {
	paths *config.Paths
	cfg   *config.Store
	log   *slog.Logger

	mu            sync.Mutex
	distros       []string
	distrosAt     time.Time
	wslStatus     WSLStatus
	wslStatusAt   time.Time
	installTokens map[string]installToken
}

// New creates a runtime manager.
func New(paths *config.Paths, cfg *config.Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{paths: paths, cfg: cfg, log: logger}
}

// MatrixPath returns the absolute path of the configured matrix file.
func (m *Manager) MatrixPath() string {
	file := m.cfg.Get().Runtime.MatrixFile
	if file == "" {
		file = "config/runtime-matrix.json"
	}
	if filepath.IsAbs(file) {
		return filepath.Clean(file)
	}
	return filepath.Join(m.paths.Root, file)
}

// Matrix loads the configured matrix, falling back to the built-in default.
func (m *Manager) Matrix() (Matrix, bool, error) {
	return LoadMatrix(m.MatrixPath())
}

// List verifies every component in the matrix.
func (m *Manager) List(ctx context.Context) ([]ComponentStatus, error) {
	matrix, _, err := m.Matrix()
	if err != nil {
		return nil, err
	}
	names := matrix.Names()
	result := make([]ComponentStatus, 0, len(names))
	for _, name := range names {
		result = append(result, m.verify(ctx, matrix, name, matrix.Components[name]))
	}
	return result, nil
}

// Verify checks one component.
func (m *Manager) Verify(ctx context.Context, name string) (ComponentStatus, error) {
	matrix, _, err := m.Matrix()
	if err != nil {
		return ComponentStatus{}, err
	}
	spec, ok := matrix.Components[name]
	if !ok {
		return ComponentStatus{}, fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	return m.verify(ctx, matrix, name, spec), nil
}

func (m *Manager) verify(ctx context.Context, matrix Matrix, name string, spec ComponentSpec) ComponentStatus {
	status := ComponentStatus{
		Name:            name,
		Required:        matrix.IsRequired(name),
		ExpectedVersion: spec.Version,
		Source:          spec.Source,
		SHA256:          spec.SHA256,
		CheckedAt:       time.Now().UTC(),
	}
	found, err := m.findComponent(ctx, name, spec)
	if err != nil {
		// Prefer the specific PATH/WSL lookup message over the generic
		// "component is not available" from resolution.
		if _, detail := m.findSpecBinary(ctx, spec); detail != nil {
			err = detail
		}
		status.Message = err.Error()
		return status
	}
	status.Path = found.Path
	status.Execution = found.Execution
	status.Available = true

	runCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result, runErr := m.RunFound(runCtx, found, RunOptions{Args: spec.VersionArgs})
	output := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if output == "" && runErr != nil {
		status.Message = fmt.Sprintf("无法读取版本: %v", runErr)
		return status
	}
	status.ActualVersion = ExtractVersion(output)
	if status.ActualVersion == "" {
		status.Message = "无法从命令输出解析版本"
		return status
	}
	compatible, cmpErr := Compatible(status.ActualVersion, spec.Version)
	if cmpErr != nil {
		status.Message = cmpErr.Error()
		return status
	}
	status.VersionCompatible = compatible

	hashOK := true
	if spec.SHA256 != "" {
		actualHash, hashErr := m.hashExecutable(runCtx, found)
		if hashErr != nil {
			status.Message = "计算 SHA-256 失败: " + hashErr.Error()
			hashOK = false
		} else if !strings.EqualFold(actualHash, spec.SHA256) {
			status.Message = "SHA-256 与版本矩阵不匹配"
			hashOK = false
		} else {
			status.HashVerified = true
		}
	}
	if !compatible {
		status.Message = fmt.Sprintf("版本不匹配：需要 %s，实际 %s", spec.Version, status.ActualVersion)
	} else if !hashOK {
		// Message already set above.
	} else {
		status.Verified = true
	}
	return status
}

func (m *Manager) findSpecBinary(ctx context.Context, spec ComponentSpec) (Found, error) {
	binary := spec.Binary
	if binary == "" {
		return Found{}, fmt.Errorf("组件未配置可执行文件")
	}
	if filepath.IsAbs(binary) {
		if goruntime.GOOS == "windows" && strings.HasPrefix(binary, "/") {
			distro := m.WSLDistro(ctx)
			if distro == "" {
				return Found{}, fmt.Errorf("%s 是 Linux 路径，但没有可用的 WSL2 发行版", binary)
			}
			found := Found{Binary: path.Base(binary), Path: binary, Execution: executionWSL + distro}
			if m.Exists(ctx, found, binary) {
				return found, nil
			}
			return Found{}, fmt.Errorf("%s 不存在", binary)
		}
		found := Found{Binary: filepath.Base(binary), Path: filepath.Clean(binary), Execution: ExecutionNative}
		if m.Exists(ctx, found, found.Path) {
			return found, nil
		}
		return Found{}, fmt.Errorf("%s 不存在", binary)
	}
	return m.FindBinary(ctx, binary)
}

// FindBinary resolves an executable for the Discourse runtime. On Windows the
// runtime lives in WSL2, so WSL is authoritative: a native Windows node/npm
// cannot operate on a WSL workspace (cmd.exe rejects UNC working directories,
// and npm on Windows would install Windows-only binaries and .cmd shims into a
// Linux tree). Native lookup remains only as a fallback when WSL is missing.
func (m *Manager) FindBinary(ctx context.Context, name string) (Found, error) {
	if name == "" {
		return Found{}, fmt.Errorf("可执行文件名不能为空")
	}
	if goruntime.GOOS == "windows" {
		distro := m.WSLDistro(ctx)
		if distro != "" {
			if found, ok := m.findInWSL(ctx, distro, name); ok {
				return found, nil
			}
			return Found{}, fmt.Errorf("未找到 %s：WSL(%s) 中不可用；运行时命令统一在 WSL2 中执行", name, distro)
		}
	}
	if resolved, err := exec.LookPath(name); err == nil {
		return Found{Binary: name, Path: resolved, Execution: ExecutionNative}, nil
	}
	if goruntime.GOOS != "windows" {
		return Found{}, fmt.Errorf("未找到 %s", name)
	}
	return Found{}, fmt.Errorf("未找到 %s：宿主 PATH 中没有，且未检测到可用的 WSL2 发行版（可在设置页指定发行版）", name)
}

// findInWSL resolves name inside distro, ignoring Windows programs exposed
// through WSL's interop PATH (/mnt/c/...), which are not Linux runtime tools.
func (m *Manager) findInWSL(ctx context.Context, distro, name string) (Found, bool) {
	probeCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	wslPath, err := m.wslWhich(probeCtx, distro, name)
	if err != nil || wslPath == "" || isWindowsInteropPath(wslPath) {
		return Found{}, false
	}
	return Found{Binary: name, Path: wslPath, Execution: executionWSL + distro}, true
}

// Sibling looks for another executable in the same directory as found.
func (m *Manager) Sibling(ctx context.Context, found Found, name string) (Found, bool) {
	if found.Execution == ExecutionNative {
		dir := filepath.Dir(found.Path)
		candidate := filepath.Join(dir, name)
		if goruntime.GOOS == "windows" && filepath.Ext(candidate) == "" {
			candidate += ".exe"
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return Found{Binary: name, Path: candidate, Execution: ExecutionNative}, true
		}
		return Found{}, false
	}
	candidate := path.Join(path.Dir(found.Path), name)
	sibling := Found{Binary: name, Path: candidate, Execution: found.Execution}
	if m.Exists(ctx, sibling, candidate) {
		return sibling, true
	}
	return Found{}, false
}

// RunBinary resolves and runs a command.
func (m *Manager) RunBinary(ctx context.Context, binary string, opts RunOptions) (RunResult, error) {
	found, err := m.FindBinary(ctx, binary)
	if err != nil {
		return RunResult{}, err
	}
	return m.RunFound(ctx, found, opts)
}

// RunFound runs an already resolved command.
func (m *Manager) RunFound(ctx context.Context, found Found, opts RunOptions) (RunResult, error) {
	command, err := m.CommandFoundInDir(ctx, found, opts.Args, opts.Env, opts.Dir)
	if err != nil {
		return RunResult{Found: found}, err
	}
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	platform.HideWindow(cmd)
	cmd.Env = command.Env
	cmd.Dir = command.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if len(opts.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(opts.Stdin)
	}
	err = cmd.Run()
	result := RunResult{Found: found, Stdout: stdout.String(), Stderr: stderr.String()}
	if IsWSL(found.Execution) {
		// wsl.exe writes its own diagnostics (for example the localhost proxy
		// warning under NAT networking) to the same streams as the command.
		// They are not command output and must not reach version or path
		// parsers.
		result.Stdout = stripWSLDiagnostics(result.Stdout)
		result.Stderr = stripWSLProxyNoise(result.Stderr)
	}
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

// RunWSL runs argv inside distro without a shell. Arguments are passed
// verbatim, so callers must translate host paths with ToWSLPath first.
func (m *Manager) RunWSL(ctx context.Context, distro string, args ...string) (RunResult, error) {
	if len(args) == 0 {
		return RunResult{}, fmt.Errorf("WSL 命令不能为空")
	}
	found := Found{Binary: args[0], Path: args[0], Execution: WSLExecution(distro)}
	return m.RunFound(ctx, found, RunOptions{Args: args[1:]})
}

// Command resolves a command for callers that need to start a long-lived
// process themselves.
func (m *Manager) Command(ctx context.Context, binary string, args, env []string) (Command, error) {
	found, err := m.FindBinary(ctx, binary)
	if err != nil {
		return Command{}, err
	}
	return m.CommandFound(ctx, found, args, env)
}

// CommandInDir resolves a command with a host working directory.
func (m *Manager) CommandInDir(ctx context.Context, binary, dir string, args, env []string) (Command, error) {
	found, err := m.FindBinary(ctx, binary)
	if err != nil {
		return Command{}, err
	}
	return m.CommandFoundInDir(ctx, found, args, env, dir)
}

// CommandFound builds a resolved command from an existing Found value.
func (m *Manager) CommandFound(ctx context.Context, found Found, args, env []string) (Command, error) {
	return m.CommandFoundInDir(ctx, found, args, env, "")
}

// CommandFoundInDir builds a resolved command from an existing Found value and
// a host working directory.
func (m *Manager) CommandFoundInDir(ctx context.Context, found Found, args, env []string, dir string) (Command, error) {
	if found.Execution == ExecutionNative {
		commandDir := ""
		if dir != "" {
			commandDir = filepath.Clean(dir)
		}
		return Command{
			Found: found,
			Path:  found.Path,
			Args:  append([]string(nil), args...),
			Env:   mergeEnv(os.Environ(), env),
			Dir:   commandDir,
		}, nil
	}
	distro := strings.TrimPrefix(found.Execution, executionWSL)
	wsl, err := exec.LookPath("wsl.exe")
	if err != nil {
		return Command{}, fmt.Errorf("wsl.exe 不可用: %w", err)
	}
	prefix := []string{"-d", distro}
	if dir != "" {
		execDir, err := m.TranslatePath(found.Execution, dir)
		if err != nil {
			return Command{}, err
		}
		prefix = append(prefix, "--cd", execDir)
	}
	prefix = append(prefix, "--", found.Path)
	cmdArgs := append(prefix, args...)
	return Command{Found: found, Path: wsl, Args: cmdArgs, Env: wslEnv(env)}, nil
}

// TranslatePath maps a host path into the execution environment.
func (m *Manager) TranslatePath(execution, hostPath string) (string, error) {
	if hostPath == "" {
		return "", nil
	}
	if execution == "" || execution == ExecutionNative {
		return filepath.Clean(hostPath), nil
	}
	if strings.HasPrefix(execution, executionWSL) {
		return ToWSLPath(hostPath)
	}
	return "", fmt.Errorf("未知执行环境 %q", execution)
}

// Exists reports whether an executable exists in the execution environment.
func (m *Manager) Exists(ctx context.Context, found Found, execPath string) bool {
	if found.Execution == "" || found.Execution == ExecutionNative {
		info, err := os.Stat(execPath)
		return err == nil && !info.IsDir()
	}
	distro := strings.TrimPrefix(found.Execution, executionWSL)
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "stat", "-L", "-c", "%n", execPath)
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	if err := cmd.Run(); err != nil {
		return false
	}
	return true
}

func (m *Manager) hashExecutable(ctx context.Context, found Found) (string, error) {
	if found.Execution == ExecutionNative {
		file, err := os.Open(found.Path)
		if err != nil {
			return "", err
		}
		defer func() { _ = file.Close() }()
		hash := sha256.New()
		if _, err := io.Copy(hash, file); err != nil {
			return "", err
		}
		return hex.EncodeToString(hash.Sum(nil)), nil
	}
	distro := strings.TrimPrefix(found.Execution, executionWSL)
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "sha256sum", found.Path)
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for _, field := range strings.Fields(trimWSLOutput(string(output))) {
		if len(field) != 64 {
			continue
		}
		if _, decodeErr := hex.DecodeString(field); decodeErr == nil {
			return field, nil
		}
	}
	return "", fmt.Errorf("sha256sum 输出无效")
}

func mergeEnv(base, extra []string) []string {
	values := make(map[string]string, len(base)+len(extra))
	order := make([]string, 0, len(base)+len(extra))
	add := func(item string) {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" {
			return
		}
		if _, seen := values[key]; !seen {
			order = append(order, key)
		}
		values[key] = value
	}
	for _, item := range base {
		add(item)
	}
	for _, item := range extra {
		add(item)
	}
	result := make([]string, 0, len(order))
	for _, key := range order {
		result = append(result, key+"="+values[key])
	}
	return result
}

func wslEnv(extra []string) []string {
	base := mergeEnv(os.Environ(), extra)
	keys := make([]string, 0, len(extra))
	for _, item := range extra {
		key, _, ok := strings.Cut(item, "=")
		if ok && key != "" {
			keys = append(keys, key+"/u")
		}
	}
	if len(keys) > 0 {
		base = append(base, "WSLENV="+strings.Join(keys, ":"))
	}
	return append(base, "WSL_UTF8=1")
}

// WSLDistro returns the configured distribution or the first installed one.
func (m *Manager) WSLDistro(ctx context.Context) string {
	if goruntime.GOOS != "windows" {
		return ""
	}
	if configured := strings.TrimSpace(m.cfg.Get().Runtime.WSLDistro); configured != "" {
		return configured
	}
	distros := m.WSLDistros(ctx)
	if len(distros) == 0 {
		return ""
	}
	return distros[0]
}

// WSLDistros lists installed WSL distributions with a short cache.
func (m *Manager) WSLDistros(ctx context.Context) []string {
	if goruntime.GOOS != "windows" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if time.Since(m.distrosAt) < 30*time.Second {
		return append([]string(nil), m.distros...)
	}
	distros, err := listWSLDistrosDetailed(ctx)
	if err != nil {
		distros = nil
	}
	m.distros = distros
	m.distrosAt = time.Now()
	return append([]string(nil), distros...)
}

// ProbeWSL reports the distribution and kernel used for the execution bridge.
func (m *Manager) ProbeWSL(ctx context.Context) (string, string, error) {
	distro := m.WSLDistro(ctx)
	if distro == "" {
		return "", "", fmt.Errorf("未检测到可用的 WSL2 发行版")
	}
	cmd := exec.CommandContext(ctx, "wsl.exe", "-d", distro, "--", "uname", "-r")
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	output, err := cmd.Output()
	if err != nil {
		return distro, "", err
	}
	return distro, trimWSLOutput(string(output)), nil
}

func (m *Manager) wslWhich(ctx context.Context, distro, name string) (string, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, "wsl.exe", "-d", distro, "--", "which", name)
	platform.HideWindow(cmd)
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	output, err := cmd.Output()
	if err == nil {
		if value := trimWSLOutput(string(output)); value != "" && !isWindowsInteropPath(value) {
			return value, nil
		}
	}
	for _, candidate := range []string{
		"/usr/local/bin/" + name,
		"/usr/bin/" + name,
		"/bin/" + name,
		"/snap/bin/" + name,
	} {
		stat := exec.CommandContext(probeCtx, "wsl.exe", "-d", distro, "--", "stat", "-L", "-c", "%n", candidate)
		platform.HideWindow(stat)
		stat.Env = append(os.Environ(), "WSL_UTF8=1")
		if stat.Run() == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("which %s failed", name)
}

// trimWSLOutput normalizes text produced by wsl.exe and drops diagnostics the
// bridge writes to the command's standard output. Newer WSL versions emit a
// localhost-proxy warning there on every invocation; treating it as command
// output corrupts paths, user names, versions and IDs.
func trimWSLOutput(value string) string {
	return stripWSLDiagnostics(value)
}

func stripWSLDiagnostics(value string) string {
	return stripWSLText(value, isWSLDiagnosticLine)
}

// stripWSLProxyNoise removes only the localhost-proxy notice. Genuine WSL
// errors also start with "wsl:" and must stay visible.
func stripWSLProxyNoise(value string) string {
	return stripWSLText(value, isWSLProxyWarning)
}

func stripWSLText(value string, drop func(string) bool) string {
	value = strings.ReplaceAll(value, "\x00", "")
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimPrefix(value, "\ufeff")
	lines := strings.Split(value, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if drop(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

func isWSLDiagnosticLine(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "wsl:")
}

func isWSLProxyWarning(line string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(line))
	if !strings.HasPrefix(trimmed, "wsl:") || !strings.Contains(trimmed, "localhost") {
		return false
	}
	return strings.Contains(trimmed, "proxy") || strings.Contains(trimmed, "代理")
}

// isWindowsInteropPath reports whether p is a Windows drive mounted into WSL
// (/mnt/c/...). WSL appends Windows directories to PATH by default, so `which`
// can return a Windows executable or npm/yarn shim. Those are not Linux
// runtime tools and must never be selected for a WSL workspace.
func isWindowsInteropPath(p string) bool {
	if len(p) < 7 || !strings.HasPrefix(p, "/mnt/") || p[6] != '/' {
		return false
	}
	c := p[5]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// ToWSLPath converts a Windows host path to its WSL2 mount path.
func ToWSLPath(hostPath string) (string, error) {
	if strings.ContainsAny(hostPath, "\x00\r\n") {
		return "", fmt.Errorf("无法把 %q 映射为 WSL 路径: 路径包含控制字符", hostPath)
	}
	cleaned := filepath.Clean(hostPath)
	for _, prefix := range []string{`\\wsl.localhost\`, `\\wsl$\`} {
		if strings.HasPrefix(cleaned, prefix) {
			rest := strings.TrimPrefix(cleaned, prefix)
			parts := strings.SplitN(rest, `\`, 2)
			if len(parts) != 2 {
				return "", fmt.Errorf("无法映射 WSL 路径 %q", hostPath)
			}
			return "/" + filepath.ToSlash(parts[1]), nil
		}
	}
	volume := filepath.VolumeName(cleaned)
	if len(volume) == 2 && volume[1] == ':' {
		rest := strings.TrimPrefix(cleaned[len(volume):], `\`)
		return "/mnt/" + strings.ToLower(volume[:1]) + "/" + filepath.ToSlash(rest), nil
	}
	if strings.HasPrefix(cleaned, "/") {
		return cleaned, nil
	}
	return "", fmt.Errorf("无法把 %q 映射为 WSL 路径", hostPath)
}
