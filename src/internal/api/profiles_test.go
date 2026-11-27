package api_test

import (
	"net/http"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func TestProfileRoutes(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/api/v1/profiles", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list profiles status = %d, body = %s", resp.StatusCode, body)
	}
	var list struct {
		Items []profile.Definition `json:"items"`
		Total int                  `json:"total"`
	}
	decodeJSON(t, body, &list)
	if list.Total != 5 || len(list.Items) != 5 {
		t.Fatalf("profiles = %+v", list)
	}
	if list.Items[0].ID != "discourse-official" {
		t.Fatalf("first profile = %q", list.Items[0].ID)
	}
	if list.Items[0].Origin.Type != "builtin" || len(list.Items[0].Operations) == 0 {
		t.Fatalf("profile provenance/operations missing: %+v", list.Items[0])
	}

	resp, body = s.do(http.MethodGet, "/api/v1/profiles/discourse-plugin", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get profile status = %d, body = %s", resp.StatusCode, body)
	}
	var def profile.Definition
	decodeJSON(t, body, &def)
	if def.ProjectType != "discourse-plugin" || def.ExtensionKind != "plugin" {
		t.Fatalf("profile = %+v", def)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/profiles/discourse-official/check", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check profile status = %d, body = %s", resp.StatusCode, body)
	}
	var report profile.CheckReport
	decodeJSON(t, body, &report)
	if !report.Ready || report.ErrorCount != 0 || report.WarningCount == 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(report.Checks) != 7 {
		t.Fatalf("checks = %+v", report.Checks)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/profiles/missing", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing profile status = %d, body = %s", resp.StatusCode, body)
	}
	if errorCode(t, body) != "PROFILE_NOT_FOUND" {
		t.Fatalf("missing profile body = %s", body)
	}
}

func TestCreateWorkspaceWithProfile(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":       "Plugin Dev",
		"profile_id": "discourse-plugin",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", resp.StatusCode, body)
	}
	var created workspace.Workspace
	decodeJSON(t, body, &created)
	if created.ProfileID != "discourse-plugin" {
		t.Fatalf("profile_id = %q", created.ProfileID)
	}
	if created.ProjectType != "discourse-plugin" {
		t.Fatalf("project_type = %q", created.ProjectType)
	}
	if created.Source.RepositoryURL != "https://github.com/discourse/discourse.git" {
		t.Fatalf("repository = %q", created.Source.RepositoryURL)
	}
	if created.Source.Branch != "main" {
		t.Fatalf("branch = %q", created.Source.Branch)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":       "Custom Dev",
		"profile_id": "discourse-custom",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("custom without source status = %d, body = %s", resp.StatusCode, body)
	}
	if errorCode(t, body) != "PROFILE_SOURCE_REQUIRED" {
		t.Fatalf("custom without source body = %s", body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":           "Custom Dev",
		"profile_id":     "discourse-custom",
		"repository_url": "https://git.example.com/team/discourse.git",
		"branch":         "develop",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("custom create status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &created)
	if created.ProfileID != "discourse-custom" || created.ProjectType != "discourse-custom" {
		t.Fatalf("custom workspace = %+v", created)
	}
	if created.Source.RepositoryURL != "https://git.example.com/team/discourse.git" ||
		created.Source.Branch != "develop" {
		t.Fatalf("custom source = %+v", created.Source)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":       "Unknown Profile",
		"profile_id": "does-not-exist",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown profile status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces", map[string]any{
		"name":           "Credential URL",
		"profile_id":     "discourse-custom",
		"repository_url": "https://user:pass@git.example.com/team/discourse.git",
	}, s.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("credential URL status = %d, body = %s", resp.StatusCode, body)
	}
	if errorCode(t, body) != "PROJECT_VALIDATION_FAILED" {
		t.Fatalf("credential URL body = %s", body)
	}
}
