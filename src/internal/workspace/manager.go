package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

const workspaceSchemaVersion = 1

// Options tunes workspace allocation.
type Options struct {
	PortStart int
	BindHost  string
}

type deleteToken struct {
	workspaceID string
	expiresAt   time.Time
}

// Manager owns workspace metadata and allocation.
type Manager struct {
	paths   *config.Paths
	cfg     *config.Store
	secrets *secret.Store
	log     *slog.Logger
	opts    Options

	mu          sync.RWMutex
	svcMu       sync.RWMutex
	provisioner ServiceProvisioner
	tokens      map[string]deleteToken
	now         func() time.Time
}

// New creates a workspace manager.
func New(paths *config.Paths, cfg *config.Store, secrets *secret.Store, log *slog.Logger, opts Options) *Manager {
	if opts.PortStart <= 0 {
		opts.PortStart = 3000
	}
	if opts.BindHost == "" {
		opts.BindHost = "127.0.0.1"
	}
	return &Manager{
		paths:   paths,
		cfg:     cfg,
		secrets: secrets,
		log:     log,
		opts:    opts,
		tokens:  map[string]deleteToken{},
		now:     time.Now,
	}
}

// EnsureDefault creates the Default workspace when no workspace exists.
// It never recreates Default after a user has deleted it.
func (m *Manager) EnsureDefault(ctx context.Context) (*Workspace, bool, error) {
	ws, created, err := m.ensureDefaultLocked(ctx)
	if err != nil || !created {
		return ws, created, err
	}
	m.provisionAfterCreate(ctx, ws)
	if updated, getErr := m.Get(ctx, ws.WorkspaceID); getErr == nil {
		ws = updated
	}
	return ws, true, nil
}

func (m *Manager) ensureDefaultLocked(ctx context.Context) (*Workspace, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cfg.Get().WorkspacesInitialized {
		return nil, false, nil
	}
	existing, err := m.listLocked()
	if err != nil {
		return nil, false, err
	}
	if len(existing) > 0 {
		if err := m.cfg.Update(func(c *config.Config) error {
			c.WorkspacesInitialized = true
			return nil
		}); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}
	ws, err := m.createLocked(ctx, CreateRequest{
		Name:        "Default",
		Description: "Default Discourse development workspace",
	})
	if err != nil {
		return nil, false, err
	}
	if err := m.cfg.Update(func(c *config.Config) error {
		c.WorkspacesInitialized = true
		return nil
	}); err != nil {
		m.log.Warn("could not record workspace initialization", "error", err)
	}
	return ws, true, nil
}

// Create adds a new workspace and provisions its isolated database when the
// shared PostgreSQL service is available. Provisioning failure is recorded on
// the workspace instead of failing creation.
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Workspace, error) {
	m.mu.Lock()
	ws, err := m.createLocked(ctx, req)
	m.mu.Unlock()
	if err != nil {
		return nil, err
	}
	m.provisionAfterCreate(ctx, ws)
	if updated, getErr := m.Get(ctx, ws.WorkspaceID); getErr == nil {
		ws = updated
	}
	return ws, nil
}

func (m *Manager) provisionAfterCreate(ctx context.Context, ws *Workspace) {
	if err := m.provisionWorkspace(ctx, ws); err != nil {
		m.log.Warn("workspace created without a database",
			"workspace_id", ws.WorkspaceID, "error", err)
	}
}

// List returns all valid workspaces sorted by creation time.
func (m *Manager) List(ctx context.Context) ([]*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.listLocked()
}

// Get finds a workspace by ID or unique display name.
func (m *Manager) Get(ctx context.Context, ref string) (*Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.getLocked(ref)
}

