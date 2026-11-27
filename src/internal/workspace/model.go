// Package workspace implements the multi-workspace development container model.
package workspace

import (
	"errors"
	"strings"
	"time"
)

// State is the persisted lifecycle state of a workspace.
type State string

const (
	StateEmpty        State = "EMPTY"
	StateInitializing State = "INITIALIZING"
	StateReady        State = "READY"
	StateStarting     State = "STARTING"
	StateRunning      State = "RUNNING"
	StateStopping     State = "STOPPING"
	StateStopped      State = "STOPPED"
	StateError        State = "ERROR"
)

// Valid reports whether s is a documented state.
func (s State) Valid() bool {
	switch s {
	case StateEmpty, StateInitializing, StateReady, StateStarting,
		StateRunning, StateStopping, StateStopped, StateError:
		return true
	default:
		return false
	}
}

// Workspace is the persisted metadata of one isolated development workspace.
type Workspace struct {
	WorkspaceSchemaVersion int            `json:"workspace_schema_version"`
	WorkspaceID            string         `json:"workspace_id"`
	Name                   string         `json:"name"`
	Description            string         `json:"description"`
	ProjectType            string         `json:"project_type"`
	ProfileID              string         `json:"profile_id,omitempty"`
	State                  State          `json:"state"`
	LastError              string         `json:"last_error"`
	CreatedAt              time.Time      `json:"created_at"`
	UpdatedAt              time.Time      `json:"updated_at"`
	Source                 SourceConfig   `json:"source"`
	RuntimeProfile         string         `json:"runtime_profile"`
	Database               DatabaseConfig `json:"database"`
	Redis                  RedisConfig    `json:"redis"`
	Network                NetworkConfig  `json:"network"`
	Project                ProjectConfig  `json:"project"`
}

// SourceConfig describes where the project source comes from.
type SourceConfig struct {
	Installed     bool       `json:"installed"`
	RepositoryURL string     `json:"repository_url"`
	Branch        string     `json:"branch"`
	Tag           string     `json:"tag"`
	Commit        string     `json:"commit"`
	Path          string     `json:"path"`
	Shallow       bool       `json:"shallow"`
	Submodules    bool       `json:"submodules"`
	AuthRef       string     `json:"auth_ref"`
	HeadCommit    string     `json:"head_commit,omitempty"`
	LastSyncAt    *time.Time `json:"last_sync_at,omitempty"`
}

// DatabaseConfig describes the workspace's isolated PostgreSQL database.
type DatabaseConfig struct {
	Mode           string     `json:"mode"`
	DatabaseName   string     `json:"database_name"`
	Username       string     `json:"username"`
	PasswordRef    string     `json:"password_ref"`
	Provisioned    bool       `json:"provisioned"`
	ProvisionedAt  *time.Time `json:"provisioned_at,omitempty"`
	ProvisionError string     `json:"provision_error,omitempty"`
}

// TestDatabaseName returns the isolated database used by RAILS_ENV=test.
// PostgreSQL identifiers are limited to 63 bytes, so a long development name
// is truncated before the suffix is appended.
func (ws *Workspace) TestDatabaseName() string {
	const suffix = "_test"
	const maxIdentifier = 63
	base := strings.TrimSpace(ws.Database.DatabaseName)
	if base == "" {
		return ""
	}
	if len(base)+len(suffix) > maxIdentifier {
		base = strings.TrimRight(base[:maxIdentifier-len(suffix)], "_")
	}
	return base + suffix
}

// RedisConfig describes the workspace's Redis namespace.
type RedisConfig struct {
	Mode      string `json:"mode"`
	DBIndex   int    `json:"db_index"`
	KeyPrefix string `json:"key_prefix"`
}

// NetworkConfig holds the workspace's development HTTP endpoint.
type NetworkConfig struct {
	Host     string `json:"host"`
	HTTPPort int    `json:"http_port"`
}

// ProjectConfig tracks the running development process.
type ProjectConfig struct {
	Initialized   bool       `json:"initialized"`
	InitializedAt *time.Time `json:"initialized_at,omitempty"`
	ProcessState  string     `json:"process_state"`
	PID           int        `json:"pid"`
	LastStartedAt *time.Time `json:"last_started_at"`
}

// CreateRequest creates a workspace. ProfileID and the source overrides are
// resolved by the API before the manager persists the workspace.
type CreateRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	ProjectType   string `json:"project_type"`
	ProfileID     string `json:"profile_id,omitempty"`
	RepositoryURL string `json:"repository_url,omitempty"`
	Branch        string `json:"branch,omitempty"`
	// AcceptProfile records explicit consent to an unverified third-party
	// template after its origin, operations and permissions were displayed.
	AcceptProfile bool `json:"accept_profile,omitempty"`
}

// UpdateRequest changes mutable workspace metadata.
type UpdateRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

// Resource is one item shown in a deletion preview.
type Resource struct {
	Kind      string `json:"kind"`
	Path      string `json:"path,omitempty"`
	Exists    bool   `json:"exists"`
	SizeBytes int64  `json:"size_bytes"`
	Shared    bool   `json:"shared"`
	Note      string `json:"note,omitempty"`
}

// DeletePlan is the preview returned before a destructive delete.
type DeletePlan struct {
	WorkspaceID  string     `json:"workspace_id"`
	Name         string     `json:"name"`
	Running      bool       `json:"running"`
	TotalBytes   int64      `json:"total_bytes"`
	Resources    []Resource `json:"resources"`
	ConfirmToken string     `json:"confirm_token"`
	ExpiresAt    time.Time  `json:"expires_at"`
}

// DeleteOptions selects which workspace resources are removed.
type DeleteOptions struct {
	ConfirmToken   string
	DeleteConfig   bool
	DeleteSource   bool
	DeleteData     bool
	DeleteDatabase bool
	DeleteRedis    bool
}

// DeleteResult summarizes what was removed or kept.
type DeleteResult struct {
	Removed         []string `json:"removed"`
	Kept            []string `json:"kept"`
	DatabaseDropped bool     `json:"database_dropped"`
	RedisCleared    bool     `json:"redis_cleared"`
	Notes           []string `json:"notes,omitempty"`
}

// Check is one runtime status assertion for a workspace.
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Status is the live view returned by the status endpoint.
type Status struct {
	WorkspaceID string  `json:"workspace_id"`
	State       State   `json:"state"`
	Running     bool    `json:"running"`
	Checks      []Check `json:"checks"`
}

// Domain errors mapped to API responses.
var (
	ErrNotFound     = errors.New("workspace not found")
	ErrInvalidName  = errors.New("invalid workspace name")
	ErrValidation   = errors.New("validation failed")
	ErrNameConflict = errors.New("workspace name already exists")
	ErrRunning      = errors.New("workspace is running")
	ErrNoPort       = errors.New("no free workspace port available")
	ErrNoRedisDB    = errors.New("no free redis database index available")
	ErrConfirmToken = errors.New("invalid or expired confirmation token")
)
