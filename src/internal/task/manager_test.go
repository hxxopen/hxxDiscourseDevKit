package task

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

func testManager() *Manager {
	return New("", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func waitTerminal(t *testing.T, manager *Manager, taskID string) Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current, ok := manager.Get(taskID)
		if !ok {
			t.Fatalf("task %s disappeared", taskID)
		}
		if current.Status.Terminal() {
			return current
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s did not finish", taskID)
	return Task{}
}

func TestSubmitRunsTask(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	started := make(chan struct{})
	submitted := manager.Submit("test.run", "ws-1", false,
		func(ctx context.Context, progress func(int, string)) error {
			close(started)
			progress(40, "half way")
			return nil
		})
	if submitted.Status != StatusQueued {
		t.Fatalf("initial status = %s, want queued", submitted.Status)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task function did not start")
	}
	final := waitTerminal(t, manager, submitted.TaskID)
	if final.Status != StatusSucceeded {
		t.Fatalf("final status = %s (%s), want succeeded", final.Status, final.ErrorMessage)
	}
	if final.Progress != 100 {
		t.Fatalf("final progress = %d, want 100", final.Progress)
	}
	if final.FinishedAt == nil || final.StartedAt == nil {
		t.Fatal("timestamps were not recorded")
	}
}

func TestSubmitKeepsFinalProgressMessage(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	submitted := manager.Submit("test.final-message", "", false,
		func(ctx context.Context, progress func(int, string)) error {
			progress(40, "half way")
			progress(100, "done: 42")
			return nil
		})
	final := waitTerminal(t, manager, submitted.TaskID)
	if final.Status != StatusSucceeded {
		t.Fatalf("final status = %s, want succeeded", final.Status)
	}
	if final.Message != "done: 42" {
		t.Fatalf("final message = %q, want %q", final.Message, "done: 42")
	}
	if final.Progress != 100 {
		t.Fatalf("final progress = %d, want 100", final.Progress)
	}
}

func TestSubmitUsesGenericMessageWithoutFinalProgress(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	submitted := manager.Submit("test.generic-message", "", false,
		func(ctx context.Context, progress func(int, string)) error {
			progress(40, "half way")
			return nil
		})
	final := waitTerminal(t, manager, submitted.TaskID)
	if final.Message != "任务完成" {
		t.Fatalf("final message = %q, want %q", final.Message, "任务完成")
	}
}

func TestSubmitFailureIsReported(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	submitted := manager.Submit("test.fail", "", false,
		func(ctx context.Context, progress func(int, string)) error {
			return errors.New("boom")
		})
	final := waitTerminal(t, manager, submitted.TaskID)
	if final.Status != StatusFailed {
		t.Fatalf("status = %s, want failed", final.Status)
	}
	if final.ErrorMessage != "boom" {
		t.Fatalf("error message = %q, want boom", final.ErrorMessage)
	}
}

func TestCancel(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	started := make(chan struct{})
	submitted := manager.Submit("test.cancel", "", true,
		func(ctx context.Context, progress func(int, string)) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not start")
	}
	if err := manager.Cancel(submitted.TaskID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	final := waitTerminal(t, manager, submitted.TaskID)
	if final.Status != StatusCanceled {
		t.Fatalf("status = %s, want canceled", final.Status)
	}
	if err := manager.Cancel(submitted.TaskID); !errors.Is(err, ErrNotCancelable) {
		t.Fatalf("second Cancel error = %v, want ErrNotCancelable", err)
	}
	if err := manager.Cancel("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel(missing) error = %v, want ErrNotFound", err)
	}
}

func TestNonCancelableTask(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	submitted := manager.Submit("test.sync", "", false,
		func(ctx context.Context, progress func(int, string)) error {
			return nil
		})
	if err := manager.Cancel(submitted.TaskID); !errors.Is(err, ErrNotCancelable) {
		t.Fatalf("Cancel(non-cancelable) error = %v, want ErrNotCancelable", err)
	}
}

func TestSubscribeReceivesEvents(t *testing.T) {
	manager := testManager()
	defer manager.Close()

	_, events, unsubscribe := manager.Subscribe()
	defer unsubscribe()

	submitted := manager.Submit("test.events", "", false,
		func(ctx context.Context, progress func(int, string)) error {
			progress(50, "half")
			return nil
		})
	_ = submitted
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event, open := <-events:
			if !open {
				t.Fatal("event channel closed unexpectedly")
			}
			if event.Task.TaskID == submitted.TaskID {
				return
			}
		case <-deadline:
			t.Fatal("no event received")
		}
	}
}
