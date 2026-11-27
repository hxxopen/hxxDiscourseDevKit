// Package runtime loads the component version matrix and verifies the
// runtime tools available on the host or inside WSL2.
package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ComponentSpec is one runtime component pinned by a release matrix.
type ComponentSpec struct {
	Version string `json:"version"`
	Source  string `json:"source"`
	SHA256  string `json:"sha256,omitempty"`
	// Binary overrides the executable name or points at an absolute path.
	Binary string `json:"binary,omitempty"`
	// VersionArgs overrides the arguments used to query the version.
	VersionArgs []string `json:"version_args,omitempty"`
	// Required controls whether a missing component is an error (true) or a
	// warning (false) in diagnostics. Unset uses the component defaults.
	Required *bool `json:"required,omitempty"`
}

// Matrix is the release-pinned runtime component set.
type Matrix struct {
	Profile            string                   `json:"profile"`
	DevkitVersionRange string                   `json:"devkit_version_range,omitempty"`
	Components         map[string]ComponentSpec `json:"components"`
}

var defaultBinaries = map[string]string{
	"ruby":       "ruby",
	"node":       "node",
	"git":        "git",
	"postgresql": "pg_ctl",
	"redis":      "redis-server",
}

var defaultRequired = map[string]bool{
	"ruby":       false,
	"node":       false,
	"git":        true,
	"postgresql": true,
	"redis":      true,
}

var componentOrder = []string{"ruby", "node", "git", "postgresql", "redis"}

// DefaultMatrix is used when the instance has no matrix file yet. Versions are
// deliberately unconstrained; a release package can pin exact versions by
// installing its own runtime-matrix.json.
func DefaultMatrix() Matrix {
	matrix := Matrix{
		Profile:            "default",
		DevkitVersionRange: ">=0.1.0 <0.2.0",
		Components: map[string]ComponentSpec{
			"ruby":       {Version: "system", Source: "system-or-bundled", Binary: "ruby"},
			"node":       {Version: "system", Source: "system-or-bundled", Binary: "node"},
			"git":        {Version: "system", Source: "system-or-bundled", Binary: "git"},
			"postgresql": {Version: "system", Source: "system-or-bundled", Binary: "pg_ctl"},
			"redis":      {Version: "system", Source: "system-or-bundled", Binary: "redis-server"},
		},
	}
	// ApplyDefaults is safe here and ensures --version is always used. Never
	// invoke redis-server without arguments, which would start a server.
	_ = matrix.ApplyDefaults()
	return matrix
}

// LoadMatrix reads and validates a matrix file. The bool result reports
// whether the built-in fallback was used.
func LoadMatrix(path string) (Matrix, bool, error) {
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		matrix := DefaultMatrix()
		if err := matrix.Validate(); err != nil {
			return Matrix{}, true, err
		}
		return matrix, true, nil
	case err != nil:
		return Matrix{}, false, fmt.Errorf("read runtime matrix: %w", err)
	}
	var matrix Matrix
	if err := json.Unmarshal(raw, &matrix); err != nil {
		return Matrix{}, false, fmt.Errorf("decode %s: %w", path, err)
	}
	if err := matrix.ApplyDefaults(); err != nil {
		return Matrix{}, false, err
	}
	if err := matrix.Validate(); err != nil {
		return Matrix{}, false, fmt.Errorf("invalid runtime matrix %s: %w", path, err)
	}
	return matrix, false, nil
}

// ApplyDefaults fills binary and version-argument defaults for known
// components.
func (m *Matrix) ApplyDefaults() error {
	if m.Components == nil {
		return fmt.Errorf("components is required")
	}
	for name, spec := range m.Components {
		if spec.Binary == "" {
			spec.Binary = defaultBinaries[name]
		}
		if len(spec.VersionArgs) == 0 {
			spec.VersionArgs = []string{"--version"}
		}
		m.Components[name] = spec
	}
	return nil
}

// Validate checks the matrix invariants.
func (m Matrix) Validate() error {
	if strings.TrimSpace(m.Profile) == "" {
		return fmt.Errorf("profile is required")
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("components must not be empty")
	}
	for name, spec := range m.Components {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("component name must not be empty")
		}
		if strings.TrimSpace(spec.Version) == "" {
			return fmt.Errorf("components.%s.version is required", name)
		}
		if strings.TrimSpace(spec.Source) == "" {
			return fmt.Errorf("components.%s.source is required", name)
		}
		if spec.SHA256 != "" {
			if len(spec.SHA256) != 64 || !isHex(spec.SHA256) {
				return fmt.Errorf("components.%s.sha256 must be 64 hex characters", name)
			}
		}
		if spec.Binary != "" && !filepath.IsAbs(spec.Binary) && !filepath.IsLocal(spec.Binary) {
			return fmt.Errorf("components.%s.binary must be a local name or absolute path", name)
		}
		for _, arg := range spec.VersionArgs {
			if strings.ContainsAny(arg, "\x00\r\n") {
				return fmt.Errorf("components.%s.version_args contains control characters", name)
			}
		}
	}
	return nil
}

// Names returns component names in stable order, with unknown components last.
func (m Matrix) Names() []string {
	names := make([]string, 0, len(m.Components))
	known := map[string]bool{}
	for _, name := range componentOrder {
		if _, ok := m.Components[name]; ok {
			names = append(names, name)
			known[name] = true
		}
	}
	var extra []string
	for name := range m.Components {
		if !known[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(names, extra...)
}

// IsRequired reports whether the component is required by default.
func (m Matrix) IsRequired(name string) bool {
	spec, ok := m.Components[name]
	if !ok || spec.Required == nil {
		return defaultRequired[name]
	}
	return *spec.Required
}

func isHex(value string) bool {
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
