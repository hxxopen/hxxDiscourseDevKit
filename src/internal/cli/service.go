package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

type serviceListResponse struct {
	Items []service.Status `json:"items"`
	Total int              `json:"total"`
}

type serviceActionResponse struct {
	Status *service.Status `json:"status"`
}

type serviceLogsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path,omitempty"`
	Total int      `json:"total"`
}

type databasesResponse struct {
	Items []string `json:"items"`
	Total int      `json:"total"`
}

func cmdService(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdServiceList(ctx, g)
	case "status", "show":
		return cmdServiceStatus(ctx, g, args)
	case "start", "stop", "restart":
		return cmdServiceAction(ctx, g, sub, args)
	case "test":
		return cmdServiceTest(ctx, g, args)
	case "logs":
		return cmdServiceLogs(ctx, g, args)
	case "update", "config":
		return cmdServiceUpdate(ctx, g, args)
	case "databases", "dbs":
		return cmdServiceDatabases(ctx, g)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 service 子命令 %q\n", sub)
		printServiceUsage(os.Stderr)
		return 2
	}
}

func cmdServiceList(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var list serviceListResponse
	if err := client.Get(ctx, "/api/v1/services", &list); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(list)
	}
	if len(list.Items) == 0 {
		fmt.Println("（没有服务）")
		return 0
	}
	for _, item := range list.Items {
		printServiceStatus(item)
		fmt.Println()
	}
	return 0
}

func cmdServiceStatus(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: service status 需要一个服务名（postgresql|redis）")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var status service.Status
	if err := client.Get(ctx, "/api/v1/services/"+url.PathEscape(args[0]), &status); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(status)
	}
	printServiceStatus(status)
	return 0
}

func cmdServiceAction(ctx context.Context, g Globals, action string, args []string) int {
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: service %s 需要一个服务名或 all\n", action)
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	client.HTTP.Timeout = 5 * time.Minute
	targets := []string{args[0]}
	if strings.EqualFold(args[0], "all") {
		var list serviceListResponse
		if err := client.Get(ctx, "/api/v1/services", &list); err != nil {
			return fail(err)
		}
		targets = targets[:0]
		for _, item := range list.Items {
			targets = append(targets, item.Name)
		}
	}
	results := make([]service.Status, 0, len(targets))
	for _, target := range targets {
		var result serviceActionResponse
		path := "/api/v1/services/" + url.PathEscape(target) + "/" + action
		if err := client.Post(ctx, path, nil, &result); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
			return fail(err)
		}
		if result.Status != nil {
			results = append(results, *result.Status)
			if !g.JSON {
				fmt.Printf("%s: %s", result.Status.Name, result.Status.State)
				if result.Status.Message != "" {
					fmt.Printf("（%s）", result.Status.Message)
				}
				fmt.Println()
			}
		}
	}
	if g.JSON {
		return printJSON(results)
	}
	return 0
}

