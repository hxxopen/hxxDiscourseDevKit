package workspace

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
)

type testEnv struct {
	manager *Manager
	cfg     *config.Store
	secrets *secret.Store
	paths   *config.Paths
	root    string
}

func newTestEnv(t *testing.T) *testEnv {
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
	manager := New(paths, cfg, secrets, logger, Options{
		PortStart: 32000,
		BindHost:  "127.0.0.1",
	})
	return &testEnv{manager: manager, cfg: cfg, secrets: secrets, paths: paths, root: root}
}

func TestEnsureDefaultCreatesOnce(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	ws, created, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if !created || ws == nil {
		t.Fatal("Default workspace was not created")
	}
	if ws.Name != "Default" || ws.State != StateEmpty {
		t.Fatalf("unexpected workspace: %+v", ws)
	}
	if ws.Database.DatabaseName == "" || ws.Database.PasswordRef == "" {
		t.Fatal("database namespace was not assigned")
	}
	if ws.Network.HTTPPort < 32000 {
		t.Fatalf("HTTP port = %d, want >= 32000", ws.Network.HTTPPort)
	}
	for _, sub := range []string{"source", "config", "logs", "data", "backups", "tmp", "workspace.json"} {
		if _, err := os.Stat(filepath.Join(env.paths.WorkspaceDir(ws.WorkspaceID), sub)); err != nil {
			t.Errorf("missing %s: %v", sub, err)
		}
	}
	password, ok := env.secrets.Get(ws.Database.PasswordRef)
	if !ok || len(password) < 16 {
		t.Fatal("database credential was not stored")
	}
	if got := env.cfg.Get().ActiveWorkspaceID; got != ws.WorkspaceID {
		t.Fatalf("active workspace = %q, want %q", got, ws.WorkspaceID)
	}
	if !env.cfg.Get().WorkspacesInitialized {
		t.Fatal("workspaces_initialized was not persisted")
	}

	again, createdAgain, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("second EnsureDefault: %v", err)
	}
	if createdAgain || again != nil {
		t.Fatal("EnsureDefault created a second Default workspace")
	}
}

func TestEnsureDefaultDoesNotResurrectAfterInitialization(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if err := env.cfg.Update(func(c *config.Config) error {
		c.WorkspacesInitialized = true
		return nil
	}); err != nil {
		t.Fatalf("mark initialized: %v", err)
	}

	ws, created, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if created || ws != nil {
		t.Fatal("EnsureDefault recreated Default after the workspace list was emptied")
	}
	if !env.cfg.Get().WorkspacesInitialized {
		t.Fatal("workspaces_initialized flag was cleared")
	}
}

func TestCreateAllocatesIsolation(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	def, _, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := env.manager.Create(ctx, CreateRequest{Name: "Alpha", Description: "first"})
	if err != nil {
		t.Fatalf("Create Alpha: %v", err)
	}
	beta, err := env.manager.Create(ctx, CreateRequest{Name: "Beta", ProjectType: "discourse"})
	if err != nil {
		t.Fatalf("Create Beta: %v", err)
	}

	ports := map[int]bool{}
	names := map[string]bool{}
	redis := map[int]bool{}
	for _, ws := range []*Workspace{def, alpha, beta} {
		if ports[ws.Network.HTTPPort] {
			t.Fatalf("duplicate HTTP port %d", ws.Network.HTTPPort)
		}
		ports[ws.Network.HTTPPort] = true
		if names[ws.Database.DatabaseName] {
			t.Fatalf("duplicate database %q", ws.Database.DatabaseName)
		}
		names[ws.Database.DatabaseName] = true
		if redis[ws.Redis.DBIndex] {
			t.Fatalf("duplicate Redis DB %d", ws.Redis.DBIndex)
		}
		redis[ws.Redis.DBIndex] = true
		if password, ok := env.secrets.Get(ws.Database.PasswordRef); !ok || len(password) < 16 {
			t.Fatalf("password missing for %s", ws.Name)
		}
	}

	list, err := env.manager.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("List length = %d, want 3", len(list))
	}

	if _, err := env.manager.Create(ctx, CreateRequest{Name: "alpha"}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("duplicate create error = %v, want ErrNameConflict", err)
	}

	if _, err := env.manager.Update(ctx, alpha.WorkspaceID, UpdateRequest{Name: stringPtr("beta")}); !errors.Is(err, ErrNameConflict) {
		t.Fatalf("rename conflict error = %v, want ErrNameConflict", err)
	}
	updated, err := env.manager.Update(ctx, alpha.WorkspaceID, UpdateRequest{
		Name:        stringPtr("Alpha Renamed"),
		Description: stringPtr("updated"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Alpha Renamed" || updated.Description != "updated" {
		t.Fatalf("update did not apply: %+v", updated)
	}

	if _, err := env.manager.SetActive(ctx, beta.WorkspaceID); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if got := env.cfg.Get().ActiveWorkspaceID; got != beta.WorkspaceID {
		t.Fatalf("active workspace = %q, want %q", got, beta.WorkspaceID)
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"Default", "我的 工作区", "alpha-1", "a b", strings.Repeat("x", 64)}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}
	invalid := map[string]error{
		"":       ErrValidation,
		".":      ErrInvalidName,
		"..":     ErrInvalidName,
		"a/b":    ErrInvalidName,
		`a\b`:    ErrInvalidName,
		"a:b":    ErrInvalidName,
		"a*b":    ErrInvalidName,
		"a?b":    ErrInvalidName,
		`a"b`:    ErrInvalidName,
		"a<b":    ErrInvalidName,
		"a>b":    ErrInvalidName,
		"a|b":    ErrInvalidName,
		"a\x00b": ErrInvalidName,
	}
	for name, want := range invalid {
		if err := ValidateName(name); !errors.Is(err, want) {
			t.Errorf("ValidateName(%q) = %v, want %v", name, err, want)
		}
	}
	if err := ValidateName(strings.Repeat("x", 65)); !errors.Is(err, ErrValidation) {
		t.Errorf("long name error = %v, want ErrValidation", err)
	}
}

