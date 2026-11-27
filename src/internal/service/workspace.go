package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// WorkspaceConnection is the effective endpoint and credentials a workspace
// process must use. It is never returned by the HTTP API.
type WorkspaceConnection struct {
	PostgresHost     string `json:"postgres_host"`
	PostgresPort     int    `json:"postgres_port"`
	PostgresDatabase string `json:"postgres_database"`
	PostgresUsername string `json:"postgres_username"`
	PostgresPassword string `json:"-"`
	PostgresTLS      bool   `json:"postgres_tls"`

	RedisHost     string `json:"redis_host"`
	RedisPort     int    `json:"redis_port"`
	RedisDatabase int    `json:"redis_database"`
	RedisUsername string `json:"redis_username,omitempty"`
	RedisPassword string `json:"-"`
	RedisTLS      bool   `json:"redis_tls"`
}

// WorkspaceConnection resolves the workspace's isolated database and Redis
// namespace from the current service configuration.
func (m *Manager) WorkspaceConnection(ws *workspace.Workspace) (WorkspaceConnection, error) {
	pgCfg := m.cfg.Get().Services.PostgreSQL
	connection := WorkspaceConnection{
		PostgresDatabase: ws.Database.DatabaseName,
		PostgresUsername: ws.Database.Username,
		RedisDatabase:    ws.Redis.DBIndex,
	}
	if pgCfg.Mode == "external" {
		connection.PostgresHost = pgCfg.External.Host
		connection.PostgresPort = pgCfg.External.Port
		connection.PostgresTLS = pgCfg.External.TLS
	} else {
		connection.PostgresHost = pgCfg.Host
		connection.PostgresPort = pgCfg.Port
	}
	if connection.PostgresHost == "" || connection.PostgresPort < 1 {
		return WorkspaceConnection{}, fmt.Errorf("%w: PostgreSQL 连接信息不完整", ErrValidation)
	}
	password, _ := m.secrets.Get(ws.Database.PasswordRef)
	if password == "" {
		return WorkspaceConnection{}, fmt.Errorf("%w: Workspace 数据库凭据缺失", ErrValidation)
	}
	connection.PostgresPassword = password

	redisCfg := m.cfg.Get().Services.Redis
	if redisCfg.Mode == "external" {
		connection.RedisHost = redisCfg.External.Host
		connection.RedisPort = redisCfg.External.Port
		connection.RedisUsername = redisCfg.External.Username
		connection.RedisTLS = redisCfg.External.TLS
		connection.RedisPassword, _ = m.secrets.Get(redisCfg.External.PasswordRef)
	} else {
		connection.RedisHost = redisCfg.Host
		connection.RedisPort = redisCfg.Port
		connection.RedisPassword, _ = m.secrets.Get(redisCfg.PasswordRef)
	}
	if connection.RedisHost == "" || connection.RedisPort < 1 {
		return WorkspaceConnection{}, fmt.Errorf("%w: Redis 连接信息不完整", ErrValidation)
	}
	return connection, nil
}

// DumpWorkspaceDatabase writes a PostgreSQL custom-format dump to hostPath.
func (m *Manager) DumpWorkspaceDatabase(ctx context.Context, ws *workspace.Workspace, hostPath string) error {
	status := m.postgresStatus(ctx)
	if !status.Reachable {
		return fmt.Errorf("%w: %s", ErrNotRunning, status.Message)
	}
	endpoint, err := m.workspacePostgresEndpoint(ws)
	if err != nil {
		return err
	}
	pgDump, err := m.findPostgresTool(ctx, "pg_dump")
	if err != nil {
		return fmt.Errorf("%w: 未找到 pg_dump", ErrNotInstalled)
	}
	execPath, err := m.runtime.TranslatePath(pgDump.Execution, hostPath)
	if err != nil {
		return err
	}
	args := []string{
		"--format=custom", "--no-owner", "--no-privileges",
		"--file", execPath,
		"-h", endpoint.Host, "-p", strconv.Itoa(endpoint.Port),
		"-U", endpoint.User, "-d", endpoint.Database,
	}
	run, runErr := m.runtime.RunFound(ctx, pgDump, runtime.RunOptions{
		Args: args,
		Env:  m.pgEnv(endpoint),
	})
	if runErr != nil || run.ExitCode != 0 {
		return fmt.Errorf("pg_dump 失败: %s", commandError(run, runErr))
	}
	if err := m.ensureOutputExists(ctx, pgDump.Execution, hostPath); err != nil {
		return err
	}
	return nil
}

