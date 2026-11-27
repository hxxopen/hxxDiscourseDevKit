// Package migration moves a legacy Windows instance's project and service
// data into the WSL2 data root. It is always user initiated.
package migration

import (
	"errors"
	"time"
)

const (
	// TaskType is the background task type used by the API.
	TaskType = "storage.migrate"

	tokenTTL         = 10 * time.Minute
	stagingDirName   = ".migration"
	staleStagingTTL  = 24 * time.Hour
	spaceMarginBytes = 32 << 20
)

// Unit actions shown in the migration plan.
const (
	ActionCopy    = "copy"
	ActionSkip    = "skip"
	ActionBlocked = "blocked"
)

// Unit kinds shown in the migration plan.
const (
	KindWorkspace = "workspace"
	KindService   = "service"
	KindBackup    = "backup"
)

// Blocker codes returned to the UI. They are stable across releases.
const (
	BlockerTargetWorkspacePartial = "TARGET_WORKSPACE_PARTIAL"
	BlockerSourceWorkspaceMissing = "SOURCE_WORKSPACE_MISSING"
	BlockerWorkspaceBusy          = "WORKSPACE_BUSY"
	BlockerServiceDataDirEscape   = "SERVICE_DATA_DIR_ESCAPES_ROOT"
)

// Errors returned by the migration service. The API maps them to HTTP codes.
var (
	ErrUnsupported       = errors.New("当前平台不支持数据目录迁移")
	ErrNotEligible       = errors.New("当前实例不满足迁移条件")
	ErrPendingRestart    = errors.New("已有待生效的数据目录，请先重启 DevKit")
	ErrWSLUnavailable    = errors.New("WSL2 不可用，无法确定迁移目标")
	ErrTargetInvalid     = errors.New("迁移目标不是可用的 WSL2 路径")
	ErrBusy              = errors.New("存在正在运行的后台任务")
	ErrMigrating         = errors.New("数据目录迁移正在进行")
	ErrConfirmToken      = errors.New("迁移确认令牌无效或已过期")
	ErrPlanStale         = errors.New("迁移计划已变化，请重新预览")
	ErrBlocked           = errors.New("迁移计划存在阻塞项")
	ErrTargetPartial     = errors.New("目标目录存在不完整的工作区")
	ErrSourceChanged     = errors.New("迁移源目录已变化，请重新预览")
	ErrInsufficientSpace = errors.New("目标数据目录可用空间不足")
	ErrStopFailed        = errors.New("停止 Workspace 或托管服务失败")
)

// Plan is the preview returned before migration. It is also the snapshot the
// background task acts on after Begin revalidates it.
type Plan struct {
	Eligible       bool   `json:"eligible"`
	Reason         string `json:"reason,omitempty"`
	SourceRoot     string `json:"source_root"`
	SourceDataRoot string `json:"source_data_root"`
	TargetRoot     string `json:"target_root"`
	// LegacyWSLRoot is the pre-~/hxxwork default WSL data root. Service data
	// still living there is copied into TargetRoot instead of being treated
	// as an untouchable custom absolute path.
	LegacyWSLRoot string          `json:"legacy_wsl_root,omitempty"`
	Distro        string          `json:"distro,omitempty"`
	Legacy        bool            `json:"legacy"`
	Units         []Unit          `json:"units"`
	Workspaces    []WorkspaceStop `json:"workspaces"`
	Services      []ServiceStop   `json:"services"`
	Warnings      []string        `json:"warnings"`
	Blockers      []Blocker       `json:"blockers"`
	TotalBytes    int64           `json:"total_bytes"`
	CheckedAt     time.Time       `json:"checked_at"`
	ConfirmToken  string          `json:"confirm_token,omitempty"`
	ExpiresAt     time.Time       `json:"expires_at,omitempty"`
}

// Unit is one workspace, service directory, or backup file to copy.
type Unit struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
	SizeBytes   int64  `json:"size_bytes"`
}

// WorkspaceStop describes a workspace that must be stopped before copying.
type WorkspaceStop struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	State       string `json:"state"`
}

// ServiceStop describes a managed service that must be stopped before copying.
type ServiceStop struct {
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	State  string `json:"state"`
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

// Blocker prevents migration until resolved.
type Blocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// Result summarizes a completed migration.
type Result struct {
	RunID             string    `json:"run_id"`
	SourceRoot        string    `json:"source_root"`
	TargetRoot        string    `json:"target_root"`
	UnitsCopied       []string  `json:"units_copied"`
	UnitsSkipped      []string  `json:"units_skipped"`
	BytesCopied       int64     `json:"bytes_copied"`
	WorkspacesStopped []string  `json:"workspaces_stopped"`
	ServicesStopped   []string  `json:"services_stopped"`
	Warnings          []string  `json:"warnings"`
	CommittedAt       time.Time `json:"committed_at"`
}
