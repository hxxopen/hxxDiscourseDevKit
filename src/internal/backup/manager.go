package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/version"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

var backupIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}$`)

// Manager creates, verifies, and restores workspace backups.
type Manager struct {
	paths      *config.Paths
	workspaces *workspace.Manager
	services   *service.Manager
	log        *slog.Logger
	now        func() time.Time
	freeSpace  func(path string) (uint64, error)

	locks  sync.Map // workspace ID -> *sync.Mutex
	mu     sync.Mutex
	tokens map[string]restoreToken
}

// New creates a backup manager.
func New(paths *config.Paths, workspaces *workspace.Manager, services *service.Manager,
	logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		paths:      paths,
		workspaces: workspaces,
		services:   services,
		log:        logger,
		now:        time.Now,
		freeSpace:  freeSpace,
		tokens:     map[string]restoreToken{},
	}
}

// Create writes a new archive for a stopped workspace.
func (m *Manager) Create(ctx context.Context, ref string, opts CreateOptions, progress func(int, string)) (*Backup, error) {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	unlock := m.lock(ws.WorkspaceID)
	defer unlock()
	ws, err = m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if workspaceActive(ws) {
		return nil, fmt.Errorf("%w: 请先停止项目再创建备份", ErrWorkspaceRunning)
	}
	progress(2, "准备备份 "+ws.Name)

	workspaceDir := m.paths.WorkspaceDir(ws.WorkspaceID)
	backupsDir := filepath.Join(workspaceDir, "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		return nil, err
	}
	tmpRoot := filepath.Join(workspaceDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return nil, err
	}
	backupID, err := m.newBackupID(backupsDir)
	if err != nil {
		return nil, err
	}
	finalPath := filepath.Join(backupsDir, backupID+archiveSuffix)
	tempPath := filepath.Join(backupsDir, "."+backupID+".tmp")

	manifest := &Manifest{
		FormatVersion:          FormatVersion,
		DevKitVersion:          version.Version,
		WorkspaceSchemaVersion: ws.WorkspaceSchemaVersion,
		WorkspaceID:            ws.WorkspaceID,
		WorkspaceName:          ws.Name,
		CreatedAt:              m.now().UTC(),
		Platform:               version.Get().OS + "/" + version.Get().Arch,
		IncludesSource:         opts.IncludeSource,
		IncludesDatabase:       false,
		SourceHeadCommit:       ws.Source.HeadCommit,
		DatabaseName:           ws.Database.DatabaseName,
		ProjectInitialized:     ws.Project.Initialized,
		Excluded:               append([]string(nil), sourceExcludes...),
	}

	var entries []archiveEntry

	metadataPath := filepath.Join(workspaceDir, "workspace.json")
	hash, size, err := hashFile(metadataPath)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(metadataPath)
	if err != nil {
		return nil, err
	}
	entries = append(entries, archiveEntry{
		Name:    "workspace/workspace.json",
		Host:    metadataPath,
		Size:    size,
		Mode:    info.Mode().Perm(),
		ModTime: info.ModTime(),
		SHA256:  hash,
	})

	roots := []scanRoot{
		{Host: filepath.Join(workspaceDir, "config"), Prefix: "workspace/config"},
		{Host: filepath.Join(workspaceDir, "data"), Prefix: "workspace/data"},
	}
	if opts.IncludeSource {
		sourceDir, sourceErr := m.sourceDir(ws)
		if sourceErr != nil {
			return nil, sourceErr
		}
		roots = append(roots, scanRoot{Host: sourceDir, Prefix: "source"})
	}
	progress(10, "扫描 Workspace 文件")
	scanned, partial, err := scanRoots(ctx, roots, func(percent int, message string) {
		progress(10+percent*35/100, message)
	})
	if err != nil {
		return nil, err
	}
	manifest.SkippedSymlinks = partial.SkippedSymlinks
	entries = append(entries, scanned...)

	var databaseEntry *archiveEntry
	if opts.IncludeDatabase {
		if !ws.Database.Provisioned {
			manifest.Warnings = append(manifest.Warnings, "数据库尚未创建，已跳过数据库备份")
		} else {
			progress(50, "导出 PostgreSQL 数据库")
			dumpPath := filepath.Join(tmpRoot, "db-"+backupID+".dump")
			_ = os.Remove(dumpPath)
			defer func() { _ = os.Remove(dumpPath) }()
			if err := m.services.DumpWorkspaceDatabase(ctx, ws, dumpPath); err != nil {
				return nil, fmt.Errorf("导出数据库失败: %w", err)
			}
			dumpHash, dumpSize, hashErr := hashFile(dumpPath)
			if hashErr != nil {
				_ = os.Remove(dumpPath)
				return nil, hashErr
			}
			databaseEntry = &archiveEntry{
				Name:    "database.dump",
				Host:    dumpPath,
				Size:    dumpSize,
				Mode:    0o600,
				ModTime: m.now().UTC(),
				SHA256:  dumpHash,
			}
			manifest.IncludesDatabase = true
		}
	}

	manifest.Files = buildManifestFiles(entries, databaseEntry)
	for _, file := range manifest.Files {
		manifest.TotalBytes += file.Size
	}
	manifest.FileCount = len(manifest.Files)

	writeEntries := entries
	if databaseEntry != nil {
		writeEntries = append(append([]archiveEntry(nil), entries...), *databaseEntry)
	}
	progress(55, "写入备份归档")
	if err := writeArchive(ctx, tempPath, manifest, writeEntries, func(percent int, message string) {
		progress(55+percent*35/100, message)
	}); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	if databaseEntry != nil {
		_ = os.Remove(databaseEntry.Host)
	}
	if err := secret.ProtectFile(tempPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("保护备份文件: %w", err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	if err := secret.ProtectFile(finalPath); err != nil {
		m.log.Warn("could not restrict backup permissions", "path", finalPath, "error", err)
	}
	progress(100, "备份完成: "+backupID)

	stat, err := os.Stat(finalPath)
	if err != nil {
		return nil, err
	}
	m.log.Info("workspace backup created", "workspace_id", ws.WorkspaceID,
		"backup_id", backupID, "size", stat.Size(), "source", opts.IncludeSource,
		"database", manifest.IncludesDatabase)
	return &Backup{
		ID:        backupID,
		FileName:  backupID + archiveSuffix,
		Path:      finalPath,
		SizeBytes: stat.Size(),
		CreatedAt: manifest.CreatedAt,
		Manifest:  manifest,
	}, nil
}

// List returns backups for a workspace, newest first.
func (m *Manager) List(ctx context.Context, ref string) ([]Backup, error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "backups")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []Backup{}, nil
		}
		return nil, err
	}
	result := make([]Backup, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), archiveSuffix) {
			continue
		}
		backupID := strings.TrimSuffix(entry.Name(), archiveSuffix)
		if !backupIDPattern.MatchString(backupID) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := entry.Info()
		if statErr != nil {
			continue
		}
		item := Backup{
			ID:        backupID,
			FileName:  entry.Name(),
			Path:      path,
			SizeBytes: info.Size(),
			CreatedAt: info.ModTime().UTC(),
		}
		if manifest, manifestErr := readManifest(path); manifestErr == nil {
			item.Manifest = manifest
			item.CreatedAt = manifest.CreatedAt
		} else {
			item.VerifyNote = manifestErr.Error()
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result, nil
}

// Get returns one backup, optionally verifying every file hash.
func (m *Manager) Get(ctx context.Context, ref, backupID string, verify bool) (*Backup, error) {
	ws, path, err := m.locate(ctx, ref, backupID)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item := &Backup{
		ID:        backupID,
		FileName:  backupID + archiveSuffix,
		Path:      path,
		SizeBytes: info.Size(),
		CreatedAt: info.ModTime().UTC(),
	}
	var manifest *Manifest
	if verify {
		manifest, err = verifyArchive(path)
	} else {
		manifest, err = readManifest(path)
	}
	if err != nil {
		return nil, err
	}
	if manifest.WorkspaceID != ws.WorkspaceID {
		return nil, ErrWorkspaceMismatch
	}
	item.Manifest = manifest
	item.CreatedAt = manifest.CreatedAt
	return item, nil
}

// Delete removes a backup archive.
func (m *Manager) Delete(ctx context.Context, ref, backupID string) error {
	ws, path, err := m.locate(ctx, ref, backupID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	m.log.Info("workspace backup deleted", "workspace_id", ws.WorkspaceID, "backup_id", backupID)
	return nil
}

// PreviewRestore verifies the archive and issues a one-time restore token.
func (m *Manager) PreviewRestore(ctx context.Context, ref, backupID string) (*RestorePreview, error) {
	ws, path, err := m.locate(ctx, ref, backupID)
	if err != nil {
		return nil, err
	}
	manifest, err := verifyArchive(path)
	if err != nil {
		return nil, err
	}
	if manifest.WorkspaceID != ws.WorkspaceID {
		return nil, ErrWorkspaceMismatch
	}
	workspaceDir := m.paths.WorkspaceDir(ws.WorkspaceID)
	actions := []RestoreAction{}
	var configBytes, dataBytes, sourceBytes int64
	for _, file := range manifest.Files {
		switch {
		case strings.HasPrefix(file.Path, "workspace/config/"):
			configBytes += file.Size
		case strings.HasPrefix(file.Path, "workspace/data/"):
			dataBytes += file.Size
		case strings.HasPrefix(file.Path, "source/"):
			sourceBytes += file.Size
		}
	}
	actions = append(actions, RestoreAction{
		Kind: "config", Path: filepath.Join(workspaceDir, "config"),
		SizeBytes: configBytes, Note: "Workspace 配置与环境变量（含数据库凭据）",
	})
	if dataBytes > 0 {
		actions = append(actions, RestoreAction{
			Kind: "data", Path: filepath.Join(workspaceDir, "data"),
			SizeBytes: dataBytes, Note: "上传文件与开发数据",
		})
	}
	if manifest.IncludesSource {
		sourceDir, sourceErr := m.sourceDir(ws)
		if sourceErr != nil {
			return nil, sourceErr
		}
		actions = append(actions, RestoreAction{
			Kind: "source", Path: sourceDir, SizeBytes: sourceBytes, Note: "项目源码",
		})
	}
	if manifest.IncludesDatabase {
		actions = append(actions, RestoreAction{
			Kind: "database", Path: ws.Database.DatabaseName,
			Note: "PostgreSQL 数据库内容（恢复前会先导出安全副本）",
		})
	}
	free, freeErr := m.freeSpace(workspaceDir)
	if freeErr != nil {
		free = 0
	}

	token, err := secret.RandomHex(24)
	if err != nil {
		return nil, err
	}
	expires := m.now().UTC().Add(confirmTTL)
	m.mu.Lock()
	m.purgeExpiredLocked()
	m.tokens[token] = restoreToken{
		workspaceID: ws.WorkspaceID,
		backupID:    backupID,
		expiresAt:   expires,
	}
	m.mu.Unlock()

	preview := &RestorePreview{
		BackupID:         backupID,
		WorkspaceID:      ws.WorkspaceID,
		Manifest:         *manifest,
		Running:          workspaceActive(ws),
		WillReplace:      actions,
		RestoreDatabase:  manifest.IncludesDatabase,
		RequiredBytes:    manifest.TotalBytes,
		FreeBytes:        free,
		UncompressedSize: manifest.TotalBytes,
		ConfirmToken:     token,
		ExpiresAt:        expires,
	}
	if free > 0 && uint64(manifest.TotalBytes) > free {
		preview.Warnings = append(preview.Warnings, "可用磁盘空间小于备份解压后的大小")
	}
	if preview.Running {
		preview.Warnings = append(preview.Warnings, "项目正在运行，执行恢复前必须停止")
	}
	return preview, nil
}

// Restore replaces workspace data with the archive contents. The supplied
// token is consumed even if the restore subsequently fails.
func (m *Manager) Restore(ctx context.Context, ref, backupID, token string, progress func(int, string)) (*RestoreResult, error) {
	if progress == nil {
		progress = func(int, string) {}
	}
	ws, path, err := m.locate(ctx, ref, backupID)
	if err != nil {
		return nil, err
	}
	unlock := m.lock(ws.WorkspaceID)
	defer unlock()
	ws, err = m.workspaces.Get(ctx, ws.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if workspaceActive(ws) {
		return nil, fmt.Errorf("%w: 请先停止项目再恢复备份", ErrWorkspaceRunning)
	}
	if err := m.consumeToken(token, ws.WorkspaceID, backupID); err != nil {
		return nil, err
	}

	workspaceDir := m.paths.WorkspaceDir(ws.WorkspaceID)
	free, freeErr := m.freeSpace(workspaceDir)
	if freeErr == nil {
		manifest, manifestErr := readManifest(path)
		if manifestErr != nil {
			return nil, manifestErr
		}
		required := uint64(manifest.TotalBytes) + 16<<20
		if free < required {
			return nil, fmt.Errorf("%w: 需要约 %d MB，可用 %d MB",
				ErrInsufficientSpace, required>>20, free>>20)
		}
	}

	tmpRoot := filepath.Join(workspaceDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o755); err != nil {
		return nil, err
	}
	extractDir := filepath.Join(tmpRoot, "restore-"+id.MustNew())
	rollbackDir := filepath.Join(tmpRoot, "rollback-"+id.MustNew())
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return nil, err
	}
	cleanupExtract := true
	defer func() {
		if cleanupExtract {
			_ = os.RemoveAll(extractDir)
		}
	}()

	progress(5, "校验并解压备份")
	extracted, err := extractArchive(ctx, path, extractDir, progress)
	if err != nil {
		return nil, err
	}
	manifest, err := readManifest(path)
	if err != nil {
		return nil, err
	}
	if manifest.WorkspaceID != ws.WorkspaceID {
		return nil, ErrWorkspaceMismatch
	}

	var safetyDump string
	if extracted.HasDatabase {
		if !ws.Database.Provisioned {
			progress(15, "创建 Workspace 数据库")
			if _, err := m.workspaces.Provision(ctx, ws.WorkspaceID); err != nil {
				return nil, fmt.Errorf("创建数据库失败: %w", err)
			}
			refreshed, getErr := m.workspaces.Get(ctx, ws.WorkspaceID)
			if getErr != nil {
				return nil, getErr
			}
			ws = refreshed
		}
		safetyDump = filepath.Join(extractDir, "safety.dump")
		if dumpErr := m.services.DumpWorkspaceDatabase(ctx, ws, safetyDump); dumpErr != nil {
			return nil, fmt.Errorf("恢复前导出数据库失败，未做任何修改: %w", dumpErr)
		}
	}

	progress(70, "替换 Workspace 数据")
	var swapped []swappedSection
	swap := func(section, target, source string) error {
		item, swapErr := swapSection(section, target, source, rollbackDir)
		if swapErr != nil {
			return swapErr
		}
		swapped = append(swapped, item)
		return nil
	}
	rollback := func() string {
		notes := rollbackSections(swapped)
		if extracted.HasDatabase && safetyDump != "" {
			if restoreErr := m.services.RestoreWorkspaceDatabase(context.WithoutCancel(ctx), ws, safetyDump); restoreErr != nil {
				notes = append(notes, "安全数据库副本恢复失败: "+restoreErr.Error())
			}
		}
		if len(notes) == 0 {
			_ = os.RemoveAll(rollbackDir)
			return ""
		}
		return strings.Join(notes, "；")
	}

	if extracted.HasConfig {
		if err := swap("config", filepath.Join(workspaceDir, "config"), filepath.Join(extractDir, "config")); err != nil {
			return nil, m.restoreFailed(ctx, ws, err, rollback, &cleanupExtract, extractDir)
		}
	}
	if extracted.HasData {
		if err := swap("data", filepath.Join(workspaceDir, "data"), filepath.Join(extractDir, "data")); err != nil {
			return nil, m.restoreFailed(ctx, ws, err, rollback, &cleanupExtract, extractDir)
		}
	}
	if extracted.HasSource {
		sourceDir, sourceErr := m.sourceDir(ws)
		if sourceErr != nil {
			return nil, m.restoreFailed(ctx, ws, sourceErr, rollback, &cleanupExtract, extractDir)
		}
		if err := swap("source", sourceDir, filepath.Join(extractDir, "source")); err != nil {
			return nil, m.restoreFailed(ctx, ws, err, rollback, &cleanupExtract, extractDir)
		}
	}

	if extracted.HasDatabase {
		progress(85, "恢复 PostgreSQL 数据库")
		if err := m.services.RestoreWorkspaceDatabase(ctx, ws, filepath.Join(extractDir, "database.dump")); err != nil {
			return nil, m.restoreFailed(ctx, ws, fmt.Errorf("恢复数据库失败: %w", err), rollback, &cleanupExtract, extractDir)
		}
	}

	cleanupExtract = false
	_ = os.RemoveAll(extractDir)
	_ = os.RemoveAll(rollbackDir)

	restored := make([]string, 0, 4)
	if extracted.HasConfig {
		restored = append(restored, filepath.Join(workspaceDir, "config"))
	}
	if extracted.HasData {
		restored = append(restored, filepath.Join(workspaceDir, "data"))
	}
	if extracted.HasSource {
		if sourceDir, sourceErr := m.sourceDir(ws); sourceErr == nil {
			restored = append(restored, sourceDir)
		}
	}
	if extracted.HasDatabase {
		restored = append(restored, ws.Database.DatabaseName)
	}

	state := workspace.StateReady
	initialized := ws.Project.Initialized
	if extracted.HasSource {
		state = workspace.StateReady
		initialized = initialized || manifest.ProjectInitialized
	} else if !ws.Source.Installed {
		state = workspace.StateEmpty
	}
	now := m.now().UTC()
	if _, err := m.workspaces.Mutate(context.WithoutCancel(ctx), ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.State = state
		current.Project.ProcessState = "STOPPED"
		current.Project.PID = 0
		current.Project.Initialized = initialized
		if initialized && current.Project.InitializedAt == nil {
			current.Project.InitializedAt = &now
		}
		if extracted.HasSource {
			current.Source.Installed = true
		}
		current.LastError = ""
		return nil
	}); err != nil {
		return nil, err
	}

	progress(100, "恢复完成: "+backupID)
	m.log.Info("workspace backup restored", "workspace_id", ws.WorkspaceID,
		"backup_id", backupID, "database", extracted.HasDatabase)
	return &RestoreResult{
		BackupID:         backupID,
		WorkspaceID:      ws.WorkspaceID,
		Restored:         restored,
		DatabaseRestored: extracted.HasDatabase,
	}, nil
}

func (m *Manager) restoreFailed(ctx context.Context, ws *workspace.Workspace, cause error,
	rollback func() string, cleanupExtract *bool, extractDir string) error {
	notes := rollback()
	*cleanupExtract = false
	if _, err := m.workspaces.Mutate(context.WithoutCancel(ctx), ws.WorkspaceID, func(current *workspace.Workspace) error {
		current.State = workspace.StateError
		current.LastError = truncateMessage(cause.Error(), 500)
		current.Project.ProcessState = "STOPPED"
		current.Project.PID = 0
		return nil
	}); err != nil {
		m.log.Warn("could not record restore failure", "workspace_id", ws.WorkspaceID, "error", err)
	}
	if notes != "" {
		return fmt.Errorf("%w（回滚未完全成功：%s；临时目录已保留：%s）", cause, notes, extractDir)
	}
	return fmt.Errorf("%w（已回滚到恢复前状态）", cause)
}

func (m *Manager) locate(ctx context.Context, ref, backupID string) (*workspace.Workspace, string, error) {
	if !backupIDPattern.MatchString(backupID) {
		return nil, "", ErrNotFound
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, "", err
	}
	path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "backups", backupID+archiveSuffix)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	return ws, path, nil
}

func (m *Manager) sourceDir(ws *workspace.Workspace) (string, error) {
	value := ws.Source.Path
	if value == "" {
		value = "source"
	}
	if !filepath.IsLocal(value) {
		return "", fmt.Errorf("%w: 源码路径必须是 Workspace 内的相对路径", ErrValidation)
	}
	return filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), value), nil
}

func (m *Manager) newBackupID(dir string) (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		backupID := m.now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(random[:])
		path := filepath.Join(dir, backupID+archiveSuffix)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return backupID, nil
		}
		time.Sleep(1100 * time.Millisecond)
	}
	return "", fmt.Errorf("生成备份 ID 失败")
}

func (m *Manager) lock(workspaceID string) func() {
	value, _ := m.locks.LoadOrStore(workspaceID, &sync.Mutex{})
	mu := value.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *Manager) consumeToken(token, workspaceID, backupID string) error {
	if token == "" {
		return ErrConfirmToken
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purgeExpiredLocked()
	stored, ok := m.tokens[token]
	if !ok || stored.workspaceID != workspaceID || stored.backupID != backupID ||
		m.now().UTC().After(stored.expiresAt) {
		return ErrConfirmToken
	}
	delete(m.tokens, token)
	return nil
}

func (m *Manager) purgeExpiredLocked() {
	now := m.now().UTC()
	for token, value := range m.tokens {
		if now.After(value.expiresAt) {
			delete(m.tokens, token)
		}
	}
}

func workspaceActive(ws *workspace.Workspace) bool {
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

type swappedSection struct {
	Section string
	Target  string
	Saved   string
}

// swapSection moves the current target aside and installs the restored copy.
func swapSection(section, target, source, rollbackDir string) (swappedSection, error) {
	if err := os.MkdirAll(rollbackDir, 0o700); err != nil {
		return swappedSection{}, err
	}
	item := swappedSection{
		Section: section,
		Target:  target,
		Saved:   filepath.Join(rollbackDir, section),
	}
	if _, err := os.Stat(target); err == nil {
		if err := os.Rename(target, item.Saved); err != nil {
			return swappedSection{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return swappedSection{}, err
	} else {
		item.Saved = ""
	}
	if err := os.Rename(source, target); err != nil {
		if item.Saved != "" {
			_ = os.Rename(item.Saved, target)
		}
		return swappedSection{}, err
	}
	return item, nil
}

func rollbackSections(sections []swappedSection) []string {
	var notes []string
	for index := len(sections) - 1; index >= 0; index-- {
		item := sections[index]
		if err := os.RemoveAll(item.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
			notes = append(notes, fmt.Sprintf("%s: %v", item.Section, err))
			continue
		}
		if item.Saved != "" {
			if err := os.Rename(item.Saved, item.Target); err != nil {
				notes = append(notes, fmt.Sprintf("%s: %v", item.Section, err))
			}
		}
	}
	return notes
}

func truncateMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
