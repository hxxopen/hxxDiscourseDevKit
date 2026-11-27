package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const previewAPIKey = "test-preview-api-key-0123456789"

type fakeImport struct {
	themeID     string
	fileName    string
	contentType string
	size        int64
}

type fakeDiscourse struct {
	server  *httptest.Server
	mu      sync.Mutex
	imports []fakeImport
}

func newFakeDiscourse(t *testing.T) *fakeDiscourse {
	t.Helper()
	fake := &fakeDiscourse{}
	mux := http.NewServeMux()
	mux.HandleFunc("/about.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"about":{"version":"3.4.0"}}`)
	})
	mux.HandleFunc("/admin/themes/import.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Api-Key") != previewAPIKey {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"errors":["invalid api key"]}`)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"errors":["invalid multipart body"]}`)
			return
		}
		file, header, err := r.FormFile("theme")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"errors":["theme file missing"]}`)
			return
		}
		data, err := io.ReadAll(file)
		_ = file.Close()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		themeID := 501
		if raw := strings.TrimSpace(r.FormValue("theme_id")); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"errors":["invalid theme_id"]}`)
				return
			}
			themeID = parsed
		}
		fake.mu.Lock()
		fake.imports = append(fake.imports, fakeImport{
			themeID:     r.FormValue("theme_id"),
			fileName:    header.Filename,
			contentType: header.Header.Get("Content-Type"),
			size:        int64(len(data)),
		})
		fake.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"theme": map[string]any{"id": themeID, "name": "Preview Theme"},
		})
	})
	fake.server = httptest.NewServer(mux)
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeDiscourse) records() []fakeImport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeImport(nil), f.imports...)
}

func pointWorkspaceAt(t *testing.T, s *stack, ws workspace.Workspace, site string, running bool) {
	t.Helper()
	parsed, err := url.Parse(site)
	if err != nil {
		t.Fatalf("parse site URL: %v", err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatalf("site port: %v", err)
	}
	_, err = s.workspaces.Mutate(context.Background(), ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.Network.Host = "127.0.0.1"
		current.Network.HTTPPort = port
		if running {
			current.State = workspace.StateRunning
			current.Project.ProcessState = "RUNNING"
		} else {
			current.State = workspace.StateStopped
			current.Project.ProcessState = "STOPPED"
		}
		return nil
	})
	if err != nil {
		t.Fatalf("point workspace: %v", err)
	}
}