// RestoreWorkspaceDatabase restores a custom-format dump produced by
// DumpWorkspaceDatabase into the workspace database.
func (m *Manager) RestoreWorkspaceDatabase(ctx context.Context, ws *workspace.Workspace, hostPath string) error {
	status := m.postgresStatus(ctx)
	if !status.Reachable {
		return fmt.Errorf("%w: %s", ErrNotRunning, status.Message)
	}
	endpoint, err := m.workspacePostgresEndpoint(ws)
	if err != nil {
		return err
	}
	pgRestore, err := m.findPostgresTool(ctx, "pg_restore")
	if err != nil {
		return fmt.Errorf("%w: 未找到 pg_restore", ErrNotInstalled)
	}
	execPath, err := m.runtime.TranslatePath(pgRestore.Execution, hostPath)
	if err != nil {
		return err
	}
	run, runErr := m.runtime.RunFound(ctx, pgRestore, runtime.RunOptions{
		Args: []string{
			"--clean", "--if-exists", "--no-owner", "--no-privileges",
			"--exit-on-error", "--single-transaction",
			"-h", endpoint.Host, "-p", strconv.Itoa(endpoint.Port),
			"-U", endpoint.User, "-d", endpoint.Database,
			execPath,
		},
		Env: m.pgEnv(endpoint),
	})
	if runErr != nil || run.ExitCode != 0 {
		return fmt.Errorf("pg_restore 失败: %s", commandError(run, runErr))
	}
	return nil
}

func (m *Manager) workspacePostgresEndpoint(ws *workspace.Workspace) (pgEndpoint, error) {
	cfg := m.cfg.Get().Services.PostgreSQL
	password, _ := m.secrets.Get(ws.Database.PasswordRef)
	if password == "" {
		return pgEndpoint{}, fmt.Errorf("%w: Workspace 数据库凭据缺失", ErrValidation)
	}
	endpoint := pgEndpoint{
		Mode:     cfg.Mode,
		User:     ws.Database.Username,
		Password: password,
		Database: ws.Database.DatabaseName,
	}
	if cfg.Mode == "external" {
		endpoint.Host = cfg.External.Host
		endpoint.Port = cfg.External.Port
		endpoint.TLS = cfg.External.TLS
	} else {
		endpoint.Host = cfg.Host
		endpoint.Port = cfg.Port
	}
	return endpoint, nil
}

func (m *Manager) ensureOutputExists(ctx context.Context, execution, hostPath string) error {
	execPath, err := m.runtime.TranslatePath(execution, hostPath)
	if err != nil {
		return err
	}
	if !m.runtime.Exists(ctx, runtime.Found{Execution: execution}, execPath) {
		return fmt.Errorf("备份输出文件未生成: %s", filepath.Base(hostPath))
	}
	info, err := os.Stat(hostPath)
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return fmt.Errorf("备份输出文件为空: %s", filepath.Base(hostPath))
	}
	return nil
}

// RedactedConnection returns a log-safe description of a workspace connection.
func RedactedConnection(connection WorkspaceConnection) string {
	return fmt.Sprintf("postgresql=%s:%d/%s redis=%s:%d/%d",
		logging.Redact(connection.PostgresHost), connection.PostgresPort,
		logging.Redact(connection.PostgresDatabase),
		logging.Redact(connection.RedisHost), connection.RedisPort, connection.RedisDatabase)
}
