package service

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type pgEndpoint struct {
	Mode     string
	Host     string
	Port     int
	User     string
	Password string
	Database string
	TLS      bool
}

func (m *Manager) postgresStatus(ctx context.Context) Status {
	cfg := m.cfg.Get().Services.PostgreSQL
	status := Status{
		Name:         string(PostgreSQL),
		Mode:         cfg.Mode,
		Host:         cfg.Host,
		Port:         cfg.Port,
		DataDir:      m.postgresDataDir(cfg),
		LogFile:      filepath.Join(m.paths.ServiceDir("postgresql"), "postgresql.log"),
		Dependencies: m.dependencies(ctx),
		CheckedAt:    m.now().UTC(),
	}
	if cfg.Mode == "external" {
		status.Host = cfg.External.Host
		status.Port = cfg.External.Port
		status.DataDir = ""
		status.LogFile = ""
		result, err := m.testPostgres(ctx, nil, TestResult{})
		status.Reachable = result.Reachable
		status.Authenticated = result.Authenticated
		status.Version = result.ServerVersion
		status.Message = result.Message
		if err != nil {
			status.State = StateError
			status.Message = err.Error()
		} else if result.Reachable {
			status.State = StateExternal
		} else {
			status.State = StateUnreachable
		}
		return status
	}

	found, err := m.runtime.FindBinary(ctx, "pg_ctl")
	if err != nil {
		status.State = StateNotInstalled
		status.Message = "未找到 pg_ctl；请安装 PostgreSQL 或在版本矩阵中配置路径"
		return status
	}
	status.Path = found.Path
	status.Execution = found.Execution
	if result, err := m.runtime.RunFound(ctx, found, runtime.RunOptions{Args: []string{"--version"}}); err == nil {
		status.Version = runtime.ExtractVersion(result.Stdout + "\n" + result.Stderr)
	}

	dataDir := m.postgresDataDir(cfg)
	if !m.fileExists(ctx, found.Execution, filepath.Join(dataDir, "PG_VERSION")) {
		status.State = StateStopped
		status.Message = "数据目录尚未初始化"
		return status
	}
	execDataDir, err := m.resolveConfiguredPath(dataDir, found.Execution)
	if err != nil {
		status.State = StateError
		status.Message = err.Error()
		return status
	}
	result, runErr := m.runtime.RunFound(ctx, found, runtime.RunOptions{
		Args: []string{"-D", execDataDir, "status"},
	})
	combined := strings.TrimSpace(result.Stdout + "\n" + result.Stderr)
	if runErr == nil && result.ExitCode == 0 {
		status.State = StateRunning
		status.Reachable = true
		status.PID = parsePID(combined)
		status.Message = "服务正在运行"
		probe, _ := m.testPostgres(ctx, nil, TestResult{})
		status.Authenticated = probe.Authenticated
		if probe.ServerVersion != "" {
			status.Version = probe.ServerVersion
		}
		if !probe.Authenticated {
			status.Message = "服务正在运行，但管理连接未通过"
		}
	} else {
		status.State = StateStopped
		status.Message = "服务已停止"
	}
	return status
}

