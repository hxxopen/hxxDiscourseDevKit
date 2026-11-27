// Package cli implements the hxxddk command-line interface.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/app"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/version"
)

// Globals are flags accepted before or after the command.
type Globals struct {
	Root  string
	JSON  bool
	Dev   bool
	Quiet bool
}

// Main is the entry point for hxxddk.
func Main(args []string) int {
	globals, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hxxddk:", err)
		return 2
	}
	if len(rest) == 0 {
		printUsage(os.Stderr)
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()

	switch rest[0] {
	case "help", "--help", "-h":
		printUsage(os.Stdout)
		return 0
	case "version":
		return cmdVersion(globals)
	case "daemon":
		if len(rest) < 2 || rest[1] != "run" {
			fmt.Fprintln(os.Stderr, "hxxddk: 用法: hxxddk daemon run [--port N] [--open]")
			return 2
		}
		return runDaemon(globals, rest[2:])
	case "status":
		return cmdStatus(ctx, globals)
	case "doctor":
		return cmdDoctor(ctx, globals)
	case "workspace":
		if len(rest) < 2 {
			printWorkspaceUsage(os.Stderr)
			return 2
		}
		return cmdWorkspace(ctx, globals, rest[1], rest[2:])
	case "start", "stop", "restart":
		return cmdLifecycle(ctx, globals, rest[0], rest[1:])
	case "project":
		if len(rest) < 2 {
			printProjectUsage(os.Stderr)
			return 2
		}
		return cmdProject(ctx, globals, rest[1], rest[2:])
	case "profile":
		if len(rest) < 2 {
			printProfileUsage(os.Stderr)
			return 2
		}
		if rest[1] == "source" {
			if len(rest) < 3 {
				printProfileUsage(os.Stderr)
				return 2
			}
			return cmdProfileSource(ctx, globals, rest[2], rest[3:])
		}
		return cmdProfile(ctx, globals, rest[1], rest[2:])
	case "service":
		if len(rest) < 2 {
			printServiceUsage(os.Stderr)
			return 2
		}
		return cmdService(ctx, globals, rest[1], rest[2:])
	case "runtime":
		if len(rest) < 2 {
			printRuntimeUsage(os.Stderr)
			return 2
		}
		return cmdRuntime(ctx, globals, rest[1], rest[2:])
	case "backup":
		if len(rest) < 2 {
			printBackupUsage(os.Stderr)
			return 2
		}
		return cmdBackup(ctx, globals, rest[1], rest[2:])
	case "audit":
		if len(rest) < 2 {
			printAuditUsage(os.Stderr)
			return 2
		}
		return cmdAudit(ctx, globals, rest[1], rest[2:])
	case "plugin", "theme":
		kind := extension.Kind(rest[0])
		if len(rest) < 2 {
			printExtensionUsage(os.Stderr, kind)
			return 2
		}
		return cmdExtension(ctx, globals, kind, rest[1], rest[2:])
	default:
		fmt.Fprintf(os.Stderr, "hxxddk: 未知命令 %q\n\n", rest[0])
		printUsage(os.Stderr)
		return 2
	}
}

// DaemonMain is the entry point for the devkitd binary.
func DaemonMain(args []string) int {
	globals, rest, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "devkitd:", err)
		return 2
	}
	return runDaemon(globals, rest)
}

func runDaemon(globals Globals, args []string) int {
	fs := flag.NewFlagSet("devkitd", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	port := fs.Int("port", 0, "管理 API 端口（覆盖配置文件）")
	open := fs.Bool("open", false, "启动后打开管理界面")
	quiet := fs.Bool("quiet", false, "不向控制台输出日志")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "用法: devkitd | hxxddk daemon run [--root DIR] [--port N] [--dev] [--open] [--quiet]")
		fs.PrintDefaults()
	}
	if err := parseFlags(fs, args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "devkitd: 未知参数 %v\n", fs.Args())
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), terminationSignals()...)
	defer stop()

	err := app.Run(ctx, app.Options{
		Root:        globals.Root,
		Port:        *port,
		Dev:         globals.Dev,
		OpenBrowser: *open,
		Console:     !(*quiet || globals.Quiet),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "devkitd:", err)
		return 1
	}
	return 0
}

func cmdVersion(g Globals) int {
	info := version.Get()
	if g.JSON {
		return printJSON(info)
	}
	fmt.Printf("hxxDiscourseDevKit %s\n", info.Version)
	fmt.Printf("commit:   %s\n", info.Commit)
	fmt.Printf("build:    %s\n", info.BuildDate)
	fmt.Printf("go:       %s\n", info.GoVersion)
	fmt.Printf("platform: %s/%s\n", info.OS, info.Arch)
	return 0
}

func parseGlobals(args []string) (Globals, []string, error) {
	var g Globals
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		switch name {
		case "--root", "-root":
			if hasValue {
				g.Root = value
			} else {
				if i+1 >= len(args) {
					return g, nil, fmt.Errorf("--root 需要一个目录参数")
				}
				i++
				g.Root = args[i]
			}
		case "--json", "-json":
			g.JSON = !(hasValue && value == "false")
		case "--dev", "-dev":
			g.Dev = !(hasValue && value == "false")
		case "--quiet", "-quiet":
			g.Quiet = !(hasValue && value == "false")
		default:
			rest = append(rest, arg)
		}
	}
	return g, rest, nil
}

