package extension_test

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type stubWorkspaces struct {
	ws *workspace.Workspace
}

func (s stubWorkspaces) Get(_ context.Context, ref string) (*workspace.Workspace, error) {
	if s.ws == nil || (ref != s.ws.WorkspaceID && ref != s.ws.Name) {
		return nil, workspace.ErrNotFound
	}
	return s.ws, nil
}

func newManager(t *testing.T, ws *workspace.Workspace) (*extension.Manager, *config.Paths) {
	t.Helper()
	paths := config.NewPaths(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return extension.New(paths, stubWorkspaces{ws: ws}, logger), paths
}

func TestCreateThemeScaffoldAndList(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-theme",
		Name:        "Theme Demo",
		Source:      workspace.SourceConfig{Path: "source"},
	}
	manager, paths := newManager(t, ws)
	ctx := context.Background()

	created, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name:    "My Theme",
		About:   "A theme for testing",
		Version: "0.2.0",
		Authors: "Tester",
	})
	if err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}
	if created.Slug != "my-theme" || created.Name != "My Theme" || !created.HasMetadata {
		t.Fatalf("unexpected item: %+v", created)
	}
	if created.About != "A theme for testing" || created.Version != "0.2.0" || created.Authors != "Tester" {
		t.Fatalf("metadata not parsed: %+v", created)
	}
	wantPath := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "data", "themes", "my-theme")
	if created.Path != wantPath {
		t.Fatalf("path = %q, want %q", created.Path, wantPath)
	}
	for _, relative := range []string{"about.json", "common/common.scss", "desktop/desktop.scss", "mobile/mobile.scss", "README.md"} {
		if _, err := os.Stat(filepath.Join(wantPath, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
	}
	var about map[string]any
	data, err := os.ReadFile(filepath.Join(wantPath, "about.json"))
	if err != nil {
		t.Fatalf("read about.json: %v", err)
	}
	if err := json.Unmarshal(data, &about); err != nil {
		t.Fatalf("parse about.json: %v", err)
	}
	if about["name"] != "My Theme" || about["theme_version"] != "2" {
		t.Fatalf("unexpected about.json: %v", about)
	}

	listing, err := manager.ListThemes(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("ListThemes: %v", err)
	}
	if listing.Total != 1 || listing.Items[0].Slug != "my-theme" {
		t.Fatalf("unexpected listing: %+v", listing)
	}
	if listing.SourceInstalled {
		t.Fatalf("source_installed = true, want false")
	}
}

func TestCreatePluginRequiresSource(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-plugin",
		Name:        "Plugin Demo",
		Source:      workspace.SourceConfig{Path: "source", Installed: false},
	}
	manager, _ := newManager(t, ws)
	_, err := manager.CreatePlugin(context.Background(), ws.WorkspaceID, extension.CreateRequest{Name: "My Plugin"})
	if !errors.Is(err, extension.ErrSourceMissing) {
		t.Fatalf("err = %v, want ErrSourceMissing", err)
	}
}

func TestCreatePluginScaffoldAndList(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-plugin",
		Name:        "Plugin Demo",
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "Gemfile"), []byte("source 'https://rubygems.org'\n"), 0o644); err != nil {
		t.Fatalf("write Gemfile: %v", err)
	}
	ctx := context.Background()

	created, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name:    "My Plugin",
		Slug:    "custom-plugin",
		About:   "Adds a test feature",
		Version: "1.2.3",
		Authors: "Plugin Author",
	})
	if err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if created.Slug != "custom-plugin" || created.Name != "My Plugin" || !created.HasMetadata {
		t.Fatalf("unexpected item: %+v", created)
	}
	if created.About != "Adds a test feature" || created.Version != "1.2.3" || created.Authors != "Plugin Author" {
		t.Fatalf("metadata not parsed: %+v", created)
	}
	pluginDir := filepath.Join(sourceDir, "plugins", "custom-plugin")
	for _, relative := range []string{
		"plugin.rb",
		"README.md",
		"config/locales/client.en.yml",
		"assets/javascripts/discourse/initializers/custom-plugin-init.js",
	} {
		if _, err := os.Stat(filepath.Join(pluginDir, filepath.FromSlash(relative))); err != nil {
			t.Fatalf("missing %s: %v", relative, err)
		}
	}

	listing, err := manager.ListPlugins(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if listing.Total != 1 || !listing.SourceInstalled {
		t.Fatalf("unexpected listing: %+v", listing)
	}
	if listing.Root != filepath.Join(sourceDir, "plugins") {
		t.Fatalf("root = %q", listing.Root)
	}
}

