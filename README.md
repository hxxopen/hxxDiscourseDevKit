# hxxDiscourseDevKit

hxxDiscourseDevKit 是一个独立、通用、跨平台的 Discourse 开发环境发行产品。它为开发者自动准备 Discourse 开发所需的运行环境、基础服务和管理工具，降低 Ruby/Rails、Node.js、PostgreSQL、Redis 等依赖的安装与配置门槛。DevKit 面向整个 Discourse 生态，不是 HXX AgentForge 专属工具。任何开发者均可使用它开发官方 Discourse、Discourse 插件、Discourse 主题，或基于 Discourse 开展二次开发。

## 当前状态

已实现 **M0–M3**，并完成 M4 的运行时、日志、设置页面与 SSE 实时刷新：

- 单守护进程 `devkitd` + 瘦 CLI `hxxddk`，CLI 可按需自动拉起守护进程；
- 多 Workspace：独立目录、HTTP 端口、PostgreSQL 数据库/用户、随机凭据与 Redis DB 索引；
- Workspace 创建、重命名/描述更新（API）、活动切换、详情与状态检查；
- 删除预览 + 一次性确认令牌 + 后台任务 + record / purge 分层删除；purge 会删除该 Workspace 的数据库与 Redis 逻辑 DB；
- 本地管理 API：回环地址绑定、Bearer Token、HttpOnly 会话 Cookie + CSRF、Host/Origin 校验；
- Runtime 版本矩阵与组件验证（Ruby、Node、Git、PostgreSQL、Redis），支持原生与 WSL2 执行桥接；
- 托管 PostgreSQL / Redis：`initdb`、`pg_ctl`、`redis-server` 生命周期，外部服务端点切换与连接测试；
- 按 Workspace 自动创建独立的 PostgreSQL 数据库/角色，启动 PostgreSQL 后自动对账；
- 服务日志、依赖检查（有运行中的 Workspace 时拒绝停止服务）与扩展诊断；
- Git 源码管理：clone / fetch / pull / checkout，脏工作区保护，凭据不进入远程 URL 或进程参数；
- Discourse 初始化：`bundle config set --local path vendor/bundle`、`bundle install`、yarn/npm、`rails db:create db:migrate`；
- 项目生命周期：`rails server` 进程监督、TCP 健康检查、按 Workspace 停止进程树、运行/初始化/Git 日志；
- 备份与恢复：tar.gz + manifest + 每文件 SHA-256，可选源码与数据库；恢复预览 + 一次性令牌 + 临时目录校验与失败回滚；
- 日志中心：统一列出 DevKit 主日志、任务历史、Workspace 项目/初始化/Git 日志与托管服务日志，支持尾部读取、行数选择、自动刷新与返回前脱敏；
- 设置页：修改管理 API 端口、运行时 Profile/矩阵/WSL 发行版、项目默认值、日志与更新策略；持久化后标出需要重启 devkitd 的字段；
- SSE 事件流：后台任务状态变化时刷新 Workspace、服务、项目与备份缓存；
- React 管理 UI（概览、Workspaces、项目与备份、运行时、服务、日志、诊断、设置），构建产物通过 `go:embed` 内嵌进二进制；
- 结构化日志、敏感值脱敏、单实例文件锁、诊断报告与 JSONL 任务历史。

M3 的真实 Discourse 仓库端到端验收（clone、bundle、启动与访问）仍需在目标机器上手动执行；M4 的 Plugins/Themes 页面依赖 M6 的插件/主题能力，操作审计、i18n 与 E2E 验收仍待补齐；平台安装包按 M5 继续。

## 环境要求

- Go 1.23+
- Node.js 20+ 与 npm（构建 UI 时需要）
- Windows 10/11、macOS 或 Linux；本体原生运行，Windows 上可通过 WSL2 执行组件与托管服务
- 托管服务需要本机或 WSL2 中可用的 PostgreSQL（`initdb`、`pg_ctl`、`psql`）与 Redis（`redis-server`）；也可切换到外部服务

