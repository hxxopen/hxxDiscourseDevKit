package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/diagnostics"
	"github.com/hxx/hxxDiscourseDevKit/internal/platform"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/version"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

type infoResponse struct {
	Version           version.Info    `json:"version"`
	InstanceID        string          `json:"instance_id"`
	Root              string          `json:"root"`
	APIAddr           string          `json:"api_addr"`
	StartedAt         time.Time       `json:"started_at"`
	Platform          platform.Info   `json:"platform"`
	ActiveWorkspaceID string          `json:"active_workspace_id"`
	WorkspaceCount    int             `json:"workspace_count"`
	Capabilities      map[string]bool `json:"capabilities"`
}

type wsListResponse struct {
	Items             []*workspace.Workspace `json:"items"`
	Total             int                    `json:"total"`
	ActiveWorkspaceID string                 `json:"active_workspace_id"`
}

type wsStatusResponse struct {
	Workspace *workspace.Workspace `json:"workspace"`
	Status    *workspace.Status    `json:"status"`
}

type diagnosticsResponse struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Items       []diagnostics.Check `json:"items"`
	Counts      map[string]int      `json:"counts"`
}

type activateResponse struct {
	ActiveWorkspaceID string               `json:"active_workspace_id"`
	Workspace         *workspace.Workspace `json:"workspace"`
}

type taskAccepted struct {
	TaskID    string `json:"task_id"`
	StatusURL string `json:"status_url"`
}

func cmdStatus(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var info infoResponse
	if err := client.Get(ctx, "/api/v1/system/info", &info); err != nil {
		return fail(err)
	}
	var list wsListResponse
	if err := client.Get(ctx, "/api/v1/workspaces", &list); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(struct {
			Info       infoResponse   `json:"info"`
			Workspaces wsListResponse `json:"workspaces"`
		}{Info: info, Workspaces: list})
	}

	activeName := ""
	for _, item := range list.Items {
		if item.WorkspaceID == list.ActiveWorkspaceID {
			activeName = item.Name
		}
	}
	if activeName == "" {
		activeName = "（未选择）"
	}

	fmt.Printf("hxxDiscourseDevKit %s\n", info.Version.Version)
	fmt.Printf("根目录:         %s\n", info.Root)
	fmt.Printf("API:            http://%s\n", info.APIAddr)
	fmt.Printf("实例:           %s\n", info.InstanceID)
	fmt.Printf("平台:           %s/%s (%s)\n", info.Platform.OS, info.Platform.Arch, info.Platform.ArchFamily)
	fmt.Printf("活动 Workspace: %s\n", activeName)
	fmt.Printf("Workspace 数:   %d\n\n", list.Total)

	if len(list.Items) == 0 {
		fmt.Println("（没有 Workspace）")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tHTTP\tDATABASE\tREDIS_DB\tACTIVE")
	for _, item := range list.Items {
		active := ""
		if item.WorkspaceID == list.ActiveWorkspaceID {
			active = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%s\n",
			item.Name, item.State, item.Network.HTTPPort,
			item.Database.DatabaseName, item.Redis.DBIndex, active)
	}
	_ = w.Flush()
	return 0
}

func cmdDoctor(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var report diagnosticsResponse
	if err := client.Get(ctx, "/api/v1/diagnostics", &report); err != nil {
		return fail(err)
	}
	exit := 0
	if report.Counts[string(diagnostics.SeverityError)] > 0 {
		exit = 1
	}
	if g.JSON {
		if code := printJSON(report); code != 0 {
			return code
		}
		return exit
	}
	for _, item := range report.Items {
		fmt.Printf("[%-5s] %s: %s\n", severityLabel(item.Severity), item.Title, item.Message)
		if item.FixAction != "" {
			fmt.Printf("        建议: %s\n", item.FixAction)
		}
	}
	fmt.Printf("\n%d 个错误, %d 个警告, %d 条信息\n",
		report.Counts[string(diagnostics.SeverityError)],
		report.Counts[string(diagnostics.SeverityWarning)],
		report.Counts[string(diagnostics.SeverityInfo)])
	return exit
}

func cmdWorkspace(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdWorkspaceList(ctx, g)
	case "create", "new":
		return cmdWorkspaceCreate(ctx, g, args)
	case "use", "activate":
		return cmdWorkspaceUse(ctx, g, args)
	case "inspect", "show":
		return cmdWorkspaceInspect(ctx, g, args)
	case "provision", "db":
		return cmdWorkspaceProvision(ctx, g, args)
	case "delete", "rm":
		return cmdWorkspaceDelete(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 workspace 子命令 %q\n", sub)
		printWorkspaceUsage(os.Stderr)
		return 2
	}
}