func TestCreateRejectsDuplicatesAndInvalidSlugs(t *testing.T) {
	ws := &workspace.Workspace{WorkspaceID: "ws-errors", Name: "Errors", Source: workspace.SourceConfig{Path: "source"}}
	manager, _ := newManager(t, ws)
	ctx := context.Background()
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{Name: "Same Theme"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{Name: "Same Theme"}); !errors.Is(err, extension.ErrExists) {
		t.Fatalf("duplicate err = %v, want ErrExists", err)
	}
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{Name: "Bad", Slug: "../escape"}); !errors.Is(err, extension.ErrValidation) {
		t.Fatalf("slug err = %v, want ErrValidation", err)
	}
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{Name: "Bad\nName"}); !errors.Is(err, extension.ErrValidation) {
		t.Fatalf("name err = %v, want ErrValidation", err)
	}
}

func TestPackageThemeZIP(t *testing.T) {
	ws := &workspace.Workspace{WorkspaceID: "ws-package", Name: "Package", Source: workspace.SourceConfig{Path: "source"}}
	manager, paths := newManager(t, ws)
	ctx := context.Background()
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name:    "Packaged Theme",
		Version: "2.1.0",
	}); err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}

	pkg, err := manager.PackageTheme(ctx, ws.WorkspaceID, "packaged-theme")
	if err != nil {
		t.Fatalf("PackageTheme: %v", err)
	}
	wantPath := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "data", "exports", "themes", "packaged-theme-2.1.0.zip")
	if pkg.Path != wantPath || pkg.FileName != "packaged-theme-2.1.0.zip" {
		t.Fatalf("package path = %q name = %q, want %q", pkg.Path, pkg.FileName, wantPath)
	}
	if pkg.FileCount != 5 || pkg.SizeBytes == 0 {
		t.Fatalf("unexpected package info: %+v", pkg)
	}
	data, err := os.ReadFile(pkg.Path)
	if err != nil {
		t.Fatalf("read package: %v", err)
	}
	sum := sha256.Sum256(data)
	if pkg.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha256 = %q, want %q", pkg.SHA256, hex.EncodeToString(sum[:]))
	}
	names := zipEntryNames(t, pkg.Path)
	for _, want := range []string{"about.json", "README.md", "common/common.scss"} {
		if !names[want] {
			t.Fatalf("package missing %s: %v", want, names)
		}
	}

	if _, err := manager.PackageTheme(ctx, ws.WorkspaceID, "missing-theme"); !errors.Is(err, extension.ErrNotFound) {
		t.Fatalf("missing theme err = %v, want ErrNotFound", err)
	}
	if _, err := manager.PackageTheme(ctx, ws.WorkspaceID, "../escape"); !errors.Is(err, extension.ErrValidation) {
		t.Fatalf("invalid slug err = %v, want ErrValidation", err)
	}
}