## 构建

```bash
# 构建前端 + devkitd + hxxddk（产物在 src/bin/）
bash scripts/build.sh

# 仅重新编译 Go 二进制，复用已有 UI 构建
SKIP_WEB=1 bash scripts/build.sh

# 指定版本号
VERSION=0.1.0 bash scripts/build.sh

# 已安装 make 时也可用
make build
VERSION=0.1.0 make build
```

Windows 上可以在 Git Bash / MSYS2 中执行同一脚本；脚本会按当前 `GOOS` 自动追加 `.exe`。

### 仅编译后端（Windows）

只重新编译 Go 二进制、复用已有前端产物：

```bash
SKIP_WEB=1 bash scripts/build.sh
```

不使用 Git Bash 时，可直接调用 Go。命令必须在 `src` 目录执行，因为 `go.mod` 位于此处：

```powershell
# PowerShell
cd D:\work\h2x\hxxDiscourseDevKit\src
$env:CGO_ENABLED = "0"
$ldflags = "-s -w -X github.com/hxx/hxxDiscourseDevKit/internal/version.Version=0.1.0-dev"
go build -trimpath -ldflags $ldflags -o bin\hxxddk.exe .\cmd\hxxddk
go build -trimpath -ldflags $ldflags -o bin\devkitd.exe .\cmd\devkitd
```

```bat
:: cmd.exe
cd /d D:\work\h2x\hxxDiscourseDevKit\src
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -X github.com/hxx/hxxDiscourseDevKit/internal/version.Version=0.1.0-dev" -o bin\hxxddk.exe .\cmd\hxxddk
go build -trimpath -ldflags "-s -w -X github.com/hxx/hxxDiscourseDevKit/internal/version.Version=0.1.0-dev" -o bin\devkitd.exe .\cmd\devkitd
```

管理 UI 通过 `go:embed all:dist` 内嵌。仅编译后端时使用 `src/internal/webembed/dist` 的现有内容；需要连同前端一起更新时，去掉 `SKIP_WEB=1`，或先在 `src/web` 执行 `npm run build`。编译要求 Go 1.23+，设置 `CGO_ENABLED=0` 可避免依赖本地 C 工具链。

编译后可验证：

```powershell
.\bin\hxxddk.exe version
.\bin\hxxddk.exe --root .\.devkit\demo status
```

## 快速开始

```bash
# CLI 会自动启动 devkitd（默认实例根目录按平台用户数据目录解析）
./src/bin/hxxddk status

# 使用隔离的根目录进行本地验收
./src/bin/hxxddk --root ./src/.devkit/demo status
./src/bin/hxxddk --root ./src/.devkit/demo workspace create Alpha
./src/bin/hxxddk --root ./src/.devkit/demo workspace create Beta
./src/bin/hxxddk --root ./src/.devkit/demo workspace list
./src/bin/hxxddk --root ./src/.devkit/demo workspace use Alpha
./src/bin/hxxddk --root ./src/.devkit/demo workspace inspect Alpha

# 删除会先显示资源预览，再要求确认
./src/bin/hxxddk --root ./src/.devkit/demo workspace delete --yes Beta
./src/bin/hxxddk --root ./src/.devkit/demo workspace delete --yes --purge Alpha
```

管理界面：守护进程启动后访问 <http://127.0.0.1:17860>。首次打开时浏览器会自动建立本地会话。
「项目与备份」页面提供源码配置、clone/pull/checkout、初始化、启停、日志与备份恢复；
「服务与运行时」页面提供启动/停止/重启、连接测试、日志、外部服务配置与组件验证。

### 服务与运行时（M2）

