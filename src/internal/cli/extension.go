package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/themepreview"
)

func cmdExtension(ctx context.Context, g Globals, kind extension.Kind, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdExtensionList(ctx, g, kind, args)
	case "create", "new":
		return cmdExtensionCreate(ctx, g, kind, args)
	case "package", "pack":
		return cmdExtensionPackage(ctx, g, kind, args)
	case "check", "verify":
		return cmdExtensionCheck(ctx, g, kind, args)
	case "build":
		if kind != extension.KindTheme {
			fmt.Fprintf(os.Stderr, "hxxddk: %s不支持构建命令；请直接打包\n", extensionLabel(kind))
			return 2
		}
		return cmdExtensionBuild(ctx, g, kind, args)
	case "preview":
		if kind != extension.KindTheme {
			fmt.Fprintf(os.Stderr, "hxxddk: 只有主题支持预览\n")
			return 2
		}
		return cmdExtensionPreview(ctx, g, kind, args)
	case "test":
		if kind != extension.KindPlugin {
			fmt.Fprintf(os.Stderr, "hxxddk: %s不支持自动化测试\n", extensionLabel(kind))
			return 2
		}
		return cmdExtensionTest(ctx, g, args)
	case "enable", "disable":
		if kind != extension.KindPlugin {
			fmt.Fprintf(os.Stderr, "hxxddk: %s不支持启用/禁用，请使用 Rails 主题工作流管理\n", extensionLabel(kind))
			return 2
		}
		return cmdExtensionToggle(ctx, g, kind, sub == "enable", args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 %s 子命令 %q\n", kind, sub)
		printExtensionUsage(os.Stderr, kind)
		return 2
	}
}

func extensionFlags(kind extension.Kind, name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(string(kind)+" "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ref := fs.String("workspace", "", "Workspace 名称或 ID（默认活动 Workspace）")
	fs.Usage = func() { printExtensionUsage(os.Stderr, kind) }
	return fs, ref
}

func cmdExtensionList(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "list")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var result extension.Listing
	path := "/api/v1/workspaces/" + url.PathEscape(item.WorkspaceID) + "/" + string(kind) + "s"
	if err := client.Get(ctx, path, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	if len(result.Items) == 0 {
		fmt.Printf("（没有%s）\n", extensionLabel(kind))
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if kind == extension.KindPlugin {
		fmt.Fprintln(w, "SLUG\t名称\t状态\t版本\t作者\t文件\t更新时间\t元数据")
	} else {
		fmt.Fprintln(w, "SLUG\t名称\t版本\t作者\t文件\t更新时间\t元数据")
	}
	for _, item := range result.Items {
		updated := item.ModifiedAt.Local().Format("2006-01-02 15:04")
		metadata := "缺失"
		if item.HasMetadata {
			metadata = "正常"
		}
		if kind == extension.KindPlugin {
			state := "已启用"
			if !item.Enabled {
				state = "已禁用"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
				item.Slug, item.Name, state, dash(item.Version), dash(item.Authors),
				item.FileCount, updated, metadata)
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			item.Slug, item.Name, dash(item.Version), dash(item.Authors),
			item.FileCount, updated, metadata)
	}
	_ = w.Flush()
	fmt.Printf("\n目录: %s\n", result.Root)
	if kind == extension.KindPlugin && !result.SourceInstalled {
		fmt.Println("提示: 当前 Workspace 尚未安装 Discourse 源码。")
	}
	return 0
}

func cmdExtensionCreate(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "create")
	slug := fs.String("slug", "", "目录名（默认根据名称生成）")
	about := fs.String("about", "", "描述")
	version := fs.String("version", "", "版本（默认 0.1.0）")
	authors := fs.String("authors", "", "作者（默认 DevKit）")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: %s create 需要一个名称\n", kind)
		return 2
	}
	name := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	body := extension.CreateRequest{
		Name:    name,
		Slug:    *slug,
		About:   *about,
		Version: *version,
		Authors: *authors,
	}
	var created extension.Item
	path := "/api/v1/workspaces/" + url.PathEscape(item.WorkspaceID) + "/" + string(kind) + "s"
	if err := client.Post(ctx, path, body, &created); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(created)
	}
	fmt.Printf("已创建%s %s（%s）\n", extensionLabel(kind), created.Name, created.Slug)
	fmt.Printf("目录: %s\n", created.Path)
	if kind == extension.KindPlugin {
		fmt.Println("提示: 重启 Rails 开发服务器后 Discourse 才会加载该插件。")
	}
	return 0
}

func cmdExtensionPackage(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "package")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: %s package 需要一个目录名\n", kind)
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var pkg extension.Package
	path := "/api/v1/workspaces/" + url.PathEscape(item.WorkspaceID) + "/" + string(kind) + "s/" +
		url.PathEscape(slug) + "/package"
	if err := client.Post(ctx, path, nil, &pkg); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(pkg)
	}
	fmt.Printf("已打包%s %s\n", extensionLabel(kind), pkg.Name)
	fmt.Printf("文件: %s\n", pkg.Path)
	fmt.Printf("大小: %s（%d 个文件）\n", humanBytes(pkg.SizeBytes), pkg.FileCount)
	fmt.Printf("SHA-256: %s\n", pkg.SHA256)
	return 0
}

