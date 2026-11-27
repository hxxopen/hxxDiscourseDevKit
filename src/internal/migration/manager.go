package migration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// RuntimeHost supplies the WSL status and default data root.
type RuntimeHost interface {
	WSLStatus(ctx context.Context) runtime.WSLStatus
	DefaultDataRoot(ctx context.Context) (string, error)
}

// WorkspaceStore lists workspace metadata.
type WorkspaceStore interface {
	List(ctx context.Context) ([]*workspace.Workspace, error)
}

// ProjectController stops supervised workspace processes.
type ProjectController interface {
	Stop(ctx context.Context, ref string, progress func(int, string)) error
	LiveProcesses() []string
}

// ServiceController stops managed services.
type ServiceController interface {
	List(ctx context.Context) []service.Status
	Stop(ctx context.Context, name service.Name, progress func(int, string)) error
}

// TaskGuard reports whether another background task is active.
type TaskGuard interface {
	Active() bool
}

// Copier performs filesystem operations inside the target WSL distribution.
type Copier interface {
	EnsureDir(ctx context.Context, hostPath string, mode os.FileMode) error
	CopyTree(ctx context.Context, srcHost, dstHost string) error
	Move(ctx context.Context, srcHost, dstHost string) error
	RemoveAll(ctx context.Context, hostPath string) error
	FreeBytes(ctx context.Context, hostPath string) (int64, error)
}

// Runnable is one reserved migration. Begin returns it with the maintenance
// flag already set; the API submits Execute as a background task.
type Runnable interface {
	Execute(ctx context.Context, progress func(int, string)) (*Result, error)
	Abort()
}

// Options wires the migration manager.
type Options struct {
	Paths      *config.Paths
	Config     *config.Store
	Runtime    RuntimeHost
	Workspaces WorkspaceStore
	Projects   ProjectController
	Services   ServiceController
	Tasks      TaskGuard
	Logger     *slog.Logger
	// Windows must reflect the host platform. Tests set it explicitly.
	Windows bool
	Now     func() time.Time
	// NewCopier creates the WSL filesystem copier for a distribution.
	NewCopier func(distro string) Copier
}

type tokenEntry struct {
	digest    string
	expiresAt time.Time
}

// Manager issues migration previews and reservations.
type Manager struct {
	paths      *config.Paths
	cfg        *config.Store
	runtime    RuntimeHost
	workspaces WorkspaceStore
	projects   ProjectController
	services   ServiceController
	tasks      TaskGuard
	log        *slog.Logger
	now        func() time.Time
	windows    bool
	newCopier  func(string) Copier

	mu        sync.Mutex
	tokens    map[string]tokenEntry
	migrating bool
	taskID    string
	runID     string
}

// New creates a migration manager.
func New(opts Options) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Manager{
		paths:      opts.Paths,
		cfg:        opts.Config,
		runtime:    opts.Runtime,
		workspaces: opts.Workspaces,
		projects:   opts.Projects,
		services:   opts.Services,
		tasks:      opts.Tasks,
		log:        opts.Logger,
		now:        opts.Now,
		windows:    opts.Windows,
		newCopier:  opts.NewCopier,
		tokens:     map[string]tokenEntry{},
	}
}

// Preview builds a migration plan and, when it is actionable, issues a
// single-use confirmation token.
func (m *Manager) Preview(ctx context.Context) (*Plan, error) {
	plan, err := m.plan(ctx)
	if err != nil {
		return nil, err
	}
	if !plan.Eligible || len(plan.Blockers) > 0 {
		return plan, nil
	}
	token, err := secret.RandomHex(24)
	if err != nil {
		return nil, fmt.Errorf("生成迁移确认令牌: %w", err)
	}
	expires := m.now().UTC().Add(tokenTTL)
	m.mu.Lock()
	m.pruneTokensLocked()
	m.tokens[token] = tokenEntry{digest: planDigest(plan), expiresAt: expires}
	m.mu.Unlock()
	plan.ConfirmToken = token
	plan.ExpiresAt = expires
	return plan, nil
}

// Begin validates a preview token, recomputes the plan, and reserves the
// maintenance window before returning. The caller must Abort when the task is
// never submitted.
func (m *Manager) Begin(ctx context.Context, confirmToken string) (Runnable, error) {
	confirmToken = strings.TrimSpace(confirmToken)
	if confirmToken == "" {
		return nil, ErrConfirmToken
	}

	m.mu.Lock()
	if m.migrating {
		m.mu.Unlock()
		return nil, ErrMigrating
	}
	entry, ok := m.tokens[confirmToken]
	if !ok || !m.now().UTC().Before(entry.expiresAt) {
		delete(m.tokens, confirmToken)
		m.mu.Unlock()
		return nil, ErrConfirmToken
	}
	// Reserve the maintenance window while revalidating. The token is only
	// consumed once the plan digest matches.
	m.migrating = true
	m.runID = id.MustNew()
	m.taskID = ""
	runID := m.runID
	m.mu.Unlock()

	fail := func(err error) (Runnable, error) {
		m.finishRun(runID)
		return nil, err
	}

	if m.tasks != nil && m.tasks.Active() {
		return fail(ErrBusy)
	}
	plan, err := m.plan(ctx)
	if err != nil {
		return fail(err)
	}
	if !plan.Eligible {
		reason := strings.TrimSpace(plan.Reason)
		if reason == "" {
			reason = "迁移条件不满足"
		}
		return fail(fmt.Errorf("%w: %s", ErrNotEligible, reason))
	}
	if len(plan.Blockers) > 0 {
		return fail(fmt.Errorf("%w: %s", ErrBlocked, plan.Blockers[0].Message))
	}
	if planDigest(plan) != entry.digest {
		return fail(ErrPlanStale)
	}

	m.mu.Lock()
	current, ok := m.tokens[confirmToken]
	if !ok || current.digest != entry.digest || m.runID != runID {
		m.mu.Unlock()
		m.finishRun(runID)
		return nil, ErrConfirmToken
	}
	delete(m.tokens, confirmToken)
	m.mu.Unlock()

	return &run{manager: m, plan: plan, runID: runID}, nil
}

// Migrating reports whether a migration reservation is active.
func (m *Manager) Migrating() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.migrating
}

// SetTaskID records the API task ID after submission.
func (m *Manager) SetTaskID(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.migrating {
		m.taskID = strings.TrimSpace(taskID)
	}
}

// CurrentTaskID returns the active migration task ID, if any.
func (m *Manager) CurrentTaskID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.migrating {
		return ""
	}
	return m.taskID
}

func (m *Manager) finishRun(runID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runID != runID {
		return
	}
	m.migrating = false
	m.taskID = ""
	m.runID = ""
}

func (m *Manager) pruneTokensLocked() {
	now := m.now().UTC()
	for token, entry := range m.tokens {
		if !now.Before(entry.expiresAt) {
			delete(m.tokens, token)
		}
	}
}
