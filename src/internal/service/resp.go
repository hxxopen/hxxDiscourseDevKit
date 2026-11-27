package service

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// redisEndpoint is the minimal connection information needed by the RESP
// client. Passwords are never logged.
type redisEndpoint struct {
	Host     string
	Port     int
	Username string
	Password string
	TLS      bool
}

// redisError is a Redis -ERR reply.
type redisError string

func (e redisError) Error() string { return string(e) }

type redisConn struct {
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
}

func dialRedis(ctx context.Context, endpoint redisEndpoint, timeout time.Duration) (*redisConn, error) {
	address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
	dialer := &net.Dialer{Timeout: timeout}
	var (
		conn net.Conn
		err  error
	)
	if endpoint.TLS {
		conn, err = tls.DialWithDialer(dialer, "tcp", address, &tls.Config{
			ServerName: endpoint.Host,
			MinVersion: tls.VersionTLS12,
		})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	return &redisConn{conn: conn, r: bufio.NewReader(conn), w: bufio.NewWriter(conn)}, nil
}

func (c *redisConn) Close() error { return c.conn.Close() }

func (c *redisConn) send(args ...string) error {
	if _, err := fmt.Fprintf(c.w, "*%d\r\n", len(args)); err != nil {
		return err
	}
	for _, arg := range args {
		if _, err := fmt.Fprintf(c.w, "$%d\r\n%s\r\n", len(arg), arg); err != nil {
			return err
		}
	}
	return c.w.Flush()
}

func (c *redisConn) read() (any, error) {
	prefix, err := c.r.ReadByte()
	if err != nil {
		return nil, err
	}
	switch prefix {
	case '+':
		line, err := c.readLine()
		return line, err
	case '-':
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		return nil, redisError(line)
	case ':':
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		return strconv.ParseInt(line, 10, 64)
	case '$':
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		length, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("invalid bulk length %q", line)
		}
		if length < 0 {
			return nil, nil
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return nil, err
		}
		return string(buf[:length]), nil
	case '*':
		line, err := c.readLine()
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(line)
		if err != nil {
			return nil, fmt.Errorf("invalid array length %q", line)
		}
		if count < 0 {
			return nil, nil
		}
		items := make([]any, 0, count)
		for i := 0; i < count; i++ {
			item, err := c.read()
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case '_':
		if _, err := c.readLine(); err != nil {
			return nil, err
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unsupported redis reply prefix %q", prefix)
	}
}

func (c *redisConn) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), nil
}

func redisCommand(ctx context.Context, endpoint redisEndpoint, timeout time.Duration, args ...string) (any, error) {
	conn, err := dialRedis(ctx, endpoint, timeout)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if endpoint.Password != "" {
		auth := []string{"AUTH", endpoint.Password}
		if endpoint.Username != "" {
			auth = []string{"AUTH", endpoint.Username, endpoint.Password}
		}
		reply, err := conn.request(auth...)
		if err != nil {
			return nil, fmt.Errorf("AUTH 失败: %w", err)
		}
		if _, ok := reply.(string); !ok {
			return nil, fmt.Errorf("AUTH 返回了意外的响应")
		}
	}
	return conn.request(args...)
}

func (c *redisConn) request(args ...string) (any, error) {
	if err := c.send(args...); err != nil {
		return nil, err
	}
	return c.read()
}

// redisPing checks authentication and returns server latency/version.
func redisPing(ctx context.Context, endpoint redisEndpoint) (time.Duration, string, error) {
	start := time.Now()
	conn, err := dialRedis(ctx, endpoint, 5*time.Second)
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = conn.Close() }()
	if endpoint.Password != "" {
		auth := []string{"AUTH", endpoint.Password}
		if endpoint.Username != "" {
			auth = []string{"AUTH", endpoint.Username, endpoint.Password}
		}
		if _, err := conn.request(auth...); err != nil {
			return time.Since(start), "", fmt.Errorf("AUTH 失败: %w", err)
		}
	}
	reply, err := conn.request("PING")
	if err != nil {
		return time.Since(start), "", err
	}
	if text, ok := reply.(string); !ok || !strings.EqualFold(text, "PONG") {
		return time.Since(start), "", fmt.Errorf("PING 返回了意外的响应")
	}
	latency := time.Since(start)
	version := ""
	if info, err := conn.request("INFO", "server"); err == nil {
		if text, ok := info.(string); ok {
			version = parseRedisVersion(text)
		}
	}
	return latency, version, nil
}

func parseRedisVersion(info string) string {
	for _, line := range strings.Split(info, "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "redis_version:"); ok {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// redisShutdown asks a managed Redis instance to stop. The server normally
// closes the connection without a reply, which is treated as success.
func redisShutdown(ctx context.Context, endpoint redisEndpoint) error {
	_, err := redisCommand(ctx, endpoint, 5*time.Second, "SHUTDOWN", "NOSAVE")
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// redisFlushDB clears one database index only.
func redisFlushDB(ctx context.Context, endpoint redisEndpoint, index int) error {
	if index < 0 || index > 15 {
		return fmt.Errorf("Redis DB index %d 超出允许范围", index)
	}
	conn, err := dialRedis(ctx, endpoint, 10*time.Second)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if endpoint.Password != "" {
		auth := []string{"AUTH", endpoint.Password}
		if endpoint.Username != "" {
			auth = []string{"AUTH", endpoint.Username, endpoint.Password}
		}
		if _, err := conn.request(auth...); err != nil {
			return fmt.Errorf("AUTH 失败: %w", err)
		}
	}
	if _, err := conn.request("SELECT", strconv.Itoa(index)); err != nil {
		return err
	}
	_, err = conn.request("FLUSHDB")
	return err
}
