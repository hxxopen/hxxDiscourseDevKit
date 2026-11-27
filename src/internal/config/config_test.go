package config

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigIsValid(t *testing.T) {
	paths := NewPaths(t.TempDir())
	cfg := Default(paths)
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
	if cfg.Network.APIPort != 17860 {
		t.Errorf("api port = %d, want 17860", cfg.Network.APIPort)
	}
	if cfg.Services.PostgreSQL.Port != 15432 {
		t.Errorf("postgres port = %d, want 15432", cfg.Services.PostgreSQL.Port)
	}
	if cfg.Services.Redis.Port != 16379 {
		t.Errorf("redis port = %d, want 16379", cfg.Services.Redis.Port)
	}
	if cfg.ActiveWorkspaceID != "" || cfg.WorkspacesInitialized {
		t.Error("new config should not claim an initialized workspace")
	}
}

func TestApplyDefaultsFillsManagedServiceFields(t *testing.T) {
	paths := NewPaths(t.TempDir())
	cfg := Config{}
	cfg.Services.PostgreSQL.Mode = "managed"
	cfg.Services.Redis.Mode = "managed"

	cfg.ApplyDefaults(paths)

	pg := cfg.Services.PostgreSQL
	if pg.Host != "127.0.0.1" || pg.Port != 15432 || pg.AdminUser != "hxxddk_admin" ||
		pg.AdminPasswordRef != "pg_admin" || pg.DataDir != "services/postgresql/data" {
		t.Errorf("postgres defaults = %+v", pg)
	}
	redis := cfg.Services.Redis
	if redis.Host != "127.0.0.1" || redis.Port != 16379 ||
		redis.PasswordRef != "redis_password" || redis.DataDir != "services/redis/data" {
		t.Errorf("redis defaults = %+v", redis)
	}
}

func TestValidateRejectsUnsafeNetwork(t *testing.T) {
	paths := NewPaths(t.TempDir())
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"non-loopback host", func(c *Config) { c.Network.Host = "0.0.0.0" }},
		{"external host", func(c *Config) { c.Network.Host = "192.168.1.10" }},
		{"port zero", func(c *Config) { c.Network.APIPort = 0 }},
		{"port too large", func(c *Config) { c.Network.APIPort = 70000 }},
		{"bad log level", func(c *Config) { c.Logging.Level = "verbose" }},
		{"absolute matrix path", func(c *Config) { c.Runtime.MatrixFile = "/tmp/matrix.json" }},
		{"bad pg mode", func(c *Config) { c.Services.PostgreSQL.Mode = "auto" }},
		{"external pg without host", func(c *Config) {
			c.Services.PostgreSQL.Mode = "external"
			c.Services.PostgreSQL.External = ExternalEndpoint{Port: 5432}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Default(paths)
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestStoreCreatesAndPersists(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(paths)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cfg := store.Get()
	if len(cfg.InstanceID) != 26 {
		t.Fatalf("instance id = %q, want a ULID", cfg.InstanceID)
	}

	if err := store.Update(func(c *Config) error {
		c.ActiveWorkspaceID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	reopened, err := Open(paths)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if got := reopened.Get().ActiveWorkspaceID; got != "01ARZ3NDEKTSV4RRFFQ69G5FAV" {
		t.Fatalf("persisted active workspace = %q", got)
	}
}

func TestStoreRejectsInvalidUpdate(t *testing.T) {
	paths := NewPaths(t.TempDir())
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Update(func(c *Config) error {
		c.Network.Host = "0.0.0.0"
		return nil
	}); err == nil {
		t.Fatal("Update accepted a non-loopback host")
	}
	if got := store.Get().Network.Host; got != "127.0.0.1" {
		t.Fatalf("in-memory config changed after rejected update: %q", got)
	}
}

func TestSetDataRootRelocatesProjectAndServiceData(t *testing.T) {
	root := t.TempDir()
	dataRoot := t.TempDir()
	paths := NewPaths(root)
	paths.SetDataRoot(dataRoot)

	if !paths.UsesSplitDataRoot() {
		t.Fatal("expected split data root")
	}
	if paths.ConfigDir != filepath.Join(root, "config") {
		t.Errorf("config dir moved unexpectedly: %s", paths.ConfigDir)
	}
	if paths.WorkspacesDir != filepath.Join(dataRoot, "workspaces") {
		t.Errorf("workspaces dir = %s", paths.WorkspacesDir)
	}
	got := paths.ResolveDataDir("services/postgresql/data")
	want := filepath.Join(dataRoot, "services", "postgresql", "data")
	if got != want {
		t.Errorf("ResolveDataDir = %s, want %s", got, want)
	}

	paths.SetDataRoot("")
	if paths.UsesSplitDataRoot() || paths.DataRoot != paths.Root {
		t.Errorf("empty data root should fall back to root, got %s", paths.DataRoot)
	}
}

func TestValidateStorageDataRoot(t *testing.T) {
	paths := NewPaths(t.TempDir())
	cfg := Default(paths)
	cfg.Storage.DataRoot = "relative/data"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected relative data root to be rejected")
	}
	cfg.Storage.DataRoot = `\\wsl.localhost\Ubuntu-22.04\home\dev\data`
	if err := cfg.Validate(); err != nil {
		t.Fatalf("UNC data root rejected: %v", err)
	}
}
