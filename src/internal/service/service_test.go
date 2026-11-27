package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func TestParsePID(t *testing.T) {
	cases := map[string]int{
		"pg_ctl: server is running (PID: 4321)\n/usr/lib/postgresql/16/bin/postgres": 4321,
		"pg_ctl: no server running": 0,
		"":                          0,
	}
	for input, want := range cases {
		if got := parsePID(input); got != want {
			t.Errorf("parsePID(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestSQLQuoting(t *testing.T) {
	if got := quoteIdent(`a"b`); got != `"a""b"` {
		t.Errorf("quoteIdent = %q", got)
	}
	if got := quoteLiteral("p'a"); got != "'p''a'" {
		t.Errorf("quoteLiteral = %q", got)
	}
	ws := &workspace.Workspace{Database: workspace.DatabaseConfig{
		DatabaseName: `db"name`,
		Username:     "user_name",
	}}
	sql := ensureWorkspaceSQL(ws, "p'ass")
	if !strings.Contains(sql, `"db""name"`) {
		t.Error("database identifier was not quoted")
	}
	if !strings.Contains(sql, `'p''ass'`) {
		t.Error("password literal was not quoted")
	}
	// quote_ident takes a text expression, so the names must be SQL literals;
	// unquoted names are parsed as column references and fail at runtime.
	if !strings.Contains(sql, `quote_ident('db"name')`) {
		t.Error("database name is not passed to quote_ident as a literal")
	}
	if !strings.Contains(sql, `quote_ident('user_name')`) {
		t.Error("role name is not passed to quote_ident as a literal")
	}
	if !strings.Contains(sql, `ALTER ROLE "user_name" WITH LOGIN PASSWORD 'p''ass'`) {
		t.Error("role password update missing")
	}
	if !strings.Contains(sql, `REVOKE CREATE ON SCHEMA public FROM PUBLIC`) {
		t.Error("public schema hardening missing")
	}
}

func TestRedisConfValue(t *testing.T) {
	got := redisConfValue("a\"b\\c\nd")
	if got != `a\"b\\cd` {
		t.Errorf("redisConfValue = %q", got)
	}
}

func TestTailLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	content := "one\ntwo\nthree\nfour\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	lines, err := tailLines(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, ",") != "three,four" {
		t.Fatalf("tailLines = %v", lines)
	}
}

func TestRedisPingAgainstFakeServer(t *testing.T) {
	host, port, _ := startFakeRedis(t, "secret")
	endpoint := redisEndpoint{Host: host, Port: port, Password: "secret"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	latency, version, err := redisPing(ctx, endpoint)
	if err != nil {
		t.Fatalf("redisPing: %v", err)
	}
	if version != "7.2.4" {
		t.Errorf("version = %q", version)
	}
	if latency < 0 {
		t.Errorf("latency = %v", latency)
	}

	wrong := endpoint
	wrong.Password = "wrong"
	if _, _, err := redisPing(ctx, wrong); err == nil {
		t.Fatal("expected AUTH failure")
	}
}

func TestRedisFlushAndShutdown(t *testing.T) {
	host, port, _ := startFakeRedis(t, "secret")
	endpoint := redisEndpoint{Host: host, Port: port, Password: "secret"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := redisFlushDB(ctx, endpoint, 7); err != nil {
		t.Fatalf("redisFlushDB: %v", err)
	}
	if err := redisFlushDB(ctx, endpoint, 16); err == nil {
		t.Fatal("expected index validation error")
	}
	if err := redisShutdown(ctx, endpoint); err != nil {
		t.Fatalf("redisShutdown: %v", err)
	}
}

func TestPgEnvNeverOmitsConnectionSettings(t *testing.T) {
	m := &Manager{}
	env := strings.Join(m.pgEnv(pgEndpoint{Password: "pw", TLS: true}), "\n")
	for _, want := range []string{"PGPASSWORD=pw", "PGSSLMODE=require", "PGCONNECT_TIMEOUT=6"} {
		if !strings.Contains(env, want) {
			t.Errorf("pgEnv missing %q: %s", want, env)
		}
	}
}

// startFakeRedis serves just enough RESP to exercise the client.
func startFakeRedis(t *testing.T, password string) (string, int, func()) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleFakeRedis(conn, password)
		}
	}()
	addr := listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", addr.Port, func() { _ = listener.Close() }
}

