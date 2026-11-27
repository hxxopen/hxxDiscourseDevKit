package app_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/app"
)

// TestDaemonEndToEnd starts a real daemon on a loopback port and exercises the
// session, workspace, log, settings, SSE, task, and delete flows over HTTP.
func TestDaemonEndToEnd(t *testing.T) {
	port := freePort(t)
	root := t.TempDir()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- app.Run(ctx, app.Options{Root: root, Port: port})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("daemon exited with error: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Errorf("daemon did not stop within 20s")
		}
	})

	waitHealthy(t, base, done)
	client := newE2EClient(t, base, port)

	// SSE must be available after the session cookie is established.
	assertSSEReady(t, client)

	total := client.listWorkspaces(t)
	if total != 1 {
		t.Fatalf("initial workspace count = %d, want 1 (Default)", total)
	}

	alpha := client.createWorkspace(t, "E2E Alpha")
	if alpha.WorkspaceID == "" || alpha.Name != "E2E Alpha" {
		t.Fatalf("unexpected workspace: %+v", alpha)
	}
	if got := client.listWorkspaces(t); got != 2 {
		t.Fatalf("workspace count after create = %d, want 2", got)
	}
	client.activateWorkspace(t, alpha.WorkspaceID)

	sources := client.logSources(t)
	if !sources.hasPrefix("devkit:") {
		t.Fatalf("no devkit log source in %v", sources.ids)
	}
	if !sources.has("workspace:" + alpha.WorkspaceID + ":project") {
		t.Fatalf("no project log source for %s in %v", alpha.WorkspaceID, sources.ids)
	}
	lines := client.readLog(t, sources.firstPrefix("devkit:"), 20)
	if len(lines) == 0 {
		t.Fatalf("devkit log is empty")
	}

	settings := client.getSettings(t)
	if settings.Config.Network.APIPort != 17860 {
		t.Fatalf("settings api_port = %d, want config default 17860", settings.Config.Network.APIPort)
	}
	if !contains(settings.RestartRequired, "network.api_port") {
		t.Fatalf("restart_required = %v, want network.api_port (daemon runs on a temporary port)",
			settings.RestartRequired)
	}
	saved := client.patchSettings(t, map[string]any{
		"project": map[string]any{"default_branch": "e2e-branch"},
		"logging": map[string]any{"level": "debug"},
	})
	if saved.Config.Project.DefaultBranch != "e2e-branch" || saved.Config.Logging.Level != "debug" {
		t.Fatalf("settings not persisted: %+v", saved.Config)
	}

	plan := client.previewDelete(t, alpha.WorkspaceID)
	if plan.ConfirmToken == "" {
		t.Fatalf("delete preview has no confirm token")
	}
	taskID := client.deleteWorkspace(t, alpha.WorkspaceID, plan.ConfirmToken)
	client.waitTask(t, taskID, 15*time.Second)
	if got := client.listWorkspaces(t); got != 1 {
		t.Fatalf("workspace count after delete = %d, want 1", got)
	}
}

type e2eWorkspace struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

type e2eLogSources struct {
	ids []string
}

func (s e2eLogSources) has(id string) bool {
	for _, item := range s.ids {
		if item == id {
			return true
		}
	}
	return false
}

func (s e2eLogSources) hasPrefix(prefix string) bool {
	return s.firstPrefix(prefix) != ""
}

func (s e2eLogSources) firstPrefix(prefix string) string {
	for _, item := range s.ids {
		if strings.HasPrefix(item, prefix) {
			return item
		}
	}
	return ""
}

type e2eSettings struct {
	Config struct {
		Network struct {
			APIPort int `json:"api_port"`
		} `json:"network"`
		Project struct {
			DefaultBranch string `json:"default_branch"`
		} `json:"project"`
		Logging struct {
			Level string `json:"level"`
		} `json:"logging"`
	} `json:"config"`
	RestartRequired []string `json:"restart_required"`
}

type e2eClient struct {
	t      *testing.T
	base   string
	port   int
	client *http.Client
	csrf   string
}

func newE2EClient(t *testing.T, base string, port int) *e2eClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	client := &e2eClient{
		t:      t,
		base:   base,
		port:   port,
		client: &http.Client{Jar: jar, Timeout: 15 * time.Second},
	}
	status, body := client.do(http.MethodGet, "/api/v1/system/session", nil)
	if status != http.StatusOK {
		t.Fatalf("session status = %d, body = %s", status, body)
	}
	var session struct {
		CSRFToken string `json:"csrf_token"`
	}
	decode(t, body, &session)
	if session.CSRFToken == "" {
		t.Fatalf("empty CSRF token")
	}
	client.csrf = session.CSRFToken
	return client
}