func TestPackagePluginExcludesMetadataDirectories(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-package-plugin",
		Name:        "Package Plugin",
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if _, err := manager.CreatePlugin(context.Background(), ws.WorkspaceID, extension.CreateRequest{
		Name: "Packaged Plugin",
	}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	pluginDir := filepath.Join(sourceDir, "plugins", "packaged-plugin")
	for _, dir := range []string{".git", "node_modules/example"} {
		if err := os.MkdirAll(filepath.Join(pluginDir, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("create %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ".git", "HEAD"), []byte("ref\n"), 0o644); err != nil {
		t.Fatalf("write .git/HEAD: %v", err)
	}

	pkg, err := manager.PackagePlugin(context.Background(), ws.WorkspaceID, "packaged-plugin")
	if err != nil {
		t.Fatalf("PackagePlugin: %v", err)
	}
	names := zipEntryNames(t, pkg.Path)
	if names[".git/HEAD"] || names["node_modules/example"] {
		t.Fatalf("package contains excluded entries: %v", names)
	}
	for _, want := range []string{"plugin.rb", "README.md", "config/locales/client.en.yml"} {
		if !names[want] {
			t.Fatalf("package missing %s: %v", want, names)
		}
	}
	if !strings.HasSuffix(pkg.FileName, ".zip") {
		t.Fatalf("file name = %q", pkg.FileName)
	}
}

func TestDisableAndEnablePlugin(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-toggle",
		Name:        "Toggle",
		State:       workspace.StateReady,
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	ctx := context.Background()
	if _, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name:    "Toggle Plugin",
		Version: "1.0.0",
	}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	enabledDir := filepath.Join(sourceDir, "plugins", "toggle-plugin")
	disabledDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "data", "disabled-plugins", "toggle-plugin")

	disabled, err := manager.DisablePlugin(ctx, ws.WorkspaceID, "toggle-plugin")
	if err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	if disabled.Enabled || disabled.Path != disabledDir {
		t.Fatalf("unexpected disabled item: %+v", disabled)
	}
	if _, err := os.Stat(enabledDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("enabled directory still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(disabledDir, "plugin.rb")); err != nil {
		t.Fatalf("disabled plugin missing files: %v", err)
	}

	listing, err := manager.ListPlugins(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if listing.Total != 1 || listing.Items[0].Enabled || listing.Items[0].Slug != "toggle-plugin" {
		t.Fatalf("unexpected listing: %+v", listing)
	}

	if _, err := manager.PackagePlugin(ctx, ws.WorkspaceID, "toggle-plugin"); err != nil {
		t.Fatalf("PackagePlugin while disabled: %v", err)
	}
	if _, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name: "Toggle Plugin",
	}); !errors.Is(err, extension.ErrExists) {
		t.Fatalf("duplicate disabled plugin err = %v, want ErrExists", err)
	}

	enabled, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "toggle-plugin")
	if err != nil {
		t.Fatalf("EnablePlugin: %v", err)
	}
	if !enabled.Enabled || enabled.Path != enabledDir {
		t.Fatalf("unexpected enabled item: %+v", enabled)
	}
	if _, err := os.Stat(filepath.Join(disabledDir, "plugin.rb")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled directory still exists: %v", err)
	}
}

func TestPluginToggleStateErrors(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-toggle-errors",
		Name:        "Toggle Errors",
		State:       workspace.StateReady,
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	if err := os.MkdirAll(filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source"), 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	ctx := context.Background()
	if _, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{Name: "Errors Plugin"}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if _, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "errors-plugin"); !errors.Is(err, extension.ErrStateConflict) {
		t.Fatalf("enable enabled err = %v, want ErrStateConflict", err)
	}
	if _, err := manager.DisablePlugin(ctx, ws.WorkspaceID, "errors-plugin"); err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	if _, err := manager.DisablePlugin(ctx, ws.WorkspaceID, "errors-plugin"); !errors.Is(err, extension.ErrStateConflict) {
		t.Fatalf("disable disabled err = %v, want ErrStateConflict", err)
	}
	if _, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "missing-plugin"); !errors.Is(err, extension.ErrNotFound) {
		t.Fatalf("missing err = %v, want ErrNotFound", err)
	}
	if _, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "../escape"); !errors.Is(err, extension.ErrValidation) {
		t.Fatalf("invalid slug err = %v, want ErrValidation", err)
	}

	ws.State = workspace.StateRunning
	if _, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "errors-plugin"); !errors.Is(err, extension.ErrProjectRunning) {
		t.Fatalf("running err = %v, want ErrProjectRunning", err)
	}
	ws.State = workspace.StateReady
	ws.Project.ProcessState = "RUNNING"
	if _, err := manager.EnablePlugin(ctx, ws.WorkspaceID, "errors-plugin"); !errors.Is(err, extension.ErrProjectRunning) {
		t.Fatalf("process running err = %v, want ErrProjectRunning", err)
	}
}

