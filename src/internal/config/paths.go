// Package config holds DevKit instance configuration and path resolution.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Paths describes the on-disk layout of one DevKit instance root. Config,
// logs, and run state always live under Root. Project and service data may be
// relocated to DataRoot, which on Windows defaults to a path inside WSL2 so
// PostgreSQL can initialize a native Linux data directory.
type Paths struct {
	Root              string
	DataRoot          string
	ConfigDir         string
	WorkspacesDir     string
	ProfileSourcesDir string
	RuntimeDir        string
	ServicesDir       string
	LogsDir           string
	AuditDir          string
	RunDir            string
	BackupsDir        string
	TmpDir            string
	// LegacyDataRoot reports that the instance predates the split data root
	// and still stores project/service data under Root.
	LegacyDataRoot bool
}

// NewPaths derives the standard layout from root. Data directories initially
// live under root; call SetDataRoot to relocate them.
func NewPaths(root string) *Paths {
	root = filepath.Clean(root)
	p := &Paths{
		Root:              root,
		ConfigDir:         filepath.Join(root, "config"),
		ProfileSourcesDir: filepath.Join(root, "profiles"),
		RuntimeDir:        filepath.Join(root, "runtime"),
		LogsDir:           filepath.Join(root, "logs"),
		AuditDir:          filepath.Join(root, "logs", "audit"),
		RunDir:            filepath.Join(root, "run"),
	}
	p.SetDataRoot(root)
	return p
}

// SetDataRoot relocates project, service, backup, and temporary data. An empty
// value keeps them under Root.
func (p *Paths) SetDataRoot(dataRoot string) {
	dataRoot = filepath.Clean(strings.TrimSpace(dataRoot))
	if dataRoot == "" || dataRoot == "." {
		dataRoot = p.Root
	}
	p.DataRoot = dataRoot
	p.WorkspacesDir = filepath.Join(dataRoot, "workspaces")
	p.ServicesDir = filepath.Join(dataRoot, "services")
	p.BackupsDir = filepath.Join(dataRoot, "backups")
	p.TmpDir = filepath.Join(dataRoot, "tmp")
}

// UsesSplitDataRoot reports whether data lives outside the configuration root.
func (p *Paths) UsesSplitDataRoot() bool {
	return !samePath(p.DataRoot, p.Root)
}

// Ensure creates every directory required before startup.
func (p *Paths) Ensure() error {
	if err := p.EnsureBase(); err != nil {
		return err
	}
	return p.EnsureDataDirs()
}

// EnsureBase creates configuration, log, and run directories that must exist
// before the data root is known.
func (p *Paths) EnsureBase() error {
	for _, dir := range []string{
		p.Root, p.ConfigDir, p.ProfileSourcesDir, p.RuntimeDir,
		p.LogsDir, p.AuditDir, p.RunDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

// EnsureDataDirs creates project and service data directories.
func (p *Paths) EnsureDataDirs() error {
	for _, dir := range []string{
		p.WorkspacesDir, p.ServicesDir, p.BackupsDir, p.TmpDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}

// SamePath reports whether two paths refer to the same location. Windows
// comparisons are case-insensitive.
func SamePath(a, b string) bool {
	return samePath(a, b)
}

// RelWithin reports whether candidate is inside root and returns the relative
// path. It rejects paths that escape root or live on another volume.
func RelWithin(root, candidate string) (string, bool) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(candidate) == "" {
		return "", false
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return rel, true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// DataRootPending reports that configured points somewhere other than the data
// root this process is using, which means a restart is needed to apply it.
func (p *Paths) DataRootPending(configured string) bool {
	configured = strings.TrimSpace(configured)
	if configured == "" {
		return false
	}
	return !samePath(configured, p.DataRoot)
}

// ConfigFile is the path to devkit.json.
func (p *Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "devkit.json") }

// SecretsFile is the path to secrets.json.
func (p *Paths) SecretsFile() string { return filepath.Join(p.ConfigDir, "secrets.json") }

// RuntimeMatrixFile is the path to runtime-matrix.json.
func (p *Paths) RuntimeMatrixFile() string { return filepath.Join(p.ConfigDir, "runtime-matrix.json") }

// ProfileSourcesFile is the path to the third-party template source registry.
func (p *Paths) ProfileSourcesFile() string {
	return filepath.Join(p.ConfigDir, "profile-sources.json")
}

// ProfileSourceDir returns the DevKit-owned checkout for a Git profile source.
func (p *Paths) ProfileSourceDir(id string) string {
	return filepath.Join(p.ProfileSourcesDir, id)
}

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
// are rooted at the data root; absolute values are used as-is.
func (p *Paths) ResolveDataDir(value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(p.DataRoot, value)
}

// ConfigExists reports whether the instance configuration already exists.
func ConfigExists(p *Paths) bool {
	info, err := os.Stat(p.ConfigFile())
	return err == nil && !info.IsDir()
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
