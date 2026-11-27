package api_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/api"
	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/migration"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
)

type fakeStorageMigrator struct {
	mu           sync.Mutex
	plan         *migration.Plan
	previewErr   error
	beginErr     error
	runnable     migration.Runnable
	migrating    bool
	taskID       string
	previewCount int
	beginCount   int
	lastToken    string
}

func (f *fakeStorageMigrator) Preview(context.Context) (*migration.Plan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previewCount++
	if f.previewErr != nil {
		return nil, f.previewErr
	}
	if f.plan == nil {
		return &migration.Plan{Eligible: true, ConfirmToken: "token-1"}, nil
	}
	return f.plan, nil
}

func (f *fakeStorageMigrator) Begin(_ context.Context, token string) (migration.Runnable, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.beginCount++
	f.lastToken = token
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	f.migrating = true
	return f.runnable, nil
}

func (f *fakeStorageMigrator) Migrating() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.migrating
}

func (f *fakeStorageMigrator) SetTaskID(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.migrating {
		f.taskID = id
	}
}

func (f *fakeStorageMigrator) CurrentTaskID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.taskID
}

func (f *fakeStorageMigrator) setMigrating(value bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.migrating = value
}

type blockingRunnable struct {
	mu      sync.Mutex
	release chan struct{}
	aborted bool
	run     func() (*migration.Result, error)
}

func newBlockingRunnable() *blockingRunnable {
	return &blockingRunnable{release: make(chan struct{})}
}

func (r *blockingRunnable) Execute(ctx context.Context, _ func(int, string)) (*migration.Result, error) {
	select {
	case <-r.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if r.run != nil {
		return r.run()
	}
	return &migration.Result{}, nil
}

func (r *blockingRunnable) Abort() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.aborted = true
}

func (r *blockingRunnable) wasAborted() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.aborted
}

func withStorageMigration(migrator api.StorageMigrator) func(*api.Options) {
	return func(opts *api.Options) { opts.StorageMigration = migrator }
}

func TestStorageMigrationPreview(t *testing.T) {
	migrator := &fakeStorageMigrator{plan: &migration.Plan{
		Eligible:     true,
		TargetRoot:   `\\wsl.localhost\Ubuntu-24.04\home\tester\.local\share\hxxDiscourseDevKit`,
		Distro:       "Ubuntu-24.04",
		ConfirmToken: "token-abc",
		Units:        []migration.Unit{},
		Warnings:     []string{},
		Blockers:     []migration.Blocker{},
	}}
	s := newStack(t, withStorageMigration(migrator))

	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", resp.StatusCode, body)
	}
	var plan migration.Plan
	decodeJSON(t, body, &plan)
	if !plan.Eligible || plan.ConfirmToken != "token-abc" || plan.Distro != "Ubuntu-24.04" {
		t.Fatalf("plan = %+v", plan)
	}
	if migrator.previewCount != 1 {
		t.Fatalf("preview count = %d", migrator.previewCount)
	}
}