func cmdServiceTest(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("service test", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "", "测试使用的模式：managed|external")
	host := fs.String("host", "", "主机名覆盖")
	port := fs.Int("port", 0, "端口覆盖")
	username := fs.String("username", "", "外部服务用户名")
	adminUser := fs.String("admin-user", "", "托管 PostgreSQL 管理员用户")
	database := fs.String("database", "", "外部数据库名")
	tlsFlag := fs.Bool("tls", false, "外部服务使用 TLS")
	passwordStdin := fs.Bool("password-stdin", false, "从标准输入读取密码（不会写入命令历史）")
	fs.Usage = func() { printServiceUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: service test 需要一个服务名")
		return 2
	}
	password, err := readServicePassword(*passwordStdin)
	if err != nil {
		return fail(err)
	}
	input := service.ConfigInput{
		Mode: *mode, Host: *host, Port: *port, Username: *username,
		AdminUser: *adminUser, Database: *database, TLS: *tlsFlag, Password: password,
	}
	var body any
	if input.Mode != "" || input.Host != "" || input.Port != 0 || input.Username != "" ||
		input.AdminUser != "" || input.Database != "" || input.TLS || input.Password != "" {
		body = input
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	client.HTTP.Timeout = time.Minute
	var result service.TestResult
	if err := client.Post(ctx, "/api/v1/services/"+url.PathEscape(fs.Arg(0))+"/test", body, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	fmt.Printf("服务:       %s（%s）\n", result.Name, result.Mode)
	fmt.Printf("地址:       %s:%d\n", result.Host, result.Port)
	fmt.Printf("网络可达:   %v\n", result.Reachable)
	fmt.Printf("认证成功:   %v\n", result.Authenticated)
	if result.ServerVersion != "" {
		fmt.Printf("服务版本:   %s\n", result.ServerVersion)
	}
	fmt.Printf("耗时:       %d ms\n", result.LatencyMS)
	if result.Message != "" {
		fmt.Printf("结果:       %s\n", result.Message)
	}
	return 0
}

func cmdServiceLogs(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("service logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tail := fs.Int("tail", 200, "显示最后 N 行")
	fs.Usage = func() { printServiceUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: service logs 需要一个服务名")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result serviceLogsResponse
	path := "/api/v1/services/" + url.PathEscape(fs.Arg(0)) + "/logs?tail=" + fmt.Sprint(*tail)
	if err := client.Get(ctx, path, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	for _, line := range result.Lines {
		fmt.Println(line)
	}
	if result.Path != "" {
		fmt.Fprintf(os.Stderr, "日志文件: %s\n", result.Path)
	}
	return 0
}

func cmdServiceUpdate(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("service update", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	mode := fs.String("mode", "", "managed|external")
	host := fs.String("host", "", "主机名")
	port := fs.Int("port", 0, "端口")
	username := fs.String("username", "", "外部服务用户名")
	adminUser := fs.String("admin-user", "", "托管 PostgreSQL 管理员用户")
	database := fs.String("database", "", "外部数据库名")
	tlsFlag := fs.Bool("tls", false, "外部服务使用 TLS")
	passwordStdin := fs.Bool("password-stdin", false, "从标准输入读取密码")
	fs.Usage = func() { printServiceUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: service update 需要一个服务名")
		return 2
	}
	password, err := readServicePassword(*passwordStdin)
	if err != nil {
		return fail(err)
	}
	input := service.ConfigInput{
		Mode: *mode, Host: *host, Port: *port, Username: *username,
		AdminUser: *adminUser, Database: *database, TLS: *tlsFlag, Password: password,
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var status service.Status
	if err := client.Do(ctx, "PATCH", "/api/v1/services/"+url.PathEscape(fs.Arg(0)), input, &status); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(status)
	}
	printServiceStatus(status)
	return 0
}

func cmdServiceDatabases(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result databasesResponse
	if err := client.Get(ctx, "/api/v1/services/postgresql/databases", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	for _, name := range result.Items {
		fmt.Println(name)
	}
	return 0
}

func readServicePassword(fromStdin bool) (string, error) {
	if value := os.Getenv("HXXDDK_SERVICE_PASSWORD"); value != "" {
		return value, nil
	}
	if !fromStdin {
		return "", nil
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 4096))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(data), "\r\n"), nil
}

func printServiceStatus(status service.Status) {
	fmt.Printf("%s (%s)\n", status.Name, status.Mode)
	fmt.Printf("  状态:     %s\n", status.State)
	fmt.Printf("  地址:     %s:%d\n", status.Host, status.Port)
	fmt.Printf("  网络可达: %v\n", status.Reachable)
	fmt.Printf("  认证成功: %v\n", status.Authenticated)
	if status.Version != "" {
		fmt.Printf("  版本:     %s\n", status.Version)
	}
	if status.Execution != "" {
		fmt.Printf("  执行环境: %s\n", status.Execution)
	}
	if status.Path != "" {
		fmt.Printf("  可执行:   %s\n", status.Path)
	}
	if status.DataDir != "" {
		fmt.Printf("  数据目录: %s\n", status.DataDir)
	}
	if status.LogFile != "" {
		fmt.Printf("  日志:     %s\n", status.LogFile)
	}
	if status.PID > 0 {
		fmt.Printf("  PID:      %d\n", status.PID)
	}
	if status.Message != "" {
		fmt.Printf("  说明:     %s\n", status.Message)
	}
	if len(status.Dependencies) > 0 {
		fmt.Printf("  依赖:     %s\n", strings.Join(status.Dependencies, ", "))
	}
}

func printServiceUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk service <子命令>

  list                              列出服务状态
  status <postgresql|redis>         查看服务详情
  start|stop|restart <name|all>     管理服务
  test <name> [flags]               测试连接
      --mode managed|external
      --host HOST --port N
      --username USER --database DB --tls
      --admin-user USER
      --password-stdin              从标准输入读取密码（也可用 HXXDDK_SERVICE_PASSWORD）
  update <name> --mode --port ...   切换 managed/external 或更新外部端点
  logs <name> [--tail N]            查看托管服务日志
  databases                         列出 PostgreSQL 数据库
`)
}

func printRuntimeUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk runtime <子命令>

  list                验证并列出全部 Runtime 组件
  verify [name]       验证一个或全部组件
  wsl                 查看 WSL2 执行环境
  discover            重新探测组件路径并写入发现文件
  install [--yes] [component...]
                      预览并安装组件到 WSL2（未加 --yes 仅预览）
`)
}
