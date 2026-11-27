// Package process supervises long-lived development processes started by
// DevKit, keeping their output in a log file and a bounded in-memory buffer.
package process

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
)

// Errors returned by the process manager.
var (
	ErrAlreadyRunning = errors.New("process is already running")
	ErrNotFound       = errors.New("process not found")
)

// Spec describes one long-lived process.
type Spec struct {
	Key     string
	Command string
	Args    []string
	Env     []string
	Dir     string
	LogPath string
}

// Process is a supervised child process.
type Process struct {
	Key       string
	PID       int
	StartedAt time.Time
	Command   string
	Args      []string
	Dir       string
	LogPath   string

	cmd    *exec.Cmd
	done   chan struct{}
	output *outputWriter

	mu            sync.Mutex
	exitCode      int
	exitErr       error
	requestedStop bool
	finished      bool
}

// Alive reports whether the process has not exited yet.
func (p *Process) Alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// ExitCode returns the exit code once the process has exited.
func (p *Process) ExitCode() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.finished {
		return 0, false
	}
	return p.exitCode, true
}

// ExitError returns the wait error once the process has exited.
func (p *Process) ExitError() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// RequestedStop reports whether Stop was called for this process.
func (p *Process) RequestedStop() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.requestedStop
}

type exitHandler func(key string, pid, exitCode int, requestedStop bool)

// Manager owns long-lived processes by key (normally a workspace ID).
type Manager struct {
	mu      sync.Mutex
	procs   map[string]*Process
	log     *slog.Logger
	onExit  exitHandler
	bufSize int
}

// New creates a process manager.
func New(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		procs:   map[string]*Process{},
		log:     logger,
		bufSize: 2000,
	}
}

// SetExitHandler installs a callback invoked after a supervised process exits.
// The callback runs in its own goroutine and must not block.
func (m *Manager) SetExitHandler(handler func(key string, pid, exitCode int, requestedStop bool)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onExit = handler
}

// Start launches a supervised process. The caller must stop it with Stop.
func (m *Manager) Start(spec Spec) (*Process, error) {
	if strings.TrimSpace(spec.Key) == "" || strings.TrimSpace(spec.Command) == "" {
		return nil, fmt.Errorf("process key and command are required")
	}

	m.mu.Lock()
	if existing, ok := m.procs[spec.Key]; ok && existing.Alive() {
		m.mu.Unlock()
		return nil, fmt.Errorf("%w: %s", ErrAlreadyRunning, spec.Key)
	}

	if spec.LogPath != "" {
		if err := os.MkdirAll(filepath.Dir(spec.LogPath), 0o755); err != nil {
			m.mu.Unlock()
			return nil, fmt.Errorf("create log directory: %w", err)
		}
	}
	logFile, err := openLog(spec.LogPath)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	writer := newOutputWriter(logFile, m.bufSize)

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdout = writer
	cmd.Stderr = writer
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = writer.Close()
		m.mu.Unlock()
		return nil, fmt.Errorf("start process: %w", err)
	}

	proc := &Process{
		Key:       spec.Key,
		PID:       cmd.Process.Pid,
		StartedAt: time.Now().UTC(),
		Command:   spec.Command,
		Args:      append([]string(nil), spec.Args...),
		Dir:       spec.Dir,
		LogPath:   spec.LogPath,
		cmd:       cmd,
		done:      make(chan struct{}),
		output:    writer,
	}
	m.procs[spec.Key] = proc
	handler := m.onExit
	m.mu.Unlock()

	m.log.Info("process started", "process", spec.Key, "pid", proc.PID,
		"command", logging.Redact(spec.Command), "dir", spec.Dir)

	go func() {
		waitErr := cmd.Wait()
		_ = writer.Close()

		code := -1
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		proc.mu.Lock()
		proc.finished = true
		proc.exitCode = code
		proc.exitErr = waitErr
		requested := proc.requestedStop
		proc.mu.Unlock()
		close(proc.done)

		m.log.Info("process exited", "process", spec.Key, "pid", proc.PID,
			"exit_code", code, "requested", requested)
		if handler != nil {
			go handler(spec.Key, proc.PID, code, requested)
		}
	}()

	return proc, nil
}

// Get returns a supervised process, running or finished.
func (m *Manager) Get(key string) (*Process, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	proc, ok := m.procs[key]
	return proc, ok
}

