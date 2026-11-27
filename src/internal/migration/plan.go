package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func (m *Manager) plan(ctx context.Context) (*Plan, error) {
	cfg := m.cfg.Get()
	plan := &Plan{
		SourceRoot:     m.paths.Root,
		SourceDataRoot: m.paths.DataRoot,
		Legacy:         !m.paths.UsesSplitDataRoot(),
		Units:          []Unit{},
		Workspaces:     []WorkspaceStop{},
		Services:       []ServiceStop{},
		Warnings:       []string{},
		Blockers:       []Blocker{},
		CheckedAt:      m.now().UTC(),
	}

	switch {
	case !m.windows:
		plan.Reason = "仅 Windows 实例支持迁移到 WSL2 数据目录"
		return plan, nil
	case m.paths.DataRootPending(cfg.Storage.DataRoot):
		plan.Reason = ErrPendingRestart.Error()
		return plan, nil
	case m.paths.UsesSplitDataRoot():
		plan.Reason = "当前实例已经使用独立的数据目录，无需迁移"
		return plan, nil
	case m.tasks != nil && m.tasks.Active():
		plan.Reason = "存在正在运行的后台任务，请稍后再试"
		return plan, nil
	case m.runtime == nil:
		plan.Reason = ErrWSLUnavailable.Error()
		return plan, nil
	}

	status := m.runtime.WSLStatus(ctx)
	if !status.Available || strings.TrimSpace(status.Distro) == "" {
		plan.Reason = ErrWSLUnavailable.Error()
		if status.Error != "" {
			plan.Reason += "：" + status.Error
		}
		return plan, nil
	}
	plan.Distro = status.Distro
	plan.LegacyWSLRoot = legacyWSLDataRoot(status)

	target, err := m.runtime.DefaultDataRoot(ctx)
	if err != nil {
		plan.Reason = "无法确定 WSL2 数据目录：" + err.Error()
		return plan, nil
	}
	target = filepath.Clean(strings.TrimSpace(target))
	plan.TargetRoot = target
	if config.SamePath(target, m.paths.DataRoot) {
		plan.Reason = "WSL2 数据目录与当前数据目录相同，无需迁移"
		return plan, nil
	}
	if err := validateTarget(target); err != nil {
		plan.Reason = err.Error()
		return plan, nil
	}

	plan.Eligible = true
	if err := m.addWorkspacePlan(ctx, plan); err != nil {
		return nil, err
	}
	m.addServicePlan(ctx, plan, cfg)
	m.addBackupPlan(plan)

	plan.Warnings = append(plan.Warnings,
		"符号链接按原样复制；指向迁移目录之外的链接不会被重写。",
		"临时目录 tmp/ 不参与迁移。",
	)
	sort.Slice(plan.Units, func(i, j int) bool { return plan.Units[i].ID < plan.Units[j].ID })
	return plan, nil
}