func TestCheckPluginDependenciesAndCompatibility(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-check-plugin",
		Name:        "Check Plugin",
		State:       workspace.StateReady,
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	writeTestFile(t, filepath.Join(sourceDir, "lib", "version.rb"), `module Discourse
  module VERSION
    MAJOR = 3
    MINOR = 4
    TINY = 0
    PRE = nil
  end
end
`)
	ctx := context.Background()
	if _, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name:    "Check Plugin",
		Version: "1.0.0",
	}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	pluginDir := filepath.Join(sourceDir, "plugins", "check-plugin")
	writeTestFile(t, filepath.Join(pluginDir, "plugin.rb"), `# frozen_string_literal: true
# name: Check Plugin
# about: Checks dependencies
# version: 1.0.0
# authors: Tester
# required_version: 3.0.0

after_initialize do
end
`)
	writeTestFile(t, filepath.Join(pluginDir, "package.json"), `{
  "dependencies": {"markdown-it": "^14.0.0"},
  "devDependencies": {"eslint": "^9.0.0"}
}
`)

	report, err := manager.CheckPlugin(ctx, ws.WorkspaceID, "check-plugin")
	if err != nil {
		t.Fatalf("CheckPlugin: %v", err)
	}
	if !report.Ready || report.ErrorCount != 0 || report.WarningCount != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.DiscourseVersion != "3.4.0" || report.RequiredVersion != "3.0.0" {
		t.Fatalf("unexpected versions: %+v", report)
	}
	if !report.Enabled {
		t.Fatalf("plugin should be enabled: %+v", report)
	}
	frontend := findingByID(report, "frontend_dependencies")
	if frontend == nil || frontend.Status != extension.SeverityOK ||
		!strings.Contains(frontend.Message, "dependencies 1") {
		t.Fatalf("unexpected frontend finding: %+v", frontend)
	}
	if compatibility := findingByID(report, "compatibility"); compatibility == nil ||
		compatibility.Status != extension.SeverityOK {
		t.Fatalf("unexpected compatibility finding: %+v", compatibility)
	}

	writeTestFile(t, filepath.Join(pluginDir, "plugin.rb"), `# name: Check Plugin
# about: Checks dependencies
# version: 1.0.0
# authors: Tester
# required_version: 9.0.0

after_initialize do
end
`)
	report, err = manager.CheckPlugin(ctx, ws.WorkspaceID, "check-plugin")
	if err != nil {
		t.Fatalf("CheckPlugin incompatible: %v", err)
	}
	if report.Ready || report.ErrorCount == 0 {
		t.Fatalf("expected incompatible report, got %+v", report)
	}
	compatibility := findingByID(report, "compatibility")
	if compatibility == nil || compatibility.Status != extension.SeverityError || !compatibility.Required {
		t.Fatalf("unexpected compatibility finding: %+v", compatibility)
	}

	writeTestFile(t, filepath.Join(pluginDir, "plugin.rb"), `# name: Check Plugin
# about: Checks dependencies
# version: 1.0.0
# authors: Tester
# required_version: 3.0.0

after_initialize do
end
`)
	disabled, err := manager.DisablePlugin(ctx, ws.WorkspaceID, "check-plugin")
	if err != nil {
		t.Fatalf("DisablePlugin: %v", err)
	}
	report, err = manager.CheckPlugin(ctx, ws.WorkspaceID, "check-plugin")
	if err != nil {
		t.Fatalf("CheckPlugin disabled: %v", err)
	}
	if report.Enabled || report.Path != disabled.Path || !report.Ready {
		t.Fatalf("unexpected disabled report: %+v", report)
	}
	state := findingByID(report, "state")
	if state == nil || state.Status != extension.SeverityWarning {
		t.Fatalf("unexpected state finding: %+v", state)
	}

	if _, err := manager.CheckPlugin(ctx, ws.WorkspaceID, "missing-plugin"); !errors.Is(err, extension.ErrNotFound) {
		t.Fatalf("missing plugin err = %v, want ErrNotFound", err)
	}
	if _, err := manager.CheckPlugin(ctx, ws.WorkspaceID, "../escape"); !errors.Is(err, extension.ErrValidation) {
		t.Fatalf("invalid slug err = %v, want ErrValidation", err)
	}
}

