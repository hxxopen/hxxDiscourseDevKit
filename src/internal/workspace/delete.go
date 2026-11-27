package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

const confirmTokenTTL = 5 * time.Minute

// PreviewDelete builds a resource inventory and issues a one-time token.
func (m *Manager) PreviewDelete(ctx context.Context, ref string) (*DeletePlan, error) {
	ws, err := m.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := m.paths.WorkspaceDir(ws.WorkspaceID)

	resources := make([]Resource, 0, 9)
	metadataPath := filepath.Join(dir, "workspace.json")
	if info, err := os.Stat(metadataPath); err == nil {
		resources = append(resources, Resource{
			Kind:      "metadata",
			Path:      metadataPath,
			Exists:    true,
			SizeBytes: info.Size(),
			Note:      "移除 DevKit 管理记录",
		})
	} else {
		resources = append(resources, Resource{Kind: "metadata", Path: metadataPath})
	}

	for _, item := range []struct {
		kind string
		dir  string
		note string
	}{
		{"source", "source", "项目源码"},
		{"config", "config", "Workspace 配置"},
		{"logs", "logs", "Workspace 日志"},
		{"data", "data", "上传文件与开发数据"},
		{"backups", "backups", "Workspace 备份"},
		{"tmp", "tmp", "临时文件"},
	} {
		path := filepath.Join(dir, item.dir)
		info, err := os.Stat(path)
		if err != nil {
			resources = append(resources, Resource{Kind: item.kind, Path: path, Note: item.note})
			continue
		}
		size := info.Size()
		if info.IsDir() {
			size = store.DirSize(path)
		}
		resources = append(resources, Resource{
			Kind:      item.kind,
			Path:      path,
			Exists:    true,
			SizeBytes: size,
			Note:      item.note,
		})
	}
	resources = append(resources,
		Resource{
			Kind:   "database",
			Exists: ws.Database.Provisioned,
			Shared: true,
			Note:   "共享 PostgreSQL 中的独立数据库与用户（不会删除其他 Workspace）",
		},
		Resource{
			Kind:   "redis",
			Exists: true,
			Shared: true,
			Note:   fmt.Sprintf("Redis DB %d 中的命名空间（仅 FLUSHDB 该索引）", ws.Redis.DBIndex),
		},
	)

	var total int64
	for _, r := range resources {
		total += r.SizeBytes
	}

	token, err := secret.RandomHex(24)
	if err != nil {
		return nil, err
	}
	expires := m.now().UTC().Add(confirmTokenTTL)

	m.mu.Lock()
	m.purgeExpiredTokensLocked()
	m.tokens[token] = deleteToken{workspaceID: ws.WorkspaceID, expiresAt: expires}
	m.mu.Unlock()

	return &DeletePlan{
		WorkspaceID:  ws.WorkspaceID,
		Name:         ws.Name,
		Running:      isRunning(ws),
		TotalBytes:   total,
		Resources:    resources,
		ConfirmToken: token,
		ExpiresAt:    expires,
	}, nil
}

