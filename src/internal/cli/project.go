package cli

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type projectResponse struct {
	Workspace *workspace.Workspace `json:"workspace"`
	Project   *project.Status      `json:"project"`
}

type projectLogsResponse struct {
	Lines []string `json:"lines"`
	Path  string   `json:"path,omitempty"`
	Total int      `json:"total"`
}

func cmdProject(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "status", "show":
		return cmdProjectStatus(ctx, g, args)
	case "source":
		return cmdProjectSource(ctx, g, args)
	case "clone", "fetch", "pull", "init":
		return cmdProjectAction(ctx, g, sub, args)
	case "checkout":
		return cmdProjectCheckout(ctx, g, args)
	case "logs", "log":
		return cmdProjectLogs(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 project 子命令 %q\n", sub)
		printProjectUsage(os.Stderr)
		return 2
	}
}

func projectFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("project "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ref := fs.String("workspace", "", "Workspace 名称或 ID（默认活动 Workspace）")
	fs.Usage = func() { printProjectUsage(os.Stderr) }
	return fs, ref
}

func cmdProjectStatus(ctx context.Context, g Globals, args []string) int {
	fs, ref := projectFlags("status")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "hxxddk: project status 不接受位置参数 %v\n", fs.Args())
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
	var result projectResponse
	if err := client.Get(ctx,
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/project", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	if result.Project == nil {
		fmt.Println("（API 未返回项目状态）")
		return 0
	}
	printProjectStatus(result.Project)
	return 0
}

func printProjectStatus(status *project.Status) {
	src := status.Source
	fmt.Printf("Workspace %q 项目状态\n", status.Name)
	if src.Configured {
		fmt.Printf("  仓库:     %s\n", src.RepositoryURL)
		selection := src.Branch
		if selection == "" {
			selection = src.Tag
		}
		if selection == "" {
			selection = src.Commit
		}
		fmt.Printf("  选择:     %s\n", orDash(selection))
	} else {
		fmt.Println("  仓库:     未配置")
	}
	fmt.Printf("  源码:     %s\n", boolLabel(src.Installed, "已检出", "未检出"))
	fmt.Printf("  HEAD:     %s\n", orDash(shortSHA(src.HeadCommit)))
	if src.IsGit {
		fmt.Printf("  分支:     %s\n", orDash(src.CurrentBranch))
		fmt.Printf("  工作区:   %s", boolLabel(src.Dirty, "有未提交修改", "干净"))
		if src.Dirty {
			fmt.Printf("（%d 个文件）", src.ChangedFiles)
		}
		fmt.Printf("，领先 %d，落后 %d\n", src.Ahead, src.Behind)
	}
	if src.Error != "" {
		fmt.Printf("  Git 错误: %s\n", src.Error)
	}

	proc := status.Process
	fmt.Printf("  初始化:   %s\n", boolLabel(proc.Initialized, "已完成", "未完成"))
	fmt.Printf("  进程:     %s", proc.ProcessState)
	if proc.Alive {
		fmt.Printf("（PID %d）", proc.PID)
	}
	fmt.Println()
	fmt.Printf("  访问:     %s\n", orDash(proc.HTTPURL))
	if proc.LastError != "" {
		fmt.Printf("  最近错误: %s\n", proc.LastError)
	}
	if len(status.Checks) > 0 {
		fmt.Println("  检查:")
		for _, check := range status.Checks {
			mark := "OK  "
			if !check.OK {
				mark = "FAIL"
			}
			line := fmt.Sprintf("    [%s] %s", mark, check.Name)
			if check.Message != "" {
				line += ": " + check.Message
			}
			fmt.Println(line)
		}
	}
}

