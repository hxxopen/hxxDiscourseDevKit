package profile

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

type fakeRuntime struct {
	statuses map[string]runtime.ComponentStatus
	errs     map[string]error
}

func (f fakeRuntime) Verify(ctx context.Context, name string) (runtime.ComponentStatus, error) {
	if err := f.errs[name]; err != nil {
		return runtime.ComponentStatus{}, err
	}
	status, ok := f.statuses[name]
	if !ok {
		return runtime.ComponentStatus{}, errors.New("unknown component")
	}
	return status, nil
}

type fakeServices struct {
	statuses map[service.Name]service.Status
	errs     map[service.Name]error
}

func (f fakeServices) Status(ctx context.Context, name service.Name) (service.Status, error) {
	if err := f.errs[name]; err != nil {
		return service.Status{}, err
	}
	status, ok := f.statuses[name]
	if !ok {
		return service.Status{}, errors.New("unknown service")
	}
	return status, nil
}

func healthyRuntime() fakeRuntime {
	return fakeRuntime{statuses: map[string]runtime.ComponentStatus{
		"ruby": {Name: "ruby", Available: true, VersionCompatible: true, Verified: true, ActualVersion: "3.3.6", Execution: "native"},
		"node": {Name: "node", Available: true, VersionCompatible: true, Verified: true, ActualVersion: "20.18.0", Execution: "native"},
		"git":  {Name: "git", Available: true, VersionCompatible: true, Verified: true, ActualVersion: "2.45.2", Execution: "native"},
	}}
}

func healthyServices() fakeServices {
	return fakeServices{statuses: map[service.Name]service.Status{
		service.PostgreSQL: {Name: "postgresql", State: service.StateRunning, Authenticated: true, Host: "127.0.0.1", Port: 15432},
		service.Redis:      {Name: "redis", State: service.StateRunning, Authenticated: true, Host: "127.0.0.1", Port: 16379},
	}}
}

func TestCatalogBuiltins(t *testing.T) {
	catalog := New(nil, nil)
	items := catalog.List()
	if len(items) != 5 {
		t.Fatalf("List() returned %d profiles, want 5", len(items))
	}
	want := []string{
		"discourse-official",
		"discourse-plugin",
		"discourse-theme",
		"discourse-custom",
		"hxx-agentforge",
	}
	for i, id := range want {
		if items[i].ID != id {
			t.Errorf("profile %d = %q, want %q", i, items[i].ID, id)
		}
		if items[i].Origin.Type != "builtin" || !items[i].Origin.Verified {
			t.Errorf("profile %s origin = %+v", id, items[i].Origin)
		}
		if len(items[i].Operations) == 0 || len(items[i].Permissions) == 0 {
			t.Errorf("profile %s is missing operations or permissions", id)
		}
	}
	if items[0].DefaultRepositoryURL == "" || items[0].DefaultBranch != "main" {
		t.Errorf("official defaults = %q @ %q", items[0].DefaultRepositoryURL, items[0].DefaultBranch)
	}
	if items[3].DefaultRepositoryURL != "" || !items[3].SourceRequired {
		t.Errorf("custom source defaults = %q, required=%v", items[3].DefaultRepositoryURL, items[3].SourceRequired)
	}
	if items[1].ExtensionKind != "plugin" || items[2].ExtensionKind != "theme" {
		t.Errorf("extension kinds = %q, %q", items[1].ExtensionKind, items[2].ExtensionKind)
	}

	items[0].Operations[0].Title = "mutated"
	again, err := catalog.Get("discourse-official")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.Operations[0].Title == "mutated" {
		t.Fatal("List returned shared slices")
	}
}

func TestCatalogGet(t *testing.T) {
	catalog := New(nil, nil)
	got, err := catalog.Get("  DISCOURSE-PLUGIN ")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "discourse-plugin" {
		t.Fatalf("Get returned %q", got.ID)
	}
	_, err = catalog.Get("missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get missing error = %v, want ErrNotFound", err)
	}
}

func TestCheckHealthyProfile(t *testing.T) {
	catalog := New(healthyRuntime(), healthyServices())
	report, err := catalog.Check(context.Background(), "discourse-official")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Ready || report.ErrorCount != 0 || report.WarningCount != 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.ProfileID != "discourse-official" || report.ProfileName == "" {
		t.Fatalf("report identity = %+v", report)
	}
	if len(report.Checks) != 7 {
		t.Fatalf("checks = %d, want 7: %+v", len(report.Checks), report.Checks)
	}
	for _, check := range report.Checks {
		if check.Status != StatusOK && check.Status != StatusInfo {
			t.Errorf("check %s = %s: %s", check.ID, check.Status, check.Message)
		}
	}
}

func TestCheckMissingRuntimeIsError(t *testing.T) {
	runtimeChecker := healthyRuntime()
	runtimeChecker.statuses["ruby"] = runtime.ComponentStatus{
		Name: "ruby", Available: false, Message: "未在 PATH 中找到 ruby",
	}
	catalog := New(runtimeChecker, healthyServices())
	report, err := catalog.Check(context.Background(), "discourse-official")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if report.Ready || report.ErrorCount != 1 {
		t.Fatalf("report = %+v", report)
	}
	check := findCheck(t, report, "runtime.ruby")
	if check.Status != StatusError || !strings.Contains(check.Message, "未在 PATH") {
		t.Fatalf("ruby check = %+v", check)
	}
	if check.FixAction == "" {
		t.Fatal("missing ruby check has no fix action")
	}
}

func TestCheckStoppedServicesAreWarnings(t *testing.T) {
	services := healthyServices()
	services.statuses[service.PostgreSQL] = service.Status{
		Name: "postgresql", State: service.StateStopped, Message: "数据目录尚未初始化",
	}
	catalog := New(healthyRuntime(), services)
	report, err := catalog.Check(context.Background(), "discourse-official")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Ready || report.ErrorCount != 0 || report.WarningCount != 1 {
		t.Fatalf("report = %+v", report)
	}
	check := findCheck(t, report, "service.postgresql")
	if check.Status != StatusWarning || !strings.Contains(check.Message, "尚未启动") {
		t.Fatalf("postgres check = %+v", check)
	}
}

func TestCheckNilCheckersAndCustomSource(t *testing.T) {
	catalog := New(nil, nil)
	report, err := catalog.Check(context.Background(), "discourse-custom")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !report.Ready {
		t.Fatalf("nil checkers should not fail readiness: %+v", report)
	}
	// 3 runtime + 2 services + 1 source.
	if report.WarningCount != 6 {
		t.Fatalf("warnings = %d, want 6: %+v", report.WarningCount, report.Checks)
	}
	source := findCheck(t, report, "source.repository")
	if source.Status != StatusWarning || !strings.Contains(source.Message, "必须提供仓库地址") {
		t.Fatalf("source check = %+v", source)
	}
}

func TestCheckInvalidProfile(t *testing.T) {
	catalog := New(nil, nil)
	_, err := catalog.Check(context.Background(), "nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Check error = %v, want ErrNotFound", err)
	}
}

func findCheck(t *testing.T, report *CheckReport, id string) Check {
	t.Helper()
	for _, check := range report.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("check %q not found in %+v", id, report.Checks)
	return Check{}
}
