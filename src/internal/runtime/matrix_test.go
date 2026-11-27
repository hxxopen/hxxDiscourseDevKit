package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultMatrixIsValidAndUsesVersionFlags(t *testing.T) {
	matrix := DefaultMatrix()
	if err := matrix.Validate(); err != nil {
		t.Fatalf("DefaultMatrix.Validate: %v", err)
	}
	for name, spec := range matrix.Components {
		if len(spec.VersionArgs) == 0 || spec.VersionArgs[0] != "--version" {
			t.Errorf("%s VersionArgs = %v, want --version", name, spec.VersionArgs)
		}
	}
	if names := matrix.Names(); len(names) != 5 || names[0] != "ruby" {
		t.Fatalf("Names() = %v", names)
	}
}

func TestLoadMatrixUsesBuiltinWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	matrix, builtin, err := LoadMatrix(path)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	if !builtin {
		t.Fatal("expected builtin fallback")
	}
	if matrix.Profile != "default" {
		t.Fatalf("profile = %q", matrix.Profile)
	}
}

func TestLoadMatrixRejectsInvalidSHA(t *testing.T) {
	path := filepath.Join(t.TempDir(), "matrix.json")
	data := `{"profile":"test","components":{"git":{"version":"2.x","source":"system","sha256":"abc"}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadMatrix(path); err == nil {
		t.Fatal("expected invalid sha256 error")
	}
}

func TestMatrixApplyDefaults(t *testing.T) {
	matrix := Matrix{
		Profile: "test",
		Components: map[string]ComponentSpec{
			"redis": {Version: "7.x", Source: "system"},
		},
	}
	if err := matrix.ApplyDefaults(); err != nil {
		t.Fatal(err)
	}
	spec := matrix.Components["redis"]
	if spec.Binary != "redis-server" {
		t.Errorf("binary = %q", spec.Binary)
	}
	if strings.Join(spec.VersionArgs, " ") != "--version" {
		t.Errorf("version args = %v", spec.VersionArgs)
	}
	if err := matrix.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
