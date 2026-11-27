// Package logging configures structured slog output with file rotation and
// secret redaction.
package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Options controls logger construction.
type Options struct {
	Level         string
	Dir           string
	Console       bool
	MaxFileMB     int
	RetentionDays int
}

// Provider owns the logger and its rotating file writer.
type Provider struct {
	logger *slog.Logger
	writer *dailyWriter
	level  *slog.LevelVar
}

// New builds a logger writing JSON lines to Dir and, optionally, text to
// stderr. The directory is created if needed.
func New(opts Options) (*Provider, error) {
	if opts.Dir == "" {
		return nil, fmt.Errorf("logging: directory is required")
	}
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create directory: %w", err)
	}
	cleanupOldFiles(opts.Dir, opts.RetentionDays)

	writer, err := newDailyWriter(opts.Dir, opts.MaxFileMB)
	if err != nil {
		return nil, err
	}

	level := &slog.LevelVar{}
	level.Set(ParseLevel(opts.Level))

	handlers := []slog.Handler{
		slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}),
	}
	if opts.Console {
		handlers = append(handlers, slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	}

	handler := redactHandler{inner: multiHandler{handlers: handlers}}
	return &Provider{
		logger: slog.New(handler),
		writer: writer,
		level:  level,
	}, nil
}

// Logger returns the configured logger.
func (p *Provider) Logger() *slog.Logger { return p.logger }

// SetLevel changes the minimum level at runtime.
func (p *Provider) SetLevel(name string) { p.level.Set(ParseLevel(name)) }

// Close flushes and closes the log file.
func (p *Provider) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

// ParseLevel maps a config string to a slog level.
func ParseLevel(name string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// --- handler plumbing -------------------------------------------------------

type multiHandler struct {
	handlers []slog.Handler
}

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m.handlers {
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, 0, len(m.handlers))
	for _, h := range m.handlers {
		next = append(next, h.WithAttrs(attrs))
	}
	return multiHandler{handlers: next}
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, 0, len(m.handlers))
	for _, h := range m.handlers {
		next = append(next, h.WithGroup(name))
	}
	return multiHandler{handlers: next}
}

type redactHandler struct {
	inner slog.Handler
}

func (h redactHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, Redact(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

func (h redactHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	redacted := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		redacted = append(redacted, redactAttr(a))
	}
	return redactHandler{inner: h.inner.WithAttrs(redacted)}
}

func (h redactHandler) WithGroup(name string) slog.Handler {
	return redactHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if sensitiveKey(a.Key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	switch a.Value.Kind() {
	case slog.KindGroup:
		group := a.Value.Group()
		converted := make([]any, 0, len(group))
		for _, child := range group {
			converted = append(converted, redactAttr(child))
		}
		return slog.Group(a.Key, converted...)
	case slog.KindString:
		return slog.String(a.Key, Redact(a.Value.String()))
	case slog.KindAny:
		if err, ok := a.Value.Any().(error); ok {
			return slog.String(a.Key, Redact(err.Error()))
		}
		return a
	default:
		return a
	}
}

// --- rotating writer --------------------------------------------------------

type dailyWriter struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	file     *os.File
	day      string
	size     int64
}

func newDailyWriter(dir string, maxFileMB int) (*dailyWriter, error) {
	w := &dailyWriter{dir: dir}
	if maxFileMB > 0 {
		w.maxBytes = int64(maxFileMB) * 1024 * 1024
	}
	if err := w.rotateLocked(time.Now()); err != nil {
		return nil, err
	}
	return w, nil
}

func (w *dailyWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	day := now.Format("20060102")
	if w.file == nil || day != w.day || (w.maxBytes > 0 && w.size+int64(len(p)) > w.maxBytes && w.size > 0) {
		if err := w.rotateLocked(now); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *dailyWriter) rotateLocked(now time.Time) error {
	if w.file != nil {
		_ = w.file.Sync()
		_ = w.file.Close()
		w.file = nil
	}
	w.day = now.Format("20060102")
	name := fmt.Sprintf("devkit-%s.jsonl", w.day)
	if w.maxBytes > 0 {
		// When rotating mid-day, include a timestamp to avoid clobbering.
		candidate := filepath.Join(w.dir, name)
		if info, err := os.Stat(candidate); err == nil && info.Size() > 0 {
			name = fmt.Sprintf("devkit-%s-%s.jsonl", w.day, now.Format("150405.000"))
		}
	}
	path := filepath.Join(w.dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("logging: open %s: %w", path, err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("logging: stat %s: %w", path, err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

func (w *dailyWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Sync()
	closeErr := w.file.Close()
	w.file = nil
	if err != nil {
		return err
	}
	return closeErr
}

func cleanupOldFiles(dir string, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "devkit-") || !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// Ensure io is referenced for older toolchains that prune unused imports in
// generated docs; the writer satisfies io.Writer.
var _ io.Writer = (*dailyWriter)(nil)
