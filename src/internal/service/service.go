// Package service manages the PostgreSQL and Redis instances shared by all
// workspaces, including managed and external modes.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Name identifies a managed service.
type Name string

const (
	PostgreSQL Name = "postgresql"
	Redis      Name = "redis"
)

// Service states reported by the API.
const (
	StateRunning      = "RUNNING"
	StateStopped      = "STOPPED"
	StateNotInstalled = "NOT_INSTALLED"
	StateExternal     = "EXTERNAL"
	StateUnreachable  = "UNREACHABLE"
	StateError        = "ERROR"
)

// Domain errors mapped to HTTP responses.
var (
	ErrUnknownService = errors.New("service not found")
	ErrExternal       = errors.New("external service cannot be started or stopped by DevKit")
	ErrServiceInUse   = errors.New("service is used by running workspaces")
	ErrNotInstalled   = errors.New("service components are not installed")
	ErrNotRunning     = errors.New("service is not running")
	ErrValidation     = errors.New("service configuration is invalid")
	// ErrDataDirUnsupported means the configured data directory cannot work in
	// the execution environment, e.g. PostgreSQL on a WSL DrvFS mount.
	ErrDataDirUnsupported = errors.New("service data directory is not supported")
)

// Status is the live state of one shared service.
type Status struct {
	Name          string    `json:"name"`
	Mode          string    `json:"mode"`
	State         string    `json:"state"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Reachable     bool      `json:"reachable"`
	Authenticated bool      `json:"authenticated"`
	Version       string    `json:"version,omitempty"`
	Execution     string    `json:"execution,omitempty"`
	Path          string    `json:"path,omitempty"`
	DataDir       string    `json:"data_dir,omitempty"`
	LogFile       string    `json:"log_file,omitempty"`
	PID           int       `json:"pid,omitempty"`
	Message       string    `json:"message,omitempty"`
	Dependencies  []string  `json:"dependencies,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

// TestResult is the outcome of a connection test. It never contains a
// password.
type TestResult struct {
	Name          string    `json:"name"`
	Mode          string    `json:"mode"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	Reachable     bool      `json:"reachable"`
	Authenticated bool      `json:"authenticated"`
	LatencyMS     int64     `json:"latency_ms"`
	ServerVersion string    `json:"server_version,omitempty"`
	Message       string    `json:"message,omitempty"`
	CheckedAt     time.Time `json:"checked_at"`
}

// ConfigInput updates a service mode or external endpoint. Password is
// write-only and is stored in secrets.json.
type ConfigInput struct {
	Mode      string `json:"mode"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	AdminUser string `json:"admin_user,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Database  string `json:"database,omitempty"`
	TLS       bool   `json:"tls,omitempty"`
}

// ProvisioningHost lets the service manager reconcile workspace databases
// after PostgreSQL starts.
type ProvisioningHost interface {
	List(ctx context.Context) ([]*workspace.Workspace, error)
	MarkDatabaseResult(ctx context.Context, workspaceID string, provisioned bool, message string) error
}

// Options tunes the service manager.
type Options struct {
	Dependencies func(ctx context.Context) []string
	Now          func() time.Time
}

// Manager owns shared service lifecycle and workspace provisioning.
type Manager struct {
	paths   *config.Paths
	cfg     *config.Store
	secrets *secret.Store
	runtime *runtime.Manager
	log     *slog.Logger
	deps    func(ctx context.Context) []string
	now     func() time.Time
	opMu    sync.Mutex
	hostsMu sync.RWMutex
	hosts   ProvisioningHost
}

// New creates a service manager.
func New(paths *config.Paths, cfg *config.Store, secrets *secret.Store,
	rt *runtime.Manager, logger *slog.Logger, opts Options) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Manager{
		paths:   paths,
		cfg:     cfg,
		secrets: secrets,
		runtime: rt,
		log:     logger,
		deps:    opts.Dependencies,
		now:     opts.Now,
	}
}

// SetWorkspaces wires the workspace store used to reconcile databases.
func (m *Manager) SetWorkspaces(host ProvisioningHost) {
	m.hostsMu.Lock()
	defer m.hostsMu.Unlock()
	m.hosts = host
}

// Names returns the supported service names in stable order.
func Names() []string {
	return []string{string(PostgreSQL), string(Redis)}
}

// ParseName validates a service name from an API path.
func ParseName(value string) (Name, error) {
	switch Name(strings.ToLower(strings.TrimSpace(value))) {
	case PostgreSQL:
		return PostgreSQL, nil
	case Redis:
		return Redis, nil
	default:
		return "", fmt.Errorf("%w: %s", ErrUnknownService, value)
	}
}

// List returns the state of both services.
func (m *Manager) List(ctx context.Context) []Status {
	result := make([]Status, 0, 2)
	for _, name := range []Name{PostgreSQL, Redis} {
		status, err := m.Status(ctx, name)
		if err != nil {
			status = Status{
				Name:      string(name),
				State:     StateError,
				Message:   err.Error(),
				CheckedAt: m.now().UTC(),
			}
		}
		result = append(result, status)
	}
	return result
}

// Status probes one service.
func (m *Manager) Status(ctx context.Context, name Name) (Status, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch name {
	case PostgreSQL:
		return m.postgresStatus(probeCtx), nil
	case Redis:
		return m.redisStatus(probeCtx), nil
	default:
		return Status{}, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
}

// Start starts a managed service.
func (m *Manager) Start(ctx context.Context, name Name, progress func(int, string)) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if progress == nil {
		progress = func(int, string) {}
	}
	switch name {
	case PostgreSQL:
		return m.startPostgres(ctx, progress)
	case Redis:
		return m.startRedis(ctx, progress)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
}

// Stop stops a managed service after checking workspace dependencies.
func (m *Manager) Stop(ctx context.Context, name Name, progress func(int, string)) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if progress == nil {
		progress = func(int, string) {}
	}
	switch name {
	case PostgreSQL:
		if m.cfg.Get().Services.PostgreSQL.Mode == "external" {
			return ErrExternal
		}
	case Redis:
		if m.cfg.Get().Services.Redis.Mode == "external" {
			return ErrExternal
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	if deps := m.dependencies(ctx); len(deps) > 0 {
		return fmt.Errorf("%w: %s", ErrServiceInUse, strings.Join(deps, ", "))
	}
	switch name {
	case PostgreSQL:
		return m.stopPostgres(ctx, progress)
	case Redis:
		return m.stopRedis(ctx, progress)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
}

// Restart restarts a managed service.
func (m *Manager) Restart(ctx context.Context, name Name, progress func(int, string)) error {
	if err := m.Stop(ctx, name, progress); err != nil {
		return err
	}
	return m.Start(ctx, name, progress)
}

// Logs returns the tail of a managed service log.
func (m *Manager) Logs(ctx context.Context, name Name, tail int) ([]string, string, error) {
	if tail <= 0 {
		tail = 100
	}
	if tail > 2000 {
		tail = 2000
	}
	var path string
	switch name {
	case PostgreSQL:
		cfg := m.cfg.Get().Services.PostgreSQL
		if cfg.Mode == "external" {
			return nil, "", fmt.Errorf("%w: 外部 PostgreSQL 日志请查看外部服务器", ErrValidation)
		}
		path = filepath.Join(m.paths.ServiceDir("postgresql"), "postgresql.log")
	case Redis:
		cfg := m.cfg.Get().Services.Redis
		if cfg.Mode == "external" {
			return nil, "", fmt.Errorf("%w: 外部 Redis 日志请查看外部服务器", ErrValidation)
		}
		path = filepath.Join(m.paths.ServiceDir("redis"), "redis.log")
	default:
		return nil, "", fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
	lines, err := tailLines(path, tail)
	for i := range lines {
		lines[i] = logging.Redact(lines[i])
	}
	return lines, path, err
}

// Databases lists non-template databases through the admin connection.
func (m *Manager) Databases(ctx context.Context) ([]string, error) {
	pgCfg := m.cfg.Get().Services.PostgreSQL
	status := m.postgresStatus(ctx)
	if !status.Reachable {
		return nil, fmt.Errorf("%w: %s", ErrNotRunning, status.Message)
	}
	endpoint, err := m.postgresEndpoint(pgCfg)
	if err != nil {
		return nil, err
	}
	output, err := m.runPSQL(ctx, endpoint, endpoint.Database,
		"SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname;")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			names = append(names, line)
		}
	}
	return names, nil
}

// Test checks connectivity and authentication for a service, optionally with
// an unsaved endpoint override.
func (m *Manager) Test(ctx context.Context, name Name, override *ConfigInput) (TestResult, error) {
	testCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := TestResult{Name: string(name), CheckedAt: m.now().UTC()}
	switch name {
	case PostgreSQL:
		return m.testPostgres(testCtx, override, result)
	case Redis:
		return m.testRedis(testCtx, override, result)
	default:
		return result, fmt.Errorf("%w: %s", ErrUnknownService, name)
	}
}

// Update switches a service between managed and external modes or updates
// the external endpoint.
func (m *Manager) Update(ctx context.Context, name Name, input ConfigInput) (Status, error) {
	mode := strings.ToLower(strings.TrimSpace(input.Mode))
	if mode != "managed" && mode != "external" {
		return Status{}, fmt.Errorf("%w: mode 必须是 managed 或 external", ErrValidation)
	}
	if input.Port < 1 || input.Port > 65535 {
		return Status{}, fmt.Errorf("%w: 端口必须在 1-65535 之间", ErrValidation)
	}
	if mode == "external" && strings.TrimSpace(input.Host) == "" {
		return Status{}, fmt.Errorf("%w: 外部服务必须填写主机名", ErrValidation)
	}

	var secretName string
	err := m.cfg.Update(func(cfg *config.Config) error {
		switch name {
		case PostgreSQL:
			pg := &cfg.Services.PostgreSQL
			pg.Mode = mode
			if mode == "managed" {
				pg.Host = strings.TrimSpace(input.Host)
				pg.Port = input.Port
				if strings.TrimSpace(input.AdminUser) != "" {
					pg.AdminUser = strings.TrimSpace(input.AdminUser)
				}
				return nil
			}
			pg.External.Host = strings.TrimSpace(input.Host)
			pg.External.Port = input.Port
			pg.External.Username = strings.TrimSpace(input.Username)
			pg.External.Database = strings.TrimSpace(input.Database)
			pg.External.TLS = input.TLS
			if pg.External.Database == "" {
				pg.External.Database = "postgres"
			}
			if pg.External.PasswordRef == "" {
				pg.External.PasswordRef = "pg_external_password"
			}
			secretName = pg.External.PasswordRef
		case Redis:
			redis := &cfg.Services.Redis
			redis.Mode = mode
			if mode == "managed" {
				redis.Host = strings.TrimSpace(input.Host)
				redis.Port = input.Port
				return nil
			}
			redis.External.Host = strings.TrimSpace(input.Host)
			redis.External.Port = input.Port
			redis.External.Username = strings.TrimSpace(input.Username)
			redis.External.Database = strings.TrimSpace(input.Database)
			redis.External.TLS = input.TLS
			if redis.External.PasswordRef == "" {
				redis.External.PasswordRef = "redis_external_password"
			}
			secretName = redis.External.PasswordRef
		default:
			return fmt.Errorf("%w: %s", ErrUnknownService, name)
		}
		return nil
	})
	if err != nil {
		return Status{}, fmt.Errorf("%w: %v", ErrValidation, err)
	}
	if input.Password != "" {
		if secretName == "" {
			return Status{}, fmt.Errorf("%w: 无法确定凭据名称", ErrValidation)
		}
		if err := m.secrets.Set(secretName, input.Password); err != nil {
			return Status{}, err
		}
	}
	m.log.Info("service configuration updated", "service", name, "mode", mode)
	return m.Status(ctx, name)
}

func (m *Manager) dependencies(ctx context.Context) []string {
	if m.deps == nil {
		return nil
	}
	return m.deps(ctx)
}

func (m *Manager) postgresEndpoint(cfg config.PostgreSQLConfig) (pgEndpoint, error) {
	if cfg.Mode == "external" {
		password := ""
		if cfg.External.PasswordRef != "" {
			password, _ = m.secrets.Get(cfg.External.PasswordRef)
		}
		database := cfg.External.Database
		if database == "" {
			database = "postgres"
		}
		username := cfg.External.Username
		if strings.TrimSpace(username) == "" {
			return pgEndpoint{}, fmt.Errorf("%w: 外部 PostgreSQL 用户名不能为空", ErrValidation)
		}
		return pgEndpoint{
			Mode: "external", Host: cfg.External.Host, Port: cfg.External.Port,
			User: username, Password: password, Database: database, TLS: cfg.External.TLS,
		}, nil
	}
	password := ""
	if cfg.AdminPasswordRef != "" {
		password, _ = m.secrets.Get(cfg.AdminPasswordRef)
	}
	return pgEndpoint{
		Mode: "managed", Host: cfg.Host, Port: cfg.Port,
		User: cfg.AdminUser, Password: password, Database: "postgres",
	}, nil
}

func (m *Manager) redisEndpoint(cfg config.RedisConfig) (redisEndpoint, error) {
	if cfg.Mode == "external" {
		password := ""
		if cfg.External.PasswordRef != "" {
			password, _ = m.secrets.Get(cfg.External.PasswordRef)
		}
		return redisEndpoint{
			Host: cfg.External.Host, Port: cfg.External.Port,
			Username: cfg.External.Username, Password: password, TLS: cfg.External.TLS,
		}, nil
	}
	password := ""
	if cfg.PasswordRef != "" {
		password, _ = m.secrets.Get(cfg.PasswordRef)
	}
	return redisEndpoint{Host: cfg.Host, Port: cfg.Port, Password: password}, nil
}

func (m *Manager) servicePassword(ref string) string {
	if ref == "" {
		return ""
	}
	value, _ := m.secrets.Get(ref)
	return value
}

func (m *Manager) ensureSecret(ref string, bytes int) (string, error) {
	if value := m.servicePassword(ref); value != "" {
		return value, nil
	}
	value, err := secret.RandomPassword(bytes)
	if err != nil {
		return "", err
	}
	if err := m.secrets.Set(ref, value); err != nil {
		return "", err
	}
	return value, nil
}

func (m *Manager) postgresDataDir(cfg config.PostgreSQLConfig) string {
	return m.paths.ResolveDataDir(cfg.DataDir)
}

func (m *Manager) redisDataDir(cfg config.RedisConfig) string {
	return m.paths.ResolveDataDir(cfg.DataDir)
}

func (m *Manager) resolveConfiguredPath(hostPath, execution string) (string, error) {
	return m.runtime.TranslatePath(execution, hostPath)
}

func tailLines(path string, tail int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	const maxTail = 128 << 10
	offset := int64(0)
	if info.Size() > maxTail {
		offset = info.Size() - maxTail
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	if offset > 0 {
		if idx := strings.IndexByte(string(data), '\n'); idx >= 0 {
			data = data[idx+1:]
		}
	}
	all := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(all) > tail {
		all = all[len(all)-tail:]
	}
	return all, nil
}

func parsePID(output string) int {
	upper := strings.ToUpper(output)
	index := strings.Index(upper, "PID:")
	if index < 0 {
		return 0
	}
	fields := strings.Fields(output[index+len("PID:"):])
	if len(fields) == 0 {
		return 0
	}
	value, err := strconv.Atoi(strings.Trim(fields[0], "(),"))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}
