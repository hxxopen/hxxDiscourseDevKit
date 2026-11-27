package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeProvisioner struct {
	ensureErr error
	dropErr   error
	clearErr  error
	ensured   int
	dropped   int
	cleared   int
}

func (f *fakeProvisioner) EnsureWorkspaceDatabase(ctx context.Context, ws *Workspace) error {
	f.ensured++
	return f.ensureErr
}

func (f *fakeProvisioner) DropWorkspaceDatabase(ctx context.Context, ws *Workspace) error {
	f.dropped++
	return f.dropErr
}

func (f *fakeProvisioner) ClearWorkspaceRedis(ctx context.Context, ws *Workspace) error {
	f.cleared++
	return f.clearErr
}

func TestCreateProvisionsDatabase(t *testing.T) {
	env := newTestEnv(t)
	fake := &fakeProvisioner{}
	env.manager.SetProvisioner(fake)

	ws, err := env.manager.Create(context.Background(), CreateRequest{Name: "Provisioned"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fake.ensured != 1 {
		t.Fatalf("ensured = %d, want 1", fake.ensured)
	}
	if !ws.Database.Provisioned || ws.Database.ProvisionedAt == nil {
		t.Fatalf("database not marked provisioned: %+v", ws.Database)
	}
	if ws.Database.ProvisionError != "" {
		t.Fatalf("unexpected provision error: %s", ws.Database.ProvisionError)
	}
}

func TestCreateRecordsProvisionFailure(t *testing.T) {
	env := newTestEnv(t)
	fake := &fakeProvisioner{ensureErr: errors.New("service is not running")}
	env.manager.SetProvisioner(fake)

	ws, err := env.manager.Create(context.Background(), CreateRequest{Name: "Pending"})
	if err != nil {
		t.Fatalf("Create should not fail when provisioning fails: %v", err)
	}
	if ws.Database.Provisioned {
		t.Fatal("database should not be marked provisioned")
	}
	if !strings.Contains(ws.Database.ProvisionError, "service is not running") {
		t.Fatalf("provision error = %q", ws.Database.ProvisionError)
	}
}

func TestProvisionRetry(t *testing.T) {
	env := newTestEnv(t)
	fake := &fakeProvisioner{ensureErr: errors.New("offline")}
	env.manager.SetProvisioner(fake)
	ws, err := env.manager.Create(context.Background(), CreateRequest{Name: "Retry"})
	if err != nil {
		t.Fatal(err)
	}
	fake.ensureErr = nil
	updated, err := env.manager.Provision(context.Background(), ws.WorkspaceID)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if !updated.Database.Provisioned {
		t.Fatal("database was not provisioned after retry")
	}
	if fake.ensured != 2 {
		t.Fatalf("ensured = %d, want 2", fake.ensured)
	}
}

func TestMarkDatabaseResultUnknownWorkspace(t *testing.T) {
	env := newTestEnv(t)
	err := env.manager.MarkDatabaseResult(context.Background(), "01ZZZZZZZZZZZZZZZZZZZZZZZZZ", true, "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestCleanupServices(t *testing.T) {
	env := newTestEnv(t)
	fake := &fakeProvisioner{}
	env.manager.SetProvisioner(fake)
	ws := &Workspace{
		Database: DatabaseConfig{Provisioned: true},
		Redis:    RedisConfig{DBIndex: 3},
	}
	result := &DeleteResult{}
	opts := DeleteOptions{DeleteDatabase: true, DeleteRedis: true}
	if err := env.manager.cleanupServices(context.Background(), ws, opts, result); err != nil {
		t.Fatalf("cleanupServices: %v", err)
	}
	if fake.dropped != 1 || fake.cleared != 1 {
		t.Fatalf("dropped/cleared = %d/%d", fake.dropped, fake.cleared)
	}
	if !result.DatabaseDropped || !result.RedisCleared {
		t.Fatalf("result = %+v", result)
	}

	fake.dropErr = errors.New("database offline")
	if err := env.manager.cleanupServices(context.Background(), ws, opts, &DeleteResult{}); err == nil {
		t.Fatal("expected cleanup error")
	}
}
