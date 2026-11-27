package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

type run struct {
	manager *Manager
	plan    *Plan
	runID   string
	once    sync.Once
}

func (r *run) Abort() {
	r.finish()
}

func (r *run) finish() {
	r.once.Do(func() {
		r.manager.finishRun(r.runID)
	})
}

func (r *run) stagingDir() string {
	return filepath.Join(r.plan.TargetRoot, stagingDirName, r.runID)
}

func (r *run) Execute(ctx context.Context, progress func(int, string)) (*Result, error) {
	defer r.finish()
	if progress == nil {
		progress = func(int, string) {}
	}
	result := &Result{
		RunID:             r.runID,
		SourceRoot:        r.plan.SourceRoot,
		TargetRoot:        r.plan.TargetRoot,
		UnitsCopied:       []string{},
		UnitsSkipped:      []string{},
		WorkspacesStopped: []string{},
		ServicesStopped:   []string{},
		Warnings:          []string{},
	}

	progress(2, "停止运行中的 Workspace 和托管服务")
	if err := r.stop(ctx, result, progress); err != nil {
		return result, err
	}

	progress(20, "准备 WSL2 数据目录")
	copier := r.copier()
	if copier == nil {
		return result, fmt.Errorf("WSL2 复制器不可用")
	}
	if err := r.prepare(ctx, copier, result); err != nil {
		return result, err
	}

	progress(25, "复制数据")
	if err := r.copyUnits(ctx, copier, result, progress); err != nil {
		r.cleanup(ctx, copier, result)
		return result, err
	}

	progress(90, "更新实例配置")
	if err := r.commit(result); err != nil {
		r.cleanup(ctx, copier, result)
		return result, err
	}

	progress(96, "清理迁移临时目录")
	r.cleanup(ctx, copier, result)
	progress(100, "迁移完成，请重启 devkitd 以切换到新数据目录")
	return result, nil
}

func (r *run) copier() Copier {
	if r.manager.newCopier == nil {
		return nil
	}
	return r.manager.newCopier(r.plan.Distro)
}

func (r *run) stop(ctx context.Context, result *Result, progress func(int, string)) error {
	if len(r.plan.Workspaces) > 0 && r.manager.projects == nil {
		return fmt.Errorf("%w: Workspace 控制器不可用", ErrStopFailed)
	}
	for i, ws := range r.plan.Workspaces {
		if err := ctx.Err(); err != nil {
			return err
		}
		progress(3+i, "停止 Workspace "+ws.Name)
		if err := r.manager.projects.Stop(ctx, ws.WorkspaceID, nil); err != nil {
			return fmt.Errorf("%w: 停止 Workspace %s 失败: %v", ErrStopFailed, ws.Name, err)
		}
		result.WorkspacesStopped = append(result.WorkspacesStopped, ws.WorkspaceID)
	}
	if r.manager.projects != nil {
		if live := r.manager.projects.LiveProcesses(); len(live) > 0 {
			return fmt.Errorf("%w: 仍有运行中的 Workspace 进程: %s",
				ErrStopFailed, strings.Join(live, ", "))
		}
	}

	stopServices := make([]ServiceStop, 0, len(r.plan.Services))
	for _, svc := range r.plan.Services {
		if svc.Action == "stop" {
			stopServices = append(stopServices, svc)
		}
	}
	if len(stopServices) > 0 && r.manager.services == nil {
		return fmt.Errorf("%w: 服务控制器不可用", ErrStopFailed)
	}
	for i, svc := range stopServices {
		if err := ctx.Err(); err != nil {
			return err
		}
		progress(10+i, "停止服务 "+svc.Name)
		err := r.manager.services.Stop(ctx, service.Name(svc.Name), nil)
		if err != nil &&
			!errors.Is(err, service.ErrExternal) &&
			!errors.Is(err, service.ErrNotInstalled) &&
			!errors.Is(err, service.ErrNotRunning) {
			return fmt.Errorf("%w: 停止 %s 失败: %v", ErrStopFailed, svc.Name, err)
		}
		result.ServicesStopped = append(result.ServicesStopped, svc.Name)
	}
	if r.manager.services != nil {
		for _, status := range r.manager.services.List(ctx) {
			if status.Mode == "external" {
				continue
			}
			if status.State == service.StateRunning {
				return fmt.Errorf("%w: 服务 %s 仍在运行", ErrStopFailed, status.Name)
			}
		}
	}
	progress(18, "所有 Workspace 和托管服务已停止")
	return nil
}

func (r *run) prepare(ctx context.Context, copier Copier, result *Result) error {
	for _, dir := range []string{
		r.plan.TargetRoot,
		filepath.Join(r.plan.TargetRoot, "workspaces"),
		filepath.Join(r.plan.TargetRoot, "services"),
		filepath.Join(r.plan.TargetRoot, "backups"),
		filepath.Join(r.plan.TargetRoot, stagingDirName),
		r.stagingDir(),
	} {
		if err := copier.EnsureDir(ctx, dir, 0o755); err != nil {
			return fmt.Errorf("创建目标目录 %s: %w", dir, err)
		}
	}

	stagingRoot := filepath.Join(r.plan.TargetRoot, stagingDirName)
	if entries, err := os.ReadDir(stagingRoot); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() || entry.Name() == r.runID {
				continue
			}
			info, err := entry.Info()
			if err != nil || r.manager.now().Sub(info.ModTime()) < staleStagingTTL {
				continue
			}
			if err := copier.RemoveAll(ctx, filepath.Join(stagingRoot, entry.Name())); err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("无法清理旧的迁移临时目录 %s: %v", entry.Name(), err))
			}
		}
	}

	free, err := copier.FreeBytes(ctx, r.plan.TargetRoot)
	if err != nil {
		result.Warnings = append(result.Warnings, "无法检查目标可用空间: "+err.Error())
		return nil
	}
	required := r.plan.TotalBytes + spaceMarginBytes
	if free > 0 && free < required {
		return fmt.Errorf("%w: 需要约 %d MB，可用 %d MB",
			ErrInsufficientSpace, required>>20, free>>20)
	}
	return nil
}