```bash
# 查看共享服务状态（NOT_INSTALLED 时需安装工具或切换外部服务）
./src/bin/hxxddk --root ./src/.devkit/demo service list
./src/bin/hxxddk --root ./src/.devkit/demo service start postgresql
./src/bin/hxxddk --root ./src/.devkit/demo service start redis

# 验证运行时组件（原生优先，Windows 自动回退 WSL2）
./src/bin/hxxddk --root ./src/.devkit/demo runtime list

# 连接测试与日志
./src/bin/hxxddk --root ./src/.devkit/demo service test postgresql
./src/bin/hxxddk --root ./src/.devkit/demo service logs postgresql --tail 100

# 切换到外部 PostgreSQL；密码只从环境变量 HXXDDK_SERVICE_PASSWORD
# 或标准输入读取，不会出现在命令历史中
printf '%s' "$PGPASSWORD" | ./src/bin/hxxddk --root ./src/.devkit/demo \
  service update postgresql --mode external --host db.example.com --port 5432 \
  --username discourse --database postgres --tls --password-stdin

# 创建/重试 Workspace 数据库
./src/bin/hxxddk --root ./src/.devkit/demo workspace provision Alpha
```

托管服务启动后会自动为所有 Workspace 创建数据库与角色。新建 Workspace 时若服务未运行，
`database.provisioned` 为 `false`，`database.provision_error` 记录原因，可稍后执行 `workspace provision` 重试。
停止 PostgreSQL/Redis 前会检查运行中的 Workspace，存在依赖时返回 `SERVICE_IN_USE`。
托管 Redis 的密码自动生成并保存在受保护的本机密钥存储中。

### 源码、项目与备份（M3）

```bash
# 选择活动 Workspace；以下命令未指定 --workspace 时都使用它
./src/bin/hxxddk --root ./src/.devkit/demo workspace use Alpha

# 配置并克隆源码（仓库地址不能包含用户名/密码）
./src/bin/hxxddk --root ./src/.devkit/demo project source \
  --repo https://github.com/discourse/discourse.git --branch main
./src/bin/hxxddk --root ./src/.devkit/demo project clone

# 查看状态与日志，更新或切换版本
./src/bin/hxxddk --root ./src/.devkit/demo project status
./src/bin/hxxddk --root ./src/.devkit/demo project fetch
./src/bin/hxxddk --root ./src/.devkit/demo project pull
./src/bin/hxxddk --root ./src/.devkit/demo project checkout v3.3.0
./src/bin/hxxddk --root ./src/.devkit/demo project logs --source init --tail 100

# 安装依赖、建库与迁移，然后启动 Rails
./src/bin/hxxddk --root ./src/.devkit/demo project init
./src/bin/hxxddk --root ./src/.devkit/demo start
./src/bin/hxxddk --root ./src/.devkit/demo stop
./src/bin/hxxddk --root ./src/.devkit/demo restart

# 备份（必须停止项目）与恢复
./src/bin/hxxddk --root ./src/.devkit/demo backup create --source --database
./src/bin/hxxddk --root ./src/.devkit/demo backup list
./src/bin/hxxddk --root ./src/.devkit/demo backup restore <备份 ID> --yes
```

初始化与 Git 操作通过后台任务执行，CLI 会轮询任务进度；API 返回 `202 Accepted` 与任务 ID。
`project init --skip-deps` / `--skip-db` 可跳过依赖或数据库步骤。
备份归档保存在 `workspaces/<ID>/backups/`，恢复前必须调用 `restore-preview` 获取 5 分钟有效的一次性令牌。
开发环境变量写入 `workspaces/<ID>/config/discourse.env`（Unix 0600 / Windows 当前用户 ACL），不会写入源码目录。

默认端口（均可在 `config/devkit.json` 中修改，重启后生效）：

| 用途 | 端口 |
|---|---|
| 管理 API / UI | 17860 |
| 托管 PostgreSQL | 15432 |
| 托管 Redis | 16379 |

Workspace 开发端口从 3000 起分配，Redis DB 从 1 起分配（上限 15 个 Workspace）。

### 日志、运行时与设置（M4）

