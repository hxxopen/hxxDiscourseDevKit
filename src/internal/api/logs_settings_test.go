package api_test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
)

type logSourceView struct {
	ID       string `json:"id"`
	Category string `json:"category"`
	Exists   bool   `json:"exists"`
}

type settingsView struct {
	Config config.Config `json:"config"`
	Paths  struct {
		Root       string `json:"root"`
		ConfigFile string `json:"config_file"`
	} `json:"paths"`
	RestartRequired []string `json:"restart_required"`
}

func TestLogsListAndRead(t *testing.T) {
	stack := newStack(t)
	ws := stack.create("Logs")
	paths := config.NewPaths(stack.root)

	devkitLog := filepath.Join(paths.LogsDir, "devkit-20260101.jsonl")
	if err := os.WriteFile(devkitLog, []byte("{\"msg\":\"first\"}\n{\"msg\":\"second\"}\n"), 0o600); err != nil {
		t.Fatalf("write devkit log: %v", err)
	}
	projectLog := filepath.Join(paths.WorkspaceDir(ws.WorkspaceID), "logs", "project.log")
	if err := os.MkdirAll(filepath.Dir(projectLog), 0o755); err != nil {
		t.Fatalf("create workspace log dir: %v", err)
	}
	if err := os.WriteFile(projectLog, []byte("boot\nlistening on 3000\n"), 0o600); err != nil {
		t.Fatalf("write project log: %v", err)
	}

	resp, body := stack.do(http.MethodGet, "/api/v1/logs/sources", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("log sources status = %d, body = %s", resp.StatusCode, body)
	}
	var list struct {
		Items []logSourceView `json:"items"`
		Total int             `json:"total"`
	}
	decodeJSON(t, body, &list)
	if list.Total != len(list.Items) {
		t.Fatalf("total = %d, items = %d", list.Total, len(list.Items))
	}
	ids := make([]string, 0, len(list.Items))
	for _, item := range list.Items {
		ids = append(ids, item.ID)
		if item.ID == "devkit:devkit-20260101.jsonl" && !item.Exists {
			t.Fatalf("devkit source reported missing")
		}
	}
	if !slices.Contains(ids, "devkit:devkit-20260101.jsonl") {
		t.Fatalf("devkit source missing from %v", ids)
	}
	workspaceSource := "workspace:" + ws.WorkspaceID + ":project"
	if !slices.Contains(ids, workspaceSource) {
		t.Fatalf("workspace source %q missing from %v", workspaceSource, ids)
	}

	resp, body = stack.do(http.MethodGet, "/api/v1/logs?source=devkit:devkit-20260101.jsonl&tail=1", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read log status = %d, body = %s", resp.StatusCode, body)
	}
	var content struct {
		Lines []string `json:"lines"`
		Total int      `json:"total"`
	}
	decodeJSON(t, body, &content)
	if content.Total != 1 || len(content.Lines) != 1 || !strings.Contains(content.Lines[0], "second") {
		t.Fatalf("unexpected tail: %+v", content)
	}

	resp, _ = stack.do(http.MethodGet, "/api/v1/logs?source="+workspaceSource, nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("read workspace log status = %d", resp.StatusCode)
	}

	resp, _ = stack.do(http.MethodGet, "/api/v1/logs?source=../../config/devkit.json", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("path traversal status = %d, want 404", resp.StatusCode)
	}
	resp, _ = stack.do(http.MethodGet, "/api/v1/logs?source=tasks&tail=5000", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid tail status = %d, want 400", resp.StatusCode)
	}
}

func TestSettingsReadAndUpdate(t *testing.T) {
	stack := newStack(t)

	resp, body := stack.do(http.MethodGet, "/api/v1/settings", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings status = %d, body = %s", resp.StatusCode, body)
	}
	var snapshot settingsView
	decodeJSON(t, body, &snapshot)
	if snapshot.Config.Network.APIPort != 17860 {
		t.Fatalf("api port = %d, want 17860", snapshot.Config.Network.APIPort)
	}
	if snapshot.Paths.Root != stack.root {
		t.Fatalf("root = %q, want %q", snapshot.Paths.Root, stack.root)
	}
	if snapshot.Paths.ConfigFile == "" {
		t.Fatalf("config_file is empty")
	}

	resp, body = stack.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"network": map[string]any{"api_port": 18080},
		"project": map[string]any{
			"default_repository_url": "https://example.com/custom.git",
			"start_timeout_seconds":  240,
		},
		"logging": map[string]any{"level": "debug", "retention_days": 7},
		"updates": map[string]any{"channel": "beta", "check_on_start": false},
	}, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch settings status = %d, body = %s", resp.StatusCode, body)
	}
	decodeJSON(t, body, &snapshot)
	if snapshot.Config.Network.APIPort != 18080 {
		t.Fatalf("patched api port = %d", snapshot.Config.Network.APIPort)
	}
	if snapshot.Config.Logging.Level != "debug" || snapshot.Config.Updates.Channel != "beta" {
		t.Fatalf("unexpected patched config: %+v", snapshot.Config)
	}
	if *stack.logLevel != "debug" {
		t.Fatalf("log level callback = %q, want debug", *stack.logLevel)
	}
	for _, field := range []string{"network.api_port", "project.start_timeout_seconds", "logging.retention_days"} {
		if !slices.Contains(snapshot.RestartRequired, field) {
			t.Fatalf("restart_required missing %q: %v", field, snapshot.RestartRequired)
		}
	}

	resp, body = stack.do(http.MethodPatch, "/api/v1/settings", map[string]any{
		"network": map[string]any{"api_port": 0},
	}, stack.tokenAuth)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid patch status = %d, want 400, body = %s", resp.StatusCode, body)
	}

	resp, body = stack.do(http.MethodGet, "/api/v1/settings", nil, stack.tokenAuth)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get settings after invalid patch status = %d", resp.StatusCode)
	}
	decodeJSON(t, body, &snapshot)
	if snapshot.Config.Network.APIPort != 18080 {
		t.Fatalf("invalid patch changed config: api port = %d", snapshot.Config.Network.APIPort)
	}
}
