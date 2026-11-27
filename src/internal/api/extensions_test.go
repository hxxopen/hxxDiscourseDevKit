package api_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

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