func TestThemeBuildAndPreviewRoutes(t *testing.T) {
	s := newStack(t)
	ws := s.create("Preview Workspace")
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/themes"

	resp, body := s.do(http.MethodPost, base, map[string]string{
		"name": "Preview Theme", "version": "1.2.3",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create theme status = %d, body = %s", resp.StatusCode, body)
	}
	var created extension.Item
	decodeJSON(t, body, &created)

	resp, body = s.do(http.MethodPost, base+"/preview-theme/build", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("build theme status = %d, body = %s", resp.StatusCode, body)
	}
	var build extension.BuildResult
	decodeJSON(t, body, &build)
	if !build.Ready || build.Package == nil || build.Package.FileCount == 0 {
		t.Fatalf("unexpected build result: %+v", build)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/settings", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status = %d", resp.StatusCode)
	}
	var settings struct {
		ThemePreview struct {
			APIKeyConfigured bool `json:"api_key_configured"`
		} `json:"theme_preview"`
	}
	decodeJSON(t, body, &settings)
	if settings.ThemePreview.APIKeyConfigured {
		t.Fatalf("preview key should not be configured yet")
	}

	fake := newFakeDiscourse(t)
	pointWorkspaceAt(t, s, ws, fake.server.URL, true)
	resp, body = s.do(http.MethodPost, base+"/preview-theme/preview", map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("preview without key status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "THEME_PREVIEW_KEY_REQUIRED" {
		t.Fatalf("preview without key error = %q", code)
	}

	resp, body = s.do(http.MethodPut, "/api/v1/settings/theme-preview",
		map[string]string{"api_key": previewAPIKey}, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save preview key status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &settings)
	if !settings.ThemePreview.APIKeyConfigured {
		t.Fatalf("preview key not marked configured")
	}

	resp, body = s.do(http.MethodPost, base+"/preview-theme/preview", map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", resp.StatusCode, body)
	}
	var preview struct {
		Build struct {
			Ready bool `json:"ready"`
		} `json:"build"`
		SiteURL    string `json:"site_url"`
		PreviewURL string `json:"preview_url"`
		AdminURL   string `json:"admin_url"`
		ThemeID    int    `json:"theme_id"`
		Action     string `json:"action"`
	}
	decodeJSON(t, body, &preview)
	if !preview.Build.Ready || preview.ThemeID != 501 || preview.Action != "created" {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if preview.SiteURL != fake.server.URL {
		t.Fatalf("site URL = %q, want %q", preview.SiteURL, fake.server.URL)
	}
	if !strings.Contains(preview.PreviewURL, "/?preview_theme_id=501") {
		t.Fatalf("preview URL = %q", preview.PreviewURL)
	}
	if !strings.HasSuffix(preview.AdminURL, "/admin/customize/themes/501") {
		t.Fatalf("admin URL = %q", preview.AdminURL)
	}

	imports := fake.records()
	if len(imports) != 1 {
		t.Fatalf("imports = %d, want 1", len(imports))
	}
	if imports[0].fileName != "preview-theme-1.2.3.zip" ||
		imports[0].contentType != "application/zip" || imports[0].size == 0 ||
		imports[0].themeID != "" {
		t.Fatalf("unexpected import: %+v", imports[0])
	}
	mapping := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "theme-previews.json")
	if raw, err := os.ReadFile(mapping); err != nil || !strings.Contains(string(raw), `"theme_id": 501`) {
		t.Fatalf("preview mapping missing: err=%v body=%s", err, raw)
	}

	resp, body = s.do(http.MethodPost, base+"/preview-theme/preview", map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second preview status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &preview)
	if preview.Action != "updated" || preview.ThemeID != 501 {
		t.Fatalf("second preview = %+v", preview)
	}
	imports = fake.records()
	if len(imports) != 2 || imports[1].themeID != "501" {
		t.Fatalf("second import did not update theme: %+v", imports)
	}

	resp, body = s.do(http.MethodDelete, "/api/v1/settings/theme-preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear preview key status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &settings)
	if settings.ThemePreview.APIKeyConfigured {
		t.Fatalf("preview key still configured after clear")
	}
}

func TestThemePreviewRejectsStoppedProject(t *testing.T) {
	s := newStack(t)
	ws := s.create("Stopped Preview")
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/themes"
	if resp, body := s.do(http.MethodPost, base, map[string]string{"name": "Stopped Theme"},
		s.tokenAuth); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create theme status = %d, body = %s", resp.StatusCode, body)
	}
	if resp, body := s.do(http.MethodPut, "/api/v1/settings/theme-preview",
		map[string]string{"api_key": previewAPIKey}, s.tokenAuth); resp.StatusCode != http.StatusOK {
		t.Fatalf("save preview key status = %d, body = %s", resp.StatusCode, body)
	}
	fake := newFakeDiscourse(t)
	pointWorkspaceAt(t, s, ws, fake.server.URL, false)

	resp, body := s.do(http.MethodPost, base+"/stopped-theme/preview",
		map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stopped preview status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "THEME_PREVIEW_NOT_RUNNING" {
		t.Fatalf("stopped preview error = %q", code)
	}
	if len(fake.records()) != 0 {
		t.Fatalf("stopped preview should not import")
	}
}

func TestThemePreviewReportsBuildErrorsAndUnreachableSite(t *testing.T) {
	s := newStack(t)
	ws := s.create("Broken Preview")
	base := "/api/v1/workspaces/" + ws.WorkspaceID + "/themes"
	if resp, body := s.do(http.MethodPost, base, map[string]string{"name": "Broken Theme"},
		s.tokenAuth); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create theme status = %d, body = %s", resp.StatusCode, body)
	}
	if resp, body := s.do(http.MethodPut, "/api/v1/settings/theme-preview",
		map[string]string{"api_key": previewAPIKey}, s.tokenAuth); resp.StatusCode != http.StatusOK {
		t.Fatalf("save preview key status = %d, body = %s", resp.StatusCode, body)
	}
	about := filepath.Join(s.root, "workspaces", ws.WorkspaceID, "data", "themes", "broken-theme", "about.json")
	if err := os.Remove(about); err != nil {
		t.Fatalf("remove about.json: %v", err)
	}
	fake := newFakeDiscourse(t)
	pointWorkspaceAt(t, s, ws, fake.server.URL, true)

	resp, body := s.do(http.MethodPost, base+"/broken-theme/preview",
		map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("broken preview status = %d, body = %s", resp.StatusCode, body)
	}
	var preview struct {
		Build struct {
			Ready bool `json:"ready"`
		} `json:"build"`
		PreviewURL string `json:"preview_url"`
	}
	decodeJSON(t, body, &preview)
	if preview.Build.Ready || preview.PreviewURL != "" {
		t.Fatalf("broken theme should not preview: %+v", preview)
	}
	if len(fake.records()) != 0 {
		t.Fatalf("broken theme should not import")
	}

	// A closed port stands in for a stale "running" state.
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	if resp, body := s.do(http.MethodPost, base, map[string]string{"name": "Online Theme"},
		s.tokenAuth); resp.StatusCode != http.StatusCreated {
		t.Fatalf("create online theme status = %d, body = %s", resp.StatusCode, body)
	}
	pointWorkspaceAt(t, s, ws, closedURL, true)
	resp, body = s.do(http.MethodPost, base+"/online-theme/preview",
		map[string]string{}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("unreachable preview status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "THEME_PREVIEW_UNREACHABLE" {
		t.Fatalf("unreachable preview error = %q", code)
	}
}