// Update changes mutable workspace metadata.
func (m *Manager) Update(ctx context.Context, ref string, req UpdateRequest) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ws, err := m.getLocked(ref)
	if err != nil {
		return nil, err
	}
	existing, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	changed := false

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		for _, other := range existing {
			if other.WorkspaceID != ws.WorkspaceID && strings.EqualFold(other.Name, name) {
				return nil, fmt.Errorf("%w: %q", ErrNameConflict, name)
			}
		}
		if name != ws.Name {
			ws.Name = name
			changed = true
		}
	}
	if req.Description != nil {
		desc := strings.TrimSpace(*req.Description)
		if utf8.RuneCountInString(desc) > 500 {
			return nil, fmt.Errorf("%w: description must be at most 500 characters", ErrValidation)
		}
		if desc != ws.Description {
			ws.Description = desc
			changed = true
		}
	}
	if changed {
		ws.UpdatedAt = m.now().UTC()
		if err := m.writeWorkspace(ws); err != nil {
			return nil, err
		}
		m.log.Info("workspace updated", "workspace_id", ws.WorkspaceID, "name", ws.Name)
	}
	return ws, nil
}

// Mutate loads a workspace, applies fn, and persists the result atomically.
// fn runs while the manager lock is held and must not call Manager methods.
func (m *Manager) Mutate(ctx context.Context, ref string, fn func(*Workspace) error) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ws, err := m.getLocked(ref)
	if err != nil {
		return nil, err
	}
	if err := fn(ws); err != nil {
		return nil, err
	}
	ws.UpdatedAt = m.now().UTC()
	if err := m.writeWorkspace(ws); err != nil {
		return nil, err
	}
	return ws, nil
}

// SetActive makes a workspace the active one.
func (m *Manager) SetActive(ctx context.Context, ref string) (*Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ws, err := m.getLocked(ref)
	if err != nil {
		return nil, err
	}
	if err := m.cfg.Update(func(c *config.Config) error {
		c.ActiveWorkspaceID = ws.WorkspaceID
		return nil
	}); err != nil {
		return nil, fmt.Errorf("set active workspace: %w", err)
	}
	m.log.Info("active workspace changed", "workspace_id", ws.WorkspaceID, "name", ws.Name)
	return ws, nil
}

// Status returns a live status view for a workspace.
func (m *Manager) Status(ctx context.Context, ref string) (*Status, error) {
	ws, err := m.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	dir := m.paths.WorkspaceDir(ws.WorkspaceID)
	sourcePath := ws.Source.Path
	if sourcePath == "" {
		sourcePath = "source"
	}
	checks := []Check{
		pathCheck("metadata", filepath.Join(dir, "workspace.json")),
		pathCheck("source", filepath.Join(dir, sourcePath)),
		pathCheck("config", filepath.Join(dir, "config")),
		pathCheck("data", filepath.Join(dir, "data")),
	}
	dbCheck := Check{Name: "database", OK: ws.Database.Provisioned}
	if !dbCheck.OK {
		dbCheck.Message = "数据库尚未创建"
		if ws.Database.ProvisionError != "" {
			dbCheck.Message = ws.Database.ProvisionError
		}
	}
	checks = append(checks, dbCheck)
	running := ws.State == StateRunning || ws.Project.ProcessState == "RUNNING"
	return &Status{
		WorkspaceID: ws.WorkspaceID,
		State:       ws.State,
		Running:     running,
		Checks:      checks,
	}, nil
}

// Reconcile repairs inconsistent persisted state after a crash.
func (m *Manager) Reconcile(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	list, err := m.listLocked()
	if err != nil {
		return err
	}
	for _, ws := range list {
		dir := m.paths.WorkspaceDir(ws.WorkspaceID)
		if ws.Source.Installed {
			sourcePath := ws.Source.Path
			if sourcePath == "" {
				sourcePath = "source"
			}
			if _, err := os.Stat(filepath.Join(dir, sourcePath)); err != nil {
				ws.State = StateError
				ws.LastError = "source directory is missing"
				ws.UpdatedAt = m.now().UTC()
				if err := m.writeWorkspace(ws); err != nil {
					return err
				}
				continue
			}
		}
		if ws.State == StateRunning || ws.State == StateStarting {
			ws.State = StateError
			ws.LastError = "process state was not recovered after restart"
			ws.Project.ProcessState = "STOPPED"
			ws.Project.PID = 0
			ws.UpdatedAt = m.now().UTC()
			if err := m.writeWorkspace(ws); err != nil {
				return err
			}
		}
	}
	return nil
}

