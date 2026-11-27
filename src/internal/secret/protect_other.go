//go:build !windows

package secret

import "os"

// protectFile keeps the secrets file owner-only on Unix-like systems.
func protectFile(path string) error {
	return os.Chmod(path, 0o600)
}
