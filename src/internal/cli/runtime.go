package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
)

type runtimeComponentsResponse struct {
	Items   []runtime.ComponentStatus `json:"items"`
	Total   int                       `json:"total"`
	Profile string                    `json:"profile,omitempty"`
	Builtin bool                      `json:"builtin,omitempty"`
}

type runtimeDiscoverResponse struct {
	Components map[string]runtime.DiscoveredComponent `json:"components"`
	Names      []string                               `json:"names"`
	Total      int                                    `json:"total"`
}

func cmdRuntime(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdRuntimeList(ctx, g)
	case "verify", "check":
		return cmdRuntimeVerify(ctx, g, args)
	case "wsl":
		return cmdRuntimeWSL(ctx, g)
	case "discover":
		return cmdRuntimeDiscover(ctx, g)
	case "install":
		return cmdRuntimeInstall(ctx, g, args)
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知 runtime 子命令 %q\n", sub)
		printRuntimeUsage(os.Stderr)
		return 2
	}
}

func cmdRuntimeList(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result runtimeComponentsResponse
	if err := client.Get(ctx, "/api/v1/runtime/components", &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	printRuntimeComponents(result.Items)
	if result.Profile != "" {
		fmt.Printf("\n矩阵配置: %s", result.Profile)
		if result.Builtin {
			fmt.Print("（内置默认）")
		}
		fmt.Println()
	}
	return 0
}

func cmdRuntimeVerify(ctx context.Context, g Globals, args []string) int {
	if len(args) > 1 {
		fmt.Fprintln(os.Stderr, "hxxddk: runtime verify 最多接受一个组件名")
		return 2
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	if len(args) == 1 {
		var item runtime.ComponentStatus
		if err := client.Get(ctx, "/api/v1/runtime/components/"+args[0], &item); err != nil {
			return fail(err)
		}
		if g.JSON {
			return printJSON(item)
		}
		printRuntimeComponents([]runtime.ComponentStatus{item})
		if !item.Verified {
			return 1
		}
		return 0
	}
	var result runtimeComponentsResponse
	if err := client.Post(ctx, "/api/v1/runtime/verify", nil, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	printRuntimeComponents(result.Items)
	exit := 0
	for _, item := range result.Items {
		if item.Required && !item.Verified {
			exit = 1
		}
	}
	return exit
}

func cmdRuntimeWSL(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var status runtime.WSLStatus
	if err := client.Get(ctx, "/api/v1/runtime/wsl", &status); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(status)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Supported:\t%v\n", status.Supported)
	fmt.Fprintf(w, "Available:\t%v\n", status.Available)
	fmt.Fprintf(w, "Distro:\t%s\n", orDash(status.Distro))
	fmt.Fprintf(w, "OS:\t%s\n", orDash(status.OSName))
	fmt.Fprintf(w, "Kernel:\t%s\n", orDash(status.Kernel))
	fmt.Fprintf(w, "User:\t%s\n", orDash(status.User))
	fmt.Fprintf(w, "Home:\t%s\n", orDash(status.Home))
	fmt.Fprintf(w, "Root:\t%v\n", status.RootAvailable)
	if status.Error != "" {
		fmt.Fprintf(w, "Error:\t%s\n", status.Error)
	}
	_ = w.Flush()
	if !status.Available {
		return 1
	}
	return 0
}

func cmdRuntimeDiscover(ctx context.Context, g Globals) int {
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var result runtimeDiscoverResponse
	if err := client.Post(ctx, "/api/v1/runtime/discover", nil, &result); err != nil {
		return fail(err)
	}
	if g.JSON {
		return printJSON(result)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tEXECUTION\tVERSION\tPATH")
	for _, name := range result.Names {
		item := result.Components[name]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, orDash(item.Execution), orDash(item.Version), item.Path)
	}
	_ = w.Flush()
	fmt.Printf("\n共探测到 %d 个组件\n", result.Total)
	return 0
}

func cmdRuntimeInstall(ctx context.Context, g Globals, args []string) int {
	confirmed := false
	var components []string
	for _, arg := range args {
		switch arg {
		case "--yes", "-y":
			confirmed = true
		default:
			for _, part := range strings.Split(arg, ",") {
				if part = strings.TrimSpace(part); part != "" {
					components = append(components, part)
				}
			}
		}
	}
	client, err := ensureDaemon(ctx, g)
	if err != nil {
		return fail(err)
	}
	var plan runtime.InstallPlan
	if err := client.Post(ctx, "/api/v1/runtime/install-preview",
		map[string]any{"components": components}, &plan); err != nil {
		return fail(err)
	}
	if g.JSON && !confirmed {
		return printJSON(plan)
	}
	printRuntimeInstallPlan(plan)
	if !confirmed {
		fmt.Println("\n尚未执行安装。确认无误后重新运行并追加 --yes。")
		return 0
	}
	if plan.ConfirmToken == "" {
		fmt.Fprintln(os.Stderr, "没有需要安装的组件")
		return 0
	}
	var accepted taskAccepted
	if err := client.Post(ctx, "/api/v1/runtime/install",
		map[string]any{"confirm_token": plan.ConfirmToken}, &accepted); err != nil {
		return fail(err)
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
	if final.Status != task.StatusSucceeded {
		fmt.Fprintf(os.Stderr, "运行时安装失败: %s\n", final.Message)
		return 1
	}
	fmt.Println("运行时组件安装完成。")
	return 0
}

func printRuntimeInstallPlan(plan runtime.InstallPlan) {
	fmt.Printf("WSL2: %s\n", orDash(plan.Distro))
	if plan.OSName != "" {
		fmt.Printf("系统: %s\n", plan.OSName)
	}
	fmt.Printf("root 可用: %v\n\n", plan.RootAvailable)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tPACKAGE\tSTATUS\tVERSION")
	for _, item := range plan.Components {
		status := item.Message
		if status == "" {
			status = "未安装"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.Name, item.Package, status, orDash(item.Version))
	}
	_ = w.Flush()
	for _, warning := range plan.Warnings {
		fmt.Println("注意:", warning)
	}
	if len(plan.Commands) > 0 {
		fmt.Println("\n将执行:")
		for _, command := range plan.Commands {
			fmt.Printf("  %s: %s\n", command.Label, command.Preview)
		}
	}
}

func printRuntimeComponents(items []runtime.ComponentStatus) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "COMPONENT\tREQUIRED\tEXPECTED\tACTUAL\tVERIFIED\tEXECUTION\tPATH")
	for _, item := range items {
		verified := "no"
		if item.Verified {
			verified = "yes"
		}
		required := "no"
		if item.Required {
			required = "yes"
		}
		actual := item.ActualVersion
		if actual == "" {
			actual = "-"
		}
		path := item.Path
		if path == "" {
			path = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			item.Name, required, item.ExpectedVersion, actual, verified,
			orDash(item.Execution), path)
		if item.Message != "" && !item.Verified {
			fmt.Fprintf(w, "\t\t\t\t\t\t%s\n", item.Message)
		}
	}
	_ = w.Flush()
}