// --- internals --------------------------------------------------------------

func (m *Manager) createLocked(ctx context.Context, req CreateRequest) (*Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Name)
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	description := strings.TrimSpace(req.Description)
	if utf8.RuneCountInString(description) > 500 {
		return nil, fmt.Errorf("%w: description must be at most 500 characters", ErrValidation)
	}
	projectType := strings.TrimSpace(req.ProjectType)
	if projectType == "" {
		projectType = "discourse"
	}

	existing, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	for _, ws := range existing {
		if strings.EqualFold(ws.Name, name) {
			return nil, fmt.Errorf("%w: %q", ErrNameConflict, name)
		}
	}

	port, err := m.allocatePort(ctx, existing)
	if err != nil {
		return nil, err
	}
	redisIndex, err := allocateRedisDB(existing)
	if err != nil {
		return nil, err
	}

	workspaceID, err := id.New()
	if err != nil {
		return nil, fmt.Errorf("generate workspace id: %w", err)
	}
	dbName := databaseName(name, workspaceID)
	password, err := secret.RandomPassword(24)
	if err != nil {
		return nil, err
	}
	dir := m.paths.WorkspaceDir(workspaceID)
	for _, sub := range []string{"source", "config", "logs", "data", "backups", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			_ = os.RemoveAll(dir)
			return nil, fmt.Errorf("create workspace directory: %w", err)
		}
	}

	now := m.now().UTC()
	ws := &Workspace{
		WorkspaceSchemaVersion: workspaceSchemaVersion,
		WorkspaceID:            workspaceID,
		Name:                   name,
		Description:            description,
		ProjectType:            projectType,
		State:                  StateEmpty,
		CreatedAt:              now,
		UpdatedAt:              now,
		Source: SourceConfig{
			Path:          "source",
			RepositoryURL: m.cfg.Get().Project.DefaultRepositoryURL,
			Branch:        m.cfg.Get().Project.DefaultBranch,
		},
		RuntimeProfile: m.cfg.Get().Runtime.DefaultProfile,
		Database: DatabaseConfig{
			Mode:         "managed",
			DatabaseName: dbName,
			Username:     dbName,
			PasswordRef:  "ws:" + workspaceID + ":db",
		},
		Redis: RedisConfig{
			Mode:      "managed",
			DBIndex:   redisIndex,
			KeyPrefix: "hxxddk:" + workspaceID + ":",
		},
		Network: NetworkConfig{
			Host:     m.opts.BindHost,
			HTTPPort: port,
		},
		Project: ProjectConfig{
			ProcessState: "STOPPED",
		},
	}

	if err := m.writeWorkspace(ws); err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	if err := m.secrets.Set(ws.Database.PasswordRef, password); err != nil {
		_ = os.RemoveAll(dir)
		_ = m.secrets.Delete(ws.Database.PasswordRef)
		return nil, fmt.Errorf("store database credential: %w", err)
	}

	wasFirst := len(existing) == 0
	if wasFirst {
		if err := m.cfg.Update(func(c *config.Config) error {
			c.ActiveWorkspaceID = ws.WorkspaceID
			return nil
		}); err != nil {
			m.log.Warn("could not set first workspace active", "error", err)
		}
	}

	m.log.Info("workspace created",
		"workspace_id", ws.WorkspaceID,
		"name", ws.Name,
		"port", ws.Network.HTTPPort,
		"database", ws.Database.DatabaseName,
	)
	return ws, nil
}

func (m *Manager) listLocked() ([]*Workspace, error) {
	entries, err := os.ReadDir(m.paths.WorkspacesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workspaces directory: %w", err)
	}
	list := make([]*Workspace, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ws, err := m.readWorkspace(entry.Name())
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				m.log.Warn("skipping unreadable workspace", "dir", entry.Name(), "error", err)
			}
			continue
		}
		list = append(list, ws)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].CreatedAt.Equal(list[j].CreatedAt) {
			return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
		}
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	return list, nil
}

