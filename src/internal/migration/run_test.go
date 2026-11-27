package migration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type fakeCopier struct {
	ensured   []string
	copies    [][2]string
	moves     [][2]string
	removes   []string
	free      int64
	copyErr   error
	moveErr   error
	removeErr error
	order     *[]string
	// removeCtxErr records whether the context passed to RemoveAll was
	// already cancelled.
	removeCtxErr error
}

func (c *fakeCopier) EnsureDir(ctx context.Context, path string, _ os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.ensured = append(c.ensured, path)
	return os.MkdirAll(path, 0o755)
}

func (c *fakeCopier) CopyTree(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.copyErr != nil {
		return c.copyErr
	}
	c.copies = append(c.copies, [2]string{src, dst})
	if c.order != nil {
		*c.order = append(*c.order, "copy:"+filepath.Base(src))
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "marker"), []byte("copied"), 0o600)
}

func (c *fakeCopier) Move(ctx context.Context, src, dst string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.moveErr != nil {
		return c.moveErr
	}
	c.moves = append(c.moves, [2]string{src, dst})
	return os.Rename(src, dst)
}

func (c *fakeCopier) RemoveAll(ctx context.Context, path string) error {
	c.removeCtxErr = ctx.Err()
	c.removes = append(c.removes, path)
	if c.removeErr != nil {
		return c.removeErr
	}
	return os.RemoveAll(path)
}

func (c *fakeCopier) FreeBytes(context.Context, string) (int64, error) {
	return c.free, nil
}

func attachCopier(f *fixture, copier *fakeCopier) {
	f.manager.newCopier = func(string) Copier { return copier }
}

func runPlan(t *testing.T, f *fixture) *Plan {
	t.Helper()
	target := t.TempDir()
	source := filepath.Join(f.paths.WorkspacesDir, "w1")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	return &Plan{
		SourceRoot:     f.paths.Root,
		SourceDataRoot: f.paths.DataRoot,
		TargetRoot:     target,
		Distro:         "Ubuntu-24.04",
		Eligible:       true,
		Legacy:         true,
		Units: []Unit{{
			ID: "workspace:w1", Kind: KindWorkspace, Name: "w1",
			Source: source, Destination: filepath.Join(target, "workspaces", "w1"),
			Action: ActionCopy, SizeBytes: 4,
		}},
		Workspaces: []WorkspaceStop{},
		Services:   []ServiceStop{},
		Warnings:   []string{},
		Blockers:   []Blocker{},
		TotalBytes: 4,
	}
}

func reserve(f *fixture, runID string) {
	f.manager.migrating = true
	f.manager.runID = runID
}

func TestExecuteStopsWorkspacesBeforeServicesBeforeCopy(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)

	order := []string{}
	f.projects.order = &order
	f.services.order = &order
	copier.order = &order

	plan := runPlan(t, f)
	plan.Workspaces = []WorkspaceStop{{WorkspaceID: "w1", Name: "w1"}}
	plan.Services = []ServiceStop{{Name: "redis", Action: "stop"}}
	f.projects.live = nil
	f.services.statuses = nil

	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	result, err := run.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.WorkspacesStopped) != 1 || result.WorkspacesStopped[0] != "w1" {
		t.Fatalf("workspaces stopped = %v", result.WorkspacesStopped)
	}
	if len(result.ServicesStopped) != 1 || result.ServicesStopped[0] != "redis" {
		t.Fatalf("services stopped = %v", result.ServicesStopped)
	}
	want := []string{"workspace:w1", "service:redis", "copy:w1"}
	if strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if len(copier.copies) != 1 {
		t.Fatalf("copies = %v", copier.copies)
	}
}

