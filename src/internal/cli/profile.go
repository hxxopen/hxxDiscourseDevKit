package cli

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
)

type profileListResponse struct {
	Items []profile.Definition `json:"items"`
	Total int                  `json:"total"`
}

type profileSourceListResponse struct {
	Items []profile.Source `json:"items"`
	Total int              `json:"total"`
}

func cmdProfile(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list":
		return cmdProfileList(ctx, g, args)
	case "show":
		return cmdProfileShow(ctx, g, args)
	case "check":
		return cmdProfileCheck(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 profile 子命令 %q\n\n", sub)
		printProfileUsage(os.Stderr)
		return 2
	}
}

func cmdProfileList(ctx context.Context, g Globals, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile list 不接受参数")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result profileListResponse
	if err := client.Get(ctx, "/api/v1/profiles", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	rows := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(rows, "ID\tNAME\tPROJECT_TYPE\tSOURCE\tORIGIN")
	for _, item := range result.Items {
		source := item.DefaultRepositoryURL
		if source == "" {
			source = "（创建时填写）"
		} else if item.DefaultBranch != "" {
			source += "@" + item.DefaultBranch
		}
		fmt.Fprintf(rows, "%s\t%s\t%s\t%s\t%s\n",
			item.ID, item.Name, item.ProjectType, source, profileOriginLabel(item.Origin))
	}
	_ = rows.Flush()
	return 0
}

func cmdProfileShow(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile show 需要一个 Profile ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var def profile.Definition
	if err := client.Get(ctx, "/api/v1/profiles/"+url.PathEscape(args[0]), &def); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(def)
	}
	fmt.Printf("Profile %s — %s\n", def.ID, def.Name)
	fmt.Printf("  项目类型: %s\n", def.ProjectType)
	fmt.Printf("  分类:     %s\n", def.Category)
	if def.DefaultRepositoryURL != "" {
		fmt.Printf("  默认源码: %s", def.DefaultRepositoryURL)
		if def.DefaultBranch != "" {
			fmt.Printf(" @ %s", def.DefaultBranch)
		}
		fmt.Println()
	} else {
		fmt.Println("  默认源码: 未内置（创建时必须提供）")
	}
	fmt.Printf("  来源:     %s\n", profileOriginLabel(def.Origin))
	fmt.Printf("  说明:     %s\n", def.Description)
	if def.ExtensionKind != "" {
		fmt.Printf("  扩展类型: %s\n", def.ExtensionKind)
	}
	fmt.Println("  操作:")
	for _, op := range def.Operations {
		destructive := ""
		if op.Destructive {
			destructive = " [破坏性]"
		}
		fmt.Printf("    - %s%s: %s\n", op.Title, destructive, op.Description)
	}
	fmt.Println("  权限:")
	for _, perm := range def.Permissions {
		fmt.Printf("    - %s [%s]: %s\n", perm.Title, perm.Risk, perm.Description)
	}
	fmt.Println("  依赖:")
	fmt.Printf("    - Runtime: %s\n", strings.Join(def.RuntimeComponents, ", "))
	fmt.Printf("    - 服务:    %s\n", strings.Join(def.Services, ", "))
	return 0
}

func cmdProfileCheck(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile check 需要一个 Profile ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var report profile.CheckReport
	if err := client.Get(ctx, "/api/v1/profiles/"+url.PathEscape(args[0])+"/check", &report); err != nil {
		return fail(err)
	}
	if g.JSON {
		if !report.Ready {
			// JSON callers still receive the full report; the exit code marks
			// the failed preflight.
			_ = printJSON(report)
			return 1
		}
		return printJSON(report)
	}
	state := "就绪"
	if !report.Ready {
		state = "存在错误"
	}
	fmt.Printf("Profile %s — %s（%d 错误，%d 提醒）\n",
		report.ProfileID, report.ProfileName, report.ErrorCount, report.WarningCount)
	for _, check := range report.Checks {
		mark := strings.ToUpper(check.Status)
		fmt.Printf("  %-5s %s: %s\n", mark, check.Title, check.Message)
		if check.FixAction != "" && check.Status != profile.StatusOK && check.Status != profile.StatusInfo {
			fmt.Printf("        → %s\n", check.FixAction)
		}
	}
	fmt.Printf("\n预检状态: %s\n", state)
	if !report.Ready {
		return 1
	}
	return 0
}

func cmdProfileSource(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdProfileSourceList(ctx, g, args)
	case "add":
		return cmdProfileSourceAdd(ctx, g, args)
	case "refresh":
		return cmdProfileSourceRefresh(ctx, g, args)
	case "remove", "rm":
		return cmdProfileSourceRemove(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 profile source 子命令 %q\n\n", sub)
		printProfileUsage(os.Stderr)
		return 2
	}
}

