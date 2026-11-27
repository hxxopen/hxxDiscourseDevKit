package cli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
)

type runtimeComponentsResponse struct {
	Items   []runtime.ComponentStatus `json:"items"`
	Total   int                       `json:"total"`
	Profile string                    `json:"profile,omitempty"`
	Builtin bool                      `json:"builtin,omitempty"`
}

func cmdRuntime(ctx context.Context, g Globals, sub string, args []string) int {
	switch sub {
	case "list", "ls":
		return cmdRuntimeList(ctx, g)
	case "verify", "check":
		return cmdRuntimeVerify(ctx, g, args)
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
