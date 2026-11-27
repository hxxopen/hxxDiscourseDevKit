package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/logging"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

func (m *Manager) redisStatus(ctx context.Context) Status {
	cfg := m.cfg.Get().Services.Redis
	status := Status{
		Name:         string(Redis),
		Mode:         cfg.Mode,
		Host:         cfg.Host,
		Port:         cfg.Port,
		DataDir:      m.redisDataDir(cfg),
		LogFile:      filepath.Join(m.paths.ServiceDir("redis"), "redis.log"),
		Dependencies: m.dependencies(ctx),
		CheckedAt:    m.now().UTC(),
	}
	if cfg.Mode == "external" {
		status.Host = cfg.External.Host
		status.Port = cfg.External.Port
		status.DataDir = ""
		status.LogFile = ""
		result, err := m.testRedis(ctx, nil, TestResult{})
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

	found, err := m.runtime.FindBinary(ctx, "redis-server")
	if err != nil {
		status.State = StateNotInstalled
		status.Message = "未找到 redis-server；请安装 Redis 或在版本矩阵中配置路径"
		return status
	}
	status.Path = found.Path
	status.Execution = found.Execution
	if result, err := m.runtime.RunFound(ctx, found, runtime.RunOptions{Args: []string{"--version"}}); err == nil {
		status.Version = runtime.ExtractVersion(result.Stdout + "\n" + result.Stderr)
	}

	endpoint, err := m.redisEndpoint(cfg)
	if err != nil {
		status.State = StateError
		status.Message = err.Error()
		return status
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, version, pingErr := redisPing(probeCtx, endpoint)
	if pingErr == nil {
		status.State = StateRunning
		status.Reachable = true
		status.Authenticated = true
		if version != "" {
			status.Version = version
		}
		status.Message = "服务正在运行"
		return status
	}
	status.Message = logging.Redact(pingErr.Error())
	if portOpen(ctx, endpoint.Host, endpoint.Port, time.Second) {
		status.State = StateError
		status.Reachable = true
		status.Message = "端口可达，但管理连接未通过: " + status.Message
	} else {
		status.State = StateStopped
		status.Message = "服务已停止"
	}
	return status
}

func (m *Manager) startRedis(ctx context.Context, progress func(int, string)) error {
	cfg := m.cfg.Get().Services.Redis
	if cfg.Mode == "external" {
		return ErrExternal
	}
	found, err := m.runtime.FindBinary(ctx, "redis-server")
	if err != nil {
		return fmt.Errorf("%w: 未找到 redis-server", ErrNotInstalled)
	}
	endpoint, err := m.redisEndpoint(cfg)
	if err != nil {
		return err
	}
	if _, _, pingErr := redisPing(ctx, endpoint); pingErr == nil {
		progress(100, "Redis 已在运行")
		return nil
	}
	if portOpen(ctx, endpoint.Host, endpoint.Port, time.Second) {
		return fmt.Errorf("%w: Redis 端口已被占用但无法认证（凭据可能不匹配）", ErrValidation)
	}

	serviceDir := m.paths.ServiceDir("redis")
	dataDir := m.redisDataDir(cfg)
	if err := os.MkdirAll(serviceDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logPath := filepath.Join(serviceDir, "redis.log")
	confPath := filepath.Join(serviceDir, "redis.conf")
	passwordRef := cfg.PasswordRef
	if passwordRef == "" {
		passwordRef = "redis_password"
	}
	password, err := m.ensureSecret(passwordRef, 24)
	if err != nil {
		return err
	}
	execDataDir, err := m.resolveConfiguredPath(dataDir, found.Execution)
	if err != nil {
		return err
	}
	execLogPath, err := m.resolveConfiguredPath(logPath, found.Execution)
	if err != nil {
		return err
	}
	execConfPath, err := m.resolveConfiguredPath(confPath, found.Execution)
	if err != nil {
		return err
	}

	conf := fmt.Sprintf(`bind %s
protected-mode yes
port %d
daemonize no
dir "%s"
dbfilename dump.rdb
save 3600 1
appendonly yes
appendfsync everysec
requirepass "%s"
logfile "%s"
loglevel notice
`, redisConfValue(cfg.Host), cfg.Port, redisConfValue(filepath.ToSlash(execDataDir)),
		redisConfValue(password), redisConfValue(filepath.ToSlash(execLogPath)))
	if err := store.WriteFileAtomicProtected(confPath, []byte(conf), 0o600, secret.ProtectFile); err != nil {
		return fmt.Errorf("写入 Redis 配置: %w", err)
	}

	progress(40, "启动 Redis")
	command, err := m.runtime.CommandFound(ctx, found, []string{execConfPath}, nil)
	if err != nil {
		return err
	}
	pid, err := startDetached(command.Path, command.Args, command.Env, logPath)
	if err != nil {
		return fmt.Errorf("启动 Redis 失败: %w", err)
	}
	m.log.Info("redis process started", "pid", pid, "execution", found.Execution)

	deadline := m.now().Add(15 * time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		_, _, pingErr := redisPing(probeCtx, endpoint)
		cancel()
		if pingErr == nil {
			progress(100, "Redis 已启动")
			return nil
		}
		if m.now().After(deadline) {
			lines, _ := tailLines(logPath, 20)
			for i := range lines {
				lines[i] = logging.Redact(lines[i])
			}
			message := strings.Join(lines, "\n")
			if message == "" {
				message = logging.Redact(pingErr.Error())
			}
			return fmt.Errorf("Redis 启动后未通过连接检查: %s", message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (m *Manager) stopRedis(ctx context.Context, progress func(int, string)) error {
	cfg := m.cfg.Get().Services.Redis
	if cfg.Mode == "external" {
		return ErrExternal
	}
	endpoint, err := m.redisEndpoint(cfg)
	if err != nil {
		return err
	}
	progress(30, "停止 Redis")
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := redisShutdown(stopCtx, endpoint); err != nil {
		if !portOpen(ctx, endpoint.Host, endpoint.Port, time.Second) {
			progress(100, "Redis 已停止")
			return nil
		}
		return fmt.Errorf("停止 Redis 失败: %s", logging.Redact(err.Error()))
	}
	deadline := m.now().Add(15 * time.Second)
	for portOpen(ctx, endpoint.Host, endpoint.Port, 500*time.Millisecond) {
		if m.now().After(deadline) {
			return fmt.Errorf("Redis 未在预期时间内停止")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	progress(100, "Redis 已停止")
	return nil
}

func portOpen(ctx context.Context, host string, port int, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return dialTCP(probeCtx, host, port) == nil
}

func redisConfValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	return value
}
