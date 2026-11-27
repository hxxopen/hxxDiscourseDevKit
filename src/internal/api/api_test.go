package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/api"
	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/diagnostics"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

const allowedOrigin = "http://127.0.0.1:17860"

type stack struct {
	t          *testing.T
	server     *httptest.Server
	workspaces *workspace.Manager
	secrets    *secret.Store
	token      string
	root       string
	logLevel   *string
}

func newStack(t *testing.T) *stack {
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
	secrets, err := secret.Open(paths.SecretsFile())
	if err != nil {
		t.Fatalf("secret.Open: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tasks := task.New(filepath.Join(paths.LogsDir, "tasks.jsonl"), logger)
	t.Cleanup(tasks.Close)
	workspaces := workspace.New(paths, cfg, secrets, logger, workspace.Options{
		PortStart: 33000,
		BindHost:  "127.0.0.1",
	})
	diag := diagnostics.New(paths, cfg, workspaces, logger)

	ui := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>DevKit Test UI</title>")},
	}
	level := ""
	server := api.New(api.Options{
		Paths:       paths,
		Config:      cfg,
		Secrets:     secrets,
		Workspaces:  workspaces,
		Tasks:       tasks,
		Diagnostics: diag,
		UI:          ui,
		Logger:      logger,
		APIPort:     17860,
		StartedAt:   time.Now(),
		SetLogLevel: func(next string) { level = next },
	})
	httpServer := httptest.NewServer(server.Handler())
	t.Cleanup(httpServer.Close)

	return &stack{
		t:          t,
		server:     httpServer,
		workspaces: workspaces,
		secrets:    secrets,
		token:      secrets.APIToken(),
		root:       root,
		logLevel:   &level,
	}
}

func (s *stack) do(method, path string, body any, mutate func(*http.Request)) (*http.Response, []byte) {
	s.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			s.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, s.server.URL+path, reader)
	if err != nil {
		s.t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if mutate != nil {
		mutate(req)
	}
	resp, err := s.server.Client().Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		s.t.Fatalf("read response: %v", err)
	}
	return resp, data
}

func (s *stack) tokenAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+s.token)
}

func (s *stack) create(name string) workspace.Workspace {
	s.t.Helper()
	resp, body := s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": name}, s.tokenAuth)
	if resp.StatusCode != http.StatusCreated {
		s.t.Fatalf("create %q status = %d, body = %s", name, resp.StatusCode, body)
	}
	var created workspace.Workspace
	decodeJSON(s.t, body, &created)
	return created
}

func (s *stack) waitTask(taskID string) task.Task {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, body := s.do(http.MethodGet, "/api/v1/tasks/"+taskID, nil, s.tokenAuth)
		if resp.StatusCode != http.StatusOK {
			s.t.Fatalf("get task status = %d, body = %s", resp.StatusCode, body)
		}
		var current task.Task
		decodeJSON(s.t, body, &current)
		if current.Status.Terminal() {
			return current
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.t.Fatalf("task %s did not finish", taskID)
	return task.Task{}
}

func decodeJSON(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

func errorCode(t *testing.T, data []byte) string {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	decodeJSON(t, data, &envelope)
	return envelope.Error.Code
}

func TestHealthIsPublicAndAuthIsRequired(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/api/v1/system/health", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", resp.StatusCode, body)
	}
	var health struct {
		Status string `json:"status"`
		Root   string `json:"root"`
	}
	decodeJSON(t, body, &health)
	if health.Status != "ok" || health.Root != s.root {
		t.Fatalf("unexpected health: %+v", health)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated info status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "UNAUTHORIZED" {
		t.Fatalf("error code = %q, want UNAUTHORIZED", code)
	}

	resp, _ = s.do(http.MethodGet, "/api/v1/system/info", nil, func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer wrong-token")
	})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want 401", resp.StatusCode)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authenticated info status = %d, body = %s", resp.StatusCode, body)
	}
	var info struct {
		Root              string          `json:"root"`
		InstanceID        string          `json:"instance_id"`
		ActiveWorkspaceID string          `json:"active_workspace_id"`
		Capabilities      map[string]bool `json:"capabilities"`
	}
	decodeJSON(t, body, &info)
	if info.Root != s.root || len(info.InstanceID) != 26 {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !info.Capabilities["workspaces"] {
		t.Fatal("workspaces capability is not advertised")
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/health", nil, func(req *http.Request) {
		req.Host = "example.com"
	})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-loopback Host status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "FORBIDDEN_HOST" {
		t.Fatalf("host error code = %q, want FORBIDDEN_HOST", code)
	}
}

