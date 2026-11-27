package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
)

func cmdExtension(ctx context.Context, g Globals, kind extension.Kind, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdExtensionList(ctx, g, kind, args)
	case "create", "new":
		return cmdExtensionCreate(ctx, g, kind, args)
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
	fmt.Fprintln(w, "SLUG\t名称\t版本\t作者\t文件\t更新时间\t元数据")
	for _, item := range result.Items {
		updated := item.ModifiedAt.Local().Format("2006-01-02 15:04")
		metadata := "缺失"
		if item.HasMetadata {
			metadata = "正常"
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

未指定 --workspace 时使用活动 Workspace。
`, kind, label, label)
}
