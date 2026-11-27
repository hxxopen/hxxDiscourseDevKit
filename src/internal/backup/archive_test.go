package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestSafeArchiveName(t *testing.T) {
	valid := []string{
		"manifest.json",
		"workspace/workspace.json",
		"workspace/config/discourse.env",
		"workspace/config/",
		"source/Gemfile",
		"database.dump",
	}
	for _, name := range valid {
		if _, err := safeArchiveName(name); err != nil {
			t.Errorf("safeArchiveName(%q) = %v", name, err)
		}
	}
	invalid := []string{
		"",
		"/etc/passwd",
		"../evil",
		"workspace/../../evil",
		"workspace\\config\\file",
		"C:/evil",
		"a//b",
		"./a",
	}
	for _, name := range invalid {
		if _, err := safeArchiveName(name); !errors.Is(err, ErrUnsafePath) {
			t.Errorf("safeArchiveName(%q) error = %v, want ErrUnsafePath", name, err)
		}
	}
}

func TestRestoreRelativePath(t *testing.T) {
	cases := map[string]struct {
		relative string
		kind     string
		ok       bool
	}{
		"workspace/config/discourse.env": {"config/discourse.env", "config", true},
		"workspace/data/disk/a.txt":      {"data/disk/a.txt", "data", true},
		"source/Gemfile":                 {"source/Gemfile", "source", true},
		"database.dump":                  {"database.dump", "database", true},
		"workspace/workspace.json":       {"", "metadata", true},
		"manifest.json":                  {"", "manifest", false},
		"other/file":                     {"", "", false},
	}
	for name, want := range cases {
		relative, kind, ok := restoreRelativePath(name)
		if relative != want.relative || kind != want.kind || ok != want.ok {
			t.Errorf("restoreRelativePath(%q) = (%q, %q, %v), want (%q, %q, %v)",
				name, relative, kind, ok, want.relative, want.kind, want.ok)
		}
	}
}

func TestArchiveRoundTripAndExtract(t *testing.T) {
	dir := t.TempDir()
	host := filepath.Join(dir, "host")
	if err := os.MkdirAll(host, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"workspace/workspace.json":       `{"workspace_id":"ws_test"}`,
		"workspace/config/discourse.env": "RAILS_ENV=\"development\"\n",
		"workspace/data/disk/a.txt":      "data-file",
		"source/Gemfile":                 "source 'https://rubygems.org'\n",
	}
	entries := make([]archiveEntry, 0, len(files))
	manifest := &Manifest{
		FormatVersion: FormatVersion,
		DevKitVersion: "test",
		WorkspaceID:   "ws_test",
		CreatedAt:     time.Now().UTC(),
	}
	for name, content := range files {
		path := filepath.Join(host, filepath.FromSlash(filepath.Base(name)))
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, archiveEntry{
			Name:   name,
			Host:   path,
			Size:   int64(len(content)),
			Mode:   0o600,
			SHA256: sha256Hex([]byte(content)),
		})
		manifest.Files = append(manifest.Files, ManifestFile{
			Path:   name,
			Size:   int64(len(content)),
			SHA256: sha256Hex([]byte(content)),
		})
	}

	archivePath := filepath.Join(dir, "backup.tar.gz")
	if err := writeArchive(context.Background(), archivePath, manifest, entries, func(int, string) {}); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}

	verified, err := verifyArchive(archivePath)
	if err != nil {
		t.Fatalf("verifyArchive: %v", err)
	}
	if verified.WorkspaceID != "ws_test" || len(verified.Files) != len(files) {
		t.Fatalf("verified manifest = %#v", verified)
	}

	outDir := filepath.Join(dir, "restore")
	extracted, err := extractArchive(context.Background(), archivePath, outDir, func(int, string) {})
	if err != nil {
		t.Fatalf("extractArchive: %v", err)
	}
	if !extracted.HasConfig || !extracted.HasData || !extracted.HasSource {
		t.Fatalf("extracted flags = %#v", extracted)
	}
	config, err := os.ReadFile(filepath.Join(outDir, "config", "discourse.env"))
	if err != nil {
		t.Fatalf("read extracted config: %v", err)
	}
	if string(config) != files["workspace/config/discourse.env"] {
		t.Fatalf("config content = %q", config)
	}
	if _, err := os.Stat(filepath.Join(outDir, "workspace.json")); err == nil {
		t.Fatal("workspace.json should not be extracted")
	}
}

func TestVerifyArchiveRejectsTamperedFile(t *testing.T) {
	dir := t.TempDir()
	content := []byte("original")
	host := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(host, content, 0o600); err != nil {
		t.Fatal(err)
	}
	name := "workspace/config/discourse.env"
	manifest := &Manifest{
		FormatVersion: FormatVersion,
		WorkspaceID:   "ws_test",
		CreatedAt:     time.Now().UTC(),
		Files: []ManifestFile{{
			Path:   name,
			Size:   int64(len(content)),
			SHA256: sha256Hex([]byte("different")),
		}},
	}
	// Leave the entry hash empty so writeArchive does not reject the source
	// while writing; verification must still notice the mismatch.
	entries := []archiveEntry{{Name: name, Host: host, Size: int64(len(content)), Mode: 0o600}}
	archivePath := filepath.Join(dir, "tampered.tar.gz")
	if err := writeArchive(context.Background(), archivePath, manifest, entries, func(int, string) {}); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	if _, err := verifyArchive(archivePath); !errors.Is(err, ErrInvalidArchive) {
		t.Fatalf("verifyArchive error = %v, want ErrInvalidArchive", err)
	}
}

func TestReadManifestRequiresLeadingEntry(t *testing.T) {
	dir := t.TempDir()
	manifest := &Manifest{
		FormatVersion: FormatVersion,
		WorkspaceID:   "ws_test",
		CreatedAt:     time.Now().UTC(),
	}
	archivePath := filepath.Join(dir, "empty.tar.gz")
	if err := writeArchive(context.Background(), archivePath, manifest, nil, func(int, string) {}); err != nil {
		t.Fatalf("writeArchive: %v", err)
	}
	read, err := readManifest(archivePath)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}
	if read.WorkspaceID != "ws_test" || read.FormatVersion != FormatVersion {
		t.Fatalf("manifest = %#v", read)
	}
}
