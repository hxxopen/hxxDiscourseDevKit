// Package backup implements workspace backup archives and transactional
// restore.
package backup

import (
	"errors"
	"time"
)

// Archive format and naming constants.
const (
	FormatVersion = 1
	archiveSuffix = ".hxxddk-backup.tar.gz"
	confirmTTL    = 5 * time.Minute
)

// Domain errors mapped to API responses.
var (
	ErrNotFound          = errors.New("backup not found")
	ErrInvalidArchive    = errors.New("backup archive is invalid")
	ErrUnsafePath        = errors.New("backup contains an unsafe path")
	ErrWorkspaceRunning  = errors.New("workspace must be stopped")
	ErrConfirmToken      = errors.New("invalid or expired confirmation token")
	ErrValidation        = errors.New("backup request is invalid")
	ErrInsufficientSpace = errors.New("insufficient disk space")
	ErrWorkspaceMismatch = errors.New("backup belongs to another workspace")
)

// CreateOptions selects optional archive contents.
type CreateOptions struct {
	IncludeSource   bool `json:"include_source"`
	IncludeDatabase bool `json:"include_database"`
}

// ManifestFile records one archived file.
type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is stored as the first entry of every archive.
type Manifest struct {
	FormatVersion          int            `json:"format_version"`
	DevKitVersion          string         `json:"devkit_version"`
	WorkspaceSchemaVersion int            `json:"workspace_schema_version"`
	WorkspaceID            string         `json:"workspace_id"`
	WorkspaceName          string         `json:"workspace_name"`
	CreatedAt              time.Time      `json:"created_at"`
	Platform               string         `json:"platform"`
	IncludesSource         bool           `json:"includes_source"`
	IncludesDatabase       bool           `json:"includes_database"`
	SourceHeadCommit       string         `json:"source_head_commit,omitempty"`
	DatabaseName           string         `json:"database_name,omitempty"`
	ProjectInitialized     bool           `json:"project_initialized"`
	TotalBytes             int64          `json:"total_bytes"`
	FileCount              int            `json:"file_count"`
	SkippedSymlinks        int            `json:"skipped_symlinks,omitempty"`
	Excluded               []string       `json:"excluded,omitempty"`
	Warnings               []string       `json:"warnings,omitempty"`
	Files                  []ManifestFile `json:"files"`
}

// Backup describes one archive on disk. Manifest is populated by Get.
type Backup struct {
	ID         string    `json:"id"`
	FileName   string    `json:"file_name"`
	Path       string    `json:"path"`
	SizeBytes  int64     `json:"size_bytes"`
	CreatedAt  time.Time `json:"created_at"`
	Manifest   *Manifest `json:"manifest,omitempty"`
	VerifyNote string    `json:"verify_note,omitempty"`
}

// RestoreAction is one path that a restore would replace.
type RestoreAction struct {
	Kind      string `json:"kind"`
	Path      string `json:"path"`
	SizeBytes int64  `json:"size_bytes"`
	Note      string `json:"note,omitempty"`
}

// RestorePreview is returned before a destructive restore.
type RestorePreview struct {
	BackupID         string          `json:"backup_id"`
	WorkspaceID      string          `json:"workspace_id"`
	Manifest         Manifest        `json:"manifest"`
	Running          bool            `json:"running"`
	WillReplace      []RestoreAction `json:"will_replace"`
	RestoreDatabase  bool            `json:"restore_database"`
	RequiredBytes    int64           `json:"required_bytes"`
	FreeBytes        uint64          `json:"free_bytes"`
	Warnings         []string        `json:"warnings,omitempty"`
	ConfirmToken     string          `json:"confirm_token"`
	ExpiresAt        time.Time       `json:"expires_at"`
	UncompressedSize int64           `json:"uncompressed_size"`
}

// RestoreResult summarizes a completed restore.
type RestoreResult struct {
	BackupID         string   `json:"backup_id"`
	WorkspaceID      string   `json:"workspace_id"`
	Restored         []string `json:"restored"`
	DatabaseRestored bool     `json:"database_restored"`
	RollbackKept     bool     `json:"rollback_kept"`
}

type restoreToken struct {
	workspaceID string
	backupID    string
	expiresAt   time.Time
}
