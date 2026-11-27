// Package audit stores an append-only operation trail for the local
// management API. Events contain request metadata only; request bodies are
// never captured, so passwords, API keys, and confirm tokens cannot leak into
// the trail.
package audit

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
)

const (
	filePrefix = "audit-"
	fileSuffix = ".jsonl"
	dateLayout = "2006-01-02"

	defaultLimit = 100
	maxLimit     = 500
	pruneEvery   = 24 * time.Hour
)

// Event is one audited operation. Result is "success", "accepted" (a
// background task was started), or "failure".
type Event struct {
	ID          string    `json:"id"`
	Timestamp   time.Time `json:"timestamp"`
	Action      string    `json:"action"`
	Result      string    `json:"result"`
	Status      int       `json:"status"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	WorkspaceID string    `json:"workspace_id,omitempty"`
	Target      string    `json:"target,omitempty"`
	Client      string    `json:"client"`
	Actor       string    `json:"actor"`
	RequestID   string    `json:"request_id,omitempty"`
	RemoteAddr  string    `json:"remote_addr,omitempty"`
	DurationMS  int64     `json:"duration_ms"`
}

// Filter narrows a List query. Zero values match everything.
type Filter struct {
	Limit       int
	Action      string
	WorkspaceID string
	Result      string
	Since       time.Time
}

// ListResult is a page of audit events. Total counts every matching event
// available on disk, while Items is capped by Limit.
type ListResult struct {
	Items []Event `json:"items"`
	Total int     `json:"total"`
	Limit int     `json:"limit"`
}

// Logger appends daily JSONL files under one directory.
type Logger struct {
	dir           string
	retentionDays int

	mu        sync.Mutex
	now       func() time.Time
	lastPrune time.Time
}

// New creates an audit logger. retentionDays follows the logging retention
// setting; zero disables cleanup.
func New(dir string, retentionDays int) (*Logger, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("audit: directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("audit: create directory %s: %w", dir, err)
	}
	logger := &Logger{
		dir:           dir,
		retentionDays: retentionDays,
		now:           time.Now,
	}
	logger.mu.Lock()
	logger.maybePruneLocked(logger.now().UTC())
	logger.mu.Unlock()
	return logger, nil
}

// Dir returns the directory containing audit files.
func (l *Logger) Dir() string { return l.dir }

// Record appends one event to the file for its timestamp's UTC date. Writes
// are serialized and each line is written with a single Write call.
func (l *Logger) Record(event Event) error {
	if strings.TrimSpace(event.Action) == "" {
		return errors.New("audit: action is required")
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now().UTC()
	if event.ID == "" {
		event.ID = id.MustNew()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = now
	} else {
		event.Timestamp = event.Timestamp.UTC()
	}
	if event.Actor == "" {
		event.Actor = "local"
	}
	l.maybePruneLocked(now)

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("audit: encode event: %w", err)
	}
	data = append(data, '\n')

	path := filepath.Join(l.dir, filePrefix+event.Timestamp.Format(dateLayout)+fileSuffix)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("audit: open %s: %w", path, err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("audit: write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("audit: close %s: %w", path, err)
	}
	return nil
}

// List returns matching events newest first. Unreadable or malformed lines are
// skipped so a partially written tail never breaks the query.
func (l *Logger) List(filter Filter) (ListResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	entries, err := os.ReadDir(l.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ListResult{Items: []Event{}, Limit: limit}, nil
		}
		return ListResult{}, fmt.Errorf("audit: read directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })

	events := make([]Event, 0, limit)
	total := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		day, ok := parseFileName(entry.Name())
		if !ok {
			continue
		}
		if !filter.Since.IsZero() && day.AddDate(0, 0, 1).Before(filter.Since) {
			continue
		}
		for _, event := range l.readFile(filepath.Join(l.dir, entry.Name())) {
			if !matches(event, filter) {
				continue
			}
			total++
			events = append(events, event)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].Timestamp.Equal(events[j].Timestamp) {
			return events[i].ID > events[j].ID
		}
		return events[i].Timestamp.After(events[j].Timestamp)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return ListResult{Items: events, Total: total, Limit: limit}, nil
}

func (l *Logger) readFile(path string) []Event {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()

	events := make([]Event, 0, 16)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var event Event
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Timestamp.IsZero() {
			continue
		}
		events = append(events, event)
	}
	return events
}

func matches(event Event, filter Filter) bool {
	if filter.Action != "" && event.Action != filter.Action {
		return false
	}
	if filter.WorkspaceID != "" && event.WorkspaceID != filter.WorkspaceID {
		return false
	}
	if filter.Result != "" && event.Result != filter.Result {
		return false
	}
	if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
		return false
	}
	return true
}

func (l *Logger) maybePruneLocked(now time.Time) {
	if l.retentionDays <= 0 {
		return
	}
	if !l.lastPrune.IsZero() && now.Sub(l.lastPrune) < pruneEvery {
		return
	}
	l.lastPrune = now
	cutoff := now.AddDate(0, 0, -l.retentionDays)
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		day, ok := parseFileName(entry.Name())
		if !ok {
			continue
		}
		if day.Before(cutoff) {
			_ = os.Remove(filepath.Join(l.dir, entry.Name()))
		}
	}
}

func parseFileName(name string) (time.Time, bool) {
	if !strings.HasPrefix(name, filePrefix) || !strings.HasSuffix(name, fileSuffix) {
		return time.Time{}, false
	}
	raw := strings.TrimSuffix(strings.TrimPrefix(name, filePrefix), fileSuffix)
	day, err := time.Parse(dateLayout, raw)
	if err != nil {
		return time.Time{}, false
	}
	return day, true
}