func cmdProjectSource(ctx context.Context, g Globals, args []string) int {
	fs, ref := projectFlags("source")
	repo := fs.String("repo", "", "仓库地址")
	branch := fs.String("branch", "", "跟踪分支")
	tag := fs.String("tag", "", "标签")
	commit := fs.String("commit", "", "提交")
	shallow := fs.Bool("shallow", false, "使用浅克隆")
	noShallow := fs.Bool("no-shallow", false, "关闭浅克隆")
	submodules := fs.Bool("submodules", false, "包含子模块")
	noSubmodules := fs.Bool("no-submodules", false, "不包含子模块")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "hxxddk: project source 不接受位置参数 %v\n", fs.Args())
		return 2
	}
	input := project.SourceInput{
		RepositoryURL: *repo,
		Branch:        *branch,
		Tag:           *tag,
		Commit:        *commit,
	}
	if *shallow && *noShallow {
		fmt.Fprintln(os.Stderr, "hxxddk: --shallow 与 --no-shallow 不能同时使用")
		return 2
	}
	if *submodules && *noSubmodules {
		fmt.Fprintln(os.Stderr, "hxxddk: --submodules 与 --no-submodules 不能同时使用")
		return 2
	}
	if *shallow || *noShallow {
		value := *shallow
		input.Shallow = &value
	}
	if *submodules || *noSubmodules {
		value := *submodules
		input.Submodules = &value
	}

	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := currentWorkspace(ctx, client, *ref)
	if err != nil {
		return fail(err)
	}
	var updated workspace.Workspace
	if err := client.Do(ctx, "PUT",
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/project/source", input, &updated); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(updated)
	}
	fmt.Printf("已更新 Workspace %q 的源码配置。\n", updated.Name)
	if updated.Source.RepositoryURL != "" {
		fmt.Printf("  仓库: %s\n", updated.Source.RepositoryURL)
	}
	return 0
}

func cmdProjectAction(ctx context.Context, g Globals, action string, args []string) int {
	fs, ref := projectFlags(action)
	force := fs.Bool("force", false, "允许覆盖本地修改（pull）")
	skipDeps := fs.Bool("skip-deps", false, "跳过依赖安装（init）")
	skipDB := fs.Bool("skip-db", false, "跳过数据库准备（init）")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "hxxddk: project %s 不接受位置参数 %v\n", action, fs.Args())
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

	var body any
	switch action {
	case "pull":
		body = map[string]bool{"force": *force}
	case "init":
		payload := map[string]bool{}
		if *skipDeps {
			payload["install_dependencies"] = false
		}
		if *skipDB {
			payload["prepare_database"] = false
		}
		body = payload
	}
	return submitAndWait(ctx, g, client, "/api/v1/workspaces/"+
		url.PathEscape(item.WorkspaceID)+"/project/"+action, body, action)
}

func cmdProjectCheckout(ctx context.Context, g Globals, args []string) int {
	fs, ref := projectFlags("checkout")
	force := fs.Bool("force", false, "丢弃本地修改")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: project checkout 需要一个分支、标签或提交")
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
	body := project.CheckoutInput{Ref: fs.Arg(0), Force: *force}
	return submitAndWait(ctx, g, client, "/api/v1/workspaces/"+
		url.PathEscape(item.WorkspaceID)+"/project/checkout", body, "checkout")
}

func cmdProjectLogs(ctx context.Context, g Globals, args []string) int {
	fs, ref := projectFlags("logs")
	source := fs.String("source", "project", "日志来源 project、init、git 或 test")
	tail := fs.Int("tail", 200, "显示行数")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "hxxddk: project logs 不接受位置参数 %v\n", fs.Args())
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
	query := url.Values{}
	query.Set("source", *source)
	query.Set("tail", strconv.Itoa(*tail))
	var result projectLogsResponse
	if err := client.Get(ctx, "/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+
		"/logs?"+query.Encode(), &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	if result.Path != "" {
		fmt.Fprintf(os.Stderr, "日志文件: %s\n", result.Path)
	}
	for _, line := range result.Lines {
		fmt.Println(line)
	}
	return 0
}

// submitAndWait submits an asynchronous project task and polls until it ends.
func submitAndWait(ctx context.Context, g Globals, client *Client,
	path string, body any, label string) int {
	var accepted taskAccepted
	if err := client.Post(ctx, path, body, &accepted); err != nil {
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
	if g.JSON {
		if code := printJSON(final); code != 0 {
			return code
		}
	}
	switch final.Status {
	case task.StatusSucceeded:
		if !g.JSON {
			if final.Message != "" && final.Message != "任务完成" {
				fmt.Println(final.Message)
			} else {
				fmt.Printf("%s 完成\n", label)
			}
		}
		return 0
	case task.StatusCanceled:
		if !g.JSON {
			fmt.Fprintf(os.Stderr, "%s 已取消\n", label)
		}
		return 1
	default:
		if !g.JSON {
			fmt.Fprintf(os.Stderr, "%s 失败: %s\n", label, final.ErrorMessage)
		}
		return 1
	}
}

func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}
	return sha[:12]
}

func boolLabel(value bool, yes, no string) string {
	if value {
		return yes
	}
	return no
}
