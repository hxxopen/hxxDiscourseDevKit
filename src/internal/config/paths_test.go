package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestSamePath(t *testing.T) {
	root := t.TempDir()
	if !SamePath(root, root) {
		t.Fatalf("SamePath(%q, %q) = false, want true", root, root)
	}
	if !SamePath(root, filepath.Join(root, "..", filepath.Base(root))) {
		t.Fatalf("cleaned paths should compare equal")
	}
	if SamePath(root, filepath.Join(root, "other")) {
		t.Fatalf("different paths compared equal")
	}
	if runtime.GOOS == "windows" {
		if !SamePath(`C:\DevKit\Root`, `c:\devkit\root`) {
			t.Fatalf("Windows path comparison should ignore case")
		}
	}
}

func TestRelWithin(t *testing.T) {
	root := t.TempDir()

	rel, ok := RelWithin(root, filepath.Join(root, "services", "redis", "data"))
	if !ok || rel != filepath.Join("services", "redis", "data") {
		t.Fatalf("RelWithin inside = %q, %v", rel, ok)
	}

	if rel, ok := RelWithin(root, root); !ok || rel != "." {
		t.Fatalf("RelWithin(root, root) = %q, %v", rel, ok)
	}

	for _, candidate := range []string{
		filepath.Join(root, ".."),
		filepath.Join(root, "..", "outside"),
	} {
		if rel, ok := RelWithin(root, candidate); ok {
			t.Fatalf("RelWithin(%q) unexpectedly inside: %q", candidate, rel)
		}
	}

	// A sibling sharing the root's name as a prefix must not be treated as
	// inside the root.
	prefix := filepath.Join(filepath.Dir(root), filepath.Base(root)+"2")
	if rel, ok := RelWithin(root, prefix); ok {
		t.Fatalf("prefix sibling treated as inside: %q", rel)
	}

	if _, ok := RelWithin("", root); ok {
		t.Fatalf("empty root should never match")
	}
	if _, ok := RelWithin(root, ""); ok {
		t.Fatalf("empty candidate should never match")
	}

	if runtime.GOOS == "windows" {
		if _, ok := RelWithin(`C:\DevKit\root`, `D:\DevKit\root\services`); ok {
			t.Fatalf("cross-volume path treated as inside")
		}
		if rel, ok := RelWithin(`C:\DevKit\root`, `c:\devkit\ROOT\services`); !ok ||
			rel != "services" {
			t.Fatalf("case-insensitive Windows path: %q, %v", rel, ok)
		}
	}
}

func TestDataRootPending(t *testing.T) {
	root := t.TempDir()
	paths := NewPaths(root)

	if paths.DataRootPending("") {
		t.Fatalf("empty configured data root should not be pending")
	}
	if paths.DataRootPending("   ") {
		t.Fatalf("blank configured data root should not be pending")
	}
	if paths.DataRootPending(root) {
		t.Fatalf("configured root equal to live data root should not be pending")
	}

	target := filepath.Join(t.TempDir(), "wsl-data")
	if !paths.DataRootPending(target) {
		t.Fatalf("different configured data root should be pending")
	}

	paths.SetDataRoot(target)
	if paths.DataRootPending(target) {
		t.Fatalf("configured data root equal to live target should not be pending")
	}
}
