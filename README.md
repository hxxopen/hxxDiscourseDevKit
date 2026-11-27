# hxxDiscourseDevKit

hxxDiscourseDevKit 是一个独立、通用、跨平台的 Discourse 开发环境发行产品。它为开发者自动准备 Discourse 开发所需的运行环境、基础服务和管理工具，降低 Ruby/Rails、Node.js、PostgreSQL、Redis 等依赖的安装与配置门槛。DevKit 面向整个 Discourse 生态，不是 HXX AgentForge 专属工具。任何开发者均可使用它开发官方 Discourse、Discourse 插件、Discourse 主题，或基于 Discourse 开展二次开发。

## 当前状态

已实现 **M0–M3**，并完成 M4 的运行时、日志、设置、操作审计、独立备份页、SSE 实时刷新，以及 M6 的插件/主题基础能力、插件自动化测试与主题构建/本地预览：

- 单守护进程 `devkitd` + 瘦 CLI `hxxddk`，CLI 可按需自动拉起守护进程；
- 多 Workspace：独立目录、HTTP 端口、PostgreSQL 数据库/用户、随机凭据与 Redis DB 索引；
- Workspace 创建、重命名/描述更新（API）、活动切换、详情与状态检查；
- 删除预览 + 一次性确认令牌 + 后台任务 + record / purge 分层删除；purge 会删除该 Workspace 的数据库与 Redis 逻辑 DB；
- 本地管理 API：回环地址绑定、Bearer Token、HttpOnly 会话 Cookie + CSRF、Host/Origin 校验；
- Runtime 版本矩阵与组件验证（Ruby、Node、Git、PostgreSQL、Redis），支持原生与 WSL2 执行桥接；
- Windows 上可在运行时页面把 PostgreSQL/Redis/Git 安装进现有 WSL2 发行版（预览确认后后台执行），并持久化组件真实路径；
- 托管 PostgreSQL / Redis：`initdb`、`pg_ctl`、`redis-server` 生命周期，外部服务端点切换与连接测试；
- 按 Workspace 自动创建独立的 PostgreSQL 数据库/角色，启动 PostgreSQL 后自动对账；
- 服务日志、依赖检查（有运行中的 Workspace 时拒绝停止服务）与扩展诊断；
- Git 源码管理：clone / fetch / pull / checkout，脏工作区保护，凭据不进入远程 URL 或进程参数；
- Discourse 初始化：`bundle config set --local path vendor/bundle`、`bundle install`、yarn/npm、`rails db:create db:migrate`；
- 项目生命周期：`rails server` 进程监督、TCP 健康检查、按 Workspace 停止进程树、运行/初始化/Git 日志；
- 备份与恢复：tar.gz + manifest + 每文件 SHA-256，可选源码与数据库；恢复预览 + 一次性令牌 + 临时目录校验与失败回滚；
- 日志中心：统一列出 DevKit 主日志、任务历史、Workspace 项目/初始化/Git/插件测试日志与托管服务日志，支持尾部读取、行数选择、自动刷新与返回前脱敏；
- 操作审计：记录本机管理 API 的写操作与拒绝请求（操作、Workspace、目标、来源、结果、HTTP 状态、请求 ID、耗时），按日写入 0600 JSONL；保留天数跟随日志保留设置。提供 `GET /api/v1/audit`、`hxxddk audit list` 与「审计」页面；只记录请求元数据，不记录请求体，因此不会包含密码、API Key 或确认令牌；
- 设置页：修改管理 API 端口、运行时 Profile/矩阵/WSL 发行版、项目默认值、日志与更新策略；持久化后标出需要重启 devkitd 的字段；
- SSE 事件流：后台任务状态变化时刷新 Workspace、服务、项目与备份缓存；
- 概览页直接展示活动 Workspace 的项目运行状态，支持启动、停止、重启并显示任务进度；
- 插件与主题：插件清单与脚手架创建（源码 `plugins/` 目录，要求 Discourse 源码已安装），主题清单与脚手架创建（Workspace `data/themes/`），解析 `plugin.rb` / `about.json` 元数据；支持依赖检查（元数据完整性、`required_version` / `minimum_discourse_version` / `maximum_discourse_version` 与已安装 Discourse 版本比较、`package.json` 可解析性与主题目录结构）；支持生成可分发/可导入的 ZIP 包；主题可一键构建（先检查、后打包）并导入当前 Workspace 运行中的本地 Discourse 预览（管理员 API Key 保存在本机密钥文件，自动复用预览主题）；插件可在项目停止时启用/禁用（在源码 `plugins/` 与 `data/disabled-plugins/` 之间迁移目录）；插件自动化测试使用 Workspace 隔离测试数据库运行 `db:create` / `db:migrate` 与插件 RSpec，并实时回收脱敏日志；提供管理 API、`hxxddk plugin|theme` 命令与独立页面；
- Project Profile：5 个内置模板（官方、插件、主题、自定义、HXX AgentForge），提供项目类型、默认源码、来源/操作/权限说明与 Runtime/服务/源码依赖预检；创建 Workspace 时可选择模板或用 `--repo` / `--branch` 覆盖；
- 第三方模板来源：注册本地目录或 Git 仓库，读取根目录的 `devkit-profiles.json` 清单并把其中的 Profile 并入模板列表；Git 来源检出到实例 `profiles/<来源 ID>`，可刷新与注销。来源清单只含声明数据，DevKit 不执行仓库中的代码；未验证模板在创建 Workspace 前必须显式确认来源、操作与权限；
- React 管理 UI 共 12 个页面（概览、Workspaces、项目、插件、主题、备份与恢复、运行时、服务、日志、审计、诊断、设置），构建产物通过 `go:embed` 内嵌进二进制；
- 守护进程 E2E 测试：真实启动 `devkitd`，覆盖会话/CSRF、SSE、Workspace 创建与删除、日志读取、设置 PATCH 与后台任务轮询；
- 浏览器端 E2E 测试：Playwright 驱动无头 Edge，覆盖 12 个页面导航与 Workspace 创建/审计记录/删除流程；使用独立端口（17870）与一次性实例根目录，并在配置中把 `storage.data_root` 固定到该根目录，不会读写本机真实实例的数据；
- 结构化日志、敏感值脱敏、单实例文件锁、诊断报告与 JSONL 任务历史。