func TestCheckThemeMetadataAndVersionRange(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-check-theme",
		Name:        "Check Theme",
		State:       workspace.StateReady,
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	writeTestFile(t, filepath.Join(sourceDir, "lib", "version.rb"), `module VERSION
  MAJOR = 3
  MINOR = 4
  TINY = 0
  PRE = nil
  STRING = [MAJOR, MINOR, TINY, PRE].compact.join(".")
end
`)
	ctx := context.Background()
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name: "Check Theme", Version: "1.0.0",
	}); err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}
	themeDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "data", "themes", "check-theme")

	report, err := manager.CheckTheme(ctx, ws.WorkspaceID, "check-theme")
	if err != nil {
		t.Fatalf("CheckTheme: %v", err)
	}
	if !report.Ready || report.ErrorCount != 0 || report.WarningCount != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.DiscourseVersion != "3.4.0" || report.RequiredVersion != "3.0.0" {
		t.Fatalf("unexpected versions: %+v", report)
	}

	writeTestFile(t, filepath.Join(themeDir, "about.json"), `{
  "name": "Check Theme",
  "about": "Checks metadata",
  "version": "1.0.0",
  "authors": "Tester",
  "license": "MIT",
  "theme_version": "2",
  "minimum_discourse_version": "3.0.0",
  "maximum_discourse_version": "3.4.0"
}
`)
	report, err = manager.CheckTheme(ctx, ws.WorkspaceID, "check-theme")
	if err != nil {
		t.Fatalf("CheckTheme range: %v", err)
	}
	if !report.Ready || report.MaximumVersion != "3.4.0" {
		t.Fatalf("unexpected report: %+v", report)
	}

	writeTestFile(t, filepath.Join(themeDir, "about.json"), `{
  "name": "Check Theme",
  "about": "Checks metadata",
  "version": "1.0.0",
  "authors": "Tester",
  "license": "MIT",
  "theme_version": "2",
  "minimum_discourse_version": "9.0.0"
}
`)
	report, err = manager.CheckTheme(ctx, ws.WorkspaceID, "check-theme")
	if err != nil {
		t.Fatalf("CheckTheme incompatible: %v", err)
	}
	if report.Ready || report.ErrorCount == 0 {
		t.Fatalf("expected incompatible theme, got %+v", report)
	}

	if err := os.Remove(filepath.Join(themeDir, "about.json")); err != nil {
		t.Fatalf("remove about.json: %v", err)
	}
	report, err = manager.CheckTheme(ctx, ws.WorkspaceID, "check-theme")
	if err != nil {
		t.Fatalf("CheckTheme missing metadata: %v", err)
	}
	if report.Ready {
		t.Fatalf("expected missing metadata to fail: %+v", report)
	}
	if manifest := findingByID(report, "manifest"); manifest == nil ||
		manifest.Status != extension.SeverityError || !manifest.Required {
		t.Fatalf("unexpected manifest finding: %+v", manifest)
	}
}

