package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteJSONReadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "data.json")
	want := struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{Name: "alpha", Count: 3}
	if err := WriteJSON(path, want, 0o600); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	var got struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	if err := ReadJSON(path, &got); err != nil {
		t.Fatalf("ReadJSON: %v", err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "data.json" {
		t.Fatalf("unexpected files left behind: %v", entries)
	}
}

func TestWriteFileAtomicProtectedFailureKeepsOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	protectErr := errors.New("no access")
	err := WriteFileAtomicProtected(path, []byte("new"), 0o600, func(candidate string) error {
		if candidate == path {
			t.Fatalf("protect received target path instead of temporary path")
		}
		if _, statErr := os.Stat(candidate); statErr != nil {
			t.Fatalf("temporary file does not exist when protect runs: %v", statErr)
		}
		return protectErr
	})
	if !errors.Is(err, protectErr) {
		t.Fatalf("WriteFileAtomicProtected error = %v, want %v", err, protectErr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old" {
		t.Fatalf("target content = %q, want old", data)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "secret.json" {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestAcquireLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.lock")
	first, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("first AcquireLock: %v", err)
	}

	second, err := AcquireLock(path)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second AcquireLock error = %v, want ErrLocked", err)
	}
	if second != nil {
		t.Fatal("second lock should be nil")
	}

	if err := first.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	third, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("third AcquireLock after release: %v", err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("third Release: %v", err)
	}
}

func TestRemoveIfEmpty(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfEmpty(dir); err != nil {
		t.Fatalf("RemoveIfEmpty: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory still exists: %v", err)
	}

	nonEmpty := filepath.Join(t.TempDir(), "full")
	if err := os.MkdirAll(nonEmpty, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nonEmpty, "keep"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIfEmpty(nonEmpty); err != nil {
		t.Fatalf("RemoveIfEmpty(non-empty): %v", err)
	}
	if _, err := os.Stat(nonEmpty); err != nil {
		t.Fatalf("non-empty directory was removed: %v", err)
	}
}
