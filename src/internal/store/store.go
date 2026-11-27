// Package store provides small, safe file primitives used across DevKit.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// ErrLocked is returned when another process holds the instance lock.
var ErrLocked = errors.New("resource is locked by another process")

// Lock is a cross-process advisory lock backed by a file handle.
type Lock struct {
	f *os.File
}

// EnsureDir creates a directory and all parents.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	return nil
}

// ProtectFunc prepares a file before its contents are written. It is used to
// apply platform-specific access restrictions to the temporary file so secrets
// are never readable with inherited permissions, even briefly.
type ProtectFunc func(path string) error

// WriteFileAtomic writes data through a temporary file in the same directory
// and renames it into place, so readers never observe a partial file.
func WriteFileAtomic(path string, data []byte, perm fs.FileMode) error {
	return WriteFileAtomicProtected(path, data, perm, nil)
}

// WriteFileAtomicProtected is WriteFileAtomic with a protection hook applied to
// the temporary file before any sensitive content is written.
func WriteFileAtomicProtected(path string, data []byte, perm fs.FileMode, protect ProtectFunc) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()

	if protect != nil {
		if err := protect(tmp); err != nil {
			_ = f.Close()
			return fmt.Errorf("protect temporary file: %w", err)
		}
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod temporary file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write temporary file: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync temporary file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close temporary file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	syncDir(dir)
	return nil
}

// WriteJSON atomically writes indented JSON with a trailing newline.
func WriteJSON(path string, v any, perm fs.FileMode) error {
	return WriteJSONProtected(path, v, perm, nil)
}

// WriteJSONProtected is WriteJSON with a protection hook for sensitive files.
func WriteJSONProtected(path string, v any, perm fs.FileMode, protect ProtectFunc) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal JSON: %w", err)
	}
	data = append(data, '\n')
	return WriteFileAtomicProtected(path, data, perm, protect)
}

// ReadJSON reads and decodes a JSON file.
func ReadJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// DirSize returns the total size of regular files below path, best effort.
// Unreadable entries are skipped rather than aborting the walk.
func DirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// RemoveIfEmpty removes path only when it is an empty directory.
func RemoveIfEmpty(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(entries) != 0 {
		return nil
	}
	return os.Remove(path)
}

func syncDir(dir string) {
	if runtime.GOOS == "windows" {
		return
	}
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