func (r *run) copyUnits(ctx context.Context, copier Copier, result *Result, progress func(int, string)) error {
	units := make([]Unit, 0, len(r.plan.Units))
	for _, unit := range r.plan.Units {
		if unit.Action == ActionCopy {
			units = append(units, unit)
		}
	}
	if len(units) == 0 {
		progress(88, "没有需要复制的数据")
		return nil
	}
	totalBytes := r.plan.TotalBytes
	if totalBytes <= 0 {
		totalBytes = 1
	}
	var copiedBytes int64
	staging := r.stagingDir()

	for i, unit := range units {
		if err := ctx.Err(); err != nil {
			return err
		}
		skip, err := r.targetChanged(unit, result)
		if err != nil {
			return err
		}
		if skip {
			continue
		}

		stagingUnit := filepath.Join(staging, fmt.Sprintf("%03d", i))
		progress(25+int(60*copiedBytes/totalBytes),
			fmt.Sprintf("复制 %s（%d/%d）", unit.Name, i+1, len(units)))
		if err := copier.CopyTree(ctx, unit.Source, stagingUnit); err != nil {
			return err
		}
		if _, err := os.Stat(stagingUnit); err != nil {
			return fmt.Errorf("复制 %s 后未找到暂存目录: %w", unit.Name, err)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Service data lives below services/<name>/data, whose parent is not
		// created by prepare. mv -T does not create intermediate directories.
		if parent := filepath.Dir(unit.Destination); parent != "" && parent != "." {
			if err := copier.EnsureDir(ctx, parent, 0o755); err != nil {
				return fmt.Errorf("创建目标父目录 %s: %w", parent, err)
			}
		}
		if err := copier.Move(ctx, stagingUnit, unit.Destination); err != nil {
			return err
		}
		result.UnitsCopied = append(result.UnitsCopied, unit.ID)
		copiedBytes += unit.SizeBytes
		if copiedBytes > totalBytes {
			copiedBytes = totalBytes
		}
	}
	progress(88, "数据复制完成")
	return nil
}

// targetChanged rechecks the destination immediately before copying. A
// workspace that became complete is skipped; a partial one aborts.
func (r *run) targetChanged(unit Unit, result *Result) (bool, error) {
	info, err := os.Stat(unit.Destination)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("检查目标 %s 失败: %w", unit.Destination, err)
	}
	if unit.Kind == KindWorkspace {
		if info.IsDir() && completeWorkspace(unit.Destination, unit.Name) {
			result.UnitsSkipped = append(result.UnitsSkipped, unit.ID)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("工作区 %s 已存在于目标目录，跳过", unit.Name))
			return true, nil
		}
		return false, fmt.Errorf("%w: %s", ErrTargetPartial, unit.Destination)
	}
	result.UnitsSkipped = append(result.UnitsSkipped, unit.ID)
	result.Warnings = append(result.Warnings,
		fmt.Sprintf("%s 已存在于目标目录，跳过", unit.ID))
	return true, nil
}

func (r *run) commit(result *Result) error {
	err := r.manager.cfg.Update(func(next *config.Config) error {
		next.Storage.DataRoot = r.plan.TargetRoot
		next.Services.PostgreSQL.DataDir = rebaseDataDir(
			next.Services.PostgreSQL.DataDir, r.plan.SourceDataRoot, r.plan.LegacyWSLRoot)
		next.Services.Redis.DataDir = rebaseDataDir(
			next.Services.Redis.DataDir, r.plan.SourceDataRoot, r.plan.LegacyWSLRoot)
		return nil
	})
	if err != nil {
		return fmt.Errorf("更新实例配置: %w", err)
	}
	result.CommittedAt = r.manager.now().UTC()
	return nil
}

// rebaseDataDir makes a service data directory relative when it lives under
// the legacy Windows data root or the previous WSL default root, because both
// are copied into the new data root at the same relative path. Target-absolute
// paths stay absolute because the running daemon still resolves relative paths
// against the old root until it restarts.
func rebaseDataDir(value, sourceRoot, legacyWSLRoot string) string {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return value
	}
	if rel, inside := config.RelWithin(sourceRoot, value); inside {
		return filepath.ToSlash(rel)
	}
	if legacyWSLRoot != "" {
		if rel, inside := config.RelWithin(legacyWSLRoot, value); inside {
			return filepath.ToSlash(rel)
		}
	}
	return value
}

func (r *run) cleanup(base context.Context, copier Copier, result *Result) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(base), 60*time.Second)
	defer cancel()
	if err := copier.RemoveAll(ctx, r.stagingDir()); err != nil {
		result.Warnings = append(result.Warnings, "清理迁移临时目录失败: "+err.Error())
		r.manager.log.Warn("could not remove migration staging directory",
			"run_id", r.runID, "error", err)
	}
}