// ValidateDeleteToken checks a token without consuming it.
func (m *Manager) ValidateDeleteToken(workspaceID, token string) error {
	if token == "" {
		return ErrConfirmToken
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	record, ok := m.tokens[token]
	if !ok || record.workspaceID != workspaceID || m.now().After(record.expiresAt) {
		return ErrConfirmToken
	}
	return nil
}

// Delete removes the selected workspace resources. The confirmation token is
// single-use; metadata is removed last so interrupted deletes stay manageable.
// Service cleanup happens before the lock is held again because the service
// manager queries workspace state.
func (m *Manager) Delete(ctx context.Context, workspaceID string, opts DeleteOptions) (*DeleteResult, error) {
	m.mu.Lock()
	record, ok := m.tokens[opts.ConfirmToken]
	if !ok || record.workspaceID != workspaceID || m.now().After(record.expiresAt) {
		m.mu.Unlock()
		return nil, ErrConfirmToken
	}
	delete(m.tokens, opts.ConfirmToken)
	ws, err := m.getLocked(workspaceID)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	if isRunning(ws) {
		m.mu.Unlock()
		return nil, ErrRunning
	}
	m.mu.Unlock()

	result := &DeleteResult{}
	if err := m.cleanupServices(ctx, ws, opts, result); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Re-read after releasing the lock: another request may have deleted it.
	ws, err = m.getLocked(workspaceID)
	if err != nil {
		return nil, err
	}
	if isRunning(ws) {
		return nil, ErrRunning
	}

	dir := m.paths.WorkspaceDir(ws.WorkspaceID)

	remove := func(kind, sub string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(dir, sub)
		removed, _, err := removePath(path)
		if err != nil {
			return fmt.Errorf("remove %s: %w", kind, err)
		}
		if removed {
			result.Removed = append(result.Removed, kind)
		} else {
			result.Notes = append(result.Notes, kind+" 不存在，已跳过")
		}
		return nil
	}
	keep := func(kind, sub string) {
		if _, err := os.Stat(filepath.Join(dir, sub)); err == nil {
			result.Kept = append(result.Kept, kind)
		}
	}

	if opts.DeleteConfig {
		for _, kind := range []string{"config", "logs", "tmp"} {
			if err := remove(kind, kind); err != nil {
				return nil, err
			}
		}
	} else {
		keep("config", "config")
		keep("logs", "logs")
		keep("tmp", "tmp")
	}

	if opts.DeleteSource {
		if err := remove("source", "source"); err != nil {
			return nil, err
		}
	} else {
		keep("source", "source")
	}

	if opts.DeleteData {
		for _, kind := range []string{"data", "backups"} {
			if err := remove(kind, kind); err != nil {
				return nil, err
			}
		}
	} else {
		keep("data", "data")
		keep("backups", "backups")
	}

	// Metadata goes last.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	metadataPath := filepath.Join(dir, "workspace.json")
	if removed, _, err := removePath(metadataPath); err != nil {
		return nil, fmt.Errorf("remove metadata: %w", err)
	} else if removed {
		result.Removed = append(result.Removed, "metadata")
	}

	if err := m.secrets.Delete(ws.Database.PasswordRef); err != nil {
		m.log.Warn("could not delete workspace credential", "workspace_id", ws.WorkspaceID, "error", err)
	}

	if m.cfg.Get().ActiveWorkspaceID == ws.WorkspaceID {
		next := ""
		if remaining, err := m.listLocked(); err == nil && len(remaining) > 0 {
			next = remaining[0].WorkspaceID
		}
		if err := m.cfg.Update(func(c *config.Config) error {
			c.ActiveWorkspaceID = next
			return nil
		}); err != nil {
			m.log.Warn("could not update active workspace after delete", "error", err)
		}
	}

	_ = store.RemoveIfEmpty(dir)
	m.log.Info("workspace deleted",
		"workspace_id", ws.WorkspaceID,
		"name", ws.Name,
		"removed", result.Removed,
		"kept", result.Kept,
	)
	return result, nil
}

func (m *Manager) cleanupServices(ctx context.Context, ws *Workspace, opts DeleteOptions, result *DeleteResult) error {
	m.svcMu.RLock()
	provisioner := m.provisioner
	m.svcMu.RUnlock()
	if provisioner == nil {
		if opts.DeleteDatabase {
			result.Notes = append(result.Notes, "服务管理器不可用，数据库已保留")
			result.Kept = append(result.Kept, "database")
		}
		if opts.DeleteRedis {
			result.Notes = append(result.Notes, "服务管理器不可用，Redis 数据已保留")
			result.Kept = append(result.Kept, "redis")
		}
		return nil
	}
	if opts.DeleteDatabase {
		if !ws.Database.Provisioned {
			result.Notes = append(result.Notes, "数据库尚未创建，跳过删除")
		} else if err := provisioner.DropWorkspaceDatabase(ctx, ws); err != nil {
			return fmt.Errorf("删除 Workspace 数据库: %w", err)
		} else {
			result.DatabaseDropped = true
			result.Removed = append(result.Removed, "database")
		}
	} else {
		result.Kept = append(result.Kept, "database")
	}
	if opts.DeleteRedis {
		if err := provisioner.ClearWorkspaceRedis(ctx, ws); err != nil {
			return fmt.Errorf("清理 Workspace Redis 数据: %w", err)
		}
		result.RedisCleared = true
		result.Removed = append(result.Removed, "redis")
	} else {
		result.Kept = append(result.Kept, "redis")
	}
	return nil
}

func (m *Manager) purgeExpiredTokensLocked() {
	now := m.now()
	for token, record := range m.tokens {
		if now.After(record.expiresAt) {
			delete(m.tokens, token)
		}
	}
}

func isRunning(ws *Workspace) bool {
	return ws.State == StateRunning || ws.State == StateStarting || ws.Project.ProcessState == "RUNNING"
}

func removePath(path string) (bool, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, 0, nil
		}
		return false, 0, err
	}
	size := info.Size()
	if info.IsDir() {
		size = store.DirSize(path)
	}
	if err := os.RemoveAll(path); err != nil {
		return false, size, err
	}
	return true, size, nil
}