func (m *Manager) getLocked(ref string) (*Workspace, error) {
	list, err := m.listLocked()
	if err != nil {
		return nil, err
	}
	for _, ws := range list {
		if ws.WorkspaceID == ref {
			return ws, nil
		}
	}
	for _, ws := range list {
		if strings.EqualFold(ws.Name, ref) {
			return ws, nil
		}
	}
	return nil, ErrNotFound
}

func (m *Manager) readWorkspace(workspaceID string) (*Workspace, error) {
	var ws Workspace
	if err := store.ReadJSON(filepath.Join(m.paths.WorkspaceDir(workspaceID), "workspace.json"), &ws); err != nil {
		return nil, err
	}
	if ws.WorkspaceSchemaVersion != workspaceSchemaVersion {
		return nil, fmt.Errorf("unsupported workspace schema_version %d", ws.WorkspaceSchemaVersion)
	}
	if ws.WorkspaceID != workspaceID || !id.Valid(ws.WorkspaceID) {
		return nil, fmt.Errorf("workspace id mismatch or invalid")
	}
	if err := ValidateName(ws.Name); err != nil {
		return nil, fmt.Errorf("invalid persisted name: %w", err)
	}
	if !ws.State.Valid() {
		return nil, fmt.Errorf("invalid persisted state %q", ws.State)
	}
	if ws.Source.Path == "" {
		ws.Source.Path = "source"
	}
	if ws.Project.ProcessState == "" {
		ws.Project.ProcessState = "STOPPED"
	}
	return &ws, nil
}

func (m *Manager) writeWorkspace(ws *Workspace) error {
	path := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "workspace.json")
	if err := store.WriteJSON(path, ws, 0o600); err != nil {
		return fmt.Errorf("persist workspace: %w", err)
	}
	return nil
}

func (m *Manager) allocatePort(ctx context.Context, existing []*Workspace) (int, error) {
	used := make(map[int]bool, len(existing))
	for _, ws := range existing {
		used[ws.Network.HTTPPort] = true
	}
	for port := m.opts.PortStart; port < m.opts.PortStart+1000; port++ {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if used[port] {
			continue
		}
		addr := net.JoinHostPort(m.opts.BindHost, strconv.Itoa(port))
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, ErrNoPort
}

func allocateRedisDB(existing []*Workspace) (int, error) {
	used := make(map[int]bool)
	for _, ws := range existing {
		if ws.Redis.Mode == "managed" {
			used[ws.Redis.DBIndex] = true
		}
	}
	for index := 1; index <= 15; index++ {
		if !used[index] {
			return index, nil
		}
	}
	return 0, ErrNoRedisDB
}

// ValidateName checks a workspace display name.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: name is required", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 64 {
		return fmt.Errorf("%w: name must be at most 64 characters", ErrValidation)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: reserved name", ErrInvalidName)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: control characters are not allowed", ErrInvalidName)
		}
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return fmt.Errorf("%w: character %q is not allowed", ErrInvalidName, r)
		}
	}
	return nil
}

func databaseName(name, workspaceID string) string {
	short := strings.ToLower(workspaceID)
	if len(short) > 8 {
		short = short[:8]
	}
	base := fmt.Sprintf("hxxddk_%s_%s", short, slugify(name))
	if len(base) > 63 {
		base = base[:63]
	}
	return strings.TrimRight(base, "_")
}

func slugify(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		slug = "ws"
	}
	if len(slug) > 24 {
		slug = strings.TrimRight(slug[:24], "_")
		if slug == "" {
			slug = "ws"
		}
	}
	return slug
}

func pathCheck(name, path string) Check {
	_, err := os.Stat(path)
	if err == nil {
		return Check{Name: name, OK: true}
	}
	return Check{Name: name, OK: false, Message: "path is missing: " + path}
}
