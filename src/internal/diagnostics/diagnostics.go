// Package diagnostics runs non-destructive health checks.
package diagnostics

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Severity classifies a check result.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// Check is one diagnostic result.
type Check struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Severity    Severity       `json:"severity"`
	Message     string         `json:"message"`
	Details     map[string]any `json:"details,omitempty"`
	FixAction   string         `json:"fix_action,omitempty"`
	Destructive bool           `json:"destructive"`
}

// Report is the complete diagnostic output.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Items       []Check   `json:"items"`
}

// Counts summarizes a report.
func (r Report) Counts() map[Severity]int {
	counts := map[Severity]int{}
	for _, item := range r.Items {
		counts[item.Severity]++
	}
	return counts
}

// Options supplies optional M2 subsystems.
type Options struct {
	Runtime  *runtime.Manager
	Services *service.Manager
}

// Service runs checks against the live instance.
type Service struct {
	paths      *config.Paths
	cfg        *config.Store
	workspaces *workspace.Manager
	runtime    *runtime.Manager
	services   *service.Manager
	log        *slog.Logger
}

// New creates a diagnostics service.
func New(paths *config.Paths, cfg *config.Store, workspaces *workspace.Manager,
	logger *slog.Logger, opts ...Options) *Service {
	svc := &Service{paths: paths, cfg: cfg, workspaces: workspaces, log: logger}
	for _, opt := range opts {
		if opt.Runtime != nil {
			svc.runtime = opt.Runtime
		}
		if opt.Services != nil {
			svc.services = opt.Services
		}
	}
	return svc
}

// Run executes all checks.
func (s *Service) Run(ctx context.Context) Report {
	report := Report{GeneratedAt: time.Now().UTC()}
	add := func(check Check) { report.Items = append(report.Items, check) }

	// Root writability.
	probe := filepath.Join(s.paths.Root, ".write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		add(Check{
			ID: "devkit.root", Title: "实例根目录",
			Severity: SeverityError,
			Message:  "根目录不可写: " + err.Error(),
			Details:  map[string]any{"root": s.paths.Root},
		})
	} else {
		_ = os.Remove(probe)
		add(Check{
			ID: "devkit.root", Title: "实例根目录",
			Severity: SeverityInfo,
			Message:  "根目录可读写",
			Details:  map[string]any{"root": s.paths.Root},
		})
	}

	cfg := s.cfg.Get()
	add(Check{
		ID: "devkit.config", Title: "实例配置",
		Severity: SeverityInfo,
		Message:  fmt.Sprintf("配置有效，实例 %s", cfg.InstanceID),
		Details:  map[string]any{"schema_version": cfg.SchemaVersion},
	})

	s.runRuntimeChecks(ctx, add)
	s.runServiceChecks(ctx, add)

	list, err := s.workspaces.List(ctx)
	if err != nil {
		add(Check{
			ID: "workspaces.metadata", Title: "Workspace 元数据",
			Severity: SeverityError,
			Message:  "无法读取 Workspace: " + err.Error(),
		})
	} else {
		add(Check{
			ID: "workspaces.metadata", Title: "Workspace 元数据",
			Severity: SeverityInfo,
			Message:  fmt.Sprintf("发现 %d 个 Workspace", len(list)),
			Details:  map[string]any{"count": len(list)},
		})
		for _, ws := range list {
			dir := s.paths.WorkspaceDir(ws.WorkspaceID)
			if ws.Source.Installed {
				sourcePath := ws.Source.Path
				if sourcePath == "" {
					sourcePath = "source"
				}
				if _, err := os.Stat(filepath.Join(dir, sourcePath)); err != nil {
					add(Check{
						ID:       "workspace.source." + ws.WorkspaceID,
						Title:    "Workspace 源码",
						Severity: SeverityError,
						Message:  fmt.Sprintf("%s: 已标记安装源码但目录不存在", ws.Name),
						Details:  map[string]any{"workspace_id": ws.WorkspaceID},
					})
				}
			}
			if s.services != nil && !ws.Database.Provisioned {
				message := fmt.Sprintf("%s: 数据库尚未创建", ws.Name)
				if ws.Database.ProvisionError != "" {
					message += "（" + ws.Database.ProvisionError + "）"
				}
				add(Check{
					ID:        "workspace.database." + ws.WorkspaceID,
					Title:     "Workspace 数据库",
					Severity:  SeverityWarning,
					Message:   message,
					Details:   map[string]any{"workspace_id": ws.WorkspaceID},
					FixAction: fmt.Sprintf("启动 PostgreSQL 后执行 hxxddk workspace provision %s 重试。", ws.Name),
				})
			}
		}
	}

	add(Check{
		ID: "devkit.lock", Title: "单实例运行",
		Severity: SeverityInfo,
		Message:  "devkitd 正在运行并持有实例锁",
		Details:  map[string]any{"pid": os.Getpid()},
	})

	sort.SliceStable(report.Items, func(i, j int) bool {
		return severityRank(report.Items[i].Severity) < severityRank(report.Items[j].Severity)
	})
	return report
}