func (m *Manager) testPostgres(ctx context.Context, override *ConfigInput, result TestResult) (TestResult, error) {
	result.CheckedAt = m.now().UTC()
	endpoint, err := m.postgresEndpointWithOverride(override)
	if err != nil {
		return result, err
	}
	result.Mode = endpoint.Mode
	result.Host = endpoint.Host
	result.Port = endpoint.Port
	start := time.Now()
	defer func() { result.LatencyMS = time.Since(start).Milliseconds() }()

	psql, psqlErr := m.findPostgresTool(ctx, "psql")
	if psqlErr == nil {
		query := "SELECT 1, current_setting('server_version');"
		args := []string{
			"-X", "-q", "-w", "-v", "ON_ERROR_STOP=1",
			"-h", endpoint.Host, "-p", strconv.Itoa(endpoint.Port),
			"-U", endpoint.User, "-d", endpoint.Database,
			"-tA", "-c", query,
		}
		runCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		run, err := m.runtime.RunFound(runCtx, psql, runtime.RunOptions{
			Args: args,
			Env:  m.pgEnv(endpoint),
		})
		if err == nil && run.ExitCode == 0 {
			result.Reachable = true
			result.Authenticated = true
			line := lastNonEmptyLine(run.Stdout)
			if _, version, ok := strings.Cut(line, "|"); ok {
				result.ServerVersion = strings.TrimSpace(version)
			}
			result.Message = "连接成功"
			return result, nil
		}
		message := strings.TrimSpace(run.Stderr)
		if message == "" {
			message = strings.TrimSpace(run.Stdout)
		}
		if message == "" && err != nil {
			message = err.Error()
		}
		result.Message = logging.Redact(message)
		if dialErr := dialTCP(ctx, endpoint.Host, endpoint.Port); dialErr == nil {
			result.Reachable = true
			if result.Message == "" {
				result.Message = "端口可达，但认证或查询失败"
			}
		} else {
			result.Reachable = false
			if result.Message == "" {
				result.Message = "无法连接: " + dialErr.Error()
			}
		}
		return result, nil
	}

	if pgIsReady, readyErr := m.findPostgresTool(ctx, "pg_isready"); readyErr == nil {
		runCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		defer cancel()
		run, _ := m.runtime.RunFound(runCtx, pgIsReady, runtime.RunOptions{
			Args: []string{
				"-h", endpoint.Host, "-p", strconv.Itoa(endpoint.Port),
				"-d", endpoint.Database, "-U", endpoint.User, "-t", "5",
			},
			Env: m.pgEnv(endpoint),
		})
		if run.ExitCode == 0 {
			result.Reachable = true
			result.Message = "服务器可达；未找到 psql，无法验证凭据"
			return result, nil
		}
	}

	if err := dialTCP(ctx, endpoint.Host, endpoint.Port); err != nil {
		result.Message = "无法连接: " + err.Error()
		return result, nil
	}
	result.Reachable = true
	result.Message = "TCP 端口可达；未找到 psql/pg_isready，无法验证数据库"
	return result, nil
}

func (m *Manager) postgresEndpointWithOverride(override *ConfigInput) (pgEndpoint, error) {
	cfg := m.cfg.Get().Services.PostgreSQL
	passwordOverride := ""
	if override != nil {
		mode := strings.ToLower(strings.TrimSpace(override.Mode))
		if mode == "" {
			mode = cfg.Mode
		}
		passwordOverride = override.Password
		if mode == "external" {
			cfg.Mode = "external"
			if override.Host != "" {
				cfg.External.Host = strings.TrimSpace(override.Host)
			}
			if override.Port > 0 {
				cfg.External.Port = override.Port
			}
			if override.Username != "" {
				cfg.External.Username = strings.TrimSpace(override.Username)
			}
			if override.Database != "" {
				cfg.External.Database = strings.TrimSpace(override.Database)
			}
			cfg.External.TLS = override.TLS
		} else {
			cfg.Mode = "managed"
			if override.Host != "" {
				cfg.Host = strings.TrimSpace(override.Host)
			}
			if override.Port > 0 {
				cfg.Port = override.Port
			}
			if override.AdminUser != "" {
				cfg.AdminUser = strings.TrimSpace(override.AdminUser)
			}
		}
	}
	endpoint, err := m.postgresEndpoint(cfg)
	if err != nil {
		return endpoint, err
	}
	if passwordOverride != "" {
		endpoint.Password = passwordOverride
	}
	return endpoint, nil
}

func (m *Manager) testRedis(ctx context.Context, override *ConfigInput, result TestResult) (TestResult, error) {
	result.CheckedAt = m.now().UTC()
	endpoint, err := m.redisEndpointWithOverride(override)
	if err != nil {
		return result, err
	}
	result.Mode = m.cfg.Get().Services.Redis.Mode
	if override != nil && override.Mode != "" {
		result.Mode = strings.ToLower(override.Mode)
	}
	result.Host = endpoint.Host
	result.Port = endpoint.Port
	latency, version, pingErr := redisPing(ctx, endpoint)
	result.LatencyMS = latency.Milliseconds()
	if pingErr != nil {
		result.Message = logging.Redact(pingErr.Error())
		if dialErr := dialTCP(ctx, endpoint.Host, endpoint.Port); dialErr == nil {
			result.Reachable = true
		}
		return result, nil
	}
	result.Reachable = true
	result.Authenticated = true
	result.ServerVersion = version
	result.Message = "连接成功"
	return result, nil
}