func (c *e2eClient) do(method, path string, body any) (int, []byte) {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("request: %v", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method != http.MethodGet {
		req.Header.Set("X-CSRF-Token", c.csrf)
		req.Header.Set("Origin", c.base)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		c.t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, data
}

func (c *e2eClient) listWorkspaces(t *testing.T) int {
	t.Helper()
	status, body := c.do(http.MethodGet, "/api/v1/workspaces", nil)
	if status != http.StatusOK {
		t.Fatalf("list workspaces status = %d, body = %s", status, body)
	}
	var list struct {
		Total int `json:"total"`
	}
	decode(t, body, &list)
	return list.Total
}

func (c *e2eClient) createWorkspace(t *testing.T, name string) e2eWorkspace {
	t.Helper()
	status, body := c.do(http.MethodPost, "/api/v1/workspaces", map[string]string{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create workspace status = %d, body = %s", status, body)
	}
	var ws e2eWorkspace
	decode(t, body, &ws)
	return ws
}

func (c *e2eClient) activateWorkspace(t *testing.T, id string) {
	t.Helper()
	status, body := c.do(http.MethodPost, "/api/v1/workspaces/"+url.PathEscape(id)+"/activate", nil)
	if status != http.StatusOK {
		t.Fatalf("activate status = %d, body = %s", status, body)
	}
}

func (c *e2eClient) logSources(t *testing.T) e2eLogSources {
	t.Helper()
	status, body := c.do(http.MethodGet, "/api/v1/logs/sources", nil)
	if status != http.StatusOK {
		t.Fatalf("log sources status = %d, body = %s", status, body)
	}
	var list struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	decode(t, body, &list)
	result := e2eLogSources{}
	for _, item := range list.Items {
		result.ids = append(result.ids, item.ID)
	}
	return result
}

func (c *e2eClient) readLog(t *testing.T, source string, tail int) []string {
	t.Helper()
	path := "/api/v1/logs?source=" + url.QueryEscape(source) + "&tail=" + strconv.Itoa(tail)
	status, body := c.do(http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("read log status = %d, body = %s", status, body)
	}
	var content struct {
		Lines []string `json:"lines"`
	}
	decode(t, body, &content)
	return content.Lines
}

func (c *e2eClient) getSettings(t *testing.T) e2eSettings {
	t.Helper()
	status, body := c.do(http.MethodGet, "/api/v1/settings", nil)
	if status != http.StatusOK {
		t.Fatalf("get settings status = %d, body = %s", status, body)
	}
	var settings e2eSettings
	decode(t, body, &settings)
	return settings
}

func (c *e2eClient) patchSettings(t *testing.T, patch map[string]any) e2eSettings {
	t.Helper()
	status, body := c.do(http.MethodPatch, "/api/v1/settings", patch)
	if status != http.StatusOK {
		t.Fatalf("patch settings status = %d, body = %s", status, body)
	}
	var settings e2eSettings
	decode(t, body, &settings)
	return settings
}

func (c *e2eClient) previewDelete(t *testing.T, id string) struct {
	ConfirmToken string `json:"confirm_token"`
} {
	t.Helper()
	status, body := c.do(http.MethodDelete, "/api/v1/workspaces/"+url.PathEscape(id)+"/preview", nil)
	if status != http.StatusOK {
		t.Fatalf("preview delete status = %d, body = %s", status, body)
	}
	var plan struct {
		ConfirmToken string `json:"confirm_token"`
	}
	decode(t, body, &plan)
	return plan
}

func (c *e2eClient) deleteWorkspace(t *testing.T, id, token string) string {
	t.Helper()
	path := "/api/v1/workspaces/" + url.PathEscape(id) + "?confirm_token=" + url.QueryEscape(token)
	status, body := c.do(http.MethodDelete, path, nil)
	if status != http.StatusAccepted {
		t.Fatalf("delete status = %d, body = %s", status, body)
	}
	var accepted struct {
		TaskID string `json:"task_id"`
	}
	decode(t, body, &accepted)
	return accepted.TaskID
}

func (c *e2eClient) waitTask(t *testing.T, id string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status, body := c.do(http.MethodGet, "/api/v1/tasks/"+url.PathEscape(id), nil)
		if status != http.StatusOK {
			t.Fatalf("get task status = %d, body = %s", status, body)
		}
		var task struct {
			Status string `json:"status"`
		}
		decode(t, body, &task)
		switch task.Status {
		case "succeeded":
			return
		case "failed", "canceled":
			t.Fatalf("task %s ended as %s: %s", id, task.Status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("task %s did not finish within %s", id, timeout)
}

func assertSSEReady(t *testing.T, c *e2eClient) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("sse request: %v", err)
	}
	resp, err := (&http.Client{Jar: c.client.Jar}).Do(req)
	if err != nil {
		t.Fatalf("sse connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sse status = %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read sse ready: %v", err)
	}
	if strings.TrimSpace(line) != "event: ready" {
		t.Fatalf("unexpected first SSE frame: %q", line)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

func waitHealthy(t *testing.T, base string, done <-chan error) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("daemon exited before healthy: %v", err)
		default:
		}
		resp, err := http.Get(base + "/api/v1/system/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("daemon did not become healthy at %s", base)
}

func decode(t *testing.T, data []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