func cmdWorkspaceList(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var list wsListResponse
	if err := client.Get(ctx, "/api/v1/workspaces", &list); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(list)
	}
	if len(list.Items) == 0 {
		fmt.Println("（没有 Workspace）")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tHTTP\tDATABASE\tREDIS_DB\tACTIVE")
	for _, item := range list.Items {
		active := ""
		if item.WorkspaceID == list.ActiveWorkspaceID {
			active = "*"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%d\t%s\n",
			item.Name, item.State, item.Network.HTTPPort,
			item.Database.DatabaseName, item.Redis.DBIndex, active)
	}
	_ = w.Flush()
	return 0
}

func cmdWorkspaceCreate(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("workspace create", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	description := fs.String("description", "", "Workspace 描述")
	projectType := fs.String("project-type", "", "项目类型，例如 discourse")
	profileID := fs.String("profile", "", "Project Profile ID，例如 discourse-official")
	repositoryURL := fs.String("repo", "", "覆盖模板的源码仓库地址")
	branch := fs.String("branch", "", "覆盖模板的源码分支")
	acceptProfile := fs.Bool("accept-profile", false, "确认使用未验证的第三方模板")
	fs.Usage = func() { printWorkspaceUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: workspace create 需要一个名称")
		return 2
	}
	name := fs.Arg(0)

	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	req := workspace.CreateRequest{
		Name:          name,
		Description:   *description,
		ProjectType:   *projectType,
		ProfileID:     *profileID,
		RepositoryURL: *repositoryURL,
		Branch:        *branch,
		AcceptProfile: *acceptProfile,
	}
	var created workspace.Workspace
	if err := client.Post(ctx, "/api/v1/workspaces", req, &created); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(created)
	}
	fmt.Printf("已创建 Workspace %q\n", created.Name)
	fmt.Printf("  ID:       %s\n", created.WorkspaceID)
	if created.ProfileID != "" {
		fmt.Printf("  Profile:  %s\n", created.ProfileID)
	}
	fmt.Printf("  HTTP:     http://%s:%d\n", created.Network.Host, created.Network.HTTPPort)
	fmt.Printf("  数据库:   %s\n", created.Database.DatabaseName)
	fmt.Printf("  Redis DB: %d\n", created.Redis.DBIndex)
	if created.Source.RepositoryURL != "" {
		fmt.Printf("  源码:     %s", created.Source.RepositoryURL)
		if created.Source.Branch != "" {
			fmt.Printf(" @ %s", created.Source.Branch)
		}
		fmt.Println()
	}
	fmt.Printf("\n使用 hxxddk workspace use %q 切换活动 Workspace。\n", created.Name)
	return 0
}

