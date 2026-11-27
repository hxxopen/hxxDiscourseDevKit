package migration

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	devruntime "github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const testTarget = `\\wsl.localhost\DevKitTest\home\tester\hxxwork`

const testLegacyWSLRoot = `\\wsl.localhost\DevKitTest\home\tester\.local\share\hxxDiscourseDevKit`

type fakeRuntime struct {
	status devruntime.WSLStatus
	target string
	err    error
}

func (f *fakeRuntime) WSLStatus(context.Context) devruntime.WSLStatus { return f.status }

func (f *fakeRuntime) DefaultDataRoot(context.Context) (string, error) { return f.target, f.err }

type fakeWorkspaces struct {
	items []*workspace.Workspace
	err   error
}

func (f *fakeWorkspaces) List(context.Context) ([]*workspace.Workspace, error) {
	return f.items, f.err
}

type fakeProjects struct {
	live    []string
	stops   []string
	stopErr error
	order   *[]string
}

func (f *fakeProjects) Stop(_ context.Context, ref string, _ func(int, string)) error {
	f.stops = append(f.stops, ref)
	if f.order != nil {
		*f.order = append(*f.order, "workspace:"+ref)
	}
	return f.stopErr
}

func (f *fakeProjects) LiveProcesses() []string { return f.live }

type fakeServices struct {
	statuses []service.Status
	stops    []string
	stopErr  error
	order    *[]string
}

func (f *fakeServices) List(context.Context) []service.Status { return f.statuses }

func (f *fakeServices) Stop(_ context.Context, name service.Name, _ func(int, string)) error {
	f.stops = append(f.stops, string(name))
	if f.order != nil {
		*f.order = append(*f.order, "service:"+string(name))
	}
	return f.stopErr
}

type fakeTasks struct{ active bool }

func (f *fakeTasks) Active() bool { return f.active }

type fixture struct {
	paths      *config.Paths
	cfg        *config.Store
	runtime    *fakeRuntime
	workspaces *fakeWorkspaces
	projects   *fakeProjects
	services   *fakeServices
	tasks      *fakeTasks
	manager    *Manager
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	paths := config.NewPaths(root)
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	cfg, err := config.Open(paths)
	if err != nil {
		t.Fatalf("config.Open: %v", err)
	}
	f := &fixture{
		paths:      paths,
		cfg:        cfg,
		runtime:    &fakeRuntime{status: devruntime.WSLStatus{Available: true, Distro: "Ubuntu-24.04"}, target: testTarget},
		workspaces: &fakeWorkspaces{},
		projects:   &fakeProjects{},
		services:   &fakeServices{},
		tasks:      &fakeTasks{},
	}
	f.manager = New(Options{
		Paths:      paths,
		Config:     cfg,
		Runtime:    f.runtime,
		Workspaces: f.workspaces,
		Projects:   f.projects,
		Services:   f.services,
		Tasks:      f.tasks,
		Windows:    true,
	})
	return f
}

func requireWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("WSL data root migration is Windows-only")
	}
}