M3 的真实 Discourse 仓库端到端验收（clone、bundle、启动与访问）仍需在目标机器上手动执行；插件/主题目前覆盖清单、脚手架创建、启用/禁用、依赖检查、插件自动化测试、ZIP 打包、主题构建与本地 Discourse 预览；Project Profile 已提供内置模板、第三方来源加载、URL/分支覆盖、依赖预检与执行前确认；i18n 结构化仍待补齐；平台安装包按 M5 继续。

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

cmd:
D:\work\h2x\hxxDiscourseDevKit\src\bin>hxxddk.exe --root ... status

In PowerShell:
```powershell
cd D:\work\h2x\hxxDiscourseDevKit\src\bin
.\hxxddk.exe --root D:\work\h2x\hxxDiscourseDevKit\src\bin daemon run

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
./src/bin/hxxddk --root ./src/.devkit/demo runtime wsl
./src/bin/hxxddk --root ./src/.devkit/demo runtime discover

# 预览并安装到 WSL2（不加 --yes 只显示预览）
./src/bin/hxxddk --root ./src/.devkit/demo runtime install
./src/bin/hxxddk --root ./src/.devkit/demo runtime install --yes postgresql redis git

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
托管 Redis 的密码自动生成并保存在受保护的本机密钥存储中；托管 PostgreSQL 的管理员密码与各 Workspace 数据库用户密码同样自动生成。需要连接已有服务时，在「服务」页对应卡片选择 External 并填写用户名与密码，凭据写入受保护的 `secrets.json`。Discourse 仅使用 PostgreSQL，不支持 MySQL/MariaDB。

#### Windows / WSL2 运行时安装与数据目录

Windows 上不安装原生 PostgreSQL/Redis 包。DevKit 复用系统已安装的 WSL2 发行版，在「运行时」页面的
WSL2 卡片中提供组件安装：

1. 先检测发行版、系统、用户、主目录与 root 执行能力；
2. 点击「安装运行时组件」后先展示将执行的 `apt-get update` / `apt-get install` 命令与目标软件包；
3. 确认后通过后台任务执行固定 argv（不经过 shell），输出写入 `logs/runtime-install.log` 并统一脱敏；
4. 安装完成后自动探测组件真实路径（如 Debian 的 `/usr/lib/postgresql/<版本>/bin/pg_ctl`），
   写入 `config/runtime-discovered.json`，后续启动服务不再依赖 PATH。

自动安装只支持 Debian/Ubuntu 系发行版，且要求发行版允许 `-u root` 执行。其他发行版会提示手动安装。
WSL2 不可用或探测超时时，服务保持 `NOT_INSTALLED`，不会回退到原生安装。

Windows 上运行时命令统一在 WSL2 内解析：默认发行版可用时，`node`、`npm`、`yarn`、`ruby`、`bundle`、
`git` 等命令优先使用 WSL2 中的可执行文件，并忽略 WSL interop 暴露的 `/mnt/c/...` Windows 程序。
Windows 原生 `node`/`npm` 无法以 WSL 工作区（UNC 路径）为当前目录运行，且会向 Linux 源码树写入
Windows 专用二进制和 `.cmd` 包装脚本。若 WSL2 中缺少某个工具，验证会明确报告未找到，而不是回退到
Windows 版本。

自动安装当前只覆盖 `postgresql`、`redis`、`git`；Ruby/Bundler 工具链尚未纳入（计划在 M5 提供
完整安装包）。官方 Discourse Profile 的预检要求 Ruby，且 Discourse `main` 的 Gemfile 要求
`~> 4.0.0`，Ubuntu 24.04 仓库中的 Ruby 3.2 不满足要求。需要时可先在 WSL2 内从源码安装（版本以
Discourse 源码的 Gemfile/`.ruby-version` 为准）：

```bash
wsl.exe -d Ubuntu-24.04 -u root -- bash -c '
  apt-get update &&
  apt-get install -y build-essential autoconf bison libssl-dev libyaml-dev \
    libreadline-dev zlib1g-dev libncurses-dev libffi-dev libgdbm-dev libgmp-dev libtool pkg-config
  cd /tmp && curl -fLO https://cache.ruby-lang.org/pub/ruby/4.0/ruby-4.0.10.tar.gz &&
  tar -xzf ruby-4.0.10.tar.gz && cd ruby-4.0.10 &&
  ./configure --prefix=/opt/ruby-4.0.10 --disable-install-doc &&
  make -j"$(nproc)" && make install &&
  for b in /opt/ruby-4.0.10/bin/*; do ln -sf "$b" "/usr/local/bin/$(basename "$b")"; done'
./src/bin/hxxddk --root ./src/bin runtime discover
./src/bin/hxxddk --root ./src/bin runtime verify ruby
```

`/usr/local/bin` 在 WSL 的非登录 PATH 中，因此无需修改 shell 配置即可被 DevKit 探测；安装后
`profile check` 的 `runtime.ruby` 应变为 `ok`。

Node.js / Yarn 也必须安装在 WSL2 内。Ubuntu 24.04 仓库中的 Node 18 可能低于 Discourse 要求，
建议从官方二进制安装（版本以 Discourse 源码 `package.json` 的 `engines` / `.nvmrc` 为准）：

```bash
wsl.exe -d Ubuntu-24.04 -- bash -c '
  curl -fsSL -o /tmp/node.tar.xz https://nodejs.org/dist/v24.22.3/node-v24.22.3-linux-x64.tar.xz'
wsl.exe -d Ubuntu-24.04 -u root -- bash -c '
  tar -xJf /tmp/node.tar.xz -C /opt &&
  for b in node npm npx corepack; do ln -sfn "/opt/node-v24.22.3-linux-x64/bin/$b" "/usr/local/bin/$b"; done &&
  npm install -g yarn@1.22.22 &&
  ln -sfn /opt/node-v24.22.3-linux-x64/bin/yarn /usr/local/bin/yarn'
./src/bin/hxxddk --root ./src/bin runtime discover
./src/bin/hxxddk --root ./src/bin runtime verify node
```

`config/runtime-discovered.json` 中 Node/Git 若仍记录为 `native`，升级后的 DevKit 会忽略该条目并在
WSL2 中重新解析；执行一次 `runtime discover` 可刷新记录。

WSL2 中的托管 Redis 以 `daemonize yes` 启动：`redis-server` 在发行版内转入后台后 `wsl.exe`
立即退出，因此不会留下标题为 `C:\Program Files\WSL\wsl.exe` 的控制台窗口；停止仍通过
Redis 的 `SHUTDOWN` 命令完成，不依赖该窗口或 Windows 进程句柄。

新实例在 Windows 上默认使用分离式存储：`config/`、`logs/`、`run/` 留在 Windows 实例根目录，
`workspaces/`、`services/`、`backups/`、`tmp/` 位于 WSL2 用户主目录下的 `hxxwork` 子目录
`~/hxxwork`（通过 `\\wsl.localhost\<发行版>\...` 访问）。这保证 PostgreSQL
能在 Linux 原生文件系统上 `initdb`，而不是在 `/mnt/...` 上因权限限制失败。

已有实例保持旧的 Windows 数据目录，不会自动迁移；概览/运行时/设置页会显示迁移提示。旧版本曾把
WSL2 数据放在 `~/.local/share/hxxDiscourseDevKit`；一键迁移会识别该位置下的托管服务数据目录，
连同旧 Windows 数据一起复制到 `~/hxxwork`，不会把它当作外部自定义路径跳过。手动迁移步骤：

```bash
# 1. 停止 devkitd 与所有 Workspace；备份实例根目录
# 2. 把 workspaces/ services/ backups/ tmp/ 复制到 WSL 数据目录
wsl.exe -d <发行版> -- mkdir -p ~/hxxwork
# 3. 编辑 <实例根目录>/config/devkit.json，写入（Windows 路径使用双反斜杠）
#    "storage": { "data_root": "\\\\wsl.localhost\\<发行版>\\home\\<用户>\\hxxwork" }
# 4. 重启 devkitd，确认设置页显示“数据目录”已切换
```

`services.postgresql.data_dir` / `services.redis.data_dir` 的相对路径以数据目录为基准；绝对路径保持不变。
外部（External）PostgreSQL/Redis 始终只做连接与检测，不提供安装、启动、停止或修复操作。

如果暂时不想迁移整个数据目录，也可以只把 PostgreSQL 数据目录放到 WSL2 文件系统：把
`services.postgresql.data_dir` 设为绝对路径
`\\wsl.localhost\<发行版>\home\<用户>\hxxwork\services\postgresql\data`。
当 PostgreSQL 通过 WSL2 执行且数据目录仍落在 `/mnt/<盘符>` 时，DevKit 会在 `initdb` 之前拒绝启动，
返回 HTTP 409 `SERVICE_DATA_DIR_UNSUPPORTED` 并给出迁移提示，而不是执行注定失败的 `initdb`。

Redis 没有 PostgreSQL 那样的 Unix 权限要求，`redis-server` 在 WSL2 中可以把数据目录设在
`/mnt/<盘符>/...`（即 Windows 本地路径）并正常读写，因此旧实例不会因 Redis 数据在 Windows 盘而报错。
但 DrvFs 的 fsync/rename 语义和性能不如 WSL 原生文件系统，迁移到 `~/hxxwork` 后 Redis 数据会随相对路径
`services/redis/data` 一起复制到 WSL2 文件系统。

#### 托管服务的连接边界

托管 PostgreSQL 与 Redis 默认只监听回环地址（`127.0.0.1`），因此局域网内的其他机器、远程主机默认都无法连接：

- PostgreSQL：`127.0.0.1:15432`，TCP 连接要求 `scram-sha-256` 密码认证；
- Redis：`127.0.0.1:16379`，启用 `requirepass` 与 `protected-mode yes`。

它们仍然是标准的本机服务，本机进程可以通过常规客户端连接：

```bash
psql -h 127.0.0.1 -p 15432 -U <数据库用户> <数据库名>
redis-cli -h 127.0.0.1 -p 16379 --no-auth-warning -a '<Redis 密码>'
```

连接凭据由 DevKit 写入 Workspace 的 `config/discourse.env`（Unix 0600 / Windows 当前用户 ACL），其中包含 `DISCOURSE_DB_*` 与 `DISCOURSE_REDIS_*` 变量；Rails 开发服务器也使用同一份配置。管理 UI 和 API 不回显密码：Workspace 详情的 `password_ref` 是密钥名称，「服务 → 数据库」只列出数据库名。

安全边界说明：

- 默认回环监听意味着**没有面向局域网或公网的连接入口**；如需外部访问，必须显式修改 Managed 监听地址（例如 `0.0.0.0`）并在防火墙放行。这会扩大暴露面，不建议在开发机上这样配置；更推荐保留回环监听，按需使用 SSH 隧道或端口转发。
- Linux/macOS 下 `initdb` 使用 `--auth-local=trust`，本机 Unix socket 默认不校验密码；能够访问该 socket 的同机其他操作系统用户可能直接连接数据库。TCP 连接仍要求密码。
- External 模式用于连接用户自己维护的 PostgreSQL/Redis。密码输入框留空表示沿用已保存的凭据；新密码只写入受保护的 `secrets.json`，不会出现在配置或日志中。
- MySQL/MariaDB 不受支持；Discourse 只使用 PostgreSQL。

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

管理界面的「源码配置」在 Workspace 尚未保存仓库地址时，会从设置中填入默认仓库（初始为
`https://github.com/discourse/discourse.git`）。这只是表单预填，不会自动克隆、安装依赖或初始化数据库；
仍需用户保存配置并显式点击「克隆源码」「初始化项目」。
备份归档保存在 `workspaces/<ID>/backups/`，恢复前必须调用 `restore-preview` 获取 5 分钟有效的一次性令牌。
开发环境变量写入 `workspaces/<ID>/config/discourse.env`（Unix 0600 / Windows 当前用户 ACL），不会写入源码目录。

默认端口（均可在 `config/devkit.json` 中修改，重启后生效）：

| 用途 | 端口 |
|---|---|
| 管理 API / UI | 17860 |
| 托管 PostgreSQL | 15432 |
| 托管 Redis | 16379 |

Workspace 开发端口从 3000 起分配，Redis DB 从 1 起分配（上限 15 个 Workspace）。

### 日志、运行时、审计与设置（M4）

管理界面新增「运行时」「日志」「审计」「设置」四个页面：

- **运行时**：展示版本矩阵、组件期望/实际版本、执行环境、路径与 SHA-256 校验状态，可单独或批量重新验证；WSL2 卡片展示发行版与 root 能力，支持预览确认后安装运行时组件、重新探测组件路径；
- **日志**：按 DevKit / 共享服务 / Workspace 分组选择日志来源，查看末尾 100–2000 行，可选自动刷新；
- **审计**：按 Workspace、操作与结果筛选写操作记录，展示时间、目标、来源（界面/CLI/API）、HTTP 状态、请求 ID 与耗时，支持自动刷新；
- **设置**：编辑网络端口、默认 Profile、矩阵文件、WSL 发行版、项目默认值与超时、日志级别/轮转、更新通道；保存后列出需要重启 devkitd 的字段。

对应本地 API（需要会话或 Bearer Token）：

```text
GET   /api/v1/logs/sources
GET   /api/v1/logs?source=<id>&tail=<1-2000>
GET   /api/v1/audit?limit=<1-500>&action=<name>&workspace_id=<id>&result=<success|accepted|failure>&since=<RFC3339|YYYY-MM-DD>
GET   /api/v1/settings
PATCH /api/v1/settings
PUT   /api/v1/settings/theme-preview
DELETE /api/v1/settings/theme-preview
GET   /api/v1/events        # SSE，任务状态事件
```

日志来源 ID 由守护进程生成并按精确匹配校验，客户端不能传入任意文件路径。

审计记录按日写入 `logs/audit/audit-YYYY-MM-DD.jsonl`（Unix 0600 / Windows 当前用户 ACL），保留天数跟随 `logging.retention_days`（重启后生效）。只记录请求方法、路径、Workspace ID、目标、来源、结果、状态码、请求 ID、远端地址与耗时，不记录请求体；异步任务在受理时标记为 `accepted`，最终结果仍以任务历史和对应日志为准。CLI 可用 `hxxddk audit list --limit 50 --action project.start --workspace Alpha` 查看。

### 插件与主题（M6 基础）

插件位于 `<Workspace>/<源码路径>/plugins/<slug>`，遵循 Discourse 的加载约定；创建插件要求当前 Workspace 已安装 Discourse 源码。主题位于 `<Workspace>/data/themes/<slug>`，会随 Workspace 的 `data` 目录一起备份。两者都会生成可解析的元数据（插件 `plugin.rb` 头部注释、主题 `about.json`），并在页面与 CLI 中列出文件数、版本、作者与更新时间。

```text
GET  /api/v1/workspaces/{id}/plugins
POST /api/v1/workspaces/{id}/plugins
GET  /api/v1/workspaces/{id}/plugins/{slug}/check
POST /api/v1/workspaces/{id}/plugins/{slug}/test
POST /api/v1/workspaces/{id}/plugins/{slug}/package
POST /api/v1/workspaces/{id}/plugins/{slug}/enable
POST /api/v1/workspaces/{id}/plugins/{slug}/disable
GET  /api/v1/workspaces/{id}/themes
POST /api/v1/workspaces/{id}/themes
GET  /api/v1/workspaces/{id}/themes/{slug}/check
POST /api/v1/workspaces/{id}/themes/{slug}/build
POST /api/v1/workspaces/{id}/themes/{slug}/preview
POST /api/v1/workspaces/{id}/themes/{slug}/package
```

```bash
# 列出、检查、创建与打包（未指定 --workspace 时使用活动 Workspace）
./src/bin/hxxddk plugin list
./src/bin/hxxddk plugin create "My Plugin" --about "..." --version 0.1.0
./src/bin/hxxddk plugin check my-plugin
./src/bin/hxxddk plugin test my-plugin
./src/bin/hxxddk plugin package my-plugin
./src/bin/hxxddk plugin disable my-plugin
./src/bin/hxxddk plugin enable my-plugin
./src/bin/hxxddk theme list
./src/bin/hxxddk theme create "My Theme" --slug my-theme
./src/bin/hxxddk theme check my-theme
./src/bin/hxxddk theme build my-theme
./src/bin/hxxddk theme preview my-theme   # 需要本地 Discourse 运行并已保存 API Key
./src/bin/hxxddk theme package my-theme
```

分发包保存在 `workspaces/<ID>/data/exports/plugins|themes/`，返回文件大小、文件数与 SHA-256；归档排除 `.git`、`node_modules` 与符号链接。插件创建后需要重启 Rails 开发服务器才会被 Discourse 加载；主题的 ZIP 可直接在 Discourse 管理后台导入，也可使用 DevKit 的本地预览。

主题构建先运行依赖检查，只有在没有阻塞错误时才打包；存在错误时返回 `ready=false` 且不生成新包（旧 ZIP 不会被当作本次结果）。主题预览要求 Workspace 项目正在运行，先探测 `http://<host>:<port>/about.json`，再通过 `POST /admin/themes/import.json` 导入 ZIP。预览只使用当前 Workspace 自己的站点地址，不会向任意远程站点发送 API Key。API Key 需要属于 Discourse 管理员、类型为 Single User 且勾选 Global Key；通过 `PUT /api/v1/settings/theme-preview` 保存到本机 0600 权限的 `secrets.json`，不会回显或写入日志。首次预览会创建主题，之后按 `data/theme-previews.json` 中记录的 slug → theme ID 更新同一个主题，返回 `action=created|updated` 以及预览/管理 URL。

依赖检查只读、不修改文件，报告分为 `ok`、`warning`、`error` 与 `skipped`。插件检查读取 `plugin.rb` 的 `# required_version:`，主题检查读取 `about.json` 的 `minimum_discourse_version` / `maximum_discourse_version`，并与源码 `lib/version.rb` 中识别出的版本比较；`3.4.0.beta1` 会被视为低于 `3.4.0`。插件还会解析可选的 `package.json`（只报告依赖数量，不自动安装）；主题会检查是否存在常见的样式/资源目录。只有 `error` 会令 `ready=false`，CLI `check` 在存在错误时退出码为 1，便于 CI 使用。

插件启用/禁用通过移动目录实现，不修改插件文件：禁用时从源码 `plugins/<slug>` 移到 Workspace 的 `data/disabled-plugins/<slug>`，启用时移回。操作要求当前 Workspace 的项目已停止，完成后需重启 Rails 才会生效；被禁用的插件仍会出现在清单中（状态为「已禁用」），也可以继续打包。尝试在项目运行中切换会返回 `PROJECT_RUNNING`，重复启用/禁用返回 `EXTENSION_STATE_CONFLICT`。

插件自动化测试要求 Workspace 已初始化、插件已启用，且插件 `spec/` 下存在 `*_spec.rb`。DevKit 使用管理连接为 Workspace 创建独立的 `<数据库名>_test` 数据库，再以 `RAILS_ENV=test LOAD_PLUGINS=1 RAILS_DB=<数据库名>_test` 执行 `rake db:create db:migrate` 与 `bundle exec rspec plugins/<slug>/spec`，不会写入开发数据库，也不需要给 Workspace 角色授予 CREATEDB。测试作为可取消的后台任务运行（API 返回 `202` 与 `task_id`，可用 `GET /api/v1/tasks/{id}` 轮询），输出按完整行脱敏后写入 `workspaces/<ID>/logs/test.log`（8 MiB 轮转），API 与 UI 通过日志来源 `workspace:<ID>:test` 读取尾部内容；CLI 在任务结束后打印日志尾部，失败时退出码为 1。purge 删除 Workspace 时会同时清理开发库与测试库。

### Project Profile（M6）

模板只决定 Workspace 的项目类型、默认源码、依赖预检与说明，不改变 DevKit 核心架构，也不会在创建时克隆仓库或执行远程代码。第三方来源清单同样只是数据；DevKit 只会为 Git 来源执行 `git clone` 读取清单，不会运行仓库中的脚本。

| Profile | 项目类型 | 默认源码 | 说明 |
|---|---|---|---|
| `discourse-official` | `discourse` | 官方仓库 `main` | 完整站点开发 |
| `discourse-plugin` | `discourse-plugin` | 官方仓库 `main` | 插件开发，插件位于源码 `plugins/` |
| `discourse-theme` | `discourse-theme` | 官方仓库 `main` | 主题开发，主题位于 `data/themes/` |
| `discourse-custom` | `discourse-custom` | 无，创建时必须提供 | 自定义 Fork / 二次开发 |
| `hxx-agentforge` | `hxx-agentforge` | 无，创建时必须提供 | HXX AgentForge 下游模板 |

```text
GET  /api/v1/profiles
GET  /api/v1/profiles/{id}
GET  /api/v1/profiles/{id}/check
GET  /api/v1/profile-sources
POST /api/v1/profile-sources
POST /api/v1/profile-sources/{id}/refresh
DELETE /api/v1/profile-sources/{id}
POST /api/v1/workspaces            # 可带 profile_id、repository_url、branch、accept_profile
```

```bash
./src/bin/hxxddk profile list
./src/bin/hxxddk profile show discourse-plugin
./src/bin/hxxddk profile check discourse-official
./src/bin/hxxddk workspace create PluginDev --profile discourse-plugin
./src/bin/hxxddk workspace create ForkDev --profile discourse-custom \
  --repo https://git.example.com/team/discourse.git --branch develop

./src/bin/hxxddk profile source list
./src/bin/hxxddk profile source add ./examples/profile-source
./src/bin/hxxddk profile source add https://git.example.com/team/profiles.git --branch main
./src/bin/hxxddk profile source refresh <来源 ID>
./src/bin/hxxddk profile source remove <来源 ID> --yes
./src/bin/hxxddk workspace create AcmeDev --profile acme-discourse-plugin \
  --repo https://git.example.com/team/discourse.git --accept-profile
```

`profile check` 返回 `ready`、错误数与提醒数：Runtime 组件缺失或不可用计为错误；服务未启动、未安装或认证未通过，以及模板未内置源码仓库，计为提醒。创建操作不会被预检错误阻止，但项目初始化会在环境修复前失败。模板的 `origin`、`operations` 与 `permissions` 会随 API 返回，并在 CLI `profile show` 与新建 Workspace 弹窗中展示。

#### 第三方模板来源

来源根目录必须包含 `devkit-profiles.json`（严格 JSON，不接受未知字段，最大 1 MiB）。示例见 [`examples/profile-source/devkit-profiles.json`](examples/profile-source/devkit-profiles.json)：

```json
{
  "schema_version": 1,
  "name": "Acme 团队模板",
  "version": "2026.11.1",
  "description": "团队内部模板来源",
  "profiles": [
    {
      "id": "acme-discourse-plugin",
      "name": "Acme 插件开发模板",
      "description": "以官方 Discourse 为宿主的插件开发模板。",
      "category": "third-party",
      "project_type": "discourse-plugin",
      "source_required": true,
      "extension_kind": "plugin",
      "runtime_components": ["ruby", "node", "git"],
      "services": ["postgresql", "redis"],
      "operations": [
        {
          "id": "workspace.create",
          "title": "创建 Workspace 目录",
          "description": "在实例根目录下创建独立 Workspace。",
          "destructive": false
        }
      ],
      "permissions": [
        {
          "id": "workspace.write",
          "title": "写入实例目录",
          "description": "在实例根目录内创建 Workspace 子目录。",
          "risk": "low"
        }
      ]
    }
  ]
}
```

- 单个来源最多 50 个 Profile、20 项操作与 20 项权限；来源注册表最多 20 个来源。
- Profile ID、项目类型与操作/权限 ID 均受限字符集；ID 不得与内置模板或其他来源重复（冲突返回 `PROFILE_SOURCE_CONFLICT`）。
- `runtime_components` 仅支持 `ruby`、`node`、`git`，`services` 仅支持 `postgresql`、`redis`；省略时使用这组默认值。
- 本地来源以绝对路径登记，刷新时重新读取目录；Git 来源以浅克隆检出到 `<实例根目录>/profiles/<来源 ID>`，刷新会重新克隆并在成功后替换旧检出。Git 不可用或拉取失败时保留旧模板并记录 `last_error`。
- 第三方 Profile 的 `origin.verified` 固定为 `false`。创建 Workspace 时 API 必须收到 `accept_profile: true`，CLI 使用 `--accept-profile`，管理 UI 会先展示来源、操作与权限并要求勾选确认；未确认返回 `PROFILE_ACK_REQUIRED`。DevKit 只运行固定的 Workspace 创建流程，不执行模板定义的操作，也不运行来源仓库代码。
- 本地来源目录不会被注销操作删除；Git 来源的 DevKit 检出目录会随注销清理。

创建 Workspace 时传入的仓库地址会经过协议、凭据与分支校验，含用户名/密码的 URL 会被拒绝。

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

# 浏览器端 E2E（需先构建 src/bin/devkitd）
npm run test:e2e          # 类型检查 + 无头 Edge
npm run test:e2e:headed   # 显示浏览器窗口
```

浏览器 E2E 使用 `http://127.0.0.1:17870` 和一次性根目录 `src/.devkit/e2e-browser`，每次运行前重建。Windows 默认复用已安装的 Edge（`channel: msedge`）；其他环境可先执行 `npx playwright install chromium`，再设置 `PLAYWRIGHT_CHANNEL=chromium`。测试通过 `storage.data_root` 把数据固定在一次性根目录，避免 Windows 新实例默认使用共享的 WSL2 用户数据目录而污染真实 Workspace。

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
<实例根目录>/
config/devkit.json        实例配置（端口、活动 Workspace、storage.data_root 等）
config/secrets.json       API Token 与 Workspace 凭据（Unix 0600 / Windows 当前用户 ACL）
config/runtime-discovered.json  探测到的组件真实路径
run/devkitd.json          守护进程 PID 与地址
logs/                     结构化 JSONL 日志（任务历史在 logs/tasks.jsonl；操作审计在 logs/audit/）
runtime/                  本地运行时目录

<数据目录>/               默认与根目录相同；Windows 新实例默认在 WSL2 用户主目录
workspaces/<ULID>/        每个 Workspace 的 source/config/logs/data/backups/tmp
services/                 托管服务的数据、日志与生成的受保护配置
backups/ tmp/             备份与临时文件
```

## 安全边界

- 管理 API 仅绑定回环地址，默认拒绝非本机 Host；
- CLI 使用 `secrets.json` 中的本地 Bearer Token；浏览器使用 SameSite=Strict 会话 Cookie，写操作还必须携带 CSRF Token；
- 任何带 Origin 的写请求都必须来自允许的本机来源；
- 凭据不会写入普通配置、日志或进程参数；日志输出前统一脱敏；
- 操作审计按日写入 `logs/audit/audit-YYYY-MM-DD.jsonl`（Unix 0600 / Windows 当前用户 ACL），只记录请求元数据，不记录请求体、密码、API Key 或确认令牌；
- 删除操作需要预览令牌，按资源分层执行，元数据最后删除；
- 首版凭据以明文保存在受 ACL/权限保护的本地文件内，同用户进程仍可读取；OS 凭据库（DPAPI/Keychain/Secret Service）计划在后续里程碑接入；
- 托管 Redis 使用生成的受保护 `redis.conf`，密钥不会出现在命令行参数中；服务日志与诊断输出在返回前统一脱敏。

## 里程碑

- **M0 + M1（已完成）**：实例、Workspace、API、CLI、管理 UI 与安全边界；
- **M2（已完成）**：Runtime 组件、托管 PostgreSQL/Redis、数据库/角色隔离、外部服务、WSL2 桥接与组件安装；
- **M3（已完成）**：Git 源码、Discourse 初始化、项目启停与日志、备份恢复；
- **M4（主体完成）**：完整 GUI 管理、SSE 实时刷新、操作审计与浏览器端 UI E2E；i18n 结构化待补；
- **M5**：离线组件包、自动部署与发布流水线；
- **M6（基础能力已实现）**：插件/主题清单、脚手架创建、插件启用/禁用、依赖检查、插件自动化测试与 ZIP 打包；主题构建与本地 Discourse 预览（管理员 API Key、预览主题复用）；内置 Project Profile、依赖预检与创建时模板解析；第三方模板来源注册、Git/本地加载、刷新/注销与执行前确认。