func (m *Manager) redisEndpointWithOverride(override *ConfigInput) (redisEndpoint, error) {
	cfg := m.cfg.Get().Services.Redis
	passwordOverride := ""
	if override != nil {
		mode := strings.ToLower(strings.TrimSpace(override.Mode))
		if mode == "" {
			mode = cfg.Mode
		}
		passwordOverride = override.Password
		if mode == "external" {
			cfg.Mode = "external"
			if override.Host != "" {
				cfg.External.Host = strings.TrimSpace(override.Host)
			}
			if override.Port > 0 {
				cfg.External.Port = override.Port
			}
			if override.Username != "" {
				cfg.External.Username = strings.TrimSpace(override.Username)
			}
			cfg.External.TLS = override.TLS
		} else {
			cfg.Mode = "managed"
			if override.Host != "" {
				cfg.Host = strings.TrimSpace(override.Host)
			}
			if override.Port > 0 {
				cfg.Port = override.Port
			}
		}
	}
	endpoint, err := m.redisEndpoint(cfg)
	if err != nil {
		return endpoint, err
	}
	if passwordOverride != "" {
		endpoint.Password = passwordOverride
	}
	return endpoint, nil
}

func (m *Manager) startPostgres(ctx context.Context, progress func(int, string)) error {
	cfg := m.cfg.Get().Services.PostgreSQL
	if cfg.Mode == "external" {
		return ErrExternal
	}
	pgCtl, err := m.runtime.FindBinary(ctx, "pg_ctl")
	if err != nil {
		return fmt.Errorf("%w: 未找到 pg_ctl", ErrNotInstalled)
	}
	initdb, ok := m.findPostgresToolFrom(ctx, pgCtl, "initdb")
	if !ok {
		return fmt.Errorf("%w: 未找到 initdb", ErrNotInstalled)
	}
	dataDir := m.postgresDataDir(cfg)
	serviceDir := m.paths.ServiceDir("postgresql")
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(serviceDir, "postgresql.log")
	passwordRef := cfg.AdminPasswordRef
	if passwordRef == "" {
		passwordRef = "pg_admin"
	}
	initialized := m.fileExists(ctx, pgCtl.Execution, filepath.Join(dataDir, "PG_VERSION"))
	if initialized {
		if status := m.postgresStatus(ctx); status.State == StateRunning {
			if status.Authenticated {
				progress(90, "PostgreSQL 已在运行，同步 Workspace 数据库")
				m.provisionAllWorkspaces(ctx, progress)
				progress(100, "PostgreSQL 已在运行")
				return nil
			}
			return fmt.Errorf("%w: PostgreSQL 正在运行，但管理凭据无法验证: %s",
				ErrValidation, status.Message)
		}
	}
	password := m.servicePassword(passwordRef)
	if initialized && password == "" {
		return fmt.Errorf("%w: PostgreSQL 数据目录已存在，但管理凭据缺失", ErrValidation)
	}
	if password == "" {
		password, err = m.ensureSecret(passwordRef, 24)
		if err != nil {
			return err
		}
	}

	execDataDir, err := m.resolveConfiguredPath(dataDir, pgCtl.Execution)
	if err != nil {
		return err
	}
	execLogPath, err := m.resolveConfiguredPath(logPath, pgCtl.Execution)
	if err != nil {
		return err
	}
	env := m.pgBinEnv(pgCtl)

	if !initialized {
		progress(10, "初始化 PostgreSQL 数据目录")
		passwordFile := filepath.Join(m.paths.TmpDir, "pg-init-"+strconv.FormatInt(m.now().UnixNano(), 36)+".pw")
		if err := store.WriteFileAtomicProtected(passwordFile, []byte(password+"\n"), 0o600, secret.ProtectFile); err != nil {
			return fmt.Errorf("写入临时口令文件: %w", err)
		}
		defer func() { _ = os.Remove(passwordFile) }()
		execPasswordFile, err := m.resolveConfiguredPath(passwordFile, initdb.Execution)
		if err != nil {
			return err
		}
		runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		run, err := m.runtime.RunFound(runCtx, initdb, runtime.RunOptions{
			Args: []string{
				"-D", execDataDir,
				"-U", cfg.AdminUser,
				"-E", "UTF8",
				"--locale=C",
				"--auth-local=trust",
				"--auth-host=scram-sha-256",
				"--pwfile", execPasswordFile,
			},
			Env: env,
		})
		if err != nil || run.ExitCode != 0 {
			return fmt.Errorf("initdb 失败: %s", commandError(run, err))
		}
	}

	progress(55, "启动 PostgreSQL")
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	options := fmt.Sprintf("-p %d -h %s", cfg.Port, cfg.Host)
	run, err := m.runtime.RunFound(runCtx, pgCtl, runtime.RunOptions{
		Args: []string{
			"-D", execDataDir,
			"-l", execLogPath,
			"-o", options,
			"-w", "-t", "60",
			"start",
		},
		Env: env,
	})
	if err != nil || run.ExitCode != 0 {
		return fmt.Errorf("启动 PostgreSQL 失败: %s", commandError(run, err))
	}

	progress(85, "验证 PostgreSQL 管理连接")
	verifyCtx, verifyCancel := context.WithTimeout(ctx, 10*time.Second)
	defer verifyCancel()
	test, testErr := m.testPostgres(verifyCtx, nil, TestResult{})
	if testErr != nil || !test.Authenticated {
		message := test.Message
		if testErr != nil {
			message = testErr.Error()
		}
		return fmt.Errorf("PostgreSQL 已启动，但管理连接失败: %s", message)
	}
	progress(95, "同步 Workspace 数据库")
	m.provisionAllWorkspaces(ctx, progress)
	progress(100, "PostgreSQL 已启动")
	return nil
}