func handleFakeRedis(conn net.Conn, password string) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	for {
		args, err := readRESPCommand(reader)
		if err != nil {
			return
		}
		if len(args) == 0 {
			return
		}
		switch strings.ToUpper(args[0]) {
		case "AUTH":
			if password != "" && len(args) > 0 && args[len(args)-1] == password {
				writeRESPLine(writer, "+OK")
			} else {
				writeRESPLine(writer, "-ERR invalid password")
			}
		case "PING":
			writeRESPLine(writer, "+PONG")
		case "INFO":
			info := "# Server\r\nredis_version:7.2.4\r\nprocess_id:1234\r\n"
			writeRESPLine(writer, "$"+strconv.Itoa(len(info)))
			_, _ = writer.WriteString(info + "\r\n")
		case "SELECT", "FLUSHDB":
			writeRESPLine(writer, "+OK")
		case "SHUTDOWN":
			return
		default:
			writeRESPLine(writer, "-ERR unsupported")
		}
		if err := writer.Flush(); err != nil {
			return
		}
	}
}

func readRESPCommand(reader *bufio.Reader) ([]string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("unexpected RESP line %q", line)
	}
	count, err := strconv.Atoi(strings.TrimSpace(line[1:]))
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(header, "$") {
			return nil, fmt.Errorf("unexpected bulk header %q", header)
		}
		length, err := strconv.Atoi(strings.TrimSpace(header[1:]))
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length+2)
		if _, err := io.ReadFull(reader, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:length]))
	}
	return args, nil
}

func writeRESPLine(writer *bufio.Writer, line string) {
	_, _ = writer.WriteString(line + "\r\n")
}

func TestValidatePostgresDataDirRejectsDrvFSMount(t *testing.T) {
	hostPath := `D:\dev\devkit\services\postgresql\data`
	execPath := "/mnt/d/dev/devkit/services/postgresql/data"

	err := validatePostgresDataDir("wsl:Ubuntu-24.04", hostPath, execPath)
	if !errors.Is(err, ErrDataDirUnsupported) {
		t.Fatalf("expected ErrDataDirUnsupported, got %v", err)
	}
	for _, want := range []string{hostPath, execPath, "0700", "storage.data_root"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error message missing %q: %s", want, err)
		}
	}

	if err := validatePostgresDataDir("wsl:Ubuntu-24.04", `\\wsl.localhost\Ubuntu-24.04\home\dev\.local\share\devkit`,
		"/home/dev/.local/share/devkit"); err != nil {
		t.Fatalf("WSL-native path rejected: %v", err)
	}
	if err := validatePostgresDataDir("native", hostPath, `D:\dev\devkit\services\postgresql\data`); err != nil {
		t.Fatalf("native path rejected: %v", err)
	}
}

func TestIsWindowsDriveMount(t *testing.T) {
	cases := map[string]bool{
		"/mnt/d/dev":  true,
		"/mnt/D":      true,
		"/mnt/wsl":    false,
		"/home/dev":   false,
		"/mnt/d":      true,
		"/mnt":        false,
		"/mnt//dev":   false,
		"/mnt/dx/dev": false,
	}
	for input, want := range cases {
		if got := isWindowsDriveMount(input); got != want {
			t.Errorf("isWindowsDriveMount(%q) = %v, want %v", input, got, want)
		}
	}
}

func TestEnsureWorkspaceTestDatabaseSQLUsesIsolatedDatabase(t *testing.T) {
	ws := &workspace.Workspace{
		Database: workspace.DatabaseConfig{
			DatabaseName: "hxxddk_12345678_demo",
			Username:     "hxxddk_12345678_demo",
		},
	}
	database := ws.TestDatabaseName()
	if database != "hxxddk_12345678_demo_test" {
		t.Fatalf("TestDatabaseName = %q", database)
	}
	sql := ensureWorkspaceTestDatabaseSQL(ws, database, `p'ass"word`)
	for _, fragment := range []string{
		`CREATE ROLE "hxxddk_12345678_demo" LOGIN PASSWORD 'p''ass"word';`,
		`quote_ident('hxxddk_12345678_demo_test')`,
		`quote_ident('hxxddk_12345678_demo')`,
		`\connect hxxddk_12345678_demo_test`,
		`GRANT CONNECT, TEMPORARY ON DATABASE "hxxddk_12345678_demo_test" TO "hxxddk_12345678_demo";`,
	} {
		if !strings.Contains(sql, fragment) {
			t.Fatalf("test database SQL missing %q:\n%s", fragment, sql)
		}
	}
	if strings.Contains(sql, "\n\\connect hxxddk_12345678_demo\n") {
		t.Fatalf("test database SQL must not connect to the development database:\n%s", sql)
	}
}
