package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func TestPreviewDoesNotIssueTokenWhenIneligible(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	f.manager.windows = false

	plan, err := f.manager.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if plan.Eligible || plan.ConfirmToken != "" || !plan.ExpiresAt.IsZero() {
		t.Fatalf("plan = %+v", plan)
	}
}

func TestPreviewIssuesSingleUseToken(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)

	plan, err := f.manager.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if !plan.Eligible || plan.ConfirmToken == "" || plan.ExpiresAt.IsZero() {
		t.Fatalf("plan = %+v", plan)
	}

	runnable, err := f.manager.Begin(context.Background(), plan.ConfirmToken)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if !f.manager.Migrating() {
		t.Fatalf("Migrating() = false after Begin")
	}

	if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrMigrating) {
		t.Fatalf("second Begin error = %v, want ErrMigrating", err)
	}

	runnable.Abort()
	if f.manager.Migrating() {
		t.Fatalf("Migrating() = true after Abort")
	}

	if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrConfirmToken) {
		t.Fatalf("replay error = %v, want ErrConfirmToken", err)
	}
}

func TestBeginRejectsInvalidAndExpiredTokens(t *testing.T) {
	requireWindows(t)

	t.Run("invalid", func(t *testing.T) {
		f := newFixture(t)
		if _, err := f.manager.Begin(context.Background(), "nope"); !errors.Is(err, ErrConfirmToken) {
			t.Fatalf("error = %v, want ErrConfirmToken", err)
		}
		if f.manager.Migrating() {
			t.Fatalf("Migrating() = true after failed Begin")
		}
	})

	t.Run("expired", func(t *testing.T) {
		f := newFixture(t)
		now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		f.manager.now = func() time.Time { return now }
		plan, err := f.manager.Preview(context.Background())
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		now = now.Add(tokenTTL + time.Second)
		if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrConfirmToken) {
			t.Fatalf("error = %v, want ErrConfirmToken", err)
		}
		if f.manager.Migrating() {
			t.Fatalf("Migrating() = true after expired Begin")
		}
	})
}

func TestBeginRejectsBusyAndChangedPlans(t *testing.T) {
	requireWindows(t)

	t.Run("active task", func(t *testing.T) {
		f := newFixture(t)
		plan, err := f.manager.Preview(context.Background())
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		f.tasks.active = true
		if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrBusy) {
			t.Fatalf("error = %v, want ErrBusy", err)
		}
		if f.manager.Migrating() {
			t.Fatalf("Migrating() = true after busy Begin")
		}
	})

	t.Run("plan became stale", func(t *testing.T) {
		f := newFixture(t)
		plan, err := f.manager.Preview(context.Background())
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		backups := filepath.Join(f.paths.BackupsDir)
		if err := os.WriteFile(filepath.Join(backups, "late.zip"), []byte("x"), 0o600); err != nil {
			t.Fatalf("write backup: %v", err)
		}
		if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrPlanStale) {
			t.Fatalf("error = %v, want ErrPlanStale", err)
		}
		if f.manager.Migrating() {
			t.Fatalf("Migrating() = true after stale Begin")
		}
	})

	t.Run("plan became blocked", func(t *testing.T) {
		f := newFixture(t)
		plan, err := f.manager.Preview(context.Background())
		if err != nil {
			t.Fatalf("Preview: %v", err)
		}
		f.workspaces.items = []*workspace.Workspace{{WorkspaceID: "ghost", Name: "ghost"}}
		if _, err := f.manager.Begin(context.Background(), plan.ConfirmToken); !errors.Is(err, ErrBlocked) {
			t.Fatalf("error = %v, want ErrBlocked", err)
		}
		if f.manager.Migrating() {
			t.Fatalf("Migrating() = true after blocked Begin")
		}
	})
}

func TestTaskIDIsOnlyRecordedWhileMigrating(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	f.manager.SetTaskID("before")
	if got := f.manager.CurrentTaskID(); got != "" {
		t.Fatalf("CurrentTaskID before Begin = %q", got)
	}

	plan, err := f.manager.Preview(context.Background())
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	runnable, err := f.manager.Begin(context.Background(), plan.ConfirmToken)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	f.manager.SetTaskID("task-1")
	if got := f.manager.CurrentTaskID(); got != "task-1" {
		t.Fatalf("CurrentTaskID = %q", got)
	}
	runnable.Abort()
	if got := f.manager.CurrentTaskID(); got != "" {
		t.Fatalf("CurrentTaskID after Abort = %q", got)
	}
}
