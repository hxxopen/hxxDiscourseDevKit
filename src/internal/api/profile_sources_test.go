package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func sourceManifest(profileID, version string) string {
	return `{
  "schema_version": 1,
  "name": "Acme API 模板",
  "version": "` + version + `",
  "description": "测试用第三方模板来源。",
  "profiles": [
    {
      "id": "` + profileID + `",
      "name": "Acme API Discourse",
      "description": "Acme API 的 Discourse 开发模板。",
      "project_type": "discourse-acme",
      "default_repository_url": "https://git.example.com/acme/discourse.git",
      "default_branch": "main",
      "operations": [
        {"id": "workspace.create", "title": "创建 Workspace", "description": "创建隔离目录。"}
      ],
      "permissions": [
        {"id": "workspace.write", "title": "写入实例目录", "description": "写入 workspaces/。", "risk": "low"}
      ]
    }
  ]
}`
}

func writeSourceDir(t *testing.T, body string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, profile.ManifestFileName), []byte(body), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return dir
}

func TestProfileSourceLifecycle(t *testing.T) {
	s := newStack(t)
	sourceDir := writeSourceDir(t, sourceManifest("acme-api", "1.0.0"))

	resp, body := s.do(http.MethodGet, "/api/v1/profile-sources", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list sources status = %d, body = %s", resp.StatusCode, body)
	}
	var list struct {
		Items []profile.Source `json:"items"`
		Total int              `json:"total"`
	}
	decodeJSON(t, body, &list)
	if list.Total != 0 {
		t.Fatalf("initial sources = %+v", list)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources", map[string]any{
		"location": sourceDir,
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("add source status = %d, body = %s", resp.StatusCode, body)
	}
	var source profile.Source
	decodeJSON(t, body, &source)
	if source.Kind != profile.SourceKindLocal || source.Name != "Acme API 模板" || len(source.ProfileIDs) != 1 {
		t.Fatalf("source = %+v", source)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/profiles", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list profiles status = %d, body = %s", resp.StatusCode, body)
	}
	var profiles struct {
		Items []profile.Definition `json:"items"`
		Total int                  `json:"total"`
	}
	decodeJSON(t, body, &profiles)
	if profiles.Total != 6 {
		t.Fatalf("profiles = %d, want 6", profiles.Total)
	}
	found := false
	for _, item := range profiles.Items {
		if item.ID == "acme-api" {
			found = true
			if item.Origin.Type != "third-party" || item.Origin.SourceID != source.ID || item.Origin.Verified {
				t.Fatalf("origin = %+v", item.Origin)
			}
		}
	}
	if !found {
		t.Fatal("third-party profile missing from list")
	}

	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources", map[string]any{
		"location": sourceDir,
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "PROFILE_SOURCE_EXISTS" {
		t.Fatalf("duplicate source status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":       "Acme Without Ack",
		"profile_id": "acme-api",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != "PROFILE_ACK_REQUIRED" {
		t.Fatalf("unacknowledged create status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":           "Acme With Ack",
		"profile_id":     "acme-api",
		"accept_profile": true,
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("acknowledged create status = %d, body = %s", resp.StatusCode, body)
	}
	var created workspace.Workspace
	decodeJSON(t, body, &created)
	if created.ProfileID != "acme-api" || created.ProjectType != "discourse-acme" {
		t.Fatalf("workspace = %+v", created)
	}
	if created.Source.RepositoryURL != "https://git.example.com/acme/discourse.git" {
		t.Fatalf("source = %+v", created.Source)
	}

	if err := os.WriteFile(filepath.Join(sourceDir, profile.ManifestFileName),
		[]byte(sourceManifest("acme-api", "2.0.0")), 0o644); err != nil {
		t.Fatalf("rewrite manifest: %v", err)
	}
	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources/"+source.ID+"/refresh", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &source)
	if source.Version != "2.0.0" {
		t.Fatalf("refreshed source = %+v", source)
	}

	resp, body = s.do(http.MethodDelete, "/api/v1/profile-sources/"+source.ID, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, "/api/v1/profiles/acme-api", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, body) != "PROFILE_NOT_FOUND" {
		t.Fatalf("removed profile status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestProfileSourceErrors(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodPost, "/api/v1/profile-sources", map[string]any{
		"location": filepath.Join(t.TempDir(), "missing"),
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, body) != "PROFILE_SOURCE_INVALID" {
		t.Fatalf("missing local source status = %d, body = %s", resp.StatusCode, body)
	}

	badDir := writeSourceDir(t, `{"schema_version": 1, "name": "Bad"}`)
	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources", map[string]any{
		"location": badDir,
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusUnprocessableEntity || errorCode(t, body) != "PROFILE_SOURCE_INVALID" {
		t.Fatalf("invalid manifest status = %d, body = %s", resp.StatusCode, body)
	}

	conflictDir := writeSourceDir(t, sourceManifest("discourse-official", "1.0.0"))
	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources", map[string]any{
		"location": conflictDir,
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict || errorCode(t, body) != "PROFILE_SOURCE_CONFLICT" {
		t.Fatalf("conflicting source status = %d, body = %s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "discourse-official") {
		t.Fatalf("conflict body = %s", body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/profile-sources/missing/refresh", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, body) != "PROFILE_SOURCE_NOT_FOUND" {
		t.Fatalf("missing refresh status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodDelete, "/api/v1/profile-sources/missing", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, body) != "PROFILE_SOURCE_NOT_FOUND" {
		t.Fatalf("missing delete status = %d, body = %s", resp.StatusCode, body)
	}
}