// parseFlags allows flags and positional arguments in any order.
func parseFlags(fs *flag.FlagSet, args []string) error {
	ordered := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			ordered = append(ordered, arg)
			name := strings.TrimLeft(arg, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				continue
			}
			f := fs.Lookup(name)
			if f != nil && !isBoolFlag(f) && i+1 < len(args) {
				i++
				ordered = append(ordered, args[i])
			}
			continue
		}
		positional = append(positional, arg)
	}
	return fs.Parse(append(ordered, positional...))
}

func isBoolFlag(f *flag.Flag) bool {
	if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok {
		return bf.IsBoolFlag()
	}
	return false
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `hxxDiscourseDevKit 开发环境管理工具

用法:
  hxxddk [--root DIR] [--json] <命令> [参数]

命令:
  version                     显示版本信息
  status                      显示 DevKit 与 Workspace 状态
  doctor                      运行环境诊断
  workspace list              列出 Workspace
  workspace create <name>     创建 Workspace
  workspace use <name|id>     切换活动 Workspace
  workspace inspect <name|id> 查看 Workspace 详情
  workspace delete <name|id>  删除 Workspace（需预览确认）
  start | stop | restart [ws] 启动、停止或重启 Workspace 项目
  project ...                 源码、初始化、状态与日志
  profile list|show|check     查看 Project Profile 与依赖预检
  profile source ...          管理第三方 Project Profile 来源
  backup ...                  创建、查看与恢复备份
  audit list                  查看操作审计记录
  plugin list|create|check|test|package
                               管理 Discourse 插件、依赖检查、自动化测试与分发包
  theme list|create|check|build|preview|package
                               管理 Discourse 主题、依赖检查、构建与本地预览
  daemon run                  在前台运行 devkitd
  service list|start|stop ... 管理 PostgreSQL 与 Redis
  runtime list|verify|wsl     查看 Runtime 组件矩阵与 WSL2 环境
  runtime install             预览并安装组件到 WSL2

全局参数:
  --root DIR    指定实例根目录（默认按平台用户数据目录）
  --json        机器可读输出
  --dev         允许 Vite 开发服务器来源（5173）
  --quiet       守护进程不输出控制台日志
`)
}

func printWorkspaceUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk workspace <子命令>

  list                         列出 Workspace
  create [flags] <name>        创建 Workspace
      --description TEXT
      --project-type TYPE
      --profile ID             使用 Project Profile 的默认项目类型与源码
      --repo URL               覆盖模板的源码仓库地址
      --branch NAME            覆盖模板的源码分支
      --accept-profile         确认已查看未验证第三方模板的来源、操作与权限
  use <name|id>                切换活动 Workspace
  inspect <name|id>            查看详情
  provision <name|id>          重试创建 Workspace 数据库
  delete [flags] <name|id>     删除 Workspace
      --yes                    非交互确认
      --purge                  删除源码、配置、数据与备份
      --delete-config          删除配置、日志、临时文件
      --delete-source          删除源码
      --delete-data            删除数据与备份
      --delete-database        删除该 Workspace 的数据库与角色
      --delete-redis           清空该 Workspace 使用的 Redis DB
`)
}

func printProfileUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk profile <子命令>

  list                         列出内置与第三方 Project Profile
  show <id>                    查看模板来源、操作、权限与默认值
  check <id>                   运行依赖预检（Runtime、服务与源码）
  source list                  列出第三方模板来源
  source add [--branch NAME] <本地目录|Git URL>
                               注册模板来源（只读取 devkit-profiles.json）
  source refresh <id>          重新加载来源清单或拉取 Git 更新
  source remove [--yes] <id>   注销来源（Git 来源的检出目录会删除）

使用未验证的第三方模板创建 Workspace 时，需要 --accept-profile 确认。
预检存在 Runtime 错误时命令返回退出码 1，但不会阻止之后修复环境。
`)
}

func printProjectUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk project <子命令> [--workspace NAME]

  status                       查看源码与进程状态
  source [flags]               配置仓库地址与分支
      --repo URL
      --branch NAME | --tag NAME | --commit SHA
      --shallow | --no-shallow
      --submodules | --no-submodules
  clone                        克隆源码
  fetch                        获取远程更新
  pull [--force]               快进合并远程分支
  checkout <ref> [--force]     切换分支、标签或提交
  init [--skip-deps] [--skip-db]
                               安装依赖并准备数据库
  logs [--source project|init|git|test] [--tail N]
                               查看项目日志

未指定 --workspace 时使用活动 Workspace。start、stop、restart 也可直接指定
Workspace：hxxddk start [NAME|ID]。
`)
}

func printBackupUsage(w io.Writer) {
	fmt.Fprint(w, `用法: hxxddk backup <子命令> [--workspace NAME]

  list                         列出备份
  create [--source] [--database]
                               创建备份（--source 包含源码，--database 包含数据库）
  inspect <id> [--verify]      查看备份详情
  delete <id> [--yes]          删除备份
  restore <id> [--yes]         预览并恢复备份

未指定 --workspace 时使用活动 Workspace。
`)
}
