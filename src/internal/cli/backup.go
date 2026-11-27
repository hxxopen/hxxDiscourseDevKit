package cli

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
)

type backupListResponse struct {
	Items []backup.Backup `json:"items"`
	Total int             `json:"total"`
}

func cmdBackup(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdBackupList(ctx, g, args)
	case "create":
		return cmdBackupCreate(ctx, g, args)
	case "inspect", "show":
		return cmdBackupInspect(ctx, g, args)
	case "delete", "rm":
		return cmdBackupDelete(ctx, g, args)
	case "restore":
		return cmdBackupRestore(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 backup 子命令 %q\n", sub)
		printBackupUsage(os.Stderr)
		return 2
	}
}

func backupFlags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("backup "+name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	ref := fs.String("workspace", "", "Workspace 名称或 ID（默认活动 Workspace）")
	fs.Usage = func() { printBackupUsage(os.Stderr) }
	return fs, ref
}

func cmdBackupList(ctx context.Context, g Globals, args []string) int {
	fs, ref := backupFlags("list")
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
	var result backupListResponse
	if err := client.Get(ctx,
		"/api/v1/workspaces/"+url.PathEscape(item.WorkspaceID)+"/backups", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	if len(result.Items) == 0 {
		fmt.Println("（没有备份）")
		return 0
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\t创建时间\t大小\t源码\t数据库")
	for _, item := range result.Items {
		created := item.CreatedAt.Local().Format("2006-01-02 15:04:05")
		source, database := "-", "-"
		if item.Manifest != nil {
			if item.Manifest.IncludesSource {
				source = "是"
			}
			if item.Manifest.IncludesDatabase {
				database = "是"
			}
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", item.ID, created,
			humanBytes(item.SizeBytes), source, database)
	}
	_ = w.Flush()
	return 0
}

func cmdBackupCreate(ctx context.Context, g Globals, args []string) int {
	fs, ref := backupFlags("create")
	includeSource := fs.Bool("source", false, "包含源码")
	includeDatabase := fs.Bool("database", false, "包含数据库")
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
	body := backup.CreateOptions{
		IncludeSource:   *includeSource,
		IncludeDatabase: *includeDatabase,
	}
	return submitAndWait(ctx, g, client, "/api/v1/workspaces/"+
		url.PathEscape(item.WorkspaceID)+"/backups", body, "backup.create")
}

func cmdBackupInspect(ctx context.Context, g Globals, args []string) int {
	fs, ref := backupFlags("inspect")
	verify := fs.Bool("verify", false, "校验全部文件哈希")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: backup inspect 需要一个备份 ID")
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
	if *verify {
		query.Set("verify", "true")
	}
	path := "/api/v1/workspaces/" + url.PathEscape(item.WorkspaceID) + "/backups/" +
		url.PathEscape(fs.Arg(0))
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var detail backup.Backup
	if err := client.Get(ctx, path, &detail); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(detail)
	}
	fmt.Printf("备份 %s\n", detail.ID)
	fmt.Printf("  文件:     %s\n", detail.FileName)
	fmt.Printf("  大小:     %s\n", humanBytes(detail.SizeBytes))
	fmt.Printf("  创建时间: %s\n", detail.CreatedAt.Local().Format("2006-01-02 15:04:05"))
	if detail.VerifyNote != "" {
		fmt.Printf("  校验:     %s\n", detail.VerifyNote)
	}
	if detail.Manifest != nil {
		printManifest(detail.Manifest)
	}
	return 0
}

func printManifest(manifest *backup.Manifest) {
	fmt.Printf("  格式版本: %d\n", manifest.FormatVersion)
	fmt.Printf("  DevKit:   %s\n", manifest.DevKitVersion)
	fmt.Printf("  源码:     %s\n", boolLabel(manifest.IncludesSource, "包含", "不包含"))
	fmt.Printf("  数据库:   %s\n", boolLabel(manifest.IncludesDatabase, "包含", "不包含"))
	fmt.Printf("  文件数:   %d\n", manifest.FileCount)
	fmt.Printf("  原始大小: %s\n", humanBytes(manifest.TotalBytes))
	if manifest.SourceHeadCommit != "" {
		fmt.Printf("  源码提交: %s\n", shortSHA(manifest.SourceHeadCommit))
	}
	if manifest.DatabaseName != "" {
		fmt.Printf("  数据库名: %s\n", manifest.DatabaseName)
	}
	for _, warning := range manifest.Warnings {
		fmt.Printf("  警告:     %s\n", warning)
	}
}

func cmdBackupDelete(ctx context.Context, g Globals, args []string) int {
	fs, ref := backupFlags("delete")
	yes := fs.Bool("yes", false, "不询问确认")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: backup delete 需要一个备份 ID")
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
	backupID := fs.Arg(0)
	if !*yes {
		if g.JSON || !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "错误: 非交互环境删除备份需要指定 --yes")
			return 5
		}
		if !confirm(fmt.Sprintf("确认删除备份 %s？此操作不可撤销 [y/N]: ", backupID)) {
			fmt.Println("已取消。")
			return 1
		}
	}
	if err := client.Do(ctx, "DELETE", "/api/v1/workspaces/"+
		url.PathEscape(item.WorkspaceID)+"/backups/"+url.PathEscape(backupID), nil, nil); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(map[string]string{"deleted": backupID})
	}
	fmt.Printf("已删除备份 %s。\n", backupID)
	return 0
}