func TestPlanEligibility(t *testing.T) {
	requireWindows(t)

	t.Run("non-windows host", func(t *testing.T) {
		f := newFixture(t)
		f.manager.windows = false
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, "Windows") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("already split", func(t *testing.T) {
		f := newFixture(t)
		f.paths.SetDataRoot(filepath.Join(t.TempDir(), "elsewhere"))
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, "无需迁移") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("pending restart", func(t *testing.T) {
		f := newFixture(t)
		if err := f.cfg.Update(func(next *config.Config) error {
			next.Storage.DataRoot = `\\wsl.localhost\Ubuntu-24.04\home\tester\data`
			return nil
		}); err != nil {
			t.Fatalf("update: %v", err)
		}
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || plan.Reason != ErrPendingRestart.Error() {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("active task", func(t *testing.T) {
		f := newFixture(t)
		f.tasks.active = true
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, "后台任务") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("wsl unavailable", func(t *testing.T) {
		f := newFixture(t)
		f.runtime.status = devruntime.WSLStatus{Available: false, Error: "no distro"}
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, "no distro") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("target equals source", func(t *testing.T) {
		f := newFixture(t)
		f.runtime.target = f.paths.Root
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, "相同") {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("target on windows mount", func(t *testing.T) {
		f := newFixture(t)
		f.runtime.target = `\\wsl.localhost\Ubuntu-24.04\mnt\c\data`
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, ErrTargetInvalid.Error()) {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("eligible empty instance", func(t *testing.T) {
		f := newFixture(t)
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if !plan.Eligible || plan.TargetRoot != testTarget || plan.Distro != "Ubuntu-24.04" {
			t.Fatalf("plan = %+v", plan)
		}
		if plan.Units == nil || plan.Warnings == nil || plan.Blockers == nil ||
			plan.Workspaces == nil || plan.Services == nil {
			t.Fatalf("plan collections must not be nil: %+v", plan)
		}
		if plan.ConfirmToken != "" {
			t.Fatalf("plan() must not issue tokens")
		}
	})

	t.Run("target invalid unc", func(t *testing.T) {
		f := newFixture(t)
		f.runtime.target = `C:\DevKit\data`
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, ErrTargetInvalid.Error()) {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("target contains control characters", func(t *testing.T) {
		f := newFixture(t)
		f.runtime.target = "\\\\wsl.localhost\\Ubuntu-24.04\\wsl: noise\n/home/tester/hxxwork"
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || !strings.Contains(plan.Reason, ErrTargetInvalid.Error()) {
			t.Fatalf("plan = %+v", plan)
		}
	})

	t.Run("nil runtime", func(t *testing.T) {
		f := newFixture(t)
		f.manager.runtime = nil
		plan, err := f.manager.plan(context.Background())
		if err != nil {
			t.Fatalf("plan: %v", err)
		}
		if plan.Eligible || plan.Reason != ErrWSLUnavailable.Error() {
			t.Fatalf("plan = %+v", plan)
		}
	})
}

func newLocalPlan(t *testing.T, paths *config.Paths) *Plan {
	t.Helper()
	return &Plan{
		SourceRoot:     paths.Root,
		SourceDataRoot: paths.DataRoot,
		TargetRoot:     t.TempDir(),
		Legacy:         true,
		Units:          []Unit{},
		Workspaces:     []WorkspaceStop{},
		Services:       []ServiceStop{},
		Warnings:       []string{},
		Blockers:       []Blocker{},
	}
}

func writeWorkspace(t *testing.T, dir, id string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := store.WriteJSON(filepath.Join(dir, "workspace.json"),
		workspace.Workspace{WorkspaceID: id, Name: id, State: workspace.StateReady}, 0o600); err != nil {
		t.Fatalf("write workspace: %v", err)
	}
}

func TestWorkspacePlanClassification(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan := newLocalPlan(t, f.paths)

	writeWorkspace(t, filepath.Join(plan.SourceDataRoot, "workspaces", "w1"), "w1")
	writeWorkspace(t, filepath.Join(plan.SourceDataRoot, "workspaces", "w2"), "w2")
	writeWorkspace(t, filepath.Join(plan.SourceDataRoot, "workspaces", "w3"), "w3")
	writeWorkspace(t, filepath.Join(plan.SourceDataRoot, "workspaces", "w4"), "w4")
	writeWorkspace(t, filepath.Join(plan.TargetRoot, "workspaces", "w2"), "w2")
	if err := os.MkdirAll(filepath.Join(plan.TargetRoot, "workspaces", "w3"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plan.TargetRoot, "workspaces", "w4"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	writeWorkspace(t, filepath.Join(plan.SourceDataRoot, "workspaces", "w5"), "w5")

	f.workspaces.items = []*workspace.Workspace{
		{WorkspaceID: "w1", Name: "one", State: workspace.StateRunning},
		{WorkspaceID: "w2", Name: "two", State: workspace.StateReady},
		{WorkspaceID: "w3", Name: "three", State: workspace.StateReady},
		{WorkspaceID: "w4", Name: "four", State: workspace.StateReady},
		{WorkspaceID: "w5", Name: "five", State: workspace.StateInitializing},
		{WorkspaceID: "missing", Name: "gone", State: workspace.StateReady},
	}

	if err := f.manager.addWorkspacePlan(context.Background(), plan); err != nil {
		t.Fatalf("addWorkspacePlan: %v", err)
	}

	byID := map[string]Unit{}
	for _, unit := range plan.Units {
		byID[unit.ID] = unit
	}
	if got := byID["workspace:w1"].Action; got != ActionCopy {
		t.Fatalf("w1 action = %q", got)
	}
	if got := byID["workspace:w2"].Action; got != ActionSkip {
		t.Fatalf("w2 action = %q", got)
	}
	if got := byID["workspace:w3"].Action; got != ActionBlocked {
		t.Fatalf("w3 action = %q", got)
	}
	if got := byID["workspace:w4"].Action; got != ActionBlocked {
		t.Fatalf("w4 action = %q", got)
	}
	if got := byID["workspace:missing"].Action; got != ActionBlocked {
		t.Fatalf("missing action = %q", got)
	}
	if plan.TotalBytes <= 0 {
		t.Fatalf("TotalBytes = %d, want copy sizes only", plan.TotalBytes)
	}

	codes := map[string]bool{}
	for _, blocker := range plan.Blockers {
		codes[blocker.Code] = true
	}
	for _, want := range []string{
		BlockerTargetWorkspacePartial,
		BlockerSourceWorkspaceMissing,
		BlockerWorkspaceBusy,
	} {
		if !codes[want] {
			t.Fatalf("missing blocker %s in %+v", want, plan.Blockers)
		}
	}

	found := false
	for _, stop := range plan.Workspaces {
		if stop.WorkspaceID == "w1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("running workspace w1 not scheduled to stop: %+v", plan.Workspaces)
	}
}

func TestServicePlanClassification(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan := newLocalPlan(t, f.paths)
	cfg := config.Default(f.paths)
	cfg.Services.PostgreSQL.DataDir = "services/postgresql/data"
	cfg.Services.Redis.DataDir = filepath.Join("custom", "redis-data")
	if err := os.MkdirAll(filepath.Join(plan.SourceDataRoot, "services", "postgresql"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(plan.SourceDataRoot, "services", "redis"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(plan.SourceDataRoot, "custom", "redis-data"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plan.SourceDataRoot, "services", "postgresql", "PG_VERSION"),
		[]byte("17\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(plan.SourceDataRoot, "services", "redis", "dump.rdb"),
		[]byte("redis"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.services.statuses = []service.Status{
		{Name: "postgresql", Mode: "managed", State: service.StateRunning},
		{Name: "redis", Mode: "managed", State: service.StateStopped},
	}

	f.manager.addServicePlan(context.Background(), plan, cfg)

	units := map[string]Unit{}
	for _, unit := range plan.Units {
		units[unit.ID] = unit
	}
	if units["service:postgresql"].Action != ActionCopy {
		t.Fatalf("postgres unit = %+v", units["service:postgresql"])
	}
	if units["service:redis"].Action != ActionCopy {
		t.Fatalf("redis unit = %+v", units["service:redis"])
	}
	if units["service:redis:data"].Source != filepath.Join(plan.SourceDataRoot, "custom", "redis-data") {
		t.Fatalf("redis data unit = %+v", units["service:redis:data"])
	}

	actions := map[string]string{}
	for _, svc := range plan.Services {
		actions[svc.Name] = svc.Action
	}
	if actions["postgresql"] != "stop" || actions["redis"] != "stop" {
		t.Fatalf("service stop plan = %+v", plan.Services)
	}
	if plan.TotalBytes <= 0 {
		t.Fatalf("TotalBytes = %d", plan.TotalBytes)
	}
}

func TestServicePlanTargetActiveAndCustom(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan := newLocalPlan(t, f.paths)
	cfg := config.Default(f.paths)
	cfg.Services.PostgreSQL.DataDir = filepath.Join(plan.TargetRoot, "services", "postgresql", "data")
	cfg.Services.Redis.DataDir = filepath.Join(t.TempDir(), "external-redis")
	if err := os.MkdirAll(filepath.Join(plan.SourceDataRoot, "services", "postgresql"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	f.manager.addServicePlan(context.Background(), plan, cfg)

	units := map[string]Unit{}
	for _, unit := range plan.Units {
		units[unit.ID] = unit
	}
	if units["service:postgresql"].Action != ActionSkip {
		t.Fatalf("target-active postgres unit = %+v", units["service:postgresql"])
	}
	if _, ok := units["service:redis"]; ok {
		t.Fatalf("custom absolute redis should not be copied: %+v", units["service:redis"])
	}
	joined := strings.Join(plan.Warnings, "\n")
	if !strings.Contains(joined, "已使用 WSL2 数据目录") || !strings.Contains(joined, "自定义绝对数据目录") {
		t.Fatalf("warnings = %v", plan.Warnings)
	}
}

func TestServicePlanExternalAndEscape(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan := newLocalPlan(t, f.paths)
	cfg := config.Default(f.paths)
	cfg.Services.PostgreSQL.Mode = "external"
	cfg.Services.PostgreSQL.External = config.ExternalEndpoint{Host: "127.0.0.1", Port: 5432}
	cfg.Services.Redis.DataDir = filepath.Join("..", "escape", "data")

	f.manager.addServicePlan(context.Background(), plan, cfg)

	var externalSkipped bool
	for _, svc := range plan.Services {
		if svc.Name == "postgresql" && svc.Action == "skip" {
			externalSkipped = true
		}
	}
	if !externalSkipped {
		t.Fatalf("external service should be skipped: %+v", plan.Services)
	}
	if len(plan.Blockers) != 1 || plan.Blockers[0].Code != BlockerServiceDataDirEscape {
		t.Fatalf("blockers = %+v", plan.Blockers)
	}
}

func TestBackupPlanClassification(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan := newLocalPlan(t, f.paths)
	backups := filepath.Join(plan.SourceDataRoot, "backups")
	if err := os.MkdirAll(backups, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backups, "a.zip"), []byte("backup"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(plan.TargetRoot, "backups", "b.zip"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(backups, "b.zip"), []byte("backup"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	f.manager.addBackupPlan(plan)

	actions := map[string]string{}
	for _, unit := range plan.Units {
		actions[unit.ID] = unit.Action
	}
	if actions["backup:a.zip"] != ActionCopy {
		t.Fatalf("a.zip action = %q", actions["backup:a.zip"])
	}
	if actions["backup:b.zip"] != ActionSkip {
		t.Fatalf("b.zip action = %q", actions["backup:b.zip"])
	}
}

func TestPlanDigestIgnoresSizesButTracksStructure(t *testing.T) {
	base := &Plan{
		SourceRoot:     `C:\src`,
		SourceDataRoot: `C:\src`,
		TargetRoot:     testTarget,
		Distro:         "Ubuntu-24.04",
		Eligible:       true,
		Legacy:         true,
		Units: []Unit{
			{ID: "workspace:w1", Kind: KindWorkspace, Source: `C:\src\workspaces\w1`,
				Destination: testTarget + `\workspaces\w1`, Action: ActionCopy, SizeBytes: 10},
		},
	}
	mirror := &Plan{
		SourceRoot:     base.SourceRoot,
		SourceDataRoot: base.SourceDataRoot,
		TargetRoot:     base.TargetRoot,
		Distro:         base.Distro,
		Eligible:       base.Eligible,
		Legacy:         base.Legacy,
		Units: []Unit{
			{ID: "workspace:w1", Kind: KindWorkspace, Source: `C:\src\workspaces\w1`,
				Destination: testTarget + `\workspaces\w1`, Action: ActionCopy, SizeBytes: 999999},
		},
	}
	if planDigest(base) != planDigest(mirror) {
		t.Fatalf("file size must not affect the digest")
	}

	mirror.Units[0].Action = ActionSkip
	if planDigest(base) == planDigest(mirror) {
		t.Fatalf("unit action must affect the digest")
	}

	mirror.Units[0].Action = ActionCopy
	mirror.Blockers = append(mirror.Blockers, Blocker{Code: BlockerWorkspaceBusy, Message: "busy"})
	if planDigest(base) == planDigest(mirror) {
		t.Fatalf("blockers must affect the digest")
	}
}

func TestPlanCollectionsAreNonNil(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	plan, err := f.manager.plan(context.Background())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.Units == nil || plan.Workspaces == nil || plan.Services == nil ||
		plan.Warnings == nil || plan.Blockers == nil {
		t.Fatalf("nil collections: %+v", plan)
	}
}

func TestLegacyWSLDataRoot(t *testing.T) {
	got := legacyWSLDataRoot(devruntime.WSLStatus{Distro: "DevKitTest", Home: "/home/tester"})
	if got != testLegacyWSLRoot {
		t.Fatalf("legacyWSLDataRoot = %q, want %q", got, testLegacyWSLRoot)
	}
	if got := legacyWSLDataRoot(devruntime.WSLStatus{Distro: "DevKitTest"}); got != "" {
		t.Fatalf("legacyWSLDataRoot without home = %q, want empty", got)
	}
}

func TestClassifyServiceDataDir(t *testing.T) {
	requireWindows(t)
	sourceRoot := `C:\DevKit\root`
	targetRoot := testTarget
	legacyRoot := testLegacyWSLRoot

	cases := []struct {
		name       string
		dataDir    string
		resolved   string
		target     bool
		legacy     bool
		legacyWSL  bool
		customOnly bool
	}{
		{"relative", "services/redis/data", `C:\DevKit\root\services\redis\data`, false, true, false, false},
		{"legacy windows", `C:\DevKit\root\services\redis\data`, `C:\DevKit\root\services\redis\data`, false, true, false, false},
		{"target", targetRoot + `\services\postgresql\data`, targetRoot + `\services\postgresql\data`, true, false, false, false},
		{"previous wsl default", legacyRoot + `\services\postgresql\data`, legacyRoot + `\services\postgresql\data`, false, false, true, false},
		{"external", `D:\elsewhere\data`, `D:\elsewhere\data`, false, false, false, true},
		{"prefix lookalike", legacyRoot + `2\services\postgresql\data`, legacyRoot + `2\services\postgresql\data`, false, false, false, true},
	}
	for _, test := range cases {
		target, legacy, legacyWSL, customOnly := classifyServiceDataDir(
			test.dataDir, test.resolved, sourceRoot, targetRoot, legacyRoot)
		if target != test.target || legacy != test.legacy ||
			legacyWSL != test.legacyWSL || customOnly != test.customOnly {
			t.Errorf("%s: got (%v, %v, %v, %v), want (%v, %v, %v, %v)",
				test.name, target, legacy, legacyWSL, customOnly,
				test.target, test.legacy, test.legacyWSL, test.customOnly)
		}
	}
}

func TestAddServicePlanMovesPreviousWSLDefaultData(t *testing.T) {
	requireWindows(t)
	f := newFixture(t)
	legacyRoot := t.TempDir()
	dataDir := filepath.Join(legacyRoot, "services", "postgresql", "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "PG_VERSION"), []byte("16"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Update(func(next *config.Config) error {
		next.Services.PostgreSQL.DataDir = dataDir
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	targetRoot := t.TempDir()
	plan := &Plan{
		SourceRoot:     f.paths.Root,
		SourceDataRoot: f.paths.DataRoot,
		TargetRoot:     targetRoot,
		LegacyWSLRoot:  legacyRoot,
		Units:          []Unit{},
		Services:       []ServiceStop{},
		Warnings:       []string{},
		Blockers:       []Blocker{},
	}
	f.manager.addServicePlan(context.Background(), plan, f.cfg.Get())

	var unit *Unit
	for i := range plan.Units {
		if plan.Units[i].ID == "service:postgresql:data" {
			unit = &plan.Units[i]
		}
	}
	if unit == nil {
		t.Fatalf("postgresql data unit missing: %+v", plan.Units)
	}
	wantDest := filepath.Join(targetRoot, "services", "postgresql", "data")
	if unit.Action != ActionCopy || unit.Destination != wantDest {
		t.Fatalf("unit = %+v, want copy to %s", *unit, wantDest)
	}
	if len(plan.Blockers) != 0 {
		t.Fatalf("blockers = %+v", plan.Blockers)
	}
	if got := rebaseDataDir(dataDir, plan.SourceDataRoot, plan.LegacyWSLRoot); got != "services/postgresql/data" {
		t.Fatalf("rebase = %q", got)
	}
	var svc *ServiceStop
	for i := range plan.Services {
		if plan.Services[i].Name == "postgresql" {
			svc = &plan.Services[i]
		}
	}
	if svc == nil || svc.Action != "stop" {
		t.Fatalf("postgresql stop = %+v", plan.Services)
	}
}