func TestStorageMigrationPreviewMapsErrors(t *testing.T) {
	migrator := &fakeStorageMigrator{previewErr: migration.ErrWSLUnavailable}
	s := newStack(t, withStorageMigration(migrator))

	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "STORAGE_MIGRATION_WSL_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}

	migrator.previewErr = migration.ErrNotEligible
	resp, body = s.do(http.MethodPost, "/api/v1/settings/storage/migration/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "STORAGE_MIGRATION_NOT_ELIGIBLE" {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestStorageMigrationFeatureUnavailable(t *testing.T) {
	s := newStack(t)
	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "FEATURE_UNAVAILABLE" {
		t.Fatalf("code = %s", code)
	}

	info := fetchInfo(t, s)
	if info.Capabilities["storage_migration"] {
		t.Fatalf("storage_migration capability should be false")
	}
}

func TestStorageMigrationStartRequiresToken(t *testing.T) {
	migrator := &fakeStorageMigrator{runnable: newBlockingRunnable()}
	s := newStack(t, withStorageMigration(migrator))

	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration",
		map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, body) != "PREVIEW_REQUIRED" {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	if migrator.beginCount != 0 {
		t.Fatalf("Begin called without token")
	}

	migrator.beginErr = migration.ErrConfirmToken
	resp, body = s.do(http.MethodPost, "/api/v1/settings/storage/migration",
		map[string]string{"confirm_token": "expired"}, s.tokenAuth)
	if resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, body) != "PREVIEW_REQUIRED" {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestStorageMigrationStartSubmitsTask(t *testing.T) {
	runID := "run-1"
	runnable := newBlockingRunnable()
	migrator := &fakeStorageMigrator{runnable: runnable}
	s := newStack(t, withStorageMigration(migrator))

	run := func() (*migration.Result, error) {
		migrator.setMigrating(false)
		return &migration.Result{RunID: runID}, nil
	}
	runnable.run = run

	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration",
		map[string]string{"confirm_token": "token-abc"}, s.tokenAuth)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var accepted struct {
		TaskID string    `json:"task_id"`
		Task   task.Task `json:"task"`
	}
	decodeJSON(t, body, &accepted)
	if accepted.TaskID == "" || accepted.Task.TaskID != accepted.TaskID {
		t.Fatalf("accepted = %+v", accepted)
	}
	if accepted.Task.Type != migration.TaskType {
		t.Fatalf("task type = %q", accepted.Task.Type)
	}
	if accepted.Task.WorkspaceID != "" {
		t.Fatalf("workspace id = %q, want empty", accepted.Task.WorkspaceID)
	}
	if !accepted.Task.Cancelable {
		t.Fatalf("migration task must be cancelable")
	}
	if migrator.lastToken != "token-abc" {
		t.Fatalf("Begin token = %q", migrator.lastToken)
	}
	if migrator.CurrentTaskID() != accepted.TaskID {
		t.Fatalf("recorded task ID = %q, want %q", migrator.CurrentTaskID(), accepted.TaskID)
	}

	close(runnable.release)
	finished := s.waitTask(accepted.TaskID)
	if finished.Status != task.StatusSucceeded {
		t.Fatalf("task status = %+v", finished)
	}

	page := auditList(t, s, "?action=settings.storage.migration.start")
	if page.Total != 1 {
		t.Fatalf("audit events = %+v", page)
	}
	if page.Items[0].Result != "accepted" || page.Items[0].Status != http.StatusAccepted {
		t.Fatalf("audit event = %+v", page.Items[0])
	}
}

func TestStorageMigrationMaintenanceGuard(t *testing.T) {
	migrator := &fakeStorageMigrator{migrating: true}
	s := newStack(t, withStorageMigration(migrator))

	resp, body := s.do(http.MethodPatch, "/api/v1/settings",
		map[string]any{"logging": map[string]string{"level": "debug"}}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "MAINTENANCE_IN_PROGRESS" {
		t.Fatalf("PATCH status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/settings/storage/migration/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "MAINTENANCE_IN_PROGRESS" {
		t.Fatalf("preview status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/settings/storage/migration",
		map[string]string{"confirm_token": "token"}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "MAINTENANCE_IN_PROGRESS" {
		t.Fatalf("start status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/settings", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET settings status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET info status = %d, body = %s", resp.StatusCode, body)
	}

	// Cancellation must remain reachable during maintenance. The unknown task
	// yields 404, which proves the request reached the handler.
	resp, body = s.do(http.MethodPost, "/api/v1/tasks/nope/cancel", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("cancel status = %d, body = %s", resp.StatusCode, body)
	}
}

type infoSnapshot struct {
	Storage struct {
		DataRoot        string `json:"data_root"`
		Configured      bool   `json:"configured"`
		Split           bool   `json:"split"`
		Legacy          bool   `json:"legacy"`
		DataRootPending bool   `json:"data_root_pending"`
		Migrating       bool   `json:"migrating"`
		MigrationTaskID string `json:"migration_task_id"`
	} `json:"storage"`
	Capabilities map[string]bool `json:"capabilities"`
}

func fetchInfo(t *testing.T, s *stack) infoSnapshot {
	t.Helper()
	resp, body := s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET info status = %d, body = %s", resp.StatusCode, body)
	}
	var info infoSnapshot
	decodeJSON(t, body, &info)
	return info
}

func TestStorageInfoShowsMigrationState(t *testing.T) {
	migrator := &fakeStorageMigrator{migrating: true, taskID: "task-42"}
	s := newStack(t, withStorageMigration(migrator))

	info := fetchInfo(t, s)
	if !info.Capabilities["storage_migration"] {
		t.Fatalf("storage_migration capability missing")
	}
	if !info.Storage.Migrating || info.Storage.MigrationTaskID != "task-42" {
		t.Fatalf("storage = %+v", info.Storage)
	}
}

func TestSettingsReportsPendingDataRoot(t *testing.T) {
	s := newStack(t)
	target := `\\wsl.localhost\Ubuntu-24.04\home\tester\.local\share\hxxDiscourseDevKit`
	if err := s.cfg.Update(func(next *config.Config) error {
		next.Storage.DataRoot = target
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	resp, body := s.do(http.MethodGet, "/api/v1/settings", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var snapshot struct {
		Paths struct {
			DataRootPending bool `json:"data_root_pending"`
		} `json:"paths"`
		RestartRequired []string `json:"restart_required"`
	}
	decodeJSON(t, body, &snapshot)
	if !snapshot.Paths.DataRootPending {
		t.Fatalf("paths.data_root_pending = false, body = %s", body)
	}
	found := false
	for _, field := range snapshot.RestartRequired {
		if field == "storage.data_root" {
			found = true
		}
	}
	if !found {
		t.Fatalf("restart_required = %v", snapshot.RestartRequired)
	}

	info := fetchInfo(t, s)
	if !info.Storage.DataRootPending || !info.Storage.Configured {
		t.Fatalf("storage = %+v", info.Storage)
	}
}

func TestStorageMigrationTaskFailureIsReported(t *testing.T) {
	migrator := &fakeStorageMigrator{}
	s := newStack(t, withStorageMigration(migrator))

	// A runnable that is already cancelled makes Execute fail immediately, so
	// the submitted task finishes before SetTaskID runs.
	runnable := newBlockingRunnable()
	close(runnable.release)
	migrator.runnable = runnable
	runnable.run = func() (*migration.Result, error) {
		return nil, errors.New("boom")
	}

	resp, body := s.do(http.MethodPost, "/api/v1/settings/storage/migration",
		map[string]string{"confirm_token": "token"}, s.tokenAuth)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, body)
	}
	var accepted struct {
		TaskID string `json:"task_id"`
	}
	decodeJSON(t, body, &accepted)
	finished := s.waitTask(accepted.TaskID)
	if finished.Status != task.StatusFailed {
		t.Fatalf("task = %+v", finished)
	}
	if !strings.Contains(finished.ErrorMessage, "boom") {
		t.Fatalf("error = %+v", finished)
	}
}