func TestCreateRejectsInvalidName(t *testing.T) {
	env := newTestEnv(t)
	if _, err := env.manager.Create(context.Background(), CreateRequest{Name: "bad/name"}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("error = %v, want ErrInvalidName", err)
	}
	if _, err := env.manager.Create(context.Background(), CreateRequest{Name: "  "}); !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

func TestDeleteKeepsUnselectedResources(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ws, _, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dataFile := filepath.Join(env.paths.WorkspaceDir(ws.WorkspaceID), "data", "upload.bin")
	if err := os.WriteFile(dataFile, make([]byte, 123), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := env.manager.PreviewDelete(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatalf("PreviewDelete: %v", err)
	}
	if plan.Running {
		t.Fatal("plan reports running for an EMPTY workspace")
	}
	if plan.ConfirmToken == "" || plan.TotalBytes < 123 {
		t.Fatalf("unexpected plan: total=%d token=%q", plan.TotalBytes, plan.ConfirmToken)
	}
	foundData := false
	for _, resource := range plan.Resources {
		if resource.Kind == "data" {
			foundData = true
			if !resource.Exists || resource.SizeBytes < 123 {
				t.Fatalf("data resource = %+v", resource)
			}
		}
	}
	if !foundData {
		t.Fatal("preview does not list the data directory")
	}

	result, err := env.manager.Delete(ctx, ws.WorkspaceID, DeleteOptions{ConfirmToken: plan.ConfirmToken})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !contains(result.Removed, "metadata") {
		t.Fatalf("metadata was not removed: %+v", result)
	}
	if !contains(result.Kept, "data") || !contains(result.Kept, "source") {
		t.Fatalf("unselected resources were not kept: %+v", result)
	}
	if _, err := os.Stat(dataFile); err != nil {
		t.Fatalf("data file was removed: %v", err)
	}
	if _, err := env.manager.Get(ctx, ws.WorkspaceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after delete = %v, want ErrNotFound", err)
	}
	if err := env.manager.ValidateDeleteToken(ws.WorkspaceID, plan.ConfirmToken); !errors.Is(err, ErrConfirmToken) {
		t.Fatalf("token was not consumed: %v", err)
	}
	if _, created, err := env.manager.EnsureDefault(ctx); err != nil || created {
		t.Fatalf("Default was recreated after deletion: created=%v err=%v", created, err)
	}
}

func TestDeletePurgeRemovesWorkspace(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, _, err := env.manager.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := env.manager.Create(ctx, CreateRequest{Name: "Second"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := env.manager.PreviewDelete(ctx, second.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := env.manager.Delete(ctx, second.WorkspaceID, DeleteOptions{
		ConfirmToken:   plan.ConfirmToken,
		DeleteConfig:   true,
		DeleteSource:   true,
		DeleteData:     true,
		DeleteDatabase: true,
		DeleteRedis:    true,
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !contains(result.Removed, "source") || !contains(result.Removed, "data") {
		t.Fatalf("purge did not remove resources: %+v", result)
	}
	if _, err := os.Stat(env.paths.WorkspaceDir(second.WorkspaceID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("workspace directory still exists: %v", err)
	}
	if _, ok := env.secrets.Get(second.Database.PasswordRef); ok {
		t.Fatal("workspace credential was not deleted")
	}
}

func TestDeleteRejectsRunningWorkspace(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ws, _, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ws.State = StateRunning
	ws.Project.ProcessState = "RUNNING"
	ws.UpdatedAt = time.Now().UTC()
	if err := env.manager.writeWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	plan, err := env.manager.PreviewDelete(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Running {
		t.Fatal("preview did not report a running workspace")
	}
	if _, err := env.manager.Delete(ctx, ws.WorkspaceID, DeleteOptions{ConfirmToken: plan.ConfirmToken}); !errors.Is(err, ErrRunning) {
		t.Fatalf("Delete error = %v, want ErrRunning", err)
	}
	if _, err := env.manager.Get(ctx, ws.WorkspaceID); err != nil {
		t.Fatalf("workspace disappeared after rejected delete: %v", err)
	}
}

func TestReconcileClearsStaleRunningState(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	ws, _, err := env.manager.EnsureDefault(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ws.State = StateRunning
	ws.Project.ProcessState = "RUNNING"
	ws.Project.PID = 1234
	ws.UpdatedAt = time.Now().UTC()
	if err := env.manager.writeWorkspace(ws); err != nil {
		t.Fatal(err)
	}
	if err := env.manager.Reconcile(ctx); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	reconciled, err := env.manager.Get(ctx, ws.WorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if reconciled.State != StateError {
		t.Fatalf("state = %s, want ERROR", reconciled.State)
	}
	if reconciled.Project.ProcessState != "STOPPED" || reconciled.Project.PID != 0 {
		t.Fatalf("process state not cleared: %+v", reconciled.Project)
	}
	if reconciled.LastError == "" {
		t.Fatal("last_error was not set")
	}
}

func stringPtr(value string) *string { return &value }

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
