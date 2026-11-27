// Package config holds DevKit instance configuration and path resolution.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths describes the on-disk layout of one DevKit instance root.
type Paths struct {
	Root          string
	ConfigDir     string
	WorkspacesDir string
	RuntimeDir    string
	ServicesDir   string
	LogsDir       string
	RunDir        string
	BackupsDir    string
	TmpDir        string
}

// NewPaths derives the standard layout from root.
func NewPaths(root string) *Paths {
	root = filepath.Clean(root)
	return &Paths{
		Root:          root,
		ConfigDir:     filepath.Join(root, "config"),
		WorkspacesDir: filepath.Join(root, "workspaces"),
		RuntimeDir:    filepath.Join(root, "runtime"),
		ServicesDir:   filepath.Join(root, "services"),
		LogsDir:       filepath.Join(root, "logs"),
		RunDir:        filepath.Join(root, "run"),
		BackupsDir:    filepath.Join(root, "backups"),
		TmpDir:        filepath.Join(root, "tmp"),
	}
}

// Ensure creates every directory required before startup.
func (p *Paths) Ensure() error {
	for _, dir := range []string{
		p.Root, p.ConfigDir, p.WorkspacesDir, p.RuntimeDir, p.ServicesDir,
		p.LogsDir, p.RunDir, p.BackupsDir, p.TmpDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

// ConfigFile is the path to devkit.json.
func (p *Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "devkit.json") }

// SecretsFile is the path to secrets.json.
func (p *Paths) SecretsFile() string { return filepath.Join(p.ConfigDir, "secrets.json") }

// RuntimeMatrixFile is the path to runtime-matrix.json.
func (p *Paths) RuntimeMatrixFile() string { return filepath.Join(p.ConfigDir, "runtime-matrix.json") }

// LockFile is the single-instance lock path.
func (p *Paths) LockFile() string { return filepath.Join(p.RunDir, "devkitd.lock") }

// RunFile describes the running daemon for CLI discovery.
func (p *Paths) RunFile() string { return filepath.Join(p.RunDir, "devkitd.json") }

// StartupLog is used when the CLI spawns the daemon in the background.
func (p *Paths) StartupLog() string { return filepath.Join(p.LogsDir, "devkitd-startup.log") }

// WorkspaceDir returns the directory for a workspace ID.
func (p *Paths) WorkspaceDir(id string) string { return filepath.Join(p.WorkspacesDir, id) }

// ServiceDir returns the state directory for a managed service.
func (p *Paths) ServiceDir(name string) string { return filepath.Join(p.ServicesDir, name) }

// ResolveDataDir resolves a configured service data directory. Relative values
// are rooted at the instance root; absolute values are used as-is.
func (p *Paths) ResolveDataDir(value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(p.Root, value)
}

// DefaultRoot returns the platform default instance root.
func DefaultRoot() (string, error) {
	switch runtime.GOOS {
	case "windows":
		if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
			return filepath.Join(dir, "hxxDiscourseDevKit"), nil
		}
		dir, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "hxxDiscourseDevKit"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "hxxDiscourseDevKit"), nil
	default:
		if dir := os.Getenv("XDG_DATA_HOME"); dir != "" {
			return filepath.Join(dir, "hxxDiscourseDevKit"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "hxxDiscourseDevKit"), nil
	}
}

// ResolveRoot picks and cleans the instance root from flag, environment, or
// platform default, in that order.
func ResolveRoot(explicit string) (string, error) {
	root := explicit
	if root == "" {
		root = os.Getenv("HXXDDK_HOME")
	}
	if root == "" {
		var err error
		root, err = DefaultRoot()
		if err != nil {
			return "", fmt.Errorf("resolve default root: %w", err)
		}
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root %q: %w", root, err)
	}
	return filepath.Clean(abs), nil
}