func (m *Manager) stopPostgres(ctx context.Context, progress func(int, string)) error {
	cfg := m.cfg.Get().Services.PostgreSQL
	if cfg.Mode == "external" {
		return ErrExternal
	}
	pgCtl, err := m.runtime.FindBinary(ctx, "pg_ctl")
	if err != nil {
		return nil
	}
	dataDir := m.postgresDataDir(cfg)
	if !m.fileExists(ctx, pgCtl.Execution, filepath.Join(dataDir, "PG_VERSION")) {
		return nil
	}
	status := m.postgresStatus(ctx)
	if status.State != StateRunning {
		return nil
	}
	progress(30, "停止 PostgreSQL")
	execDataDir, err := m.resolveConfiguredPath(dataDir, pgCtl.Execution)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	run, runErr := m.runtime.RunFound(runCtx, pgCtl, runtime.RunOptions{
		Args: []string{"-D", execDataDir, "-m", "fast", "-w", "-t", "60", "stop"},
		Env:  m.pgBinEnv(pgCtl),
	})
	if runErr != nil || run.ExitCode != 0 {
		return fmt.Errorf("停止 PostgreSQL 失败: %s", commandError(run, runErr))
	}
	progress(100, "PostgreSQL 已停止")
	return nil
}

func (m *Manager) runPSQL(ctx context.Context, endpoint pgEndpoint, database, sql string) (string, error) {
	psql, err := m.findPostgresTool(ctx, "psql")
	if err != nil {
		return "", fmt.Errorf("%w: 未找到 psql", ErrNotInstalled)
	}
	runCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	run, runErr := m.runtime.RunFound(runCtx, psql, runtime.RunOptions{
		Args: []string{
			"-X", "-q", "-w", "-v", "ON_ERROR_STOP=1",
			"-h", endpoint.Host, "-p", strconv.Itoa(endpoint.Port),
			"-U", endpoint.User, "-d", database,
			"-tA",
		},
		Env:   m.pgEnv(endpoint),
		Stdin: []byte(sql),
	})
	if runErr != nil || run.ExitCode != 0 {
		return "", fmt.Errorf("psql 执行失败: %s", commandError(run, runErr))
	}
	return run.Stdout, nil
}

func (m *Manager) EnsureWorkspaceDatabase(ctx context.Context, ws *workspace.Workspace) error {
	status := m.postgresStatus(ctx)
	if !status.Reachable {
		return fmt.Errorf("%w: %s", ErrNotRunning, status.Message)
	}
	cfg := m.cfg.Get().Services.PostgreSQL
	endpoint, err := m.postgresEndpoint(cfg)
	if err != nil {
		return err
	}
	password, ok := m.secrets.Get(ws.Database.PasswordRef)
	if !ok || password == "" {
		return fmt.Errorf("Workspace 数据库凭据缺失")
	}
	sql := ensureWorkspaceSQL(ws, password)
	if _, err := m.runPSQL(ctx, endpoint, endpoint.Database, sql); err != nil {
		return err
	}
	return nil
}

