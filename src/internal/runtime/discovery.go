package runtime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

const discoverySchemaVersion = 1

// DiscoveredComponent records where DevKit found a runtime component. It is
// used as a fallback when the executable is not on PATH, notably for
// PostgreSQL utilities installed by a Debian/Ubuntu package.
type DiscoveredComponent struct {
	Path       string    `json:"path"`
	Execution  string    `json:"execution"`
	Version    string    `json:"version,omitempty"`
	DetectedAt time.Time `json:"detected_at"`
}

// Discovery is the persisted component discovery result.
type Discovery struct {
	SchemaVersion int                            `json:"schema_version"`
	Components    map[string]DiscoveredComponent `json:"components"`
	UpdatedAt     time.Time                      `json:"updated_at"`
}

// DiscoveryPath returns the DevKit-managed discovery file.
func (m *Manager) DiscoveryPath() string {
	return filepath.Join(m.paths.ConfigDir, "runtime-discovered.json")
}

// LoadDiscovery reads the discovery file. A missing file yields an empty
// result rather than an error.
func (m *Manager) LoadDiscovery() (Discovery, error) {
	result := Discovery{
		SchemaVersion: discoverySchemaVersion,
		Components:    map[string]DiscoveredComponent{},
	}
	err := store.ReadJSON(m.DiscoveryPath(), &result)
	if errors.Is(err, fs.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if result.Components == nil {
		result.Components = map[string]DiscoveredComponent{}
	}
	return result, nil
}

// Discover resolves every matrix component, records the successful paths, and
// persists them. Missing components are omitted instead of failing the whole
// run.
func (m *Manager) Discover(ctx context.Context) (Discovery, error) {
	matrix, _, err := m.Matrix()
	if err != nil {
		return Discovery{}, err
	}
	result := Discovery{
		SchemaVersion: discoverySchemaVersion,
		Components:    map[string]DiscoveredComponent{},
		UpdatedAt:     time.Now().UTC(),
	}
	for _, name := range matrix.Names() {
		spec := matrix.Components[name]
		found, err := m.resolveComponent(ctx, name, spec)
		if err != nil {
			continue
		}
		entry := DiscoveredComponent{
			Path:       found.Path,
			Execution:  found.Execution,
			DetectedAt: result.UpdatedAt,
		}
		if version, err := m.runVersion(ctx, found, spec); err == nil {
			entry.Version = version
		}
		result.Components[name] = entry
	}
	if err := store.WriteJSON(m.DiscoveryPath(), result, 0o600); err != nil {
		return result, fmt.Errorf("write runtime discovery: %w", err)
	}
	return result, nil
}

// FindComponent resolves a component using the matrix, the persisted
// discovery result, and finally PATH/WSL lookup.
func (m *Manager) FindComponent(ctx context.Context, name string) (Found, error) {
	matrix, _, err := m.Matrix()
	if err != nil {
		return Found{}, err
	}
	spec, ok := matrix.Components[name]
	if !ok {
		return Found{}, fmt.Errorf("%w: %s", ErrUnknownComponent, name)
	}
	return m.findComponent(ctx, name, spec)
}

// findComponent resolves one component. A persisted discovery entry wins over
// a live PATH/WSL lookup because Debian/Ubuntu install PostgreSQL utilities
// outside PATH; verification and service startup must agree on that path.
func (m *Manager) findComponent(ctx context.Context, name string, spec ComponentSpec) (Found, error) {
	if spec.Binary != "" && (filepath.IsAbs(spec.Binary) || (goruntime.GOOS == "windows" && strings.HasPrefix(spec.Binary, "/"))) {
		return m.findSpecBinary(ctx, spec)
	}
	if discovered, err := m.LoadDiscovery(); err == nil {
		if entry, ok := discovered.Components[name]; ok {
			found := Found{Binary: componentBase(entry.Path, entry.Execution), Path: entry.Path, Execution: entry.Execution}
			if m.Exists(ctx, found, entry.Path) && m.discoveryUsable(ctx, entry.Execution) {
				return found, nil
			}
		}
	}
	return m.resolveComponent(ctx, name, spec)
}

// discoveryUsable reports whether a persisted discovery entry may be used.
// On Windows the runtime is WSL2, so a native entry (for example Node or Git
// resolved before WSL was set up) is stale: running it against a Linux
// workspace fails or pollutes the tree with Windows artifacts. Re-resolve in
// WSL instead; the next Discover run rewrites the entry.
func (m *Manager) discoveryUsable(ctx context.Context, execution string) bool {
	if goruntime.GOOS != "windows" || execution != ExecutionNative {
		return true
	}
	return m.WSLDistro(ctx) == ""
}

func (m *Manager) resolveComponent(ctx context.Context, name string, spec ComponentSpec) (Found, error) {
	if found, err := m.findSpecBinary(ctx, spec); err == nil {
		return found, nil
	}
	if name == "postgresql" {
		if found, err := m.findPostgresInWSL(ctx); err == nil {
			return found, nil
		}
	}
	return Found{}, fmt.Errorf("component %s is not available", name)
}

func (m *Manager) findPostgresInWSL(ctx context.Context) (Found, error) {
	distro := m.WSLDistro(ctx)
	if distro == "" {
		return Found{}, fmt.Errorf("no WSL distribution")
	}
	output, err := runWSL(ctx, distro, false, "find", "/usr/lib/postgresql", "-maxdepth", "3", "-name", "pg_ctl", "-type", "f", "-print")
	if err != nil {
		return Found{}, err
	}
	var candidates []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		candidates = append(candidates, line)
	}
	if len(candidates) == 0 {
		return Found{}, fmt.Errorf("pg_ctl not found in %s", distro)
	}
	target := sortPostgresCandidates(candidates)[0]
	return Found{
		Binary:    "pg_ctl",
		Path:      target,
		Execution: executionWSL + distro,
	}, nil
}

func (m *Manager) runVersion(ctx context.Context, found Found, spec ComponentSpec) (string, error) {
	args := spec.VersionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	result, _ := m.RunFound(runCtx, found, RunOptions{Args: args})
	version := ExtractVersion(result.Stdout + "\n" + result.Stderr)
	if version == "" {
		return "", fmt.Errorf("version not detected")
	}
	return version, nil
}

func componentBase(value, execution string) string {
	if strings.HasPrefix(execution, executionWSL) {
		return path.Base(value)
	}
	return filepath.Base(value)
}

func (d Discovery) Names() []string {
	names := make([]string, 0, len(d.Components))
	for name := range d.Components {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