管理界面新增「运行时」「日志」「设置」三个页面：

- **运行时**：展示版本矩阵、组件期望/实际版本、执行环境、路径与 SHA-256 校验状态，可单独或批量重新验证；
- **日志**：按 DevKit / 共享服务 / Workspace 分组选择日志来源，查看末尾 100–2000 行，可选自动刷新；
- **设置**：编辑网络端口、默认 Profile、矩阵文件、WSL 发行版、项目默认值与超时、日志级别/轮转、更新通道；保存后列出需要重启 devkitd 的字段。

对应本地 API（需要会话或 Bearer Token）：

```text
GET   /api/v1/logs/sources
GET   /api/v1/logs?source=<id>&tail=<1-2000>
GET   /api/v1/settings
PATCH /api/v1/settings
GET   /api/v1/events        # SSE，任务状态事件
```

日志来源 ID 由守护进程生成并按精确匹配校验，客户端不能传入任意文件路径。

## 前端开发

```bash
cd src/web
npm install
npm run dev
```

Vite 开发服务器监听 `127.0.0.1:5173`，并将 `/api` 代理到 `127.0.0.1:17860`。以 `--dev` 启动守护进程可允许 5173 来源：

```bash
./src/bin/hxxddk --dev daemon run
```

生产构建：

```bash
cd src/web
npm run build   # tsc --noEmit + vite build + 保留 go:embed 所需的 .keep
```

## 测试

```bash
cd src
go test ./...
go vet ./...

cd web
npm run build   # 包含 TypeScript 类型检查
```

## 目录

```text
docs/                     设计规范与开发实施计划
scripts/build.sh          前端 + Go 二进制构建脚本
src/cmd/devkitd/          守护进程入口
src/cmd/hxxddk/           CLI 入口
src/internal/             API、CLI、配置、Workspace、任务、诊断、日志等
src/web/                  React + TypeScript + Vite + Tailwind 管理 UI
src/internal/webembed/    内嵌前端产物（构建后填充）
```

实例根目录布局：

```text
config/devkit.json        实例配置（端口、活动 Workspace 等）
config/secrets.json       API Token 与 Workspace 凭据（Unix 0600 / Windows 当前用户 ACL）
run/devkitd.json          守护进程 PID 与地址
logs/                     结构化 JSONL 日志（任务历史在 logs/tasks.jsonl）
runtime/ services/        组件与共享服务目录（数据、日志、生成的受保护配置）
workspaces/<ULID>/        每个 Workspace 的 source/config/logs/data/backups/tmp
```

## 安全边界

- 管理 API 仅绑定回环地址，默认拒绝非本机 Host；
- CLI 使用 `secrets.json` 中的本地 Bearer Token；浏览器使用 SameSite=Strict 会话 Cookie，写操作还必须携带 CSRF Token；
- 任何带 Origin 的写请求都必须来自允许的本机来源；
- 凭据不会写入普通配置、日志或进程参数；日志输出前统一脱敏；
- 删除操作需要预览令牌，按资源分层执行，元数据最后删除；
- 首版凭据以明文保存在受 ACL/权限保护的本地文件内，同用户进程仍可读取；OS 凭据库（DPAPI/Keychain/Secret Service）计划在后续里程碑接入；
- 托管 Redis 使用生成的受保护 `redis.conf`，密钥不会出现在命令行参数中；服务日志与诊断输出在返回前统一脱敏。

## 里程碑

- **M0 + M1（已完成）**：实例、Workspace、API、CLI、管理 UI 与安全边界；
- **M2（已完成）**：Runtime 组件、托管 PostgreSQL/Redis、数据库/角色隔离、外部服务、WSL2 桥接；
- **M3（已完成）**：Git 源码、Discourse 初始化、项目启停与日志、备份恢复；
- **M4（下一步）**：完整 GUI 管理、服务状态与操作审计；
- **M5**：离线组件包、自动部署与发布流水线。