func (m *Manager) DropWorkspaceDatabase(ctx context.Context, ws *workspace.Workspace) error {
	cfg := m.cfg.Get().Services.PostgreSQL
	endpoint, err := m.postgresEndpoint(cfg)
	if err != nil {
		return err
	}
	sql := fmt.Sprintf(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
WHERE datname = %s AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS %s;
DROP ROLE IF EXISTS %s;
`, quoteLiteral(ws.Database.DatabaseName), quoteIdent(ws.Database.DatabaseName), quoteIdent(ws.Database.Username))
	_, err = m.runPSQL(ctx, endpoint, endpoint.Database, sql)
	return err
}

func (m *Manager) ClearWorkspaceRedis(ctx context.Context, ws *workspace.Workspace) error {
	cfg := m.cfg.Get().Services.Redis
	endpoint, err := m.redisEndpoint(cfg)
	if err != nil {
		return err
	}
	return redisFlushDB(ctx, endpoint, ws.Redis.DBIndex)
}

func (m *Manager) provisionAllWorkspaces(ctx context.Context, progress func(int, string)) {
	m.hostsMu.RLock()
	host := m.hosts
	m.hostsMu.RUnlock()
	if host == nil {
		return
	}
	list, err := host.List(ctx)
	if err != nil {
		m.log.Warn("could not list workspaces for database provisioning", "error", err)
		return
	}
	for _, ws := range list {
		if ws.Database.Provisioned {
			continue
		}
		progress(95, "同步 Workspace 数据库 "+ws.Name)
		err := m.EnsureWorkspaceDatabase(ctx, ws)
		message := ""
		if err != nil {
			message = err.Error()
			m.log.Warn("workspace database provisioning failed",
				"workspace_id", ws.WorkspaceID, "error", err)
		}
		if markErr := host.MarkDatabaseResult(ctx, ws.WorkspaceID, err == nil, message); markErr != nil {
			m.log.Warn("could not record database provisioning result",
				"workspace_id", ws.WorkspaceID, "error", markErr)
		}
	}
}

func (m *Manager) findPostgresTool(ctx context.Context, name string) (runtime.Found, error) {
	if found, err := m.runtime.FindBinary(ctx, name); err == nil {
		return found, nil
	}
	pgCtl, err := m.runtime.FindBinary(ctx, "pg_ctl")
	if err != nil {
		return runtime.Found{}, err
	}
	if sibling, ok := m.runtime.Sibling(ctx, pgCtl, name); ok {
		return sibling, nil
	}
	return runtime.Found{}, fmt.Errorf("%s not found", name)
}

func (m *Manager) findPostgresToolFrom(ctx context.Context, from runtime.Found, name string) (runtime.Found, bool) {
	if sibling, ok := m.runtime.Sibling(ctx, from, name); ok {
		return sibling, true
	}
	found, err := m.findPostgresTool(ctx, name)
	if err != nil {
		return runtime.Found{}, false
	}
	return found, true
}

func (m *Manager) pgEnv(endpoint pgEndpoint) []string {
	mode := "disable"
	if endpoint.TLS {
		mode = "require"
	}
	return []string{
		"PGPASSWORD=" + endpoint.Password,
		"PGSSLMODE=" + mode,
		"PGCONNECT_TIMEOUT=6",
		"PGAPPNAME=hxxddk",
	}
}

func (m *Manager) pgBinEnv(found runtime.Found) []string {
	if found.Execution != runtime.ExecutionNative {
		return nil
	}
	dir := filepath.Dir(found.Path)
	return []string{"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH")}
}

func (m *Manager) fileExists(ctx context.Context, execution, hostPath string) bool {
	execPath, err := m.resolveConfiguredPath(hostPath, execution)
	if err != nil {
		return false
	}
	return m.runtime.Exists(ctx, runtime.Found{Execution: execution}, execPath)
}

func dialTCP(ctx context.Context, host string, port int) error {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func commandError(run runtime.RunResult, err error) string {
	message := strings.TrimSpace(run.Stderr)
	if message == "" {
		message = strings.TrimSpace(run.Stdout)
	}
	if message == "" && err != nil {
		message = err.Error()
	}
	if message == "" {
		message = fmt.Sprintf("退出码 %d", run.ExitCode)
	}
	return logging.Redact(message)
}

func lastNonEmptyLine(value string) string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}

func ensureWorkspaceSQL(ws *workspace.Workspace, password string) string {
	role := quoteIdent(ws.Database.Username)
	database := quoteIdent(ws.Database.DatabaseName)
	return fmt.Sprintf(`DO $hxxddk$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = %s) THEN
    CREATE ROLE %s LOGIN PASSWORD %s;
  ELSE
    ALTER ROLE %s WITH LOGIN PASSWORD %s;
  END IF;
END
$hxxddk$;
SELECT 'CREATE DATABASE ' || quote_ident(%s) || ' OWNER ' || quote_ident(%s)
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = %s)
\gexec
\connect %s
REVOKE ALL ON DATABASE %s FROM PUBLIC;
GRANT CONNECT, TEMPORARY ON DATABASE %s TO %s;
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO %s;
`,
		quoteLiteral(ws.Database.Username), role, quoteLiteral(password), role, quoteLiteral(password),
		ws.Database.DatabaseName, ws.Database.Username, quoteLiteral(ws.Database.DatabaseName),
		database, database, database, role, role)
}

func quoteIdent(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
