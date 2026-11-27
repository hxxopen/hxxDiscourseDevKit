package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedClock(value time.Time) func() time.Time {
	return func() time.Time { return value }
}

func TestRecordAndListNewestFirst(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, 30)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	logger.now = fixedClock(base.Add(2 * time.Hour))

	events := []Event{
		{Timestamp: base, Action: "workspace.create", Result: "success", Status: 201, Method: "POST", Path: "/api/v1/workspaces", Client: "cli"},
		{Timestamp: base.Add(time.Minute), Action: "project.start", Result: "accepted", Status: 202, Method: "POST", Path: "/api/v1/workspaces/A/start", Client: "ui", WorkspaceID: "A"},
		{Timestamp: base.Add(2 * time.Minute), Action: "workspace.create", Result: "failure", Status: 400, Method: "POST", Path: "/api/v1/workspaces", Client: "api"},
	}
	for _, event := range events {
		if err := logger.Record(event); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	page, err := logger.List(Filter{Limit: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("Total = %d, want 3", page.Total)
	}
	if len(page.Items) != 2 {
		t.Fatalf("len(Items) = %d, want 2", len(page.Items))
	}
	if page.Items[0].Action != "workspace.create" || page.Items[0].Result != "failure" {
		t.Fatalf("first event = %+v", page.Items[0])
	}
	if page.Items[1].Action != "project.start" {
		t.Fatalf("second event = %+v", page.Items[1])
	}
	if page.Items[0].ID == "" || page.Items[0].Actor != "local" {
		t.Fatalf("event defaults not applied: %+v", page.Items[0])
	}
}

func TestListFilters(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, 30)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	base := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	logger.now = fixedClock(base.Add(5 * time.Hour))

	entries := []Event{
		{Timestamp: base, Action: "workspace.create", Result: "success", Status: 201, Method: "POST", Path: "/", WorkspaceID: "A", Client: "cli"},
		{Timestamp: base.Add(time.Hour), Action: "workspace.create", Result: "failure", Status: 400, Method: "POST", Path: "/", WorkspaceID: "B", Client: "ui"},
		{Timestamp: base.Add(2 * time.Hour), Action: "backup.create", Result: "accepted", Status: 202, Method: "POST", Path: "/", WorkspaceID: "A", Client: "ui"},
	}
	for _, event := range entries {
		if err := logger.Record(event); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	page, err := logger.List(Filter{Action: "workspace.create"})
	if err != nil {
		t.Fatalf("List action: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("action filter = %+v", page)
	}

	page, err = logger.List(Filter{WorkspaceID: "A", Result: "accepted"})
	if err != nil {
		t.Fatalf("List workspace/result: %v", err)
	}
	if page.Total != 1 || page.Items[0].Action != "backup.create" {
		t.Fatalf("workspace/result filter = %+v", page)
	}

	page, err = logger.List(Filter{Since: base.Add(90 * time.Minute)})
	if err != nil {
		t.Fatalf("List since: %v", err)
	}
	if page.Total != 1 || page.Items[0].Action != "backup.create" {
		t.Fatalf("since filter = %+v", page)
	}

	page, err = logger.List(Filter{Limit: 9999})
	if err != nil {
		t.Fatalf("List limit: %v", err)
	}
	if page.Limit != maxLimit {
		t.Fatalf("Limit = %d, want %d", page.Limit, maxLimit)
	}
}

func TestMalformedLinesAreSkipped(t *testing.T) {
	dir := t.TempDir()
	logger, err := New(dir, 30)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	path := filepath.Join(dir, filePrefix+"2026-11-27"+fileSuffix)
	content := "not json\n" +
		`{"id":"one","timestamp":"2026-11-27T10:00:00Z","action":"workspace.create","result":"success"}` + "\n" +
		`{"id":"two","timestamp":"2026-11-27T11:00:00Z","action":"project.start","result":"accepted"}` + "\n" +
		`{"id":"truncated"`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	page, err := logger.List(Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	if page.Items[0].Action != "project.start" {
		t.Fatalf("first = %+v", page.Items[0])
	}
}

func TestRetentionRemovesOldDailyFiles(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, filePrefix+"2020-01-01"+fileSuffix)
	if err := os.WriteFile(oldPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	logger, err := New(dir, 30)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	logger.now = fixedClock(now)
	logger.lastPrune = time.Time{}
	if err := logger.Record(Event{Action: "workspace.create"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, filePrefix+"2026-11-27"+fileSuffix)); err != nil {
		t.Fatalf("current file missing: %v", err)
	}
}

func TestRecordRejectsEmptyAction(t *testing.T) {
	logger, err := New(t.TempDir(), 30)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := logger.Record(Event{}); err == nil || !strings.Contains(err.Error(), "action") {
		t.Fatalf("Record error = %v", err)
	}
}

func TestListMissingDirectoryIsEmpty(t *testing.T) {
	logger := &Logger{dir: filepath.Join(t.TempDir(), "missing"), now: time.Now}
	page, err := logger.List(Filter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(page.Items) != 0 || page.Total != 0 {
		t.Fatalf("page = %+v", page)
	}
}