func TestWorkspaceLifecycleOverAPI(t *testing.T) {
	s := newStack(t)
	alpha := s.create("Alpha")
	if alpha.WorkspaceID == "" || alpha.Network.HTTPPort == 0 || alpha.Database.DatabaseName == "" {
		t.Fatalf("unexpected created workspace: %+v", alpha)
	}

	resp, body := s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "alpha"}, s.tokenAuth)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate create status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "WORKSPACE_NAME_CONFLICT" {
		t.Fatalf("duplicate error code = %q", code)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/workspaces/Alpha", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get by name status = %d, body = %s", resp.StatusCode, body)
	}
	var fetched workspace.Workspace
	decodeJSON(t, body, &fetched)
	if fetched.WorkspaceID != alpha.WorkspaceID {
		t.Fatalf("get by name returned %q", fetched.WorkspaceID)
	}

	description := "changed by test"
	resp, body = s.do(http.MethodPatch, "/api/v1/workspaces/"+alpha.WorkspaceID,
		workspace.UpdateRequest{Description: &description}, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch status = %d, body = %s", resp.StatusCode, body)
	}
	var patched workspace.Workspace
	decodeJSON(t, body, &patched)
	if patched.Description != description {
		t.Fatalf("description = %q", patched.Description)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/workspaces", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", resp.StatusCode, body)
	}
	var list struct {
		Items             []*workspace.Workspace `json:"items"`
		Total             int                    `json:"total"`
		ActiveWorkspaceID string                 `json:"active_workspace_id"`
	}
	decodeJSON(t, body, &list)
	if list.Total != 1 || list.ActiveWorkspaceID != alpha.WorkspaceID {
		t.Fatalf("unexpected list: total=%d active=%q", list.Total, list.ActiveWorkspaceID)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces/"+alpha.WorkspaceID+"/activate", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("activate status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodDelete, "/api/v1/workspaces/"+alpha.WorkspaceID+"/preview", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("preview status = %d, body = %s", resp.StatusCode, body)
	}
	var plan workspace.DeletePlan
	decodeJSON(t, body, &plan)
	if plan.ConfirmToken == "" || len(plan.Resources) == 0 {
		t.Fatalf("unexpected delete plan: %+v", plan)
	}

	resp, body = s.do(http.MethodDelete, "/api/v1/workspaces/"+alpha.WorkspaceID, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("delete without token status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "PREVIEW_REQUIRED" {
		t.Fatalf("delete error code = %q, want PREVIEW_REQUIRED", code)
	}

	resp, body = s.do(http.MethodDelete,
		"/api/v1/workspaces/"+alpha.WorkspaceID+"?confirm_token="+url.QueryEscape(plan.ConfirmToken),
		nil, s.tokenAuth)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("delete status = %d, body = %s", resp.StatusCode, body)
	}
	var accepted struct {
		TaskID    string `json:"task_id"`
		StatusURL string `json:"status_url"`
	}
	decodeJSON(t, body, &accepted)
	if accepted.TaskID == "" {
		t.Fatal("delete did not return a task id")
	}

	final := s.waitTask(accepted.TaskID)
	if final.Status != task.StatusSucceeded {
		t.Fatalf("delete task status = %s, error = %s", final.Status, final.ErrorMessage)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/workspaces", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list after delete status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &list)
	if list.Total != 0 {
		t.Fatalf("workspace count after delete = %d, want 0", list.Total)
	}

	resp, _ = s.do(http.MethodGet, "/api/v1/workspaces/"+alpha.WorkspaceID, nil, s.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("get deleted workspace status = %d, want 404", resp.StatusCode)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/tasks", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("task list status = %d, body = %s", resp.StatusCode, body)
	}
	var tasks struct {
		Items []task.Task `json:"items"`
		Total int         `json:"total"`
	}
	decodeJSON(t, body, &tasks)
	if tasks.Total < 1 || tasks.Items[0].Type != "workspace.delete" {
		t.Fatalf("unexpected task history: %+v", tasks)
	}
}