func cmdProfileSourceList(ctx context.Context, g Globals, args []string) int {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile source list 不接受参数")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result profileSourceListResponse
	if err := client.Get(ctx, "/api/v1/profile-sources", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	if len(result.Items) == 0 {
		fmt.Println("（没有第三方模板来源）")
		return 0
	}
	rows := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(rows, "ID\tKIND\tNAME\tVERSION\tPROFILES\tSTATUS\tLOCATION")
	for _, item := range result.Items {
		status := "正常"
		if item.LastError != "" {
			status = "错误: " + item.LastError
		}
		version := item.Version
		if version == "" {
			version = "-"
		}
		fmt.Fprintf(rows, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			item.ID, profileSourceKindLabel(item.Kind), item.Name, version,
			len(item.ProfileIDs), status, item.Location)
	}
	_ = rows.Flush()
	return 0
}

func cmdProfileSourceAdd(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("profile source add", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	branch := fs.String("branch", "", "Git 来源的分支")
	fs.Usage = func() { printProfileUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile source add 需要一个本地目录或 Git URL")
		return 2
	}
	location := strings.TrimSpace(fs.Arg(0))
	if location == "" {
		fmt.Fprintln(os.Stderr, "hxxddk: 来源地址不能为空")
		return 2
	}
	if !isRemoteSourceLocation(location) {
		abs, err := filepath.Abs(location)
		if err != nil {
			return fail(fmt.Errorf("解析本地路径: %w", err))
		}
		location = abs
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	client.HTTP.Timeout = 10 * time.Minute
	var source profile.Source
	if err := client.Post(ctx, "/api/v1/profile-sources", profile.SourceRequest{
		Location: location,
		Branch:   strings.TrimSpace(*branch),
	}, &source); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(source)
	}
	fmt.Printf("已注册模板来源 %s\n", source.ID)
	printProfileSource(source)
	return 0
}

func cmdProfileSourceRefresh(ctx context.Context, g Globals, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile source refresh 需要一个来源 ID")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	client.HTTP.Timeout = 10 * time.Minute
	var source profile.Source
	if err := client.Post(ctx,
		"/api/v1/profile-sources/"+url.PathEscape(args[0])+"/refresh", nil, &source); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(source)
	}
	fmt.Printf("已刷新模板来源 %s\n", source.ID)
	printProfileSource(source)
	return 0
}

func cmdProfileSourceRemove(ctx context.Context, g Globals, args []string) int {
	fs := flag.NewFlagSet("profile source remove", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	yes := fs.Bool("yes", false, "不询问确认")
	fs.Usage = func() { printProfileUsage(os.Stderr) }
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: profile source remove 需要一个来源 ID")
		return 2
	}
	sourceID := fs.Arg(0)
	if !*yes {
		if g.JSON || !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "错误: 非交互环境注销模板来源需要指定 --yes")
			return 5
		}
		if !confirm(fmt.Sprintf("确认注销模板来源 %s？本地目录不会被删除 [y/N]: ", sourceID)) {
			fmt.Println("已取消。")
			return 1
		}
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	if err := client.Do(ctx, "DELETE",
		"/api/v1/profile-sources/"+url.PathEscape(sourceID), nil, nil); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(map[string]string{"removed": sourceID})
	}
	fmt.Printf("已注销模板来源 %s。\n", sourceID)
	return 0
}

func printProfileSource(source profile.Source) {
	fmt.Printf("  名称:     %s\n", source.Name)
	fmt.Printf("  类型:     %s\n", profileSourceKindLabel(source.Kind))
	fmt.Printf("  地址:     %s\n", source.Location)
	if source.Branch != "" {
		fmt.Printf("  分支:     %s\n", source.Branch)
	}
	if source.Version != "" {
		fmt.Printf("  版本:     %s\n", source.Version)
	}
	if source.Commit != "" {
		fmt.Printf("  提交:     %s\n", source.Commit)
	}
	fmt.Printf("  Profiles: %s\n", strings.Join(source.ProfileIDs, ", "))
	if source.LastError != "" {
		fmt.Printf("  错误:     %s\n", source.LastError)
	}
}

func profileSourceKindLabel(kind string) string {
	switch kind {
	case profile.SourceKindLocal:
		return "本地"
	case profile.SourceKindGit:
		return "Git"
	default:
		return kind
	}
}

func isRemoteSourceLocation(value string) bool {
	return strings.Contains(value, "://") || (strings.Contains(value, "@") && strings.Contains(value, ":"))
}

func profileOriginLabel(origin profile.Origin) string {
	kind := origin.Type
	switch origin.Type {
	case "builtin":
		kind = "内置"
	case "official":
		kind = "官方"
	case "third-party":
		kind = "第三方"
	}
	name := origin.Name
	if name == "" {
		name = "未知来源"
	}
	label := kind + " · " + name
	if origin.Version != "" {
		label += " " + origin.Version
	}
	if origin.Verified {
		label += "（已验证）"
	} else {
		label += "（未验证）"
	}
	return label
}