func cmdBackupRestore(ctx context.Context, g Globals, args []string) int {
	fs, ref := backupFlags("restore")
	yes := fs.Bool("yes", false, "不询问确认")
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: backup restore 需要一个备份 ID")
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
	backupID := fs.Arg(0)
	base := "/api/v1/workspaces/" + url.PathEscape(item.WorkspaceID) +
		"/backups/" + url.PathEscape(backupID)

	var preview backup.RestorePreview
	if err := client.Post(ctx, base+"/restore-preview", nil, &preview); err != nil {
		return fail(err)
	}
	if g.JSON && !*yes {
		fmt.Fprintln(os.Stderr, "hxxddk: --json 模式恢复备份需要同时指定 --yes")
		return 5
	}
	if !g.JSON {
		printRestorePreview(&preview)
	}
	if preview.Running {
		fmt.Fprintln(os.Stderr, "错误: Workspace 正在运行，请先停止后再恢复")
		return 4
	}
	if !*yes {
		if !stdinIsTerminal() {
			fmt.Fprintln(os.Stderr, "错误: 非交互环境恢复备份需要指定 --yes")
			return 5
		}
		if !confirm(fmt.Sprintf("\n确认恢复备份 %s？现有配置、数据与源码将被替换 [y/N]: ", backupID)) {
			fmt.Println("已取消。")
			return 1
		}
	}

	job := map[string]string{"confirm_token": preview.ConfirmToken}
	return submitAndWait(ctx, g, client, base+"/restore", job, "backup.restore")
}

func printRestorePreview(preview *backup.RestorePreview) {
	fmt.Printf("恢复预览: 备份 %s → Workspace %s\n", preview.BackupID, preview.WorkspaceID)
	if preview.Manifest.WorkspaceName != "" {
		fmt.Printf("  备份来源: %s\n", preview.Manifest.WorkspaceName)
	}
	fmt.Printf("  需要空间: %s（可用 %s）\n",
		humanBytes(preview.RequiredBytes), humanBytes(int64(preview.FreeBytes)))
	fmt.Printf("  解压大小: %s\n", humanBytes(preview.UncompressedSize))
	if preview.RestoreDatabase {
		fmt.Println("  数据库:   将恢复")
	} else {
		fmt.Println("  数据库:   不恢复")
	}
	if len(preview.WillReplace) > 0 {
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  类型\t路径\t大小")
		for _, action := range preview.WillReplace {
			fmt.Fprintf(w, "  %s\t%s\t%s\n", action.Kind, action.Path,
				humanBytes(action.SizeBytes))
		}
		_ = w.Flush()
	}
	for _, warning := range preview.Warnings {
		fmt.Printf("  警告: %s\n", warning)
	}
	fmt.Printf("  令牌有效期至: %s\n", preview.ExpiresAt.Local().Format(time.DateTime))
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}