func TestExecuteAbortsWhenLiveProcessesRemain(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)
	f.projects.live = []string{"w1"}

	run := &run{manager: f.manager, plan: runPlan(t, f), runID: "run-1"}
	_, err := run.Execute(context.Background(), nil)
	if !errors.Is(err, ErrStopFailed) {
		t.Fatalf("error = %v, want ErrStopFailed", err)
	}
	if len(copier.copies) != 0 {
		t.Fatalf("copy ran despite live processes: %v", copier.copies)
	}
	if got := f.cfg.Get().Storage.DataRoot; got != "" {
		t.Fatalf("config changed on failure: %q", got)
	}
}

func TestExecuteCopyFailureKeepsConfigAndClearsReservation(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30, copyErr: errors.New("disk full")}
	attachCopier(f, copier)
	reserve(f, "run-1")

	run := &run{manager: f.manager, plan: runPlan(t, f), runID: "run-1"}
	_, err := run.Execute(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error = %v", err)
	}
	if got := f.cfg.Get().Storage.DataRoot; got != "" {
		t.Fatalf("config changed on failure: %q", got)
	}
	if f.manager.Migrating() {
		t.Fatalf("reservation was not cleared")
	}
}

func TestExecuteInsufficientSpaceFailsBeforeCopy(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1}
	attachCopier(f, copier)

	run := &run{manager: f.manager, plan: runPlan(t, f), runID: "run-1"}
	_, err := run.Execute(context.Background(), nil)
	if !errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("error = %v, want ErrInsufficientSpace", err)
	}
	if len(copier.copies) != 0 {
		t.Fatalf("copies = %v", copier.copies)
	}
}

func TestExecuteCommitsDataRootAndRebasesLegacyAbsoluteDataDir(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)

	legacyPG := filepath.Join(f.paths.DataRoot, "services", "postgresql", "data")
	if err := f.cfg.Update(func(next *config.Config) error {
		next.Services.PostgreSQL.DataDir = legacyPG
		next.Services.Redis.DataDir = "services/redis/data"
		return nil
	}); err != nil {
		t.Fatalf("update config: %v", err)
	}

	plan := runPlan(t, f)
	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	result, err := run.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.CommittedAt.IsZero() {
		t.Fatalf("CommittedAt not set")
	}

	cfg := f.cfg.Get()
	if cfg.Storage.DataRoot != plan.TargetRoot {
		t.Fatalf("storage.data_root = %q, want %q", cfg.Storage.DataRoot, plan.TargetRoot)
	}
	if cfg.Services.PostgreSQL.DataDir != "services/postgresql/data" {
		t.Fatalf("postgres data dir = %q", cfg.Services.PostgreSQL.DataDir)
	}
	if cfg.Services.Redis.DataDir != "services/redis/data" {
		t.Fatalf("redis data dir = %q", cfg.Services.Redis.DataDir)
	}
}

func TestExecuteCreatesParentDirectoryForServiceData(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)

	dataSource := filepath.Join(t.TempDir(), "pgdata")
	if err := os.MkdirAll(dataSource, 0o700); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	plan := runPlan(t, f)
	plan.Units = []Unit{{
		ID: "service:postgresql:data", Kind: KindService, Name: "postgresql",
		Source:      dataSource,
		Destination: filepath.Join(plan.TargetRoot, "services", "postgresql", "data"),
		Action:      ActionCopy,
		SizeBytes:   4,
	}}
	plan.TotalBytes = 4
	plan.Workspaces = []WorkspaceStop{}
	plan.Services = []ServiceStop{}

	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	result, err := run.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.UnitsCopied) != 1 || result.UnitsCopied[0] != "service:postgresql:data" {
		t.Fatalf("copied = %v", result.UnitsCopied)
	}
	if _, err := os.Stat(filepath.Join(plan.TargetRoot, "services", "postgresql", "data", "marker")); err != nil {
		t.Fatalf("nested service data was not moved into place: %v", err)
	}
}

