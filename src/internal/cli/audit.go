package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/hxx/hxxDiscourseDevKit/internal/audit"
)

func cmdAudit(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdAuditList(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 audit 子命令 %q\n", sub)
		printAuditUsage(os.Stderr)
		return 2
	}
}

func cmdAuditList(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	limit := fs.Int("limit", 100, "返回条数（1–500）")
	action := fs.String("action", "", "按操作名过滤")
	workspace := fs.String("workspace", "", "按 Workspace 名称或 ID 过滤")
	result := fs.String("result", "", "success | accepted | failure")
	since := fs.String("since", "", "RFC3339 时间或 YYYY-MM-DD")
	fs.Usage = func() { printAuditUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "hxxddk: 未知参数 %v\n", fs.Args())
		return 2
	}
	if *limit < 1 || *limit > 500 {
		fmt.Fprintln(os.Stderr, "hxxddk: --limit 必须是 1–500 之间的整数")
		return 2
	}
	switch *result {
	case "", "success", "accepted", "failure":
	default:
		fmt.Fprintln(os.Stderr, "hxxddk: --result 必须是 success、accepted 或 failure")
		return 2
	}

	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}

	workspaceID := strings.TrimSpace(*workspace)
	if workspaceID != "" {
		var list wsListResponse
		if err := client.Get(ctx, "/api/v1/workspaces", &list); err == nil {
			for _, item := range list.Items {
				if item.WorkspaceID == workspaceID || strings.EqualFold(item.Name, workspaceID) {
					workspaceID = item.WorkspaceID
					break
				}
			}
		}
	}

	query := url.Values{}
	query.Set("limit", strconv.Itoa(*limit))
	if value := strings.TrimSpace(*action); value != "" {
		query.Set("action", value)
	}
	if workspaceID != "" {
		query.Set("workspace_id", workspaceID)
	}
	if value := strings.TrimSpace(*result); value != "" {
		query.Set("result", value)
	}
	if value := strings.TrimSpace(*since); value != "" {
		query.Set("since", value)
	}

	var page audit.ListResult
	if err := client.Get(ctx, "/api/v1/audit?"+query.Encode(), &page); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(page)
	}
	if len(page.Items) == 0 {
		fmt.Println("没有匹配的审计记录。")
		return 0
	}

	var workspaces wsListResponse
	nameByID := map[string]string{}
	if err := client.Get(ctx, "/api/v1/workspaces", &workspaces); err == nil {
		for _, item := range workspaces.Items {
			nameByID[item.WorkspaceID] = item.Name
		}
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tACTION\tRESULT\tWORKSPACE\tTARGET\tCLIENT\tSTATUS\tDURATION\tPATH")
	for _, event := range page.Items {
		workspaceName := nameByID[event.WorkspaceID]
		if workspaceName == "" {
			workspaceName = event.WorkspaceID
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\t%dms\t%s\n",
			event.Timestamp.Local().Format("2006-01-02 15:04:05"),
			event.Action,
			event.Result,
			orDash(workspaceName),
			orDash(event.Target),
			orDash(event.Client),
			event.Status,
			event.DurationMS,
			event.Path,
		)
	}
	_ = w.Flush()
	fmt.Printf("\n共 %d 条匹配记录，显示 %d 条。\n", page.Total, len(page.Items))
	return 0
}

func printAuditUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk audit list [flags]

  --limit N          返回条数（1–500，默认 100）
  --action NAME      按操作名过滤，例如 project.start
  --workspace REF    按 Workspace 名称或 ID 过滤
  --result VALUE     success | accepted | failure
  --since TIME       RFC3339 时间或 YYYY-MM-DD

审计记录只包含请求元数据，不包含请求体；保留天数跟随日志保留设置。
异步任务显示为 accepted，最终结果可在任务历史或对应日志中查看。
`)
}
