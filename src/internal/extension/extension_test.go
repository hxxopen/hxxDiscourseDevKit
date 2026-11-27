package extension_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
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