func TestExecuteSkipsWorkspaceCompletedByEarlierRun(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)

	plan := runPlan(t, f)
	unit := plan.Units[0]
	if err := os.MkdirAll(unit.Destination, 0o755); err != nil {
		t.Fatalf("mkdir destination: %v", err)
	}
	if err := store.WriteJSON(filepath.Join(unit.Destination, "workspace.json"),
		workspace.Workspace{WorkspaceID: "w1"}, 0o600); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	result, err := run.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(copier.copies) != 0 {
		t.Fatalf("already completed workspace was copied: %v", copier.copies)
	}
	if len(result.UnitsSkipped) != 1 || result.UnitsSkipped[0] != "workspace:w1" {
		t.Fatalf("skipped = %v", result.UnitsSkipped)
	}
}

func TestExecuteStopsWhenContextCancelled(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)
	reserve(f, "run-1")

	plan := runPlan(t, f)
	plan.Workspaces = []WorkspaceStop{{WorkspaceID: "w1", Name: "w1"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	if _, err := run.Execute(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(copier.copies) != 0 || len(copier.moves) != 0 {
		t.Fatalf("copy ran after cancellation")
	}
	if got := f.cfg.Get().Storage.DataRoot; got != "" {
		t.Fatalf("config changed after cancellation: %q", got)
	}
	if f.manager.Migrating() {
		t.Fatalf("reservation was not cleared")
	}
}

func TestCleanupIgnoresCancelledContext(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}

	plan := runPlan(t, f)
	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := &Result{}
	run.cleanup(ctx, copier, result)
	if copier.removeCtxErr != nil {
		t.Fatalf("cleanup used a cancelled context: %v", copier.removeCtxErr)
	}
	if len(copier.removes) != 1 || copier.removes[0] != run.stagingDir() {
		t.Fatalf("removes = %v", copier.removes)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestRebaseDataDir(t *testing.T) {
	requireWindows(t)
	source := `C:\DevKit\root`
	legacyWSL := `\\wsl.localhost\Ubuntu-24.04\home\tester\.local\share\hxxDiscourseDevKit`

	if got := rebaseDataDir(`C:\DevKit\root\services\redis\data`, source, legacyWSL); got != "services/redis/data" {
		t.Fatalf("legacy absolute rebase = %q", got)
	}
	if got := rebaseDataDir(`c:\devkit\ROOT\services\redis\data`, source, legacyWSL); got != "services/redis/data" {
		t.Fatalf("case-insensitive rebase = %q", got)
	}
	if got := rebaseDataDir(legacyWSL+`\services\postgresql\data`, source, legacyWSL); got != "services/postgresql/data" {
		t.Fatalf("previous WSL default rebase = %q", got)
	}
	targetAbs := `\\wsl.localhost\Ubuntu-24.04\home\tester\hxxwork\services\postgresql\data`
	if got := rebaseDataDir(targetAbs, source, legacyWSL); got != targetAbs {
		t.Fatalf("target absolute changed to %q", got)
	}
	if got := rebaseDataDir(`D:\elsewhere\data`, source, legacyWSL); got != `D:\elsewhere\data` {
		t.Fatalf("external absolute changed to %q", got)
	}
	if got := rebaseDataDir(`\\wsl.localhost\Ubuntu-24.04\home\tester\.local\share\hxxDiscourseDevKit\services\redis\data`, source, ""); got != `\\wsl.localhost\Ubuntu-24.04\home\tester\.local\share\hxxDiscourseDevKit\services\redis\data` {
		t.Fatalf("path changed without legacy root: %q", got)
	}
	if got := rebaseDataDir("services/redis/data", source, legacyWSL); got != "services/redis/data" {
		t.Fatalf("relative path changed to %q", got)
	}
	if got := rebaseDataDir("", source, legacyWSL); got != "" {
		t.Fatalf("empty path = %q", got)
	}
}

func TestServiceStopToleratesAlreadyStopped(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	copier := &fakeCopier{free: 1 << 30}
	attachCopier(f, copier)
	f.services.stopErr = service.ErrNotRunning

	plan := runPlan(t, f)
	plan.Services = []ServiceStop{{Name: "redis", Action: "stop"}}
	run := &run{manager: f.manager, plan: plan, runID: "run-1"}
	if _, err := run.Execute(context.Background(), nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
}