// Alive reports whether a key is currently running.
func (m *Manager) Alive(key string) bool {
	proc, ok := m.Get(key)
	return ok && proc.Alive()
}

// Stop terminates a supervised process. It sends a graceful signal first and
// force-kills the process tree when grace expires.
func (m *Manager) Stop(ctx context.Context, key string, grace time.Duration) error {
	proc, ok := m.Get(key)
	if !ok || !proc.Alive() {
		return nil
	}
	proc.mu.Lock()
	proc.requestedStop = true
	proc.mu.Unlock()

	terminate(proc.PID)
	select {
	case <-proc.done:
		return nil
	case <-time.After(grace):
	case <-ctx.Done():
	}
	forceKill(proc.PID)
	select {
	case <-proc.done:
	case <-time.After(5 * time.Second):
		return fmt.Errorf("process %s did not exit after force kill", key)
	}
	return nil
}

// StopAll stops every running process.
func (m *Manager) StopAll(ctx context.Context, grace time.Duration) {
	m.mu.Lock()
	keys := make([]string, 0, len(m.procs))
	for key, proc := range m.procs {
		if proc.Alive() {
			keys = append(keys, key)
		}
	}
	m.mu.Unlock()
	for _, key := range keys {
		if err := m.Stop(ctx, key, grace); err != nil {
			m.log.Warn("could not stop process", "process", key, "error", err)
		}
	}
}

// Logs returns buffered output lines for a supervised process.
func (m *Manager) Logs(key string, tail int) ([]string, bool) {
	proc, ok := m.Get(key)
	if !ok {
		return nil, false
	}
	return proc.logs(tail), true
}

func (p *Process) logs(tail int) []string {
	if p == nil || p.output == nil {
		return nil
	}
	return p.output.Tail(tail)
}

// KillTree terminates a process and its children outside the supervisor. It
// is used to clean up a stale PID after the daemon restarts.
func KillTree(pid int, force bool) {
	if force {
		forceKill(pid)
		return
	}
	terminate(pid)
}

// TailFile returns the last lines of a log file.
func TailFile(path string, tail int) ([]string, error) {
	if tail <= 0 {
		tail = 200
	}
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const maxTail = 256 << 10
	offset := int64(0)
	if info.Size() > maxTail {
		offset = info.Size() - maxTail
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if index := strings.IndexByte(string(data), '\n'); index >= 0 {
			data = data[index+1:]
		}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []string{}, nil
	}
	for i := range lines {
		lines[i] = logging.Redact(strings.TrimRight(lines[i], "\r"))
	}
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return lines, nil
}

func openLog(path string) (*os.File, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open process log: %w", err)
	}
	return file, nil
}

// outputWriter splits child output into lines, redacts them, and mirrors each
// line into the log file and a bounded ring buffer.
type outputWriter struct {
	mu      sync.Mutex
	file    *os.File
	partial string
	lines   []string
	max     int
}

func newOutputWriter(file *os.File, maxLines int) *outputWriter {
	if maxLines <= 0 {
		maxLines = 2000
	}
	return &outputWriter{file: file, max: maxLines}
}

func (w *outputWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial += string(data)
	for {
		index := strings.IndexByte(w.partial, '\n')
		if index < 0 {
			break
		}
		line := strings.TrimRight(w.partial[:index], "\r")
		w.partial = w.partial[index+1:]
		w.appendLocked(logging.Redact(line))
	}
	if len(w.partial) > 64<<10 {
		w.appendLocked(logging.Redact(w.partial))
		w.partial = ""
	}
	return len(data), nil
}

func (w *outputWriter) appendLocked(line string) {
	if w.file != nil {
		_, _ = io.WriteString(w.file, line+"\n")
	}
	w.lines = append(w.lines, line)
	if len(w.lines) > w.max {
		w.lines = w.lines[len(w.lines)-w.max:]
	}
}

func (w *outputWriter) Tail(tail int) []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	lines := w.lines
	if len(w.partial) > 0 {
		lines = append(append([]string(nil), lines...), logging.Redact(w.partial))
	}
	if tail > 0 && len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	return append([]string(nil), lines...)
}

func (w *outputWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.partial) > 0 {
		w.appendLocked(logging.Redact(w.partial))
		w.partial = ""
	}
	if w.file != nil {
		err := w.file.Close()
		w.file = nil
		return err
	}
	return nil
}