func TestBrowserSessionAndCSRF(t *testing.T) {
	s := newStack(t)

	resp, body := s.do(http.MethodGet, "/api/v1/system/session", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", resp.StatusCode, body)
	}
	var session struct {
		CSRFToken string    `json:"csrf_token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	decodeJSON(t, body, &session)
	if session.CSRFToken == "" {
		t.Fatal("session returned an empty CSRF token")
	}
	cookies := resp.Cookies()
	if len(cookies) == 0 {
		t.Fatal("session did not set a cookie")
	}
	cookie := cookies[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie: %+v", cookie)
	}

	withCookie := func(req *http.Request) {
		req.AddCookie(cookie)
	}

	resp, body = s.do(http.MethodGet, "/api/v1/system/info", nil, withCookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie-only GET info status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Browser"}, func(req *http.Request) {
			req.AddCookie(cookie)
			req.Header.Set("Origin", allowedOrigin)
		})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("mutation without CSRF status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "CSRF_INVALID" {
		t.Fatalf("CSRF error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Browser"}, func(req *http.Request) {
			req.AddCookie(cookie)
			req.Header.Set("Origin", "http://evil.example")
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
		})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bad-origin status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "FORBIDDEN_ORIGIN" {
		t.Fatalf("origin error code = %q", code)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Browser"}, func(req *http.Request) {
			req.AddCookie(cookie)
			req.Header.Set("Origin", allowedOrigin)
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
		})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("CSRF-protected create status = %d, body = %s", resp.StatusCode, body)
	}

	resp, body = s.do(http.MethodPost, "/api/v1/workspaces",
		map[string]string{"name": "Bearer"}, func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+s.token)
			req.Header.Set("Origin", "http://evil.example")
		})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("bearer bad-origin status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestPlaceholderRoutesAndUI(t *testing.T) {
	s := newStack(t)
	ws := s.create("Cookies")

	resp, body := s.do(http.MethodPost, "/api/v1/workspaces/"+ws.WorkspaceID+"/start", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("start status = %d, body = %s", resp.StatusCode, body)
	}
	if code := errorCode(t, body); code != "FEATURE_UNAVAILABLE" {
		t.Fatalf("start error code = %q", code)
	}

	resp, body = s.do(http.MethodGet, "/", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("DevKit Test UI")) {
		t.Fatalf("UI status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, "/some/spa/route", nil, nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("DevKit Test UI")) {
		t.Fatalf("SPA fallback status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, "/assets/missing.js", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset status = %d, body = %s", resp.StatusCode, body)
	}
	resp, body = s.do(http.MethodGet, "/api/v1/unknown", nil, nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown API status = %d, body = %s", resp.StatusCode, body)
	}
}

func TestDiagnosticsRequireAuthAndReportRoot(t *testing.T) {
	s := newStack(t)
	resp, body := s.do(http.MethodGet, "/api/v1/diagnostics", nil, s.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("diagnostics status = %d, body = %s", resp.StatusCode, body)
	}
	var report struct {
		Items  []diagnostics.Check `json:"items"`
		Counts map[string]int      `json:"counts"`
	}
	decodeJSON(t, body, &report)
	if len(report.Items) == 0 {
		t.Fatal("diagnostics returned no checks")
	}
	if report.Counts["error"] != 0 {
		t.Fatalf("diagnostics reported errors: %+v", report.Items)
	}

	ctx := context.Background()
	if _, err := s.workspaces.List(ctx); err != nil {
		t.Fatalf("workspace manager unusable: %v", err)
	}
}