func TestCheckVersionPrereleaseIsOlderThanStable(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-prerelease",
		Name:        "Prerelease",
		State:       workspace.StateReady,
		Source:      workspace.SourceConfig{Path: "source", Installed: true},
	}
	manager, paths := newManager(t, ws)
	sourceDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	writeTestFile(t, filepath.Join(sourceDir, "lib", "version.rb"), `module VERSION
  MAJOR = 3
  MINOR = 4
  TINY = 0
  PRE = "beta1"
end
`)
	ctx := context.Background()
	if _, err := manager.CreatePlugin(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name: "Prerelease Plugin",
	}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	pluginDir := filepath.Join(sourceDir, "plugins", "prerelease-plugin")
	writeTestFile(t, filepath.Join(pluginDir, "plugin.rb"), `# name: Prerelease Plugin
# about: Requires a stable version
# version: 1.0.0
# authors: Tester
# required_version: 3.4.0

after_initialize do
end
`)
	report, err := manager.CheckPlugin(ctx, ws.WorkspaceID, "prerelease-plugin")
	if err != nil {
		t.Fatalf("CheckPlugin: %v", err)
	}
	if report.DiscourseVersion != "3.4.0.beta1" {
		t.Fatalf("discourse version = %q", report.DiscourseVersion)
	}
	if report.Ready {
		t.Fatalf("expected beta release to be older than stable: %+v", report)
	}
	if compatibility := findingByID(report, "compatibility"); compatibility == nil ||
		compatibility.Status != extension.SeverityError {
		t.Fatalf("unexpected compatibility finding: %+v", compatibility)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func findingByID(report *extension.CheckReport, id string) *extension.Finding {
	for i := range report.Findings {
		if report.Findings[i].ID == id {
			return &report.Findings[i]
		}
	}
	return nil
}

func zipEntryNames(t *testing.T, path string) map[string]bool {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer func() { _ = reader.Close() }()
	names := make(map[string]bool, len(reader.File))
	for _, file := range reader.File {
		names[file.Name] = true
	}
	return names
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Plugin":       "my-plugin",
		"  Hello, World ": "hello-world",
		"数量统计":            "",
		"a__b":            "a-b",
		"v1.2.3":          "v1-2-3",
	}
	for input, want := range cases {
		if got := extension.Slugify(input); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildThemePackagesOnlyWhenCheckPasses(t *testing.T) {
	ws := &workspace.Workspace{
		WorkspaceID: "ws-build-theme",
		Name:        "Build Theme",
	}
	manager, paths := newManager(t, ws)
	ctx := context.Background()
	if _, err := manager.CreateTheme(ctx, ws.WorkspaceID, extension.CreateRequest{
		Name: "Build Theme", Version: "1.1.0",
	}); err != nil {
		t.Fatalf("CreateTheme: %v", err)
	}

	result, err := manager.BuildTheme(ctx, ws.WorkspaceID, "build-theme")
	if err != nil {
		t.Fatalf("BuildTheme: %v", err)
	}
	if !result.Ready || result.Package == nil {
		t.Fatalf("expected ready build, got %+v", result)
	}
	if result.Package.Path != filepath.Join(
		paths.WorkspaceDir(ws.WorkspaceID), "data", "exports", "themes", "build-theme-1.1.0.zip") {
		t.Fatalf("package path = %q", result.Package.Path)
	}
	if _, err := os.Stat(result.Package.Path); err != nil {
		t.Fatalf("package missing: %v", err)
	}

	themeDir := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "data", "themes", "build-theme")
	if err := os.Remove(filepath.Join(themeDir, "about.json")); err != nil {
		t.Fatalf("remove about.json: %v", err)
	}
	result, err = manager.BuildTheme(ctx, ws.WorkspaceID, "build-theme")
	if err != nil {
		t.Fatalf("BuildTheme invalid: %v", err)
	}
	if result.Ready || result.Package != nil || result.Check == nil || result.Check.ErrorCount == 0 {
		t.Fatalf("expected blocked build, got %+v", result)
	}
}