func cmdWorkspaceUse(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: workspace use 需要一个名称或 ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := resolveWorkspace(ctx, client, args[0])
	if err != nil {
		return fail(err)
	}
	var result activateResponse
	if err := client.Post(ctx, "/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/activate", nil, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	fmt.Printf("活动 Workspace 已切换为 %q\n", item.Name)
	return 0
}

func cmdWorkspaceInspect(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: workspace inspect 需要一个名称或 ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := resolveWorkspace(ctx, client, args[0])
	if err != nil {
		return fail(err)
	}
	var result wsStatusResponse
	if err := client.Get(ctx, "/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/status", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	ws := result.Workspace
	fmt.Printf("Workspace %q (%s)\n", ws.Name, ws.WorkspaceID)
	fmt.Printf("  状态:     %s\n", ws.State)
	fmt.Printf("  项目类型: %s\n", orDash(ws.ProjectType))
	fmt.Printf("  描述:     %s\n", orDash(ws.Description))
	fmt.Printf("  创建时间: %s\n", ws.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if ws.Source.Installed {
		fmt.Printf("  源码:     %s (%s)\n", ws.Source.Path, orDash(ws.Source.Branch+ws.Source.Tag))
	} else {
		fmt.Printf("  源码:     未安装\n")
	}
	dbState := "未创建"
	if ws.Database.Provisioned {
		dbState = "已创建"
	}
	fmt.Printf("  数据库:   %s（%s）\n", ws.Database.DatabaseName, dbState)
	if ws.Database.ProvisionError != "" && !ws.Database.Provisioned {
		fmt.Printf("  数据库错误: %s\n", ws.Database.ProvisionError)
	}
	fmt.Printf("  Redis:    DB %d\n", ws.Redis.DBIndex)
	fmt.Printf("  HTTP:     http://%s:%d\n", ws.Network.Host, ws.Network.HTTPPort)
	if ws.LastError != "" {
		fmt.Printf("  最近错误: %s\n", ws.LastError)
	}
	if result.Status != nil {
		fmt.Println("  检查:")
		for _, check := range result.Status.Checks {
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
	return 0
}

func cmdWorkspaceProvision(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: workspace provision 需要一个名称或 ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := resolveWorkspace(ctx, client, args[0])
	if err != nil {
		return fail(err)
	}
	client.HTTP.Timeout = 2 * time.Minute
	var ws workspace.Workspace
	if err := client.Post(ctx,
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/provision", nil, &ws); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(ws)
	}
	if ws.Database.Provisioned {
		fmt.Printf("Workspace %q 的数据库已就绪: %s\n", ws.Name, ws.Database.DatabaseName)
		return 0
	}
	fmt.Printf("数据库尚未创建: %s\n", ws.Database.ProvisionError)
	return 1
}

func cmdWorkspaceDelete(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("workspace delete", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	yes := fs.Bool("yes", false, "不询问确认")
	purge := fs.Bool("purge", false, "删除源码、配置、数据与备份")
	delConfig := fs.Bool("delete-config", false, "删除配置、日志与临时文件")
	delSource := fs.Bool("delete-source", false, "删除源码")
	delData := fs.Bool("delete-data", false, "删除数据与备份")
	delDatabase := fs.Bool("delete-database", false, "删除该 Workspace 的数据库与角色")
	delRedis := fs.Bool("delete-redis", false, "清空该 Workspace 使用的 Redis DB")
	fs.Usage = func() { printWorkspaceUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: workspace delete 需要一个名称或 ID")
		return 2
	}

	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := resolveWorkspace(ctx, client, fs.Arg(0))
	if err != nil {
		return fail(err)
	}

	var plan workspace.DeletePlan
	if err := client.Do(ctx, "DELETE",
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/preview", nil, &plan); err != nil {
		return fail(err)
	}
	if g.JSON && !*yes {
		fmt.Fprintln(os.Stderr, "hxxddk: --json 模式执行删除需要同时指定 --yes")
		return 5
	}
	if !g.JSON {
		printDeletePlan(&plan)
	}
	if plan.Running {
		fmt.Fprintln(os.Stderr, "错误: Workspace 正在运行，请先停止后再删除")
		return 4
	}
	if !*yes {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "错误: 非交互环境执行删除需要指定 --yes")
			return 5
		}
		fmt.Printf("\n确认删除 Workspace %q？此操作不可撤销 [y/N]: ", plan.Name)
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		answer := strings.ToLower(strings.TrimSpace(line))
		if answer != "y" && answer != "yes" {
			fmt.Println("已取消。")
			return 1
		}
	}

	query := url.Values{}
	query.Set("confirm_token", plan.ConfirmToken)
	switch {
	case *purge:
		query.Set("mode", "purge")
	case *delConfig || *delSource || *delData || *delDatabase || *delRedis:
		query.Set("mode", "custom")
	}
	for name, enabled := range map[string]bool{
		"delete_config":   *delConfig,
		"delete_source":   *delSource,
		"delete_data":     *delData,
		"delete_database": *delDatabase,
		"delete_redis":    *delRedis,
	} {
		if enabled {
			query.Set(name, "true")
		}
	}

	var accepted taskAccepted
	if err := client.Do(ctx, "DELETE",
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"?"+query.Encode(), nil, &accepted); err != nil {
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
			fmt.Printf("已删除 Workspace %q。%s\n", plan.Name, final.Message)
		}
		return 0
	case task.StatusCanceled:
		if !g.JSON {
			fmt.Fprintln(os.Stderr, "删除任务已取消")
		}
		return 1
	default:
		if !g.JSON {
			fmt.Fprintf(os.Stderr, "删除失败: %s\n", final.ErrorMessage)
		}
		return 1
	}
}

func cmdLifecycle(ctx context.Context, g Globals, action string, args []string) int {
	if len(args) > 1 {
		fmt.Fprintf(os.Stderr, "hxxddk: %s 最多接受一个 Workspace 名称或 ID\n", action)
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	item, err := currentWorkspace(ctx, client, firstArg(args))
	if err != nil {
		return fail(err)
	}
	var accepted taskAccepted
	if err := client.Post(ctx,
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/"+action, nil, &accepted); err != nil {
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
				fmt.Printf("%s 完成\n", action)
			}
			if (action == "start" || action == "restart") && !strings.Contains(final.Message, "http") {
				fmt.Printf("访问地址: http://%s:%d\n", item.Network.Host, item.Network.HTTPPort)
			}
		}
		return 0
	case task.StatusCanceled:
		if !g.JSON {
			fmt.Fprintln(os.Stderr, "任务已取消")
		}
		return 1
	default:
		if !g.JSON {
			fmt.Fprintf(os.Stderr, "%s 失败: %s\n", action, final.ErrorMessage)
		}
		return 1
	}
}

// currentWorkspace resolves an explicit workspace reference or falls back to
// the active workspace.
func currentWorkspace(ctx context.Context, client *Client, ref string) (*workspace.Workspace, error) {
	if ref != "" {
		return resolveWorkspace(ctx, client, ref)
	}
	info, err := fetchInfo(ctx, client)
	if err != nil {
		return nil, err
	}
	if info.ActiveWorkspaceID == "" {
		return nil, fmt.Errorf("没有活动 Workspace，请使用 --workspace 指定")
	}
	return resolveWorkspace(ctx, client, info.ActiveWorkspaceID)
}

func fetchInfo(ctx context.Context, client *Client) (infoResponse, error) {
	var info infoResponse
	err := client.Get(ctx, "/api/v1/system/info", &info)
	return info, err
}

func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

func resolveWorkspace(ctx context.Context, client *Client, ref string) (*workspace.Workspace, error) {
	var list wsListResponse
	if err := client.Get(ctx, "/api/v1/workspaces", &list); err != nil {
		return nil, err
	}
	for _, item := range list.Items {
		if item.WorkspaceID == ref {
			return item, nil
		}
	}
	for _, item := range list.Items {
		if strings.EqualFold(item.Name, ref) {
			return item, nil
		}
	}
	return nil, fmt.Errorf("找不到 Workspace %q", ref)
}

func waitTask(ctx context.Context, client *Client, taskID string, quiet bool) (task.Task, error) {
	var current task.Task
	lastProgress := -1
	lastMessage := ""
	for {
		if err := client.Get(ctx, "/api/v1/tasks/"+url.PathEscape(taskID), &current); err != nil {
			return current, err
		}
		if !quiet && (current.Progress != lastProgress || current.Message != lastMessage) {
			fmt.Fprintf(os.Stderr, "\r%-10s %3d%% %s", current.Status, current.Progress, current.Message)
			lastProgress = current.Progress
			lastMessage = current.Message
		}
		if current.Status.Terminal() {
			if !quiet {
				fmt.Fprintln(os.Stderr)
			}
			return current, nil
		}
		select {
		case <-ctx.Done():
			return current, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func printDeletePlan(plan *workspace.DeletePlan) {
	fmt.Printf("将删除 Workspace %q (%s)\n", plan.Name, plan.WorkspaceID)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "  资源\t状态\t大小\t说明")
	for _, resource := range plan.Resources {
		state := "不存在"
		if resource.Exists {
			state = "存在"
		}
		size := "-"
		if resource.SizeBytes > 0 {
			size = humanBytes(resource.SizeBytes)
		}
		note := resource.Note
		if resource.Shared {
			note += "（共享）"
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", resource.Kind, state, size, note)
	}
	_ = w.Flush()
	fmt.Printf("  合计: %s\n", humanBytes(plan.TotalBytes))
	if plan.Running {
		fmt.Println("  警告: Workspace 正在运行")
	}
	fmt.Printf("  确认令牌有效期至: %s\n", plan.ExpiresAt.Local().Format("15:04:05"))
}

func severityLabel(severity diagnostics.Severity) string {
	switch severity {
	case diagnostics.SeverityError:
		return "ERROR"
	case diagnostics.SeverityWarning:
		return "WARN"
	default:
		return "INFO"
	}
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func printJSON(value any) int {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, "输出 JSON 失败:", err)
		return 1
	}
	return 0
}

func fail(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		fmt.Fprintf(os.Stderr, "错误: %s\n", apiErr.Message)
		if apiErr.RequestID != "" {
			fmt.Fprintf(os.Stderr, "请求 ID: %s\n", apiErr.RequestID)
		}
		switch apiErr.Status {
		case 400:
			return 2
		case 404:
			return 3
		case 409:
			return 4
		case 422:
			return 5
		default:
			return 1
		}
	}
	if errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "操作已取消")
		return 1
	}
	fmt.Fprintln(os.Stderr, "错误:", err)
	return 1
}
