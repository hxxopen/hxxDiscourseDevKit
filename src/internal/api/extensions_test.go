package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type fakePluginTestRunner struct {
	mu    sync.Mutex
	calls int
	plan  extension.PluginTestPlan
	err   error
}

func (f *fakePluginTestRunner) RunPluginTest(_ context.Context, plan extension.PluginTestPlan,
	progress func(int, string)) error {
	progress(50, "运行测试")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.plan = plan
	return f.err
}

func (f *fakePluginTestRunner) snapshot() (int, extension.PluginTestPlan) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.plan
}

func TestThemeExtensionRoutes(t *testing.T) {
	s := newStack(t)
	ws := s.create("Theme Workspace")
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/themes"

	resp, body := s.do(http.MethodPost, base, map[string]string{
		"name":    "API Theme",
		"about":   "Created through the API",
		"version": "0.3.0",
		"authors": "API Tester",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create theme status = %d, body = %s", resp.StatusCode, body)
	}
	var created extension.Item
	decodeJSON(t, body, &created)
	if created.Slug != "api-theme" || created.Name != "API Theme" || !created.HasMetadata {
		t.Fatalf("unexpected theme: %+v", created)
	}

	resp, body = s.do(http.MethodGet, base, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list themes status = %d, body = %s", resp.StatusCode, body)
	}
	var listing extension.Listing
	decodeJSON(t, body, &listing)
	if listing.Total != 1 || listing.Kind != extension.KindTheme || listing.SourceInstalled {
		t.Fatalf("unexpected listing: %+v", listing)
	}
	if listing.Root != filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "themes") {
		t.Fatalf("root = %q", listing.Root)
	}

	resp, body = s.do(http.MethodPost, base, map[string]string{"name": "API Theme"}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_EXISTS" {
		t.Fatalf("duplicate error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, base, map[string]string{"name": "Bad", "slug": "../escape"}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid slug status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_VALIDATION_FAILED" {
		t.Fatalf("invalid slug error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, base+"/api-theme/package", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("package theme status = %d, body = %s", resp.StatusCode, body)
	}
	var pkg extension.Package
	decodeJSON(t, body, &pkg)
	wantPackage := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "exports", "themes", "api-theme-0.3.0.zip")
	if pkg.Path != wantPackage || pkg.SHA256 == "" || pkg.FileCount == 0 {
		t.Fatalf("unexpected package: %+v", pkg)
	}
	if _, err := os.Stat(pkg.Path); err != nil {
		t.Fatalf("package missing on disk: %v", err)
	}

	resp, body = s.do(http.MethodPost, base+"/missing-theme/package", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing package status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_NOT_FOUND" {
		t.Fatalf("missing package error code = %q", code)
	}
}

func TestPluginExtensionRequiresInstalledSource(t *testing.T) {
	s := newStack(t)
	ws := s.create("Plugin Workspace")
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/plugins"

	resp, body := s.do(http.MethodPost, base, map[string]string{"name": "API Plugin"}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("create without source status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "SOURCE_MISSING" {
		t.Fatalf("error code = %q", code)
	}

	s.markSourceInstalled(t, ws.WorkspaceID)
	resp, body = s.do(http.MethodPost, base, map[string]string{
		"name":    "API Plugin",
		"about":   "Plugin created through the API",
		"version": "1.0.0",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create plugin status = %d, body = %s", resp.StatusCode, body)
	}
	var created extension.Item
	decodeJSON(t, body, &created)
	if created.Slug != "api-plugin" || created.Kind != extension.KindPlugin {
		t.Fatalf("unexpected plugin: %+v", created)
	}

	resp, body = s.do(http.MethodGet, base, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list plugins status = %d, body = %s", resp.StatusCode, body)
	}
	var listing extension.Listing
	decodeJSON(t, body, &listing)
	if listing.Total != 1 || !listing.SourceInstalled {
		t.Fatalf("unexpected listing: %+v", listing)
	}
}

func TestPluginEnableDisableRoutes(t *testing.T) {
	s := newStack(t)
	ws := s.create("Toggle Workspace")
	s.markSourceInstalled(t, ws.WorkspaceID)
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/plugins"

	resp, body := s.do(http.MethodPost, base, map[string]string{"name": "Toggle Plugin"}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create plugin status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, base+"/toggle-plugin/disable", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", resp.StatusCode, body)
	}
	var disabled extension.Item
	decodeJSON(t, body, &disabled)
	wantDisabled := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "disabled-plugins", "toggle-plugin")
	if disabled.Enabled || disabled.Path != wantDisabled {
		t.Fatalf("unexpected disabled plugin: %+v", disabled)
	}

	resp, body = s.do(http.MethodGet, base, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list plugins status = %d, body = %s", resp.StatusCode, body)
	}
	var listing extension.Listing
	decodeJSON(t, body, &listing)
	if listing.Total != 1 || listing.Items[0].Enabled {
		t.Fatalf("unexpected listing: %+v", listing)
	}

	resp, body = s.do(http.MethodPost, base+"/toggle-plugin/disable", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("disable twice status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_STATE_CONFLICT" {
		t.Fatalf("disable twice error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, base+"/toggle-plugin/enable", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status = %d, body = %s", resp.StatusCode, body)
	}
	var enabled extension.Item
	decodeJSON(t, body, &enabled)
	wantEnabled := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "source", "plugins", "toggle-plugin")
	if !enabled.Enabled || enabled.Path != wantEnabled {
		t.Fatalf("unexpected enabled plugin: %+v", enabled)
	}

	resp, body = s.do(http.MethodPost, base+"/missing-plugin/enable", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing plugin status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_NOT_FOUND" {
		t.Fatalf("missing plugin error code = %q", code)
	}
}

func TestExtensionCheckRoutes(t *testing.T) {
	s := newStack(t)
	ws := s.create("Check Workspace")
	s.markSourceInstalled(t, ws.WorkspaceID)
	sourceDir := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "source")
	versionPath := filepath.Join(sourceDir, "lib", "version.rb")
	if err := os.MkdirAll(filepath.Dir(versionPath), 0o755); err != nil {
		t.Fatalf("create version dir: %v", err)
	}
	if err := os.WriteFile(versionPath, []byte("module VERSION\n  MAJOR = 3\n  MINOR = 4\n  TINY = 0\n  PRE = nil\nend\n"), 0o644); err != nil {
		t.Fatalf("write version.rb: %v", err)
	}

	themesBase := "/api/v1/workspaces/" + ws.WorkspaceID + "/themes"
	resp, body := s.do(http.MethodPost, themesBase, map[string]string{"name": "Check Theme"}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create theme status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, themesBase+"/check-theme/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check theme status = %d, body = %s", resp.StatusCode, body)
	}
	var themeReport extension.CheckReport
	decodeJSON(t, body, &themeReport)
	if !themeReport.Ready || themeReport.Kind != extension.KindTheme ||
		themeReport.DiscourseVersion != "3.4.0" || themeReport.RequiredVersion != "3.0.0" {
		t.Fatalf("unexpected theme report: %+v", themeReport)
	}

	themeAbout := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "themes", "check-theme", "about.json")
	about := `{"name":"Check Theme","about":"Checks","version":"0.1.0","authors":"Tester","license":"MIT","theme_version":"2","minimum_discourse_version":"9.0.0"}`
	if err := os.WriteFile(themeAbout, []byte(about), 0o644); err != nil {
		t.Fatalf("write about.json: %v", err)
	}
	resp, body = s.do(http.MethodGet, themesBase+"/check-theme/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check incompatible theme status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &themeReport)
	if themeReport.Ready || themeReport.ErrorCount == 0 {
		t.Fatalf("expected incompatible theme report: %+v", themeReport)
	}

	pluginsBase := "/api/v1/workspaces/" + ws.WorkspaceID + "/plugins"
	resp, body = s.do(http.MethodPost, pluginsBase, map[string]string{"name": "Check Plugin"}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create plugin status = %d, body = %s", resp.StatusCode, body)
	}
	pluginFile := filepath.Join(sourceDir, "plugins", "check-plugin", "plugin.rb")
	pluginSource := "# name: Check Plugin\n# about: Checks\n# version: 1.0.0\n# authors: Tester\n# required_version: 3.0.0\n\nafter_initialize do\nend\n"
	if err := os.WriteFile(pluginFile, []byte(pluginSource), 0o644); err != nil {
		t.Fatalf("write plugin.rb: %v", err)
	}
	resp, body = s.do(http.MethodGet, pluginsBase+"/check-plugin/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check plugin status = %d, body = %s", resp.StatusCode, body)
	}
	var pluginReport extension.CheckReport
	decodeJSON(t, body, &pluginReport)
	if !pluginReport.Ready || pluginReport.Kind != extension.KindPlugin ||
		pluginReport.RequiredVersion != "3.0.0" {
		t.Fatalf("unexpected plugin report: %+v", pluginReport)
	}

	resp, body = s.do(http.MethodGet, pluginsBase+"/missing-plugin/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing check status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_NOT_FOUND" {
		t.Fatalf("missing check error code = %q", code)
	}

	resp, body = s.do(http.MethodGet, pluginsBase+"/bad%20slug/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid slug check status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_VALIDATION_FAILED" {
		t.Fatalf("invalid slug check error code = %q", code)
	}
}

func (s *stack) markSourceInstalled(t *testing.T, workspaceID string) {
	t.Helper()
	workspaceDir := filepath.Join(s.root, "workspaces", workspaceID)
	path := filepath.Join(workspaceDir, "workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workspace.json: %v", err)
	}
	var ws workspace.Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		t.Fatalf("decode workspace.json: %v", err)
	}
	ws.Source.Installed = true
	data, err = json.MarshalIndent(ws, "", "  ")
	if err != nil {
		t.Fatalf("encode workspace.json: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write workspace.json: %v", err)
	}
	sourceDir := filepath.Join(workspaceDir, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Gemfile"), []byte("source 'https://rubygems.org'\n"), 0o644); err != nil {
		t.Fatalf("write Gemfile: %v", err)
	}
}

func TestPluginTestRoute(t *testing.T) {
	s := newStack(t)
	ws := s.create("Plugin Test Workspace")
	s.markSourceInstalled(t, ws.WorkspaceID)
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/plugins"

	resp, body := s.do(http.MethodPost, base, map[string]string{
		"name":    "Spec Plugin",
		"version": "1.0.0",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create plugin status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, base+"/spec-plugin/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("test before init status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "PROJECT_NOT_INITIALIZED" {
		t.Fatalf("test before init error code = %q", code)
	}

	s.markProjectInitialized(t, ws.WorkspaceID)

	resp, body = s.do(http.MethodPost, base+"/spec-plugin/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("test without spec status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_VALIDATION_FAILED" {
		t.Fatalf("test without spec error code = %q", code)
	}

	specFile := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "source", "plugins",
		"spec-plugin", "spec", "plugin_spec.rb")
	if err := os.MkdirAll(filepath.Dir(specFile), 0o755); err != nil {
		t.Fatalf("create spec dir: %v", err)
	}
	if err := os.WriteFile(specFile, []byte("RSpec.describe \"plugin\" do\nend\n"), 0o644); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	resp, body = s.do(http.MethodPost, base+"/spec-plugin/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("test status = %d, body = %s", resp.StatusCode, body)
	}
	var accepted struct {
		TaskID string `json:"task_id"`
	}
	decodeJSON(t, body, &accepted)
	final := s.waitTask(accepted.TaskID)
	if final.Status != task.StatusSucceeded {
		t.Fatalf("test task status = %s, error = %s", final.Status, final.ErrorMessage)
	}
	if final.Type != "plugin.test" {
		t.Fatalf("test task type = %q", final.Type)
	}
	calls, plan := s.pluginTests.snapshot()
	if calls != 1 {
		t.Fatalf("plugin test runner calls = %d, want 1", calls)
	}
	if plan.Slug != "spec-plugin" || plan.SpecFiles != 1 ||
		plan.TestDatabase != ws.Database.DatabaseName+"_test" {
		t.Fatalf("unexpected test plan: %+v", plan)
	}
	if !strings.Contains(plan.Command, "RAILS_ENV=test") ||
		!strings.Contains(plan.Command, "plugins/spec-plugin/spec") {
		t.Fatalf("unexpected test command: %q", plan.Command)
	}

	resp, body = s.do(http.MethodPost, base+"/spec-plugin/disable", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("disable status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodPost, base+"/spec-plugin/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("test disabled status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_STATE_CONFLICT" {
		t.Fatalf("test disabled error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, base+"/missing-plugin/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("test missing status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_NOT_FOUND" {
		t.Fatalf("test missing error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, base+"/bad%20slug/test", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("test invalid slug status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "EXTENSION_VALIDATION_FAILED" {
		t.Fatalf("test invalid slug error code = %q", code)
	}
}

func (s *stack) markProjectInitialized(t *testing.T, workspaceID string) {
	t.Helper()
	path := filepath.Join(s.root, "workspaces", workspaceID, "workspace.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read workspace.json: %v", err)
	}
	var ws workspace.Workspace
	if err := json.Unmarshal(data, &ws); err != nil {
		t.Fatalf("decode workspace.json: %v", err)
	}
	ws.Project.Initialized = true
	data, err = json.MarshalIndent(ws, "", "  ")
	if err != nil {
		t.Fatalf("encode workspace.json: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatalf("write workspace.json: %v", err)
	}
}
