package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

// TestVerifyUsesDiscoveryEntry guards against verification and service startup
// resolving a component differently. PostgreSQL utilities are installed under
// /usr/lib/postgresql/<ver>/bin and are not on PATH, so verification must read
// the same persisted discovery result that service startup uses.
func TestVerifyUsesDiscoveryEntry(t *testing.T) {
	root := t.TempDir()
	paths := config.NewPaths(root)
	if err := paths.Ensure(); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	manager := New(paths, cfg, nil)

	binary := filepath.Join(root, "fake-redis-server")
	if err := os.WriteFile(binary, []byte("not an executable"), 0o600); err != nil {
		t.Fatal(err)
	}
	discovery := Discovery{
		SchemaVersion: discoverySchemaVersion,
		Components: map[string]DiscoveredComponent{
			"redis": {Path: binary, Execution: ExecutionNative, DetectedAt: time.Now().UTC()},
		},
	}
	if err := store.WriteJSON(manager.DiscoveryPath(), discovery, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if distro := manager.WSLDistro(ctx); distro != "" {
		// On Windows the runtime is WSL2, so a native discovery entry is
		// intentionally ignored: verification must resolve through WSL.
		status, err := manager.Verify(ctx, "redis")
		if err != nil {
			t.Fatal(err)
		}
		if status.Available && status.Path == binary {
			t.Fatalf("native discovery entry was used although WSL(%s) is available", distro)
		}
		return
	}

	status, err := manager.Verify(ctx, "redis")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Available {
		t.Fatalf("redis was not resolved through discovery: %+v", status)
	}
	if status.Path != binary {
		t.Fatalf("path = %q, want discovery path %q", status.Path, binary)
	}
	if status.Execution != ExecutionNative {
		t.Fatalf("execution = %q, want %q", status.Execution, ExecutionNative)
	}
}