func (s *Service) runRuntimeChecks(ctx context.Context, add func(Check)) {
	if s.runtime == nil {
		if _, err := os.Stat(s.paths.RuntimeMatrixFile()); err != nil {
			add(Check{
				ID: "runtime.matrix", Title: "Runtime 版本矩阵",
				Severity: SeverityWarning,
				Message:  "版本矩阵尚未生成；Runtime 管理未启用",
			})
		}
		return
	}
	matrix, builtin, err := s.runtime.Matrix()
	if err != nil {
		add(Check{
			ID: "runtime.matrix", Title: "Runtime 版本矩阵",
			Severity:  SeverityError,
			Message:   "无法加载版本矩阵: " + err.Error(),
			FixAction: "修正 config/runtime-matrix.json 的 JSON 结构与字段后重新检查。",
		})
		return
	}
	fixAction := ""
	severity := SeverityInfo
	message := fmt.Sprintf("版本矩阵 %s，包含 %d 个组件", matrix.Profile, len(matrix.Components))
	if builtin {
		severity = SeverityWarning
		message = "使用内置默认版本矩阵；发布包尚未提供 runtime-matrix.json"
		fixAction = "运行发布包的初始化流程，或把 runtime-matrix.json 复制到 config/ 固定组件版本。"
	}
	add(Check{
		ID: "runtime.matrix", Title: "Runtime 版本矩阵",
		Severity: severity,
		Message:  message,
		Details: map[string]any{
			"profile": matrix.Profile,
			"builtin": builtin,
			"path":    s.runtime.MatrixPath(),
		},
		FixAction: fixAction,
	})

	items, err := s.runtime.List(ctx)
	if err != nil {
		add(Check{
			ID: "runtime.components", Title: "Runtime 组件",
			Severity: SeverityError,
			Message:  "无法验证组件: " + err.Error(),
		})
		return
	}
	for _, item := range items {
		check := Check{
			ID:    "runtime.component." + item.Name,
			Title: "Runtime 组件 " + item.Name,
			Details: map[string]any{
				"required":           item.Required,
				"expected_version":   item.ExpectedVersion,
				"actual_version":     item.ActualVersion,
				"source":             item.Source,
				"path":               item.Path,
				"execution":          item.Execution,
				"hash_verified":      item.HashVerified,
				"version_compatible": item.VersionCompatible,
			},
		}
		switch {
		case !item.Available:
			check.FixAction = fmt.Sprintf("安装 %s，或在 config/runtime-matrix.json 中配置 binary 路径与执行环境。", item.Name)
			check.Severity = SeverityWarning
			if item.Required {
				check.Severity = SeverityError
			}
			check.Message = fmt.Sprintf("未安装或不可用（要求 %s）", item.ExpectedVersion)
			if item.Message != "" {
				check.Message += ": " + item.Message
			}
		case !item.VersionCompatible || (item.SHA256 != "" && !item.HashVerified):
			check.FixAction = "调整组件版本，或更新 runtime-matrix.json 中的期望版本与 SHA-256。"
			check.Severity = SeverityWarning
			if item.Required {
				check.Severity = SeverityError
			}
			check.Message = item.Message
			if check.Message == "" {
				check.Message = "版本或哈希校验未通过"
			}
		default:
			check.Severity = SeverityInfo
			check.Message = fmt.Sprintf("已验证（实际版本 %s）", item.ActualVersion)
		}
		add(check)
	}

	if goruntime.GOOS == "windows" {
		distro, kernel, err := s.runtime.ProbeWSL(ctx)
		if err != nil {
			add(Check{
				ID: "runtime.wsl", Title: "WSL2 执行环境",
				Severity:  SeverityWarning,
				Message:   "WSL2 不可用；需要 WSL 的组件将无法运行: " + err.Error(),
				FixAction: "安装 WSL2 发行版，或在 config/devkit.json 的 runtime.wsl_distro 中指定发行版。",
			})
		} else {
			add(Check{
				ID: "runtime.wsl", Title: "WSL2 执行环境",
				Severity: SeverityInfo,
				Message:  "WSL2 可用",
				Details:  map[string]any{"distro": distro, "kernel": kernel},
			})
		}
	}
}

func (s *Service) runServiceChecks(ctx context.Context, add func(Check)) {
	if s.services == nil {
		return
	}
	for _, status := range s.services.List(ctx) {
		check := Check{
			ID:    "service." + status.Name,
			Title: "服务 " + status.Name,
			Details: map[string]any{
				"mode":          status.Mode,
				"state":         status.State,
				"host":          status.Host,
				"port":          status.Port,
				"reachable":     status.Reachable,
				"authenticated": status.Authenticated,
				"version":       status.Version,
				"path":          status.Path,
				"execution":     status.Execution,
			},
		}
		switch status.State {
		case "RUNNING", "EXTERNAL":
			check.Severity = SeverityInfo
		case "STOPPED":
			check.Severity = SeverityWarning
			check.FixAction = fmt.Sprintf("运行 hxxddk service start %s，或在「服务与运行时」启动。", status.Name)
		case "NOT_INSTALLED":
			check.Severity = SeverityWarning
			check.FixAction = fmt.Sprintf("安装 %s 所需工具，或在「服务与运行时」切换为 external 模式。", status.Name)
		default:
			check.Severity = SeverityError
			check.FixAction = fmt.Sprintf("检查地址、端口与凭据，并运行 hxxddk service test %s 重新验证。", status.Name)
		}
		check.Message = status.Message
		if check.Message == "" {
			check.Message = status.State
		}
		add(check)
	}
}

func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}