func cmdExtensionCheck(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "check")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: %s check 需要一个目录名\n", kind)
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	workspace, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var report extension.CheckReport
	path := "/api/v1/workspaces/" + url.PathEscape(workspace.WorkspaceID) + "/" + string(kind) + "s/" +
		url.PathEscape(slug) + "/check"
	if err := client.Get(ctx, path, &report); err != nil {
		return fail(err)
	}
	if g.JSON {
		code := printJSON(report)
		if code != 0 {
			return code
		}
		if !report.Ready {
			return 1
		}
		return 0
	}
	printCheckReport(kind, report)
	if !report.Ready {
		return 1
	}
	return 0
}

func printCheckReport(kind extension.Kind, report extension.CheckReport) {
	state := "就绪"
	if !report.Ready {
		state = "需要处理"
	}
	fmt.Printf("%s依赖检查：%s（%s）\n", extensionLabel(kind), report.Name, report.Slug)
	fmt.Printf("状态: %s（%d 个错误，%d 个提醒）\n", state, report.ErrorCount, report.WarningCount)
	if report.DiscourseVersion != "" {
		fmt.Printf("Discourse 版本: %s\n", report.DiscourseVersion)
	}
	if report.RequiredVersion != "" {
		fmt.Printf("最低版本要求: %s\n", report.RequiredVersion)
	}
	if report.MaximumVersion != "" {
		fmt.Printf("最高版本限制: %s\n", report.MaximumVersion)
	}
	fmt.Println()
	for _, finding := range report.Findings {
		fmt.Printf("[%s] %s: %s\n", findingStatusLabel(finding.Status), finding.Title, finding.Message)
		if finding.Fix != "" {
			fmt.Printf("       处理: %s\n", finding.Fix)
		}
	}
}

func cmdExtensionBuild(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "build")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: theme build 需要一个目录名")
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	workspace, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var result extension.BuildResult
	path := "/api/v1/workspaces/" + url.PathEscape(workspace.WorkspaceID) + "/themes/" +
		url.PathEscape(slug) + "/build"
	if err := client.Post(ctx, path, nil, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		code := printJSON(result)
		if code != 0 {
			return code
		}
		if !result.Ready {
			return 1
		}
		return 0
	}
	if result.Check != nil {
		printCheckReport(kind, *result.Check)
	}
	if !result.Ready || result.Package == nil {
		fmt.Printf("\n构建未生成分发包：%s\n", result.Message)
		return 1
	}
	fmt.Printf("\n构建完成：%s\n", result.Package.FileName)
	fmt.Printf("文件: %s\n", result.Package.Path)
	fmt.Printf("大小: %s（%d 个文件）\n", humanBytes(result.Package.SizeBytes), result.Package.FileCount)
	fmt.Printf("SHA-256: %s\n", result.Package.SHA256)
	return 0
}

func cmdExtensionPreview(ctx context.Context, g Globals, kind extension.Kind, args []string) int {
	fs, ref := extensionFlags(kind, "preview")
	apiKey := fs.String("api-key", "", "Discourse 管理员 API Key（默认读取 DISCOURSE_API_KEY）")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: theme preview 需要一个目录名")
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	workspace, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	body := map[string]string{}
	key := strings.TrimSpace(*apiKey)
	if key == "" {
		key = strings.TrimSpace(os.Getenv("DISCOURSE_API_KEY"))
	}
	if key != "" {
		body["api_key"] = key
	}
	var result themepreview.Result
	path := "/api/v1/workspaces/" + url.PathEscape(workspace.WorkspaceID) + "/themes/" +
		url.PathEscape(slug) + "/preview"
	if err := client.Post(ctx, path, body, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		code := printJSON(result)
		if code != 0 {
			return code
		}
		if result.Build == nil || !result.Build.Ready {
			return 1
		}
		return 0
	}
	if result.Build != nil && result.Build.Check != nil {
		printCheckReport(kind, *result.Build.Check)
	}
	if result.Build == nil || !result.Build.Ready {
		message := "主题构建未通过，未导入预览。"
		if result.Build != nil && result.Build.Message != "" {
			message = result.Build.Message
		}
		fmt.Printf("\n%s\n", message)
		return 1
	}
	fmt.Printf("\n已导入预览主题（ID %d，%s）\n", result.ThemeID, result.Action)
	fmt.Printf("预览: %s\n", result.PreviewURL)
	fmt.Printf("管理: %s\n", result.AdminURL)
	return 0
}

type pluginTestResult struct {
	Task     task.Task `json:"task"`
	LogLines []string  `json:"log_lines"`
	LogPath  string    `json:"log_path,omitempty"`
}

