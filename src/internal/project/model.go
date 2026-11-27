// Package project manages Discourse source checkouts, dependency
// initialization, and the development process lifecycle.
package project

import (
	"errors"
	"time"
)

// Domain errors mapped to API responses.
var (
	ErrSourceNotConfigured = errors.New("source repository is not configured")
	ErrSourceNotEmpty      = errors.New("source directory is not empty")
	ErrSourceInstalled     = errors.New("source is already installed")
	ErrSourceMissing       = errors.New("source directory is missing")
	ErrNotGitRepository    = errors.New("source is not a git repository")
	ErrNotInitialized      = errors.New("project is not initialized")
	ErrAlreadyRunning      = errors.New("project is already running")
	ErrNotRunning          = errors.New("project is not running")
	ErrDirtyWorktree       = errors.New("git worktree has uncommitted changes")
	ErrInvalidSource       = errors.New("source configuration is invalid")
	ErrInvalidRef          = errors.New("git reference is invalid")
	ErrValidation          = errors.New("project operation is invalid")
)

// SourceInput updates a workspace's repository selection. Empty strings leave
// the existing value unchanged; nil booleans keep the existing flags.
type SourceInput struct {
	RepositoryURL string `json:"repository_url,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Tag           string `json:"tag,omitempty"`
	Commit        string `json:"commit,omitempty"`
	Shallow       *bool  `json:"shallow,omitempty"`
	Submodules    *bool  `json:"submodules,omitempty"`
}

// CheckoutInput switches the checkout to a branch, tag, or commit. A commit is
// detected when Ref is a hexadecimal object name.
type CheckoutInput struct {
	Ref   string `json:"ref"`
	Force bool   `json:"force,omitempty"`
}

// SourceStatus is the live Git and source view of a workspace.
type SourceStatus struct {
	Configured     bool       `json:"configured"`
	Installed      bool       `json:"installed"`
	IsGit          bool       `json:"is_git"`
	Path           string     `json:"path"`
	RepositoryURL  string     `json:"repository_url,omitempty"`
	Branch         string     `json:"branch,omitempty"`
	Tag            string     `json:"tag,omitempty"`
	Commit         string     `json:"commit,omitempty"`
	HeadCommit     string     `json:"head_commit,omitempty"`
	CurrentBranch  string     `json:"current_branch,omitempty"`
	Shallow        bool       `json:"shallow"`
	Submodules     bool       `json:"submodules"`
	Dirty          bool       `json:"dirty"`
	ChangedFiles   int        `json:"changed_files"`
	Ahead          int        `json:"ahead"`
	Behind         int        `json:"behind"`
	LastSyncAt     *time.Time `json:"last_sync_at,omitempty"`
	Error          string     `json:"error,omitempty"`
	GitDescription string     `json:"git_description,omitempty"`
}

// ProcessStatus is the development process view of a workspace.
type ProcessStatus struct {
	Initialized   bool       `json:"initialized"`
	InitializedAt *time.Time `json:"initialized_at,omitempty"`
	ProcessState  string     `json:"process_state"`
	PID           int        `json:"pid"`
	Alive         bool       `json:"alive"`
	HTTPURL       string     `json:"http_url"`
	EnvFile       string     `json:"env_file"`
	LogFile       string     `json:"log_file"`
	LastStartedAt *time.Time `json:"last_started_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// Check is one live status assertion.
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Status is the project view returned by the API.
type Status struct {
	WorkspaceID string        `json:"workspace_id"`
	Name        string        `json:"name"`
	Source      SourceStatus  `json:"source"`
	Process     ProcessStatus `json:"process"`
	Checks      []Check       `json:"checks"`
}

// InitOptions selects optional initialization work.
type InitOptions struct {
	InstallDependencies bool `json:"install_dependencies"`
	PrepareDatabase     bool `json:"prepare_database"`
}

// DefaultInitOptions returns the standard init behavior.
func DefaultInitOptions() InitOptions {
	return InitOptions{InstallDependencies: true, PrepareDatabase: true}
}
