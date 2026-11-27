package workspace

import (
	"context"
	"fmt"
	"strings"
)

// ServiceProvisioner applies a workspace's database and Redis resources to
// the shared services. Implementations must be idempotent.
type ServiceProvisioner interface {
	EnsureWorkspaceDatabase(ctx context.Context, ws *Workspace) error
	DropWorkspaceDatabase(ctx context.Context, ws *Workspace) error
	ClearWorkspaceRedis(ctx context.Context, ws *Workspace) error
}

// SetProvisioner wires the shared service manager.
func (m *Manager) SetProvisioner(provisioner ServiceProvisioner) {
	m.svcMu.Lock()
	defer m.svcMu.Unlock()
	m.provisioner = provisioner
}

// Provision retries database provisioning for an existing workspace.
func (m *Manager) Provision(ctx context.Context, ref string) (*Workspace, error) {
	ws, err := m.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	provisionErr := m.provisionWorkspace(ctx, ws)
	if updated, getErr := m.Get(ctx, ws.WorkspaceID); getErr == nil {
		ws = updated
	}
	return ws, provisionErr
}

// MarkDatabaseResult records the outcome of a provisioning attempt. It is
// called by the service manager when PostgreSQL starts and reconciles every
// workspace.
func (m *Manager) MarkDatabaseResult(ctx context.Context, workspaceID string, provisioned bool, message string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	ws, err := m.getLocked(workspaceID)
	if err != nil {
		return err
	}
	m.applyDatabaseResult(ws, provisioned, message)
	return m.writeWorkspace(ws)
}

// RunningNames returns display names of workspaces whose development process
// is active. Used to block service shutdown while dependencies exist.
func (m *Manager) RunningNames(ctx context.Context) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil
	}
	list, err := m.listLocked()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(list))
	for _, ws := range list {
		if isRunning(ws) {
			names = append(names, ws.Name)
		}
	}
	return names
}

func (m *Manager) provisionWorkspace(ctx context.Context, ws *Workspace) error {
	m.svcMu.RLock()
	provisioner := m.provisioner
	m.svcMu.RUnlock()
	if provisioner == nil {
		return nil
	}
	err := provisioner.EnsureWorkspaceDatabase(ctx, ws)
	message := ""
	if err != nil {
		message = err.Error()
		m.log.Warn("workspace database provisioning failed",
			"workspace_id", ws.WorkspaceID, "error", err)
	}
	if markErr := m.MarkDatabaseResult(ctx, ws.WorkspaceID, err == nil, message); markErr != nil {
		m.log.Warn("could not record database provisioning result",
			"workspace_id", ws.WorkspaceID, "error", markErr)
	}
	if err != nil {
		return fmt.Errorf("provision workspace database: %w", err)
	}
	return nil
}

func (m *Manager) applyDatabaseResult(ws *Workspace, provisioned bool, message string) {
	ws.Database.Provisioned = provisioned
	ws.Database.ProvisionError = truncateMessage(message, 500)
	if provisioned {
		now := m.now().UTC()
		ws.Database.ProvisionedAt = &now
	} else {
		ws.Database.ProvisionedAt = nil
	}
	ws.UpdatedAt = m.now().UTC()
}

func truncateMessage(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}