func cmdExtensionTest(ctx context.Context, g Globals, args []string) int {
	fs, ref := extensionFlags(extension.KindPlugin, "test")
	tail := fs.Int("tail", 80, "测试结束后显示的日志行数")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: plugin test 需要一个目录名")
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	workspace, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var accepted taskAccepted
	path := "/api/v1/workspaces/" + url.PathEscape(workspace.WorkspaceID) + "/plugins/" +
		url.PathEscape(slug) + "/test"
	if err := client.Post(ctx, path, nil, &accepted); err != nil {
		return fail(err)
	}
	if accepted.TaskID == "" {
		fmt.Fprintln(os.Stderr, "错误: API 未返回任务 ID")
		return 1
	}
	final, err := waitTask(ctx, client, accepted.TaskID, g.JSON)
	if err != nil {
		return fail(err)
	}

	logs, logPath := fetchPluginTestLogs(ctx, client, workspace.WorkspaceID, *tail)
	if g.JSON {
		if code := printJSON(pluginTestResult{Task: final, LogLines: logs, LogPath: logPath}); code != 0 {
			return code
		}
	} else {
		if final.Status == task.StatusSucceeded {
			fmt.Printf("插件 %s 测试通过。\n", slug)
		} else {
			message := final.ErrorMessage
			if message == "" {
				message = final.Message
			}
			fmt.Fprintf(os.Stderr, "插件 %s 测试失败: %s\n", slug, message)
		}
		printPluginTestLogs(logs, logPath, final.Status != task.StatusSucceeded)
	}
	if final.Status != task.StatusSucceeded {
		return 1
	}
	return 0
}

func fetchPluginTestLogs(ctx context.Context, client *Client, workspaceID string, tail int) ([]string, string) {
	query := url.Values{}
	query.Set("source", "workspace:"+workspaceID+":test")
	query.Set("tail", fmt.Sprint(tail))
	var result projectLogsResponse
	if err := client.Get(ctx, "/api/v1/workspaces/"+url.PathEscape(workspaceID)+
		"/logs?"+query.Encode(), &result); err != nil {
		return nil, ""
	}
	return result.Lines, result.Path
}

func printPluginTestLogs(lines []string, path string, stderr bool) {
	if len(lines) == 0 {
		return
	}
	out := os.Stdout
	if stderr {
		out = os.Stderr
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "—— 插件测试日志 ——")
	if path != "" {
		fmt.Fprintf(out, "日志文件: %s\n", path)
	}
	for _, line := range lines {
		fmt.Fprintln(out, line)
	}
}

func findingStatusLabel(status string) string {
	switch status {
	case extension.SeverityOK:
		return "正常"
	case extension.SeverityWarning:
		return "提醒"
	case extension.SeverityError:
		return "错误"
	default:
		return "跳过"
	}
}

func cmdExtensionToggle(ctx context.Context, g Globals, kind extension.Kind, enabled bool, args []string) int {
	action, verb := "disable", "禁用"
	if enabled {
		action, verb = "enable", "启用"
	}
	fs, ref := extensionFlags(kind, action)
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: plugin %s 需要一个目录名\n", action)
		return 2
	}
	slug := fs.Arg(0)
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	workspace, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var item extension.Item
	path := "/api/v1/workspaces/" + url.PathEscape(workspace.WorkspaceID) + "/plugins/" +
		url.PathEscape(slug) + "/" + action
	if err := client.Post(ctx, path, nil, &item); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(item)
	}
	fmt.Printf("已%s插件 %s（%s）\n", verb, item.Name, item.Slug)
	fmt.Printf("目录: %s\n", item.Path)
	fmt.Println("提示: 重启 Rails 开发服务器后变更才会生效。")
	return 0
}

func extensionLabel(kind extension.Kind) string {
	if kind == extension.KindPlugin {
		return "插件"
	}
	return "主题"
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printExtensionUsage(w io.Writer, kind extension.Kind) {
	label := extensionLabel(kind)
	fmt.Fprintf(w, `用法: hxxddk %s <子命令> [--workspace NAME]

  list                         列出%s
  create <name> [flags]        创建%s脚手架
      --slug NAME              目录名（默认根据名称生成）
      --about TEXT             描述
      --version VERSION        版本（默认 0.1.0）
      --authors TEXT           作者（默认 DevKit）
  package <slug>               生成 ZIP 分发包（保存到 data/exports/）
  check <slug>                 检查元数据、依赖与 Discourse 版本兼容性
                               （发现错误时退出码为 1）

未指定 --workspace 时使用活动 Workspace。
`, kind, label, label)
	if kind == extension.KindTheme {
		fmt.Fprint(w, `
主题额外子命令：

  build <slug>                 检查并构建主题 ZIP（检查有错误时不打包）
  preview <slug> [--api-key KEY]
                               构建并导入运行中的本地 Discourse，返回预览链接
                               API Key 也可通过 DISCOURSE_API_KEY 环境变量提供
                               未显式提供时使用设置页保存的 Key
`)
	}
	if kind == extension.KindPlugin {
		fmt.Fprint(w, `
插件额外子命令：

  test <slug> [--tail N]       运行插件 RSpec 测试（隔离测试数据库）
                               测试结束后显示 N 行插件测试日志
  enable <slug>                启用插件（移回源码 plugins/，需要先停止项目）
  disable <slug>               禁用插件（移到 data/disabled-plugins/，需要先停止项目）
`)
	}
}
