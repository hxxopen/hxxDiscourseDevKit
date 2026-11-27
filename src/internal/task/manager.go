package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
)

const (
	maxRetainedTasks = 200
	maxSubscriberBuf = 64
)

// Errors returned by task operations.
var (
	ErrNotFound      = errors.New("task not found")
	ErrNotCancelable = errors.New("task is not cancelable")
)

// Manager owns in-memory tasks, their cancellation, and SSE subscribers.
type Manager struct {
	mu      sync.Mutex
	tasks   map[string]*Task
	order   []string
	cancels map[string]context.CancelFunc
	subs    map[int]chan Event
	nextSub int
	closed  bool

	logMu   sync.Mutex
	logPath string
	log     *slog.Logger
	wg      sync.WaitGroup
}

// New creates a task manager. logPath may be empty to disable JSONL history.
func New(logPath string, logger *slog.Logger) *Manager {
	return &Manager{
		tasks:   map[string]*Task{},
		cancels: map[string]context.CancelFunc{},
		subs:    map[int]chan Event{},
		logPath: logPath,
		log:     logger,
	}
}

// Submit starts fn in the background and returns the initial task snapshot.
func (m *Manager) Submit(typ, workspaceID string, cancelable bool, fn func(context.Context, func(int, string)) error) Task {
	now := time.Now().UTC()
	taskID := id.MustNew()
	t := &Task{
		TaskID:      taskID,
		Type:        typ,
		WorkspaceID: workspaceID,
		Status:      StatusQueued,
		Progress:    0,
		Message:     "排队中",
		CreatedAt:   now,
		Cancelable:  cancelable,
	}

	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		t.Status = StatusCanceled
		t.Message = "任务管理器已关闭"
		return *t
	}
	m.tasks[taskID] = t
	m.order = append(m.order, taskID)
	m.cancels[taskID] = cancel
	snapshot := *t
	m.publishLocked(t)
	m.pruneLocked()
	m.mu.Unlock()

	m.wg.Add(1)
	go m.run(ctx, cancel, taskID, fn)
	return snapshot
}

func (m *Manager) run(ctx context.Context, cancel context.CancelFunc, taskID string, fn func(context.Context, func(int, string)) error) {
	defer m.wg.Done()
	defer cancel()

	started := time.Now().UTC()
	m.mu.Lock()
	t, ok := m.tasks[taskID]
	if !ok {
		m.mu.Unlock()
		return
	}
	t.Status = StatusRunning
	t.Progress = 2
	t.Message = "任务开始"
	t.StartedAt = &started
	m.publishLocked(t)
	m.mu.Unlock()

	progress := func(value int, message string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		current, ok := m.tasks[taskID]
		if !ok {
			return
		}
		if value < 0 {
			value = 0
		}
		if value > 100 {
			value = 100
		}
		current.Progress = value
		if message != "" {
			current.Message = message
		}
		m.publishLocked(current)
	}

	err := fn(ctx, progress)

	finished := time.Now().UTC()
	m.mu.Lock()
	current := m.tasks[taskID]
	if current == nil {
		m.mu.Unlock()
		return
	}
	current.FinishedAt = &finished
	delete(m.cancels, taskID)
	switch {
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		current.Status = StatusCanceled
		current.Message = "任务已取消"
	case err != nil:
		current.Status = StatusFailed
		current.ErrorCode = "TASK_FAILED"
		current.ErrorMessage = err.Error()
		current.Message = "任务失败"
	default:
		current.Status = StatusSucceeded
		// 保留任务函数在 progress(100, ...) 中给出的最终说明，
		// 例如"备份完成: <ID>"，避免被通用文案覆盖。
		if current.Progress != 100 || current.Message == "" {
			current.Message = "任务完成"
		}
		current.Progress = 100
	}
	m.publishLocked(current)
	snapshot := *current
	m.pruneLocked()
	m.mu.Unlock()

	m.appendHistory(snapshot)
	if err != nil && !errors.Is(err, context.Canceled) {
		m.log.Warn("background task failed",
			"task_id", snapshot.TaskID,
			"type", snapshot.Type,
			"workspace_id", snapshot.WorkspaceID,
			"error", err,
		)
	}
}

// Get returns a task snapshot.
func (m *Manager) Get(taskID string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return Task{}, false
	}
	return *t, true
}

// List returns tasks newest first.
func (m *Manager) List(limit int) []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if limit <= 0 || limit > len(m.order) {
		limit = len(m.order)
	}
	out := make([]Task, 0, limit)
	for i := len(m.order) - 1; i >= 0 && len(out) < limit; i-- {
		if t, ok := m.tasks[m.order[i]]; ok {
			out = append(out, *t)
		}
	}
	return out
}

// Cancel requests cancellation of a running task.
func (m *Manager) Cancel(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[taskID]
	if !ok {
		return ErrNotFound
	}
	if !t.Cancelable || t.Status.Terminal() {
		return ErrNotCancelable
	}
	cancel, ok := m.cancels[taskID]
	if !ok {
		return ErrNotCancelable
	}
	cancel()
	t.Message = "正在取消"
	m.publishLocked(t)
	return nil
}

// Subscribe returns a channel of task events. The returned function
// unsubscribes; it is safe to call more than once.
func (m *Manager) Subscribe() (int, <-chan Event, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	subID := m.nextSub
	m.nextSub++
	ch := make(chan Event, maxSubscriberBuf)
	m.subs[subID] = ch
	var once sync.Once
	return subID, ch, func() {
		once.Do(func() { m.unsubscribe(subID) })
	}
}

func (m *Manager) unsubscribe(subID int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.subs[subID]; ok {
		delete(m.subs, subID)
		close(ch)
	}
}

func (m *Manager) publishLocked(t *Task) {
	event := Event{Type: "task", Task: *t}
	for _, ch := range m.subs {
		select {
		case ch <- event:
		default:
			// Drop events for slow consumers; they can poll the REST endpoint.
		}
	}
}

func (m *Manager) pruneLocked() {
	for len(m.order) > maxRetainedTasks {
		removed := false
		for i, taskID := range m.order {
			t, ok := m.tasks[taskID]
			if ok && t.Status.Terminal() {
				delete(m.tasks, taskID)
				m.order = append(m.order[:i], m.order[i+1:]...)
				removed = true
				break
			}
		}
		if !removed {
			return
		}
	}
}

func (m *Manager) appendHistory(t Task) {
	if m.logPath == "" {
		return
	}
	data, err := json.Marshal(t)
	if err != nil {
		return
	}
	data = append(data, '\n')

	m.logMu.Lock()
	defer m.logMu.Unlock()
	f, err := os.OpenFile(m.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		m.log.Warn("could not append task history", "error", err)
		return
	}
	if _, err := f.Write(data); err != nil {
		m.log.Warn("could not write task history", "error", err)
	}
	_ = f.Close()
}

// Close cancels running tasks and closes subscriber channels. It waits up to
// five seconds for task goroutines to exit.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	for _, cancel := range m.cancels {
		cancel()
	}
	for subID, ch := range m.subs {
		delete(m.subs, subID)
		close(ch)
	}
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.log.Warn("timed out waiting for background tasks to stop")
	}
}

// FormatProgress renders a progress message for CLI output.
func FormatProgress(t Task) string {
	return fmt.Sprintf("%3d%% %s", t.Progress, t.Message)
}