func validateTarget(target string) error {
	if strings.ContainsAny(target, "\x00\r\n") {
		return fmt.Errorf("%w: 目标路径包含控制字符", ErrTargetInvalid)
	}
	lower := strings.ToLower(target)
	if !strings.HasPrefix(lower, `\\wsl.localhost\`) && !strings.HasPrefix(lower, `\\wsl$\`) {
		return fmt.Errorf("%w: 目标必须是 \\\\wsl.localhost\\<发行版>\\... 路径", ErrTargetInvalid)
	}
	linux, err := runtime.ToWSLPath(target)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTargetInvalid, err)
	}
	linux = path.Clean(linux)
	if linux == "/" || strings.HasPrefix(linux, "/mnt/") {
		return fmt.Errorf("%w: 目标不能位于 Windows 挂载点 %s", ErrTargetInvalid, linux)
	}
	return nil
}

func (m *Manager) addWorkspacePlan(ctx context.Context, plan *Plan) error {
	sourceDir := filepath.Join(plan.SourceDataRoot, "workspaces")
	entries, err := os.ReadDir(sourceDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("读取工作区目录: %w", err)
	}

	live := map[string]bool{}
	if m.projects != nil {
		for _, id := range m.projects.LiveProcesses() {
			live[id] = true
		}
	}

	seenDirs := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("工作区目录中的 %s 不是目录，已跳过", entry.Name()))
			continue
		}
		id := entry.Name()
		seenDirs[id] = true
		source := filepath.Join(sourceDir, id)
		destination := filepath.Join(plan.TargetRoot, "workspaces", id)
		unit := Unit{
			ID:          "workspace:" + id,
			Kind:        KindWorkspace,
			Name:        id,
			Source:      source,
			Destination: destination,
			Action:      ActionCopy,
		}
		var sourceMeta workspace.Workspace
		if err := store.ReadJSON(filepath.Join(source, "workspace.json"), &sourceMeta); err != nil {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("工作区 %s 的元数据无法读取，将按目录原样复制", id))
		}
		unit.SizeBytes = store.DirSize(source)

		if info, statErr := os.Stat(destination); statErr == nil {
			switch {
			case !info.IsDir():
				unit.Action = ActionBlocked
				unit.Reason = "目标位置已存在同名文件"
			case completeWorkspace(destination, id):
				unit.Action = ActionSkip
				unit.Reason = "目标中已存在完整的工作区"
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("工作区 %s 已存在于目标目录，不会覆盖", id))
			default:
				unit.Action = ActionBlocked
				unit.Reason = "目标工作区已存在但不完整"
			}
			if unit.Action == ActionBlocked {
				plan.Blockers = append(plan.Blockers, Blocker{
					Code:    BlockerTargetWorkspacePartial,
					Message: fmt.Sprintf("目标工作区 %s 不完整，请先清理 %s", id, destination),
					Path:    destination,
				})
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			unit.Action = ActionBlocked
			unit.Reason = statErr.Error()
			plan.Blockers = append(plan.Blockers, Blocker{
				Code:    BlockerTargetWorkspacePartial,
				Message: fmt.Sprintf("无法检查目标工作区 %s：%v", id, statErr),
				Path:    destination,
			})
		}
		if unit.Action == ActionCopy {
			plan.TotalBytes += unit.SizeBytes
		}
		plan.Units = append(plan.Units, unit)
	}

	if m.workspaces == nil {
		return nil
	}
	list, err := m.workspaces.List(ctx)
	if err != nil {
		return fmt.Errorf("列出工作区: %w", err)
	}
	for _, ws := range list {
		if !seenDirs[ws.WorkspaceID] {
			source := filepath.Join(sourceDir, ws.WorkspaceID)
			plan.Blockers = append(plan.Blockers, Blocker{
				Code:    BlockerSourceWorkspaceMissing,
				Message: fmt.Sprintf("工作区 %s 的源目录不存在：%s", ws.Name, source),
				Path:    source,
			})
			plan.Units = append(plan.Units, Unit{
				ID:          "workspace:" + ws.WorkspaceID,
				Kind:        KindWorkspace,
				Name:        ws.Name,
				Source:      source,
				Destination: filepath.Join(plan.TargetRoot, "workspaces", ws.WorkspaceID),
				Action:      ActionBlocked,
				Reason:      "源工作区目录不存在",
			})
		}
		if ws.State == workspace.StateInitializing {
			plan.Blockers = append(plan.Blockers, Blocker{
				Code:    BlockerWorkspaceBusy,
				Message: fmt.Sprintf("工作区 %s 正在初始化，请完成后再迁移", ws.Name),
			})
		}
		if workspaceBusy(ws, live[ws.WorkspaceID]) {
			plan.Workspaces = append(plan.Workspaces, WorkspaceStop{
				WorkspaceID: ws.WorkspaceID,
				Name:        ws.Name,
				State:       string(ws.State),
			})
		}
	}
	return nil
}

func (m *Manager) addServicePlan(ctx context.Context, plan *Plan, cfg config.Config) {
	statuses := map[string]service.Status{}
	if m.services != nil {
		for _, status := range m.services.List(ctx) {
			statuses[status.Name] = status
		}
	}
	specs := []struct {
		name    service.Name
		mode    string
		dataDir string
	}{
		{service.PostgreSQL, cfg.Services.PostgreSQL.Mode, cfg.Services.PostgreSQL.DataDir},
		{service.Redis, cfg.Services.Redis.Mode, cfg.Services.Redis.DataDir},
	}
	for _, spec := range specs {
		name := string(spec.name)
		status := statuses[name]
		if strings.TrimSpace(spec.mode) == "" {
			spec.mode = "managed"
		}
		stateDir := filepath.Join(plan.SourceDataRoot, "services", name)
		targetStateDir := filepath.Join(plan.TargetRoot, "services", name)

		if spec.mode == "external" {
			plan.Services = append(plan.Services, ServiceStop{
				Name: name, Mode: spec.mode, State: status.State,
				Action: "skip", Reason: "外部服务不迁移",
			})
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("%s 使用外部连接，迁移不会修改其配置", name))
			continue
		}

		dataDir := strings.TrimSpace(spec.dataDir)
		if dataDir != "" && !filepath.IsAbs(dataDir) && !filepath.IsLocal(dataDir) {
			message := fmt.Sprintf("%s 的数据目录 %q 指向数据根目录之外", name, dataDir)
			plan.Blockers = append(plan.Blockers, Blocker{
				Code: BlockerServiceDataDirEscape, Message: message, Path: dataDir,
			})
			plan.Units = append(plan.Units, Unit{
				ID: "service:" + name, Kind: KindService, Name: name,
				Source: stateDir, Destination: targetStateDir,
				Action: ActionBlocked, Reason: message,
			})
			continue
		}

		resolved := m.paths.ResolveDataDir(dataDir)
		targetActive, legacyActive, legacyWSLActive, customAbsolute := classifyServiceDataDir(
			dataDir, resolved, plan.SourceDataRoot, plan.TargetRoot, plan.LegacyWSLRoot)

		hasCopy := false
		switch {
		case targetActive:
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("%s 当前已使用 WSL2 数据目录，旧服务目录不会复制", name))
			plan.Units = append(plan.Units, Unit{
				ID: "service:" + name, Kind: KindService, Name: name,
				Source: stateDir, Destination: targetStateDir,
				Action: ActionSkip, Reason: "目标数据目录已在使用",
			})
		case legacyActive:
			if _, err := os.Stat(stateDir); err == nil {
				hasCopy = appendCopyUnit(plan, Unit{
					ID: "service:" + name, Kind: KindService, Name: name,
					Source: stateDir, Destination: targetStateDir,
					Action: ActionCopy,
				}) || hasCopy
			}
			if resolved != "" {
				if _, covered := config.RelWithin(stateDir, resolved); !covered {
					rel, ok := config.RelWithin(plan.SourceDataRoot, resolved)
					if !ok {
						plan.Blockers = append(plan.Blockers, Blocker{
							Code:    BlockerServiceDataDirEscape,
							Message: fmt.Sprintf("%s 的数据目录 %s 不在源数据目录内", name, resolved),
							Path:    resolved,
						})
						break
					}
					hasCopy = appendCopyUnit(plan, Unit{
						ID: "service:" + name + ":data", Kind: KindService, Name: name,
						Source:      resolved,
						Destination: filepath.Join(plan.TargetRoot, rel),
						Action:      ActionCopy,
					}) || hasCopy
				}
			}
		case legacyWSLActive:
			rel, ok := config.RelWithin(plan.LegacyWSLRoot, resolved)
			if !ok {
				plan.Blockers = append(plan.Blockers, Blocker{
					Code:    BlockerServiceDataDirEscape,
					Message: fmt.Sprintf("%s 的数据目录 %s 不在迁移前的 WSL2 默认目录内", name, resolved),
					Path:    resolved,
				})
				break
			}
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("%s 的数据目录位于迁移前的 WSL2 默认目录，将复制到新数据目录；旧 Windows 服务状态目录不迁移", name))
			hasCopy = appendCopyUnit(plan, Unit{
				ID: "service:" + name + ":data", Kind: KindService, Name: name,
				Source:      resolved,
				Destination: filepath.Join(plan.TargetRoot, rel),
				Action:      ActionCopy,
			}) || hasCopy
		case customAbsolute:
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("%s 使用自定义绝对数据目录 %s，迁移不会复制或修改它", name, resolved))
		}

		action := "skip"
		reason := "无需停止"
		if status.State == service.StateRunning ||
			(hasCopy && status.State != service.StateNotInstalled) {
			action = "stop"
			reason = "迁移前停止服务"
		}
		plan.Services = append(plan.Services, ServiceStop{
			Name: name, Mode: spec.mode, State: status.State,
			Action: action, Reason: reason,
		})
	}
}

// classifyServiceDataDir decides how a configured service data directory is
// handled during migration. An absolute path under the previous default WSL
// root is still DevKit-managed data, not a user-supplied external path, so it
// is copied into the new root instead of being left behind.
func classifyServiceDataDir(dataDir, resolved, sourceRoot, targetRoot, legacyWSLRoot string) (
	targetActive, legacyActive, legacyWSLActive, customAbsolute bool) {
	if !filepath.IsAbs(dataDir) {
		return false, true, false, false
	}
	if _, inside := config.RelWithin(targetRoot, resolved); inside {
		return true, false, false, false
	}
	if _, inside := config.RelWithin(sourceRoot, resolved); inside {
		return false, true, false, false
	}
	if legacyWSLRoot != "" {
		if _, inside := config.RelWithin(legacyWSLRoot, resolved); inside {
			return false, false, true, false
		}
	}
	return false, false, false, true
}

// legacyWSLDataRoot returns the WSL data root used before the default moved
// to ~/hxxwork. Older builds persisted absolute service paths below it.
func legacyWSLDataRoot(status runtime.WSLStatus) string {
	home := strings.TrimRight(strings.TrimSpace(status.Home), "/")
	if home == "" && strings.TrimSpace(status.User) != "" {
		home = "/home/" + strings.TrimSpace(status.User)
	}
	if home == "" || home == "/home" {
		return ""
	}
	return runtime.WSLUNCPath(status.Distro, home+"/.local/share/hxxDiscourseDevKit")
}

func (m *Manager) addBackupPlan(plan *Plan) {
	dir := filepath.Join(plan.SourceDataRoot, "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			plan.Warnings = append(plan.Warnings, "无法读取备份目录: "+err.Error())
		}
		return
	}
	for _, entry := range entries {
		appendCopyUnit(plan, Unit{
			ID: "backup:" + entry.Name(), Kind: KindBackup, Name: entry.Name(),
			Source:      filepath.Join(dir, entry.Name()),
			Destination: filepath.Join(plan.TargetRoot, "backups", entry.Name()),
			Action:      ActionCopy,
		})
	}
}

// appendCopyUnit adds a unit, skipping missing sources and existing
// destinations. It reports whether a real copy is planned.
func appendCopyUnit(plan *Plan, unit Unit) bool {
	if _, err := os.Stat(unit.Source); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("无法读取 %s：%v", unit.Source, err))
			unit.Action = ActionSkip
			unit.Reason = "源不可读"
			plan.Units = append(plan.Units, unit)
		}
		return false
	}
	unit.SizeBytes = store.DirSize(unit.Source)
	if _, err := os.Stat(unit.Destination); err == nil {
		unit.Action = ActionSkip
		unit.Reason = "目标已存在，不会覆盖"
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("%s 已存在于目标目录，不会覆盖", unit.ID))
		plan.Units = append(plan.Units, unit)
		return false
	} else if !errors.Is(err, fs.ErrNotExist) {
		unit.Action = ActionSkip
		unit.Reason = "无法检查目标"
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("无法检查目标 %s：%v", unit.Destination, err))
		plan.Units = append(plan.Units, unit)
		return false
	}
	plan.Units = append(plan.Units, unit)
	plan.TotalBytes += unit.SizeBytes
	return true
}

func completeWorkspace(dir, id string) bool {
	var ws workspace.Workspace
	if err := store.ReadJSON(filepath.Join(dir, "workspace.json"), &ws); err != nil {
		return false
	}
	return ws.WorkspaceID == id
}

func workspaceBusy(ws *workspace.Workspace, live bool) bool {
	if live {
		return true
	}
	switch ws.State {
	case workspace.StateRunning, workspace.StateStarting, workspace.StateStopping:
		return true
	}
	switch ws.Project.ProcessState {
	case "RUNNING", "STARTING", "STOPPING":
		return true
	}
	return false
}

// planDigest captures the structural parts of a plan that must not change
// between preview and Begin. File sizes are intentionally excluded because
// logs and other files can grow without making the plan unsafe.
func planDigest(plan *Plan) string {
	h := sha256.New()
	for _, part := range []string{
		plan.SourceRoot,
		plan.SourceDataRoot,
		plan.TargetRoot,
		plan.LegacyWSLRoot,
		plan.Distro,
		strconv.FormatBool(plan.Eligible),
		strconv.FormatBool(plan.Legacy),
	} {
		fmt.Fprintf(h, "%s\x00", part)
	}
	blockers := append([]Blocker(nil), plan.Blockers...)
	sort.Slice(blockers, func(i, j int) bool {
		return blockers[i].Code+blockers[i].Path+blockers[i].Message <
			blockers[j].Code+blockers[j].Path+blockers[j].Message
	})
	for _, blocker := range blockers {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", blocker.Code, blocker.Path, blocker.Message)
	}
	units := append([]Unit(nil), plan.Units...)
	sort.Slice(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	for _, unit := range units {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00",
			unit.ID, unit.Kind, unit.Source, unit.Destination, unit.Action)
	}
	return hex.EncodeToString(h.Sum(nil))
}
