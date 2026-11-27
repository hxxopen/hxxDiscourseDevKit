package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/secret"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

// Client is a minimal DevKit HTTP API client.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// APIError is a non-2xx API response.
type APIError struct {
	Status    int
	Code      string
	Message   string
	RequestID string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (HTTP %d, %s)", e.Message, e.Status, e.Code)
	}
	return fmt.Sprintf("%s (HTTP %d)", e.Message, e.Status)
}

// Do performs one API request.
func (c *Client) Do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		return fmt.Errorf("无法连接 DevKit API %s: %w", c.BaseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("读取 API 响应: %w", err)
	}
	if resp.StatusCode >= 400 {
		var envelope struct {
			Error struct {
				Code      string `json:"code"`
				Message   string `json:"message"`
				RequestID string `json:"request_id"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil && envelope.Error.Message != "" {
			return &APIError{
				Status:    resp.StatusCode,
				Code:      envelope.Error.Code,
				Message:   envelope.Error.Message,
				RequestID: envelope.Error.RequestID,
			}
		}
		message := strings.TrimSpace(string(data))
		if message == "" {
			message = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Code: "HTTP_ERROR", Message: message}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("解析 API 响应: %w", err)
		}
	}
	return nil
}

// Get is a convenience wrapper for GET requests.
func (c *Client) Get(ctx context.Context, path string, out any) error {
	return c.Do(ctx, http.MethodGet, path, nil, out)
}

// Post is a convenience wrapper for POST requests.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, http.MethodPost, path, body, out)
}

type runFile struct {
	PID       int       `json:"pid"`
	Addr      string    `json:"addr"`
	Root      string    `json:"root"`
	StartedAt time.Time `json:"started_at"`
}

type healthInfo struct {
	Status string `json:"status"`
	Root   string `json:"root"`
}

func ensureDaemon(ctx context.Context, g Globals) (*Client, error) {
	root, err := config.ResolveRoot(g.Root)
	if err != nil {
		return nil, err
	}
	paths := config.NewPaths(root)
	if err := paths.Ensure(); err != nil {
		return nil, err
	}
	addr, err := resolveDaemonAddr(ctx, g, paths, root)
	if err != nil {
		return nil, err
	}
	token, err := secret.ReadToken(paths.SecretsFile())
	if err != nil {
		return nil, fmt.Errorf("读取 API 令牌失败（根目录 %s）: %w", root, err)
	}
	return &Client{
		BaseURL: "http://" + addr,
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func resolveDaemonAddr(ctx context.Context, g Globals, paths *config.Paths, root string) (string, error) {
	if run, ok := readRunFile(paths.RunFile()); ok && run.Root == root && checkHealth(ctx, run.Addr, root) {
		return run.Addr, nil
	}

	host, port := configuredEndpoint(paths)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	if health, ok := probeHealth(ctx, addr); ok {
		if health.Root == root {
			return addr, nil
		}
		return "", fmt.Errorf("端口 %d 上运行着另一个 DevKit 实例（根目录 %s），请先停止它或使用 --root 指定其他实例", port, health.Root)
	}
	return startDaemon(ctx, g, paths, root)
}

func startDaemon(ctx context.Context, g Globals, paths *config.Paths, root string) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("定位可执行文件: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}

	logFile, err := os.OpenFile(paths.StartupLog(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", fmt.Errorf("打开启动日志: %w", err)
	}

	args := make([]string, 0, 4)
	if !isDaemonBinary(exe) {
		args = append(args, "daemon", "run")
	}
	args = append(args, "--root", root)
	if g.Dev {
		args = append(args, "--dev")
	}

	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = detachedSysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return "", fmt.Errorf("启动 devkitd 失败: %w", err)
	}
	_ = cmd.Process.Release()
	_ = logFile.Close()

	deadline := time.Now().Add(30 * time.Second)
	for {
		if run, ok := readRunFile(paths.RunFile()); ok && run.Root == root && checkHealth(ctx, run.Addr, root) {
			return run.Addr, nil
		}
		host, port := configuredEndpoint(paths)
		if health, ok := probeHealth(ctx, net.JoinHostPort(host, strconv.Itoa(port))); ok && health.Root == root {
			return net.JoinHostPort(host, strconv.Itoa(port)), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("devkitd 未能在 30 秒内启动。启动日志 %s:\n%s",
				paths.StartupLog(), tailFile(paths.StartupLog(), 30))
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func isDaemonBinary(exe string) bool {
	base := strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
	return strings.HasPrefix(base, "devkitd")
}

func readRunFile(path string) (runFile, bool) {
	var run runFile
	if err := store.ReadJSON(path, &run); err != nil {
		return run, false
	}
	return run, run.Addr != "" && run.Root != ""
}

func configuredEndpoint(paths *config.Paths) (string, int) {
	host := "127.0.0.1"
	port := 17860
	var cfg struct {
		Network struct {
			Host    string `json:"host"`
			APIPort int    `json:"api_port"`
		} `json:"network"`
	}
	if err := store.ReadJSON(paths.ConfigFile(), &cfg); err == nil {
		if cfg.Network.Host != "" {
			host = cfg.Network.Host
		}
		if cfg.Network.APIPort > 0 {
			port = cfg.Network.APIPort
		}
	}
	return host, port
}

func probeHealth(ctx context.Context, addr string) (healthInfo, bool) {
	hctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(hctx, http.MethodGet, "http://"+addr+"/api/v1/system/health", nil)
	if err != nil {
		return healthInfo{}, false
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return healthInfo{}, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return healthInfo{}, false
	}
	var health healthInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&health); err != nil {
		return healthInfo{}, false
	}
	return health, health.Status == "ok"
}

func checkHealth(ctx context.Context, addr, root string) bool {
	health, ok := probeHealth(ctx, addr)
	return ok && health.Root == root
}

func tailFile(path string, lines int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	const maxTail = 64 << 10
	offset := int64(0)
	if info.Size() > maxTail {
		offset = info.Size() - maxTail
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return ""
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return ""
	}
	all := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}
