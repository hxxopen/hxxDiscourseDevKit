# hxxDiscourseDevKit 开发实施计划

- **文档版本**：0.1.0
- **文档状态**：待确认（确认后按本计划进入实现）
- **上游设计文档**：`docs/hxxDiscourseDevKit_跨平台技术架构与实施规范_v0.1.md`（下称"设计规范"）
- **项目根目录 / 默认工作目录**：`D:\work\h2x\hxxDiscourseDevKit`
- **实现位置**：`src/`
- **适用范围**：从空仓库到第一版可用产品的工程落地计划

---

## 0. 实施边界与阅读说明

### 0.1 设计规范与本计划的关系

设计规范定义**产品定位、边界、架构原则和验收标准**；本计划把这些内容转换为：

1. 具体的工程决策（技术选型、目录、数据模型、接口）；
2. 可执行的里程碑（M0–M6）与任务拆分；
3. 每个里程碑的验收标准（DoD）；
4. 需要用户确认的关键决策。

设计规范中标注为"建议""示例"的内容（如目录名、端口、JSON 字段），本计划会给出确定性取值；与设计规范冲突或需要调整的地方会在文中显式标注"**偏差说明**"。

### 0.2 本轮实施范围（建议）

设计规范覆盖 Phase 1–6，完整实现是数月级工程。建议按里程碑推进，**本次确认后先实施 M0 + M1**：

- **M0**：工程骨架、配置存储、日志、错误模型、守护进程、CLI 骨架、CI。
- **M1**：Workspace 核心能力 + Default 自动创建 + 本地管理 API + 最小可用管理 UI。

M1 完成后即可在 Windows / macOS / Linux 上运行 DevKit 本体、创建和管理多个 Workspace；Runtime/服务/项目初始化按 M2–M3 继续。这样能尽早验证核心架构，而不是一次性铺开全部模块。

### 0.3 明确不在本轮范围

- 不安装、不启动真实 PostgreSQL/Redis（M1 只生成隔离配置和凭据）。
- 不克隆真实 Discourse 源码（M3）。
- 不实现 Windows WSL2 发行版自动部署（M5；M2 仅支持已存在 WSL2 发行版的执行桥接）。
- 不做安装包、签名、公证、离线包（M5）。
- 不做插件/主题开发的完整工具链（M6）。

---

## 1. 关键工程决策

| 编号 | 决策项 | 选择 | 理由 |
|---|---|---|---|
| D1 | 核心语言 | Go 1.23+ | 设计规范指定；单二进制、跨平台、进程管理便利 |
| D2 | HTTP 框架 | 标准库 `net/http`（Go 1.22+ 路由模式） | 依赖少、攻击面小、长期可维护；不引入 Web 框架 |
| D3 | DevKit 自身状态存储 | 版本化 JSON 文件 + 原子写 + 文件锁 | DevKit 管理 PostgreSQL，若自身依赖数据库会形成循环依赖；Workspace 数量有限，JSON 足够且可人工检查 |
| D4 | 进程模型 | 单守护进程 `devkitd` + CLI `hxxddk` | 避免 GUI/CLI 两套实现；所有变更走同一服务层 |
| D5 | CLI 与 Core 的关系 | CLI 是本地 HTTP API 的瘦客户端，必要时自动拉起 `devkitd` | 强制 GUI 与 CLI 使用同一条业务路径（设计规范 §10） |
| D6 | 管理 UI | React 18 + TypeScript + Vite + Tailwind CSS + TanStack Query | 设计规范指定 React+TS；不引入重型组件库，打包通过 `go:embed` 内嵌 |
| D7 | Workspace 目录命名 | `workspaces/<workspace_id>/`，显示名存于元数据 | 支持重命名、防重名、防路径注入；设计规范 §5.3 为"建议逻辑结构"，此处为有意细化（见 §3.3） |
| D8 | 默认端口 | DevKit API `17860`；托管 PostgreSQL `15432`；托管 Redis `16379` | 避开用户本机常见的 5432/6379/3000，减少冲突；全部可配置 |
| D9 | 凭据存储 | 首版：`config/secrets.json`（权限 0600）；后续接 OS 凭据库 | Windows DPAPI / macOS Keychain / Linux Secret Service 在 M2+ 实现；首版不得把密码写入日志或普通配置 |
| D10 | 平台适配 | 小接口 + 构建标签（`//go:build`）分平台实现 | 避免"万能 Adapter"上帝接口；Windows WSL2 通过 `wsl.exe` 执行桥接 |
| D11 | 日志 | 控制台文本 + JSONL 文件；字段化 + 敏感信息脱敏 | 满足设计规范 §13.1；JSONL 便于诊断和 UI 展示 |
| D12 | 前端资源 | 构建产物内嵌进二进制；开发时 Vite 代理到 17860 | 单文件分发，无需 Node 运行时 |
| D13 | 测试 | Go `testing` + `httptest` + 平台 Fake；前端 Vitest | 不依赖真实 PostgreSQL/Redis 即可跑核心测试 |
| D14 | 发布 | GitHub Actions + GoReleaser + 前端构建 | 对应设计规范 §11.3 的分发方式 |
| D15 | 桌面壳 | 首版不做；浏览器访问本机管理页 | 设计规范 §3.3 明确不要求 Electron |

### 1.1 一个重要架构约束：DevKit 自身不依赖被管理服务

DevKit Core 的状态、配置、任务记录、Workspace 元数据**全部保存在文件系统中**，不使用它自己管理的 PostgreSQL/Redis。这样在数据库损坏、未启动或用户选择外部服务时，DevKit 仍能启动、诊断和修复。

---

## 2. 仓库与代码布局

```text
D:\work\h2x\hxxDiscourseDevKit\
├── docs/
│   ├── hxxDiscourseDevKit_跨平台技术架构与实施规范_v0.1.md
│   └── hxxDiscourseDevKit_开发实施计划_v0.1.md
├── src/
│   ├── go.mod                          # module github.com/hxx/hxxDiscourseDevKit（待确认）
│   ├── go.sum
│   ├── cmd/
│   │   ├── devkitd/main.go             # 守护进程入口
│   │   └── hxxddk/main.go              # CLI 入口
│   ├── internal/
│   │   ├── app/                        # 组合根：装配、启动顺序、优雅退出
│   │   ├── api/                        # HTTP server、路由、中间件、handler
│   │   ├── cli/                        # CLI 命令解析与输出
│   │   ├── config/                     # devkit.json 模型、默认值、校验
│   │   ├── workspace/                  # Workspace 领域模型与管理器
│   │   ├── project/                    # 项目初始化、检查、启动/停止编排
│   │   ├── gitx/                       # Git 命令封装与安全检查
│   │   ├── runtime/                    # Runtime 组件注册、版本矩阵、校验
│   │   ├── service/                    # PostgreSQL/Redis 生命周期与外部服务
│   │   ├── process/                    # 进程启动、日志泵、终止、进程树
│   │   ├── task/                       # 异步任务、进度、事件、取消
│   │   ├── platform/
│   │   │   ├── platform.go             # 能力接口与公共类型
│   │   │   ├── linux/                  # Linux 实现（systemd 用户服务可选）
│   │   │   ├── darwin/                 # macOS 实现（launchd 可选）
│   │   │   └── windows/                # Windows + WSL2 实现
│   │   ├── diagnostics/                # 检查项框架与各检查实现
│   │   ├── backup/                     # 备份格式、创建、校验、恢复
│   │   ├── secret/                     # 密钥生成、存储、脱敏
│   │   ├── store/                      # 原子文件读写、锁、目录管理
│   │   ├── id/                         # ULID 生成
│   │   ├── logging/                    # slog 配置、JSONL、轮转、脱敏
│   │   ├── httpx/                      # 错误响应、参数校验、请求 ID
│   │   ├── migrate/                    # schema 迁移框架
│   │   └── version/                    # 版本与构建信息（ldflags 注入）
│   ├── web/                            # 管理 UI
│   │   ├── package.json
│   │   ├── vite.config.ts
│   │   ├── index.html
│   │   └── src/{api,pages,components,hooks,i18n,styles}/
│   ├── webembed/                       # go:embed 封装（web/dist）
│   └── testdata/
├── .github/workflows/
├── .gitignore
├── Makefile
└── README.md
```

**说明**

- Go module 放在 `src/` 下，使 `src/web` 与 Go 代码同属一个构建根。
- 不把运行数据放在仓库内。开发时使用 `--root ./src/.devkit`（已加入 `.gitignore`）。
- `internal/` 防止外部包误用实现细节。

---

## 3. 实例根目录与路径

### 3.1 默认根目录

| 平台 | 默认根目录 |
|---|---|
| Windows | `%LOCALAPPDATA%\hxxDiscourseDevKit` |
| macOS | `~/Library/Application Support/hxxDiscourseDevKit` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/hxxDiscourseDevKit` |
| 开发/测试 | 显式 `--root` 或环境变量 `HXXDDK_HOME` |

优先级：`--root` > `HXXDDK_HOME` > 平台默认。解析后的绝对路径写入 `run/devkitd.json`，CLI 通过它找到正在运行的实例。

**设计规范偏差说明**：设计规范 §5.3 的目录树是一个逻辑布局；实际实现会把只读 Runtime 与用户数据分开放置（设计规范 §5.3 也允许）。首版先统一放在根目录下，M5 再按平台规范拆分。

### 3.2 运行根目录布局

```text
<root>/
├── config/
│   ├── devkit.json          # 实例配置
│   ├── secrets.json         # API token、数据库密码等（0600）
│   └── runtime-matrix.json  # 组件版本矩阵（随版本发布/可覆盖）
├── workspaces/
│   └── <workspace_id>/      # 不可变 ID，显示名在 workspace.json
│       ├── workspace.json
│       ├── source/
│       ├── config/
│       ├── logs/
│       ├── data/
│       ├── backups/
│       └── tmp/
├── runtime/
│   ├── ruby/  node/  git/  tools/
├── services/
│   ├── postgresql/{data,logs,run}/
│   └── redis/{data,logs,run}/
├── logs/
│   ├── devkit-20261127.jsonl
│   └── tasks.jsonl
├── run/
│   ├── devkitd.json         # pid、addr、started_at
│   └── devkitd.lock         # 单实例锁
└── backups/                 # 全局导出的备份（可选）
```

### 3.3 Workspace 物理目录用 ID 而非名称

- 目录：`workspaces/01JXXXXXXXXXXXX/`；
- 显示名 `Default` 保存在 `workspace.json`；
- 重命名只改元数据，不移动源码；
- 避免名称重名、大小写问题（Windows）、路径分隔符注入和保留名（`CON`、`NUL` 等）。

**偏差说明**：设计规范示例使用 `workspaces/Default/`。此调整不改变产品行为，只改变磁盘目录命名，并在 UI 中始终展示显示名。

### 3.4 路径安全

- 所有用户提供的相对路径必须通过 `filepath.IsLocal` 校验；
- 最终路径必须位于 `<root>` 或对应 Workspace 目录内（解析符号链接后校验）；
- 禁止把 `..`、盘符、UNC 路径（除 WSL 受控映射外）写入配置；
- Workspace 名称校验：1–64 字符，允许字母、数字、中文、空格、`-`、`_`；禁止 `/ \ : * ? " < > |` 及控制字符。

---

## 4. 数据模型

### 4.1 `config/devkit.json`

```json
{
  "schema_version": 1,
  "instance_id": "01J9Z8Q7K3V2M4N6P8R0T2W4X6",
  "active_workspace_id": "01J9Z8Q7K3V2M4N6P8R0T2W4X6",
  "created_at": "2026-11-27T09:00:00+08:00",
  "updated_at": "2026-11-27T09:00:00+08:00",
  "network": {
    "host": "127.0.0.1",
    "api_port": 17860
  },
  "runtime": {
    "default_profile": "default",
    "matrix_file": "config/runtime-matrix.json"
  },
  "services": {
    "postgresql": {
      "mode": "managed",
      "host": "127.0.0.1",
      "port": 15432,
      "admin_user": "hxxddk_admin",
      "admin_password_ref": "pg_admin",
      "data_dir": "services/postgresql/data"
    },
    "redis": {
      "mode": "managed",
      "host": "127.0.0.1",
      "port": 16379,
      "password_ref": "redis_password"
    }
  },
  "logging": {
    "level": "info",
    "retention_days": 30,
    "max_file_mb": 50
  },
  "updates": {
    "channel": "stable",
    "check_on_start": true,
    "auto_install": false
  }
}
```

**要点**

- `mode` 为 `managed` 或 `external`；`external` 时使用 `external` 子对象（host/port/database/username/password_ref/tls）。
- 密码只保存 **ref**，真实值在 `secrets.json`。
- 托管 PostgreSQL/Redis 默认使用非标准端口，避免与用户已有实例冲突。
- `active_workspace_id` 由服务端校验存在性；指向不存在时自动回退到第一个可用 Workspace 并记录警告。

### 4.2 `workspaces/<id>/workspace.json`

```json
{
  "workspace_schema_version": 1,
  "workspace_id": "01J9Z8Q7K3V2M4N6P8R0T2W4X6",
  "name": "Default",
  "description": "Default Discourse development workspace",
  "project_type": "discourse",
  "state": "EMPTY",
  "last_error": "",
  "created_at": "2026-11-27T09:00:00+08:00",
  "updated_at": "2026-11-27T09:00:00+08:00",
  "source": {
    "installed": false,
    "repository_url": "",
    "branch": "",
    "tag": "",
    "commit": "",
    "path": "source",
    "shallow": false,
    "submodules": false,
    "auth_ref": ""
  },
  "runtime_profile": "default",
  "database": {
    "mode": "managed",
    "database_name": "hxxddk_01j9z8q7_default",
    "username": "hxxddk_01j9z8q7_default",
    "password_ref": "ws:01J9Z8Q7K3V2M4N6P8R0T2W4X6:db"
  },
  "redis": {
    "mode": "managed",
    "db_index": 1,
    "key_prefix": "hxxddk:01J9Z8Q7K3V2M4N6P8R0T2W4X6:"
  },
  "network": {
    "host": "127.0.0.1",
    "http_port": 3000
  },
  "project": {
    "initialized": false,
    "process_state": "STOPPED",
    "pid": 0,
    "last_started_at": null
  }
}
```

**数据库/用户名规则**

- 基础名：`hxxddk_<workspace_id 前 8 位小写>_<slug>`；slug 由显示名转写，空则用 `ws`。
- PostgreSQL 标识符上限 63 字节；生成后强制截断并加短哈希后缀保证唯一。
- 每个 Workspace 独立 Database + 独立 Role，Role 仅拥有自己 Database 的权限。

**Redis 隔离**（重要技术说明）

- 设计规范 §5.4 使用 Key Prefix。但 Discourse 是否支持 Redis key namespace 需要以实际版本验证。
- 实施策略：首版使用**独立 Redis DB index**（0–15 中分配）作为 Discourse 的隔离手段，`key_prefix` 仅用于 DevKit 自身写入的键。
- 若 M2 验证 Discourse 支持 namespace/prefix，可切换为 prefix；否则提供"每 Workspace 独立 Redis 实例"作为强隔离选项。
- Redis DB index 只是逻辑隔离，UI 需明确提示，不能宣称为安全隔离（与设计规范 §5.6 一致）。

### 4.3 `config/secrets.json`

```json
{
  "schema_version": 1,
  "api_token": "64-char-random-hex",
  "entries": {
    "pg_admin": "random-password",
    "redis_password": "random-password",
    "ws:01J9Z8Q7K3V2M4N6P8R0T2W4X6:db": "random-password",
    "git:github-main": "token"
  }
}
```

- 权限：Unix `0600`；Windows 使用当前用户 ACL（创建时限制继承）。
- 文件写入使用原子替换；读取失败不自动重建，避免覆盖导致凭据丢失。
- 所有日志、错误详情、API 响应中对这些值做脱敏。

### 4.4 任务记录

```json
{
  "task_id": "01J9ZA...",
  "type": "workspace.delete",
  "workspace_id": "01J9Z8...",
  "status": "running",
  "progress": 40,
  "message": "正在删除源码目录",
  "error_code": "",
  "error_message": "",
  "created_at": "...",
  "started_at": "...",
  "finished_at": null,
  "cancelable": true
}
```

- 每个异步任务写入 `logs/tasks.jsonl`（追加）。
- 内存中保留最近 200 条完整任务与事件；更早的从 JSONL 查询。
- 可取消任务在 `context.Context` 上响应取消；不可取消操作（如数据库 drop 已发出）标记 `cancelable=false` 并说明。

### 4.5 Schema 版本与迁移

- `devkit.json` 和 `workspace.json` 都有 `schema_version`。
- 启动时：
  1. 读取版本；
  2. 若高于当前程序支持版本 → 拒绝启动并提示升级 DevKit（不得降级写入）；
  3. 若低 → 先备份到 `backups/schema-<from>-<timestamp>/`，再执行迁移；
  4. 迁移函数按版本顺序注册，必须幂等。
- 迁移失败 → 回滚备份，守护进程以只读诊断模式启动。

### 4.6 写入与并发

- 单个 `devkitd` 实例持有 `<root>/run/devkitd.lock`（平台文件锁）。
- 进程内以 Workspace 粒度加锁；全局配置以单锁保护。
- 所有 JSON 写入：写临时文件 → `fsync` → `rename` → 目录 `fsync`（Unix）。
- API 读操作加读锁；长任务开始时 snapshot 所需配置，避免运行中配置漂移。

---

## 5. 核心服务层设计

### 5.1 启动顺序（`internal/app`）

1. 解析根目录与命令行参数；
2. 获取单实例锁（失败则读取已有实例地址并退出，提示已有实例）；
3. 初始化日志（先控制台，后文件）；
4. 加载或创建 `devkit.json`；执行 schema 迁移；
5. 加载 secrets（不存在则生成）与 runtime matrix；
6. 初始化 store、task manager、process manager、platform adapter；
7. 协调 Workspace：修复状态字段、确保 Default 存在（幂等）；
8. 启动 HTTP API，写 `run/devkitd.json`；
9. 等待信号；收到 SIGINT/SIGTERM 后：停止 HTTP → 取消任务 → 停止由本进程启动的子进程 → 释放锁 → 退出。

**Default 创建幂等性**：若没有任何 Workspace，则创建 `Default`；若已有但不是名为 Default 的 Workspace，不强行创建（避免用户删除后自动复活）。

### 5.2 WorkspaceManager

职责：CRUD、状态机、端口分配、凭据与命名空间分配、删除预览与执行。

关键方法（示意）：

```go
type Manager struct { /* store, clock, idgen, ports, secrets, adapter, log */ }

func (m *Manager) EnsureDefault(ctx context.Context) (*Workspace, error)
func (m *Manager) Create(ctx context.Context, req CreateRequest) (*Workspace, error)
func (m *Manager) List(ctx context.Context) ([]*Workspace, error)
func (m *Manager) Get(ctx context.Context, id string) (*Workspace, error)
func (m *Manager) Update(ctx context.Context, id string, req UpdateRequest) (*Workspace, error)
func (m *Manager) SetActive(ctx context.Context, id string) error
func (m *Manager) DeletePreview(ctx context.Context, id string) (*DeletePlan, error)
func (m *Manager) Delete(ctx context.Context, id string, opts DeleteOptions) (*Task, error)
func (m *Manager) Reconcile(ctx context.Context, id string) (State, error)
```

**状态机**

```text
EMPTY ──init──> INITIALIZING ──ok──> READY ──start──> STARTING ──ok──> RUNNING
  ▲                    │                 ▲                         │
  │                    └──fail──> ERROR  │                         │stop
  │                                    └──── STOPPING <────────────┘
  │                                            │
  └───────────── (清除错误/删除源码) <──────────┘
STOPPED 为 READY 的运行子状态：建议用 `state` + `project.process_state` 两个字段表达，
避免 READY/STOPPED 二义性；对外 API 合并计算展示状态。
```

**约束**

- 状态只能由真实检查/进程事件驱动，不允许仅凭"命令返回 0"写入 `RUNNING`。
- `Reconcile` 检查：元数据存在、源码目录是否存在、`.git` 是否存在、进程是否存活、端口是否被本 Workspace 进程占用。
- 同一 Workspace 的并发状态变更（start/stop/delete）通过锁串行化；冲突返回 `409 WORKSPACE_BUSY`。

### 5.3 端口分配

- 默认从 3000 开始；检查：(a) 本机 bind 是否成功；(b) DevKit 内部已分配表是否冲突；(c) 是否被其他 Workspace 进程占用。
- 分配结果写入 `workspace.json`，创建后固定；启动时若端口被外部占用，返回明确错误和可用替代端口建议，不静默改端口。
- 释放：仅删除 Workspace 时释放；停止运行不释放。
- 支持用户显式指定端口；冲突时拒绝并返回占用进程信息（尽力而为）。

### 5.4 ProjectManager（M3 详细实现）

- `Init`：校验源码存在 → 生成 `.env` 式配置文件（写入 `workspace/config/discourse.env`，不写入源码目录）→ 执行依赖安装与数据库初始化。
- `Start`：按顺序检查/启动 PostgreSQL、Redis → 创建/校验数据库 → 启动 Rails 进程 → 健康检查端口 → 状态置 RUNNING。
- `Stop`：只终止本 Workspace 进程（进程组/Job Object）；共享服务按引用计数决定是否停止。
- Discourse 启动命令以实际检出源码为准；候选：
  - `bin/rails server -b 127.0.0.1 -p <port>`
  - 前端开发模式可能需 `bin/ember-cli` 或 `bin/dev`，需在 M3 用真实版本验证。
- 所有 Discourse 相关环境变量（`DISCOURSE_DB_*`、`DISCOURSE_REDIS_*`、`RAILS_ENV=development`）由 DevKit 生成并通过进程环境传入，不修改用户源码。

### 5.5 TaskManager

- 任务类型：`workspace.create`、`workspace.delete`、`project.clone`、`project.init`、`backup.create`、`backup.restore`、`runtime.install`、`service.start` 等。
- API 对耗时操作返回 `202 Accepted` + `task_id`。
- 事件通过 SSE `/api/v1/events` 推送；同时支持 `GET /api/v1/tasks/{id}` 轮询。
- 任务超时（默认 60 分钟，可配置）后取消并标记失败。
- 进度为启发式百分比 + 人类可读消息；不伪造精确进度。

### 5.6 ProcessManager

- 统一接口：`Start(spec) -> Process`、`Stop(force bool)`、`Status()`、`Logs(tail)`。
- 记录：PID、启动时间、命令（脱敏后）、工作目录、退出码、信号。
- 日志：stdout/stderr 分流合并到 Workspace 日志文件 + 内存环形缓冲（最近 2000 行）。
- 终止：先优雅（Unix SIGTERM / Windows CTRL_BREAK 或 `taskkill /T`），超时后强制；确保子进程树被清理。
- 不通过 shell 执行命令；参数始终为 argv 数组。

### 5.7 Diagnostics

检查项返回统一结构：

```json
{
  "id": "postgresql.connection",
  "title": "PostgreSQL 连接",
  "severity": "error",
  "message": "无法连接 127.0.0.1:15432",
  "details": {},
  "fix_action": "service.start.postgresql",
  "destructive": false
}
```

`fix_action` 只是可执行动作的标识；UI/CLI 展示后再执行，涉及删除/覆盖的动作必须二次确认。首版 `doctor` 覆盖设计规范 §13.2 的检查项，按模块分步落地。

### 5.8 BackupManager（M3 起）

备份包格式：`<workspace>-<timestamp>.hxxddk-backup.tar.gz`

```text
manifest.json
workspace/workspace.json
workspace/config/...
workspace/data/...
source/...            # 可选
database.dump         # pg_dump custom format，可选
```

`manifest.json` 包含：备份格式版本、DevKit 版本、Workspace schema 版本、创建时间、平台、是否包含源码/数据库、每个文件的 SHA-256、总大小。

恢复：先校验 manifest 与校验和 → 检查磁盘空间 → 生成恢复预览（将覆盖哪些路径）→ 用户确认 → 原子恢复到临时目录再替换 → 失败保留原数据。

---

## 6. 平台适配层

### 6.1 接口拆分（避免上帝接口）

```go
type Info struct {
    OS, Arch, Distro, DistroVersion, ArchFamily string
    IsWSL bool
    GlibcVersion string
}

type Executor interface {
    Run(ctx context.Context, spec ExecSpec) (ExecResult, error)
    Start(ctx context.Context, spec ExecSpec) (Process, error)
    Which(ctx context.Context, name string) (string, error)
}

type FileSystem interface {
    // 宿主路径与执行环境路径的转换（WSL2 下为 Linux 路径）
    ToExecPath(hostPath string) (string, error)
    EnsureDir(hostPath string, perm fs.FileMode) error
    Stat(hostPath string) (fs.FileInfo, error)
    RemoveAll(hostPath string) error
}

type ServiceHost interface {
    Start(ctx context.Context, svc ServiceSpec) error
    Stop(ctx context.Context, name string) error
    Status(ctx context.Context, name string) (ServiceStatus, error)
    Logs(ctx context.Context, name string, tail int) ([]string, error)
}

type SystemServiceHost interface { // 可选：launchd/systemd 用户服务
    Install(ctx context.Context, spec UnitSpec) error
    Uninstall(ctx context.Context, name string) error
    Status(ctx context.Context, name string) (string, error)
}
```

`platform.Adapter` 组合上述能力并提供 `Info()`、`CheckPrereqs()`、`OpenBrowser()`。

### 6.2 各平台实现策略

| 平台 | 执行环境 | 服务管理 | 首版里程碑 |
|---|---|---|---|
| Linux | 宿主原生 | 用户级进程；可选 systemd user unit | M2 |
| macOS | 宿主原生（Intel/ARM 分包） | 用户级进程；可选 launchd | M2 |
| Windows | 宿主进程管理 + WSL2 内执行 Runtime/服务 | `wsl.exe` 启停；不依赖 Docker Desktop | M2 执行桥接、M5 自动部署 |
| 国产 Linux | 同 Linux，按 ABI 验证 | 同 Linux | M5 |

### 6.3 Windows + WSL2 设计要点

- **DevKit Core 运行在 Windows 侧**，负责 UI/API/状态管理；开发命令通过 `wsl.exe -d <distro> --cd <linux-path> -- <cmd>` 执行。
- Workspace 源码放在 WSL2 文件系统（如 `~/.hxxddk/workspaces/...`），**不放在 `/mnt/c`**；Windows 侧通过 `\\wsl.localhost\<distro>\...` 访问文件。
- 路径映射集中在 `windows.FileSystem.ToExecPath`，禁止在其他模块拼接 Linux 路径。
- 检测项：Windows 版本、虚拟化、WSL 功能、发行版存在性、systemd 可用性、发行版内 glibc/Ruby 依赖。
- 自动启用 WSL、导入发行版、安装发行版内依赖属于 M5，需要管理员权限与可能的重启；必须先检测并给出可恢复说明。
- M2 只支持"用户已有可用 WSL2 发行版"的场景，并提供清晰错误。

### 6.4 进程树与信号

- Unix：子进程设置 `Setpgid`，终止整个进程组；超时升级 SIGKILL。
- Windows：优先使用 Job Object（`golang.org/x/sys/windows`）确保子孙进程回收；首版可用 `CREATE_NEW_PROCESS_GROUP` + `taskkill /T /F` 作为回退，M2 替换为 Job Object。

---

## 7. 本地管理 API 规范

### 7.1 基本约定

- 地址：`http://127.0.0.1:<api_port>`，默认 `17860`；**只监听回环地址**。
- 前缀：`/api/v1`。
- 编码：UTF-8 JSON；请求/响应 `Content-Type: application/json`。
- 时间：RFC 3339（含时区）。
- ID：ULID 字符串。
- 长任务：`202 Accepted` + `{"task_id":"...","status_url":"/api/v1/tasks/..."}`。
- 幂等：`PUT` 用于配置覆盖；`POST` 用于动作；重复启动/停止返回 `409` 与当前状态。

### 7.2 认证与 CSRF

**两种客户端：**

1. **CLI**：读取 `config/secrets.json` 中的 `api_token`，发送 `Authorization: Bearer <token>`。
2. **浏览器 UI**：
   - 同源加载 `GET /` 时，服务端设置 `hxxddk_session` Cookie（HttpOnly、SameSite=Strict、Path=/）；
   - 页面 HTML 注入本次会话的 `csrf_token`（`<meta name="csrf-token">`）；
   - 所有非 GET 请求必须带 `X-CSRF-Token` 头，值与会话绑定；
   - 校验 `Origin`：必须为同源或缺失（CLI 无 Origin）；非法 Origin 返回 `403`；
   - 校验 `Host`：仅允许 `127.0.0.1:<port>`、`localhost:<port>`、`[::1]:<port>`。

**补充**

- 不配置 CORS 响应头（默认拒绝跨源读取）。
- SSE 端点同样要求认证；浏览器用 Cookie，CLI 用 Bearer。
- Token 比较使用 `crypto/subtle.ConstantTimeCompare`。
- 会话 Cookie 不持久化，服务重启后需重新加载页面。
- 所有 API 响应加 `Cache-Control: no-store`、`X-Content-Type-Options: nosniff`；UI 加 CSP。

### 7.3 响应与错误格式

成功：直接返回资源 JSON（列表为 `{"items":[...], "total":n}`）。

错误：

```json
{
  "error": {
    "code": "WORKSPACE_NOT_FOUND",
    "message": "Workspace 不存在",
    "details": {"workspace_id": "01J..."},
    "request_id": "req_..."
  }
}
```

| HTTP | 错误码示例 | 说明 |
|---|---|---|
| 400 | `VALIDATION_FAILED` | 参数错误 |
| 401 | `UNAUTHORIZED` | 缺少/错误 token |
| 403 | `FORBIDDEN_ORIGIN` / `CSRF_INVALID` | 来源或 CSRF 校验失败 |
| 404 | `WORKSPACE_NOT_FOUND` / `TASK_NOT_FOUND` | 资源不存在 |
| 409 | `WORKSPACE_BUSY` / `PORT_IN_USE` / `INVALID_STATE` | 状态冲突 |
| 422 | `PREVIEW_REQUIRED` | 破坏性操作缺少预览/确认 |
| 500 | `INTERNAL` | 未预期错误（不回显内部细节） |
| 503 | `SERVICE_UNAVAILABLE` | 依赖服务不可用 |

### 7.4 端点清单

**系统 / 认证**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/system/info` | 版本、平台、根目录、运行时长 |
| GET | `/api/v1/system/health` | 存活检查（无需认证，仅回环） |
| GET | `/api/v1/system/csrf` | 获取当前会话 CSRF token |
| GET | `/api/v1/diagnostics` | 运行诊断（可选 `?scope=`） |
| POST | `/api/v1/diagnostics/{id}/fix` | 执行非破坏性修复 |
| GET | `/api/v1/events` | SSE 事件流（任务/状态） |
| GET | `/api/v1/tasks` | 任务列表 |
| GET | `/api/v1/tasks/{id}` | 任务详情 |
| POST | `/api/v1/tasks/{id}/cancel` | 取消任务 |

**Workspace**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/workspaces` | 列表 |
| POST | `/api/v1/workspaces` | 创建（异步可返回 202） |
| GET | `/api/v1/workspaces/{id}` | 详情（含实时状态） |
| PATCH | `/api/v1/workspaces/{id}` | 改名/描述/端口等 |
| DELETE | `/api/v1/workspaces/{id}/preview` | 返回删除计划与确认令牌 |
| DELETE | `/api/v1/workspaces/{id}` | 执行删除（需 `confirm_token`） |
| POST | `/api/v1/workspaces/{id}/activate` | 设为活动 Workspace |
| POST | `/api/v1/workspaces/{id}/start` | 启动项目（202） |
| POST | `/api/v1/workspaces/{id}/stop` | 停止项目（202） |
| POST | `/api/v1/workspaces/{id}/restart` | 重启（202） |
| GET | `/api/v1/workspaces/{id}/status` | 实时状态与检查结果 |
| GET | `/api/v1/workspaces/{id}/logs?source=&tail=` | 日志尾部 |

**项目 / Git**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/workspaces/{id}/project` | 项目与源码状态 |
| PUT | `/api/v1/workspaces/{id}/project/source` | 保存仓库配置 |
| POST | `/api/v1/workspaces/{id}/project/clone` | 克隆（202） |
| POST | `/api/v1/workspaces/{id}/project/fetch` | fetch（202） |
| POST | `/api/v1/workspaces/{id}/project/pull` | pull（202，带脏工作区保护） |
| POST | `/api/v1/workspaces/{id}/project/checkout` | 切换 branch/tag/commit（202） |
| POST | `/api/v1/workspaces/{id}/project/init` | 安装依赖/初始化数据库（202） |
| GET | `/api/v1/workspaces/{id}/project/status` | 分支、HEAD、脏文件、ahead/behind |

**Runtime / 服务**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/runtime` | 组件清单与版本 |
| GET | `/api/v1/runtime/matrix` | 当前版本矩阵 |
| POST | `/api/v1/runtime/{component}/verify` | 校验 |
| POST | `/api/v1/runtime/{component}/repair` | 修复（202） |
| GET | `/api/v1/services` | PostgreSQL/Redis 状态 |
| GET | `/api/v1/services/{name}` | 详情 |
| POST | `/api/v1/services/{name}/start` | 启动 |
| POST | `/api/v1/services/{name}/stop` | 停止 |
| POST | `/api/v1/services/{name}/restart` | 重启 |
| GET | `/api/v1/services/{name}/logs` | 日志 |
| PUT | `/api/v1/services/{name}/config` | managed/external 配置 |
| POST | `/api/v1/services/{name}/test` | 连接测试 |
| GET | `/api/v1/services/postgresql/databases` | 数据库列表 |

**备份 / 设置**

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/v1/workspaces/{id}/backups` | 备份列表 |
| POST | `/api/v1/workspaces/{id}/backups` | 创建备份（202） |
| GET | `/api/v1/workspaces/{id}/backups/{bid}` | 备份详情/校验 |
| POST | `/api/v1/workspaces/{id}/backups/{bid}/restore-preview` | 恢复预览 |
| POST | `/api/v1/workspaces/{id}/backups/{bid}/restore` | 执行恢复（需确认） |
| DELETE | `/api/v1/workspaces/{id}/backups/{bid}` | 删除备份文件 |
| GET | `/api/v1/settings` | 全局设置（脱敏） |
| PUT | `/api/v1/settings` | 更新设置 |

**版本策略**：`/api/v1` 内保持向后兼容；破坏性变更升 `v2`，并保留至少一个版本的过渡期。

### 7.5 破坏性操作确认流程

1. 客户端请求 `preview` 端点；
2. 服务端生成 `confirm_token`（随机、短时效 5 分钟、绑定操作与目标）；
3. 响应列出将删除/覆盖的资源、大小、是否共享；
4. 客户端提交 `confirm_token` 执行；
5. 服务端校验 token、目标与操作一致后执行，token 一次性。

适用于：删除 Workspace、恢复覆盖、强制 checkout/reset、删除备份、停止被依赖的服务。

---

## 8. CLI 规范

### 8.1 定位

- CLI 是 API 的瘦客户端；默认自动发现并启动 `devkitd`（后台）。
- 与 GUI 共用服务层，不复制业务逻辑。
- 支持 `--json` 机器输出；人类输出默认表格/彩色（可关）。
- 退出码：`0` 成功、`1` 一般错误、`2` 参数错误、`3` 未找到、`4` 状态冲突、`5` 需要确认。

### 8.2 命令

```text
hxxddk version [--json]
hxxddk doctor [--json]
hxxddk status [--json]

hxxddk workspace list [--json]
hxxddk workspace create <name> [--description] [--project-type]
hxxddk workspace use <name|id>
hxxddk workspace inspect <name|id> [--json]
hxxddk workspace delete <name|id> [--yes] [--keep-source] [--keep-data]
hxxddk workspace start|stop|restart [<name|id>]

hxxddk project init
hxxddk project clone
hxxddk project status
hxxddk project update

hxxddk start|stop|restart
hxxddk service status|start|stop <postgresql|redis>
hxxddk backup create [--include-source] [--include-database]
hxxddk backup restore <file> [--overwrite]
```

**约定**

- `<name|id>` 支持名称唯一匹配；重名时要求用 ID。
- `workspace delete` 默认只移除管理记录；删除文件必须显式 `--purge` 并交互确认（或在非交互下 `--yes --purge`）。
- 运行中的 Workspace 删除默认拒绝（`WORKSPACE_BUSY`），需先停止。
- `--root` / `HXXDDK_HOME` 全局可用。
- 守护进程自动启动失败时，打印启动日志路径与手动命令。

---

## 9. 管理 UI

### 9.1 页面与路由

| 路由 | 页面 | M 阶段 |
|---|---|---|
| `/` | Dashboard：版本、活动 Workspace、服务状态、启动/停止 | M1 基础 / M4 完整 |
| `/workspaces` | Workspace 列表、创建、删除预览 | M1 |
| `/workspaces/:id` | 详情：状态、端口、数据库、日志、启动/停止 | M1 基础 / M3 完整 |
| `/runtime` | 组件版本、路径、校验/修复 | M4 |
| `/services` | PostgreSQL/Redis 状态、外部配置、连接测试 | M4 |
| `/project` | Git 配置、clone/pull/checkout、脏工作区提示 | M4 |
| `/plugins`、`/themes` | 插件/主题管理 | M6 |
| `/diagnostics` | 检查结果与修复动作 | M4 |
| `/logs` | 按模块/Workspace 查看日志 | M4 |
| `/backups` | 备份、恢复预览 | M4 |
| `/settings` | 路径、端口、代理、更新策略 | M4 |

### 9.2 技术结构

```text
web/src/
├── api/            # 类型化 API client（与 Go 模型对应）
├── components/     # 布局、表格、状态徽章、确认对话框、日志查看器
├── pages/          # 路由页面
├── hooks/          # useWorkspaces, useTask, useEventStream
├── i18n/           # zh-CN 默认，en-US 预留
└── styles/         # Tailwind 入口
```

- 服务端状态全部通过 TanStack Query 获取；不加 Redux。
- 任务进度使用 SSE；断线自动重连 + 轮询兜底。
- 破坏性操作统一使用"预览 → 二次确认"对话框，展示资源清单。
- 状态徽章：EMPTY / READY / RUNNING / STOPPED / ERROR，颜色与文案集中管理。
- 可访问性：键盘可达、焦点管理、颜色不作为唯一信息来源。
- UI 默认中文，结构上预留英文文案（M4 完成）。

### 9.3 构建与内嵌

- 开发：`npm --prefix src/web run dev`，Vite 代理 `/api` 到 `127.0.0.1:17860`。
- 生产：`npm --prefix src/web run build` 输出 `src/web/dist`，Go `//go:embed all:dist` 内嵌。
- 服务端路由：`/` 返回 `index.html`；`/assets/*` 返回静态文件；未知非 API 路径回退 SPA。
- 安全头：CSP 默认仅允许 `self`；不使用内联脚本（CSRF token 通过 `/api/v1/system/csrf` 获取或 meta 注入时使用 nonce/hash）。

---

## 10. Runtime 与服务管理细化

### 10.1 版本矩阵

`runtime-matrix.json` 由每个 DevKit Release 固定：

```json
{
  "profile": "default",
  "devkit_version_range": ">=0.1.0 <0.2.0",
  "components": {
    "ruby": {"version": "3.3.x", "source": "bundled", "sha256": "..."},
    "node": {"version": "22.x", "source": "bundled", "sha256": "..."},
    "postgresql": {"version": "16.x", "source": "bundled", "sha256": "..."},
    "redis": {"version": "7.x", "source": "bundled", "sha256": "..."},
    "git": {"version": "2.x", "source": "system-or-bundled", "sha256": ""}
  }
}
```

- 具体版本号在每次 Release 时通过真实 Discourse 兼容性测试确定，不在代码里硬编码。
- M1 只校验文件存在与格式；M2 实现安装/校验/回滚；M5 打包离线组件。

### 10.2 托管 PostgreSQL

- `initdb` 使用 UTF-8、独立 data_dir；
- 监听 `127.0.0.1:15432`，仅本机；
- 创建 `hxxddk_admin` 超级用户（随机密码）；
- 每个 Workspace：`CREATE ROLE` + `CREATE DATABASE ... OWNER ...`；
- 启动/停止使用 `pg_ctl`（或平台服务管理），记录 PID 与日志；
- 备份使用 `pg_dump -Fc`；恢复使用 `pg_restore`；
- 停止前检查依赖该服务的运行中 Workspace，提示确认。

### 10.3 托管 Redis

- 独立 data_dir、日志、密码（requirepass）；
- 监听 `127.0.0.1:16379`；
- 持久化策略由配置决定（开发默认 RDB）；
- Workspace 使用独立 DB index；内存/连接数通过 `INFO` 读取；
- 清理命名空间仅在明确指定 DB index 且确认后执行。

### 10.4 外部服务

- 支持 host/port/database/username/password/TLS；
- "测试连接"执行带超时的轻量查询（PG `SELECT 1`、Redis `PING`）；
- 保存前测试可选；失败不回写配置；
- 切换到外部后，托管服务实例不被删除，仅停止/失活；
- 恢复为 managed 时重新校验数据目录与端口。

### 10.5 凭据与权限

- 数据库密码长度 ≥ 24，随机生成；
- 不在 API 响应的 `details` 中返回密码；仅创建时可选一次性返回（若 UI 需要展示）；
- Git token 只通过 `GIT_ASKPASS` 辅助程序传递，不进入命令行参数、remote URL 或日志；
- 所有子进程环境变量在日志中脱敏。

### 10.6 Git 操作

- 检出优先级：Commit > Tag > Branch > 仓库默认分支 > 空项目（设计规范 §8.2）。
- 默认分支识别：`git ls-remote --symref <url> HEAD`。
- 脏工作区检查：`git status --porcelain=v2`；有修改时默认拒绝 pull/checkout。
- 破坏性操作（reset/clean/force checkout）必须预览 + 确认。
- 更新失败保留原状态；记录完整但脱敏的 Git 错误输出。
- 浅克隆与子模块按配置执行。

### 10.7 Discourse 项目初始化（M3）

流程（以实际检出源码为准，逐步验证）：

1. 校验 Ruby/Bundler/Node 版本符合 `runtime-matrix` 与 Discourse 的 `.ruby-version`/`package.json`；
2. `bundle config set --local path vendor/bundle`（避免污染全局）；
3. `bundle install`；
4. `yarn install`（或目标版本要求的包管理器）；
5. 创建/校验数据库；
6. `RAILS_ENV=development bundle exec rails db:create db:migrate`；
7. 写入 Workspace 配置文件（`config/discourse.env`），不修改源码；
8. 标记 `project.initialized=true`、状态 READY。

所有命令以原始 argv 执行、记录脱敏日志、支持取消。若 Discourse 版本要求 `bin/dev` 或多进程前端，M3 用真实仓库验证后再固定启动策略。

---

## 11. 日志、诊断、备份

### 11.1 日志字段

```json
{
  "time": "2026-11-27T09:00:00.123+08:00",
  "level": "info",
  "module": "workspace",
  "workspace_id": "01J...",
  "task_id": "01J...",
  "request_id": "req_...",
  "error_code": "",
  "message": "workspace created",
  "fields": {}
}
```

- 文件：`logs/devkit-YYYYMMDD.jsonl`，按大小/日期轮转，保留 `retention_days`。
- 控制台：文本格式（开发可读）。
- 脱敏：token、password、secret、authorization、cookie、git URL 中的 userinfo。
- 敏感值禁止进入 `fields`；若必须记录，记录 `<redacted:sha256:xxxxxxxx>`。

### 11.2 诊断项（首批）

| ID | 检查 | 严重级别 |
|---|---|---|
| `devkit.files` | 根目录、配置、版本文件完整性 | error |
| `devkit.lock` | 单实例与残留锁 | warn |
| `runtime.ruby` / `runtime.node` / `runtime.git` | 存在性、版本 | error/warn |
| `service.postgresql.connection` | 连接与版本 | error |
| `service.redis.connection` | PING 与版本 | error |
| `workspace.metadata` | 元数据可解析、schema 一致 | error |
| `workspace.source` | 源码目录/Git 状态 | warn |
| `port.http` | 端口占用 | error |
| `disk.space` | 可用空间 | warn |
| `permissions` | 目录可读写 | error |
| `discourse.dependencies` | bundle/yarn 状态 | warn |

诊断结果区分 error/warn/info；修复动作默认非破坏性，破坏性动作必须确认。

### 11.3 备份

- 每个 Workspace 独立备份；
- 默认包含：元数据、配置、数据、数据库；源码可选（默认记录远程 + commit 而不是打包，减少体积）；
- 备份文件带 manifest 和 SHA-256；
- 恢复前校验版本兼容、完整性、磁盘空间；
- 默认不覆盖；覆盖恢复需确认令牌；
- 敏感备份提示用户保护，M5 考虑加密选项。

---

## 12. 安全设计

| 面 | 措施 |
|---|---|
| 监听范围 | 仅 `127.0.0.1`/`::1`；拒绝 `0.0.0.0` 配置（除非显式高级开关，第一版不提供） |
| API 认证 | CLI Bearer token；浏览器 HttpOnly 会话 + CSRF token |
| 来源校验 | Host/Origin 白名单；无 CORS |
| 凭据 | 0600 文件；日志脱敏；不写入进程参数；Windows ACL |
| 命令执行 | 无 shell；argv 数组；环境变量白名单；工作目录校验 |
| 路径 | `filepath.IsLocal` + 根目录包含校验 + 符号链接解析 |
| 破坏性操作 | 预览 + 一次性确认令牌；运行中拒绝删除 |
| Workspace 隔离 | 独立 DB/Role；Redis 独立 DB index 并明确非强隔离 |
| 共享服务保护 | 停止/删除前检查其他 Workspace 依赖；不 drop 共享实例 |
| UI | CSP、`nosniff`、`no-store`、不内联脚本 |
| 升级 | 校验组件哈希/签名（M2 起）；升级不覆盖用户源码与配置 |
| 日志 | 不记录密码、Token、Cookie、私有仓库凭据 |

**本地安全边界说明**：本机其他进程仍可读取 0600 文件（同一用户），这是本地开发工具的合理边界；API 认证主要防御浏览器跨站请求和非预期调用，不宣称防御同用户恶意进程。

---

## 13. 测试策略

### 13.1 分层

| 层 | 工具 | 覆盖 |
|---|---|---|
| 单元测试 | Go `testing` | store、config、id、port、workspace 状态机、脱敏、路径校验 |
| API 集成 | `httptest` + 临时根目录 | 认证、CSRF、CRUD、错误码、并发冲突 |
| 任务/进程 | Fake adapter + 真实短生命周期命令 | 进度、取消、日志、进程树回收 |
| 前端 | Vitest + Testing Library | 关键页面、确认流程、错误态 |
| E2E | 构建二进制 + CLI + HTTP | 首次启动、Default、创建多 Workspace、重启持久化 |
| 平台 | CI 矩阵（Windows/macOS/Ubuntu） | 路径、锁、进程终止差异 |
| 真实服务 | 可选集成（环境变量开启） | PostgreSQL/Redis 启停与连接（M2） |
| 真实 Discourse | 手动/夜间（M3+） | clone、bundle、启动、访问 |

### 13.2 必测场景（M1）

- 空根目录首次启动 → 自动创建 Default；
- 重复启动 → 不重复创建；
- 创建 3 个 Workspace → 端口、数据库名、用户名、密码互不相同；
- 重启守护进程 → 数据与活动 Workspace 保持；
- 并发创建同名 Workspace → 一个成功，一个 409/422；
- 删除预览资源准确；record-only 删除保留文件；purge 需要确认；
- 无 Token → 401；错误 Origin → 403；缺 CSRF → 403；
- 路径穿越与非法名称 → 400，且不产生目录；
- 日志中不出现测试密码；
- 单实例锁：第二个实例拒绝启动。

### 13.3 质量门槛

- `go test ./...` 通过；核心包覆盖率 ≥ 70%（M1 起逐步提高，不牺牲测试质量换数字）；
- `go vet ./...`、`gofmt` 无差异；
- 前端 `tsc --noEmit`、`eslint`、`vitest` 通过；
- CI 三平台构建通过；
- 任何平台相关代码必须有对应 build tag 与至少一个可运行测试或明确的"需实机验证"标记。

---

## 14. 构建、开发与 CI

### 14.1 本地开发

```bash
cd D:/work/h2x/hxxDiscourseDevKit/src

# 后端（前台，开发根目录）
go run ./cmd/devkitd --root ./.devkit --foreground

# 前端（另开终端）
npm --prefix web install
npm --prefix web run dev        # http://127.0.0.1:5173，代理 /api 到 17860

# CLI
go run ./cmd/hxxddk version
go run ./cmd/hxxddk workspace list
```

### 14.2 构建

```bash
make build          # 前端 + Go 二进制品
make test           # Go + 前端测试
make lint           # gofmt/vet/eslint/tsc
make dist           # 当前平台压缩包（M5 完善）
```

Windows 无 GNU make 时提供 `build.ps1`/`build.cmd`，二者调用同一组命令，避免逻辑分叉。

### 14.3 CI（GitHub Actions）

- `ci.yml`：三平台 `go test ./...`、`go vet`、前端 lint/build；
- `release.yml`（M5）：标签触发，GoReleaser 构建多平台包 + SHA-256 + 第三方许可证清单；
- 缓存 Go module 与 npm 依赖；
- 不在 CI 中启动真实 PostgreSQL/Redis，除非使用服务容器且仅做可选 job。

### 14.4 版本注入

```go
var (
    Version   = "0.1.0-dev"
    Commit    = "unknown"
    BuildDate = "unknown"
)
```

通过 `-ldflags "-X .../internal/version.Version=..."` 注入；`hxxddk version` 和 `/system/info` 返回。

---

## 15. 里程碑与任务分解

### 15.0 总览

| 里程碑 | 对应设计阶段 | 目标 | 交付物 |
|---|---|---|---|
| M0 | Phase 1（基础） | 工程骨架可运行 | devkitd/CLI 骨架、配置、日志、CI |
| M1 | Phase 1 + 部分 Phase 4 | Workspace 核心闭环 | Default、CRUD、API、最小 UI |
| M2 | Phase 2 | Runtime/服务管理 | 版本矩阵、PG/Redis、诊断、外部服务 |
| M3 | Phase 3 | 项目与 Git | clone/checkout、初始化、启动/停止 |
| M4 | Phase 4 | 完整管理 UI | 全部页面与交互 |
| M5 | Phase 5 | 平台安装包 | Windows/WSL2、macOS、Linux、离线包 |
| M6 | Phase 6 | 开发者体验 | 插件/主题、模板、CLI 完善 |

### 15.1 M0：工程骨架（预计 2–4 个工作日）

**任务**

- [ ] 初始化 Go module、目录布局、`.gitignore`、`Makefile`
- [ ] `internal/version` + ldflags 注入
- [ ] `internal/store`：原子写、读、锁、目录创建
- [ ] `internal/id`：ULID
- [ ] `internal/logging`：slog、JSONL、轮转、脱敏
- [ ] `internal/httpx`：错误模型、请求 ID、JSON 响应
- [ ] `internal/config`：devkit.json 默认值、加载、校验、写回
- [ ] `internal/migrate`：迁移框架 + 空迁移
- [ ] `internal/platform`：接口定义 + 三平台 Info/Executor 最小实现
- [ ] `cmd/devkitd`：启动、配置加载、单实例锁、健康端点、优雅退出
- [ ] `cmd/hxxddk`：version/doctor/status 骨架；自动拉起 daemon
- [ ] `GET /api/v1/system/info`、`/health`
- [ ] 单元测试（store/config/paths）
- [ ] GitHub Actions：三平台 build/test

**DoD**

- 在 Windows/macOS/Linux 上 `go test ./...` 通过；
- `devkitd --root <tmp>` 启动后 `/health` 返回 200；
- 第二个实例启动被拒绝且不损坏配置；
- 重启后配置保持；
- 日志文件生成且无敏感字段；
- CLI 可启动 daemon 并显示版本。

### 15.2 M1：Workspace 核心闭环（预计 5–8 个工作日）

**任务**

- [ ] Workspace 模型、存储、schema 迁移
- [ ] `EnsureDefault` 幂等创建
- [ ] 创建/改名/描述/活动 Workspace
- [ ] 端口分配器 + 冲突检测
- [ ] DB 名/用户名生成 + 每 Workspace 随机密码
- [ ] Redis DB index 分配
- [ ] 状态机与 `Reconcile`（无真实服务版）
- [ ] TaskManager + SSE + 任务持久化
- [ ] 删除预览 + 确认令牌 + 安全删除（record/config/purge 分层）
- [ ] Workspace API 全套
- [ ] CLI `workspace list/create/use/inspect/delete`
- [ ] 最小 UI：布局、Dashboard、Workspaces 列表/创建/删除
- [ ] API 认证（Bearer + 会话 + CSRF + Host/Origin）
- [ ] 集成测试 + E2E 脚本
- [ ] 日志按 Workspace 分文件

**DoD（M1 验收）**

1. 全新根目录启动 → 自动创建 `Default` Workspace，状态 `EMPTY`；
2. 创建 A/B 两个 Workspace：端口分别为 3000/3001，数据库名、用户名、密码、Redis DB index 均唯一；
3. 守护进程重启后，Workspace、活动状态、端口保持；
4. `hxxddk workspace list` 与 UI 列表一致；
5. 删除预览准确列出目录与凭据；record-only 删除不删源码；purge 必须确认；
6. 无 token 访问 API 返回 401；跨源 POST 返回 403；
7. 非法 Workspace 名称/路径穿越被拒绝且无副作用；
8. 第二个 daemon 实例被拒绝；
9. Windows/macOS/Linux CI 全绿；
10. 日志与 API 响应中无明文密码/token。

### 15.3 M2：Runtime 与服务管理（预计 8–12 个工作日）

- Runtime 组件注册、版本矩阵加载、校验、安装/回滚框架；
- 托管 PostgreSQL：initdb、启停、状态、日志、备份/恢复基础；
- 托管 Redis：配置、启停、状态、日志；
- Workspace 数据库/Role 创建与权限隔离；
- 外部服务配置、TLS、连接测试、切换；
- 诊断项与修复动作；
- Windows WSL2 执行桥接（已有发行版）；
- Linux/macOS 原生服务管理；systemd/launchd 用户服务可选；
- 组件清单/哈希校验（签名留到 M5）。

**DoD**

- Linux/macOS：托管 PG/Redis 可由 DevKit 启动/停止，Workspace 可连接；
- Windows：检测 WSL2 发行版，能在其中执行命令并管理服务；
- 外部服务可配置并通过测试；切回 managed 正常；
- 停止共享服务前检查依赖；
- `hxxddk doctor` 覆盖首批检查项；
- 不泄露任何凭据。

### 15.4 M3：项目、Git 与运行（预计 8–12 个工作日）

> **实施状态（2026-11-27）**：已实现 `internal/project`、`internal/backup`、M3 API/CLI 与「项目与备份」页面，并通过 `go test ./...`、`go vet ./...` 与前端构建。真实 Discourse 仓库的 clone → bundle → 启动 → 访问冒烟测试仍需在目标机器执行；M3 DoD 中以真实仓库为准的最后一项尚未验收。

- Git 配置、clone、fetch、pull、checkout、状态；
- Commit > Tag > Branch > 默认分支优先级；
- 脏工作区保护与破坏性操作确认；
- Discourse 项目初始化（bundle/yarn/db）；
- Rails 启动/停止、健康检查、日志；
- 多 Workspace 并行端口与进程管理；
- 备份/恢复（含 pg_dump）；
- 真实 Discourse 冒烟测试。

**DoD**

- 能在一个 Workspace 中克隆官方 Discourse、检出指定分支，完成依赖安装并启动访问；
- 本地修改时 pull/checkout 被阻止；
- 两个 Workspace 同时运行且数据库/Redis/端口隔离；
- 停止一个不影响另一个；共享服务在有依赖时不停止；
- 备份可恢复并通过校验。

### 15.5 M4：完整管理 UI（预计 8–10 个工作日）

> **实施状态（2026-11-27）**：已新增 Runtime / Logs / Settings 页面、日志聚合 API 与设置读写 API，并将 Backup 拆分为独立页面（Workspace 选择、摘要、共享备份卡片）；管理 UI 接入 SSE 任务事件刷新；概览页补充活动 Workspace 的项目启停/重启与任务进度。Plugins / Themes 页面已基于 M6 基础能力实现（清单、元数据、脚手架创建），11 个页面全部可访问。新增守护进程 E2E 测试，覆盖会话/CSRF、SSE、Workspace 创建与删除、日志读取、设置 PATCH 与后台任务轮询。i18n 结构化与浏览器端 UI E2E 待补齐。`go test ./...`、`go vet ./...`、`npm run build` 通过。

- Runtime、Services、Project、Diagnostics、Logs、Backup、Settings 页面；
- SSE 实时任务与状态；
- 确认流程、错误态、空状态、加载态；
- i18n 结构化、可访问性检查。

**DoD**

- 设计规范 §9.2 的 11 个页面全部可用；
- 所有破坏性操作都有预览与确认；
- 前端构建内嵌后单二进制可完整使用；
- 关键流程有 E2E 测试。

### 15.6 M5：平台安装包（预计 10–15 个工作日）

- Windows：WSL2 检测/启用/发行版部署、EXE/MSI、快捷方式、卸载；
- macOS：DMG/PKG、签名、公证、launchd 用户服务；
- Linux：DEB/RPM/TAR.GZ、systemd 用户服务、glibc 检测；
- 国产 Linux 专项验证矩阵与离线包；
- Full Offline Installer、第三方许可证与 SHA-256；
- 安装/升级/卸载/回滚测试。

**DoD**

- 各平台安装包可在干净环境完成安装并创建 Default；
- 安装失败可恢复，不破坏已有数据；
- 升级不覆盖 Workspace；
- 第三方许可证清单与校验文件齐全；
- 平台支持状态与测试记录一致（未验证不宣称支持）。

### 15.7 M6：开发者体验（预计 8–12 个工作日）

> **实施状态（2026-11-27）**：已实现插件/主题基础能力：按 Workspace 列出插件（源码 `plugins/`）与主题（`data/themes/`），解析 `plugin.rb` / `about.json` 元数据，并提供经过名称、slug、长度与目录边界校验的脚手架创建；插件创建要求源码已安装。已接入管理 API、`hxxddk plugin|theme list|create` 与 Plugins / Themes 页面。启用/禁用、依赖检查、测试、打包与 Project Profile/模板机制尚未实现。

- 插件开发：创建、依赖检查、测试、打包；
- 主题开发：创建、预览、构建；
- Project Profile/模板机制（`discourse-official`、`discourse-plugin`、`discourse-theme`、`discourse-custom`、`hxx-agentforge`）；
- 模板来源展示与权限提示；
- CLI 完善与文档；
- 可选桌面壳评估（不承诺）。

**DoD**

- 模板可创建对应项目并给出正确依赖检查；
- 第三方模板执行前展示来源与操作；
- CLI 覆盖设计规范 §10 命令；
- 文档齐备。

---

## 16. 设计规范验收标准映射

| 设计规范 §19 条目 | 里程碑 | 说明 |
|---|---|---|
| 1 下载安装包 | M5 | 平台包 |
| 2 安装器完成 Runtime 安装 | M5（框架 M2） | |
| 3 无需预装依赖 | M5 | Full Installer |
| 4 PG/Redis/Git 默认安装并管理 | M2/M5 | 实际随包安装 M5 |
| 5 源码默认不安装 | M1/M3 | 默认空 Workspace |
| 6 仓库/Branch/Tag/Commit 配置 | M3 | |
| 7 自动创建 Default | M1 | |
| 8 多 Workspace | M1 | |
| 9 源码/配置/DB/日志/端口隔离 | M1（配置）/M3（运行） | |
| 10 PostgreSQL 独立 DB/用户 | M1 生成 / M2 落地 | |
| 11 Redis 独立前缀/DB | M1 分配 / M2 落地 | |
| 12 安装后添加源码 | M3 | |
| 13 GUI 状态/启停/诊断 | M4（基础 M1） | |
| 14 外部 PG/Redis + 测试 | M2 | |
| 15 删除不误伤 | M1 | 预览+确认 |
| 16 许可证/校验 | M5 | |
| 17 可复现安装测试 | M5 | |

---

## 17. 风险与待确认事项

### 17.1 主要风险

| 风险 | 影响 | 应对 |
|---|---|---|
| Windows WSL2 自动部署受系统/策略/虚拟化限制 | 无法承诺零交互安装 | 先检测、明确报告、提供手动步骤；M5 专项 |
| Discourse 版本对 Ruby/Node/PG/Redis 的要求变化快 | 版本矩阵失效 | 每次 Release 实测；矩阵外置、可更新 |
| Redis Key Prefix 与 Discourse 兼容性未验证 | 隔离方案需调整 | M2 实测；默认用独立 DB index，prefix 仅用于 DevKit 键 |
| 国产 Linux ABI/原生扩展 | 二进制不兼容 | 按发行版+架构构建与测试；未验证不标支持 |
| 打包体积（Ruby/Node/PG/Redis） | 安装包过大 | 分卷、按平台裁剪、可选组件；M5 决策 |
| 凭据本地存储强度 | 同用户可读 | 首版 0600 + 明确边界；M2+ OS 凭据库 |
| Discourse 开发启动方式差异（Rails + Ember） | 启动命令不通用 | M3 用真实版本验证并适配 |
| 端口冲突 | 启动失败 | 显式报错 + 建议；不静默改配置 |

### 17.2 需要确认的决策

| # | 事项 | 建议 | 需确认 |
|---|---|---|---|
| Q1 | 首轮实施范围 | M0 + M1，确认后开始 | ☐ |
| Q2 | Go module 路径 | `github.com/hxx/hxxDiscourseDevKit`（可改为实际仓库地址） | ☐ |
| Q3 | UI 技术栈 | React 18 + TS + Vite + Tailwind + TanStack Query，无重型组件库 | ☐ |
| Q4 | Workspace 物理目录用 ID | 显示名仍为 `Default` | ☐ |
| Q5 | 默认端口 | API 17860 / PG 15432 / Redis 16379 | ☐ |
| Q6 | Windows 策略 | M1 核心原生运行；M2 WSL2 执行桥接；M5 自动部署 | ☐ |
| Q7 | 代码注释/文档语言 | 代码与标识符英文，注释与文档中文；UI 默认中文 | ☐ |
| Q8 | 是否每个里程碑完成后提交 git | 建议是（单独分支/提交，不推送） | ☐ |

### 17.3 需要后续详细设计（不阻塞 M1）

- 组件仓库元数据格式、签名与升级协议；
- macOS 签名/公证/自动更新；
- 国产 Linux 具体支持矩阵；
- 备份加密方案；
- 桌面壳是否提供；
- 插件/主题自动化边界；
- Full Installer 分卷与源码缓存策略。

---

## 18. 确认后的第一步

用户确认本计划后，按以下顺序启动：

1. 创建 Go module 与目录骨架（M0）；
2. 实现 store/config/logging/httpx/version 与对应测试；
3. 实现 `devkitd` 启动、单实例锁、健康端点；
4. 实现 CLI 骨架与 daemon 自动启动；
5. 进入 M1：Workspace 模型、Default 创建、端口/凭据分配、API；
6. 每完成一个可验证切片即运行 `go test ./...` 并汇报结果；
7. M1 完成后提供一次可操作的本地验收步骤（含 UI 截图/访问地址）。

**默认工作目录约定**：后续所有开发工作均以 `D:\work\h2x\hxxDiscourseDevKit` 为项目根；运行数据使用仓库外目录或 `src/.devkit`（gitignore），不污染源码。

---

## 附录 A：M1 目录与文件级任务清单

```text
src/
├── go.mod
├── cmd/devkitd/main.go
├── cmd/hxxddk/main.go
├── internal/app/app.go              # 装配与生命周期
├── internal/app/bootstrap.go        # 首次初始化、Default 创建
├── internal/api/server.go           # HTTP server、路由注册
├── internal/api/middleware.go       # auth、csrf、origin、request id、recover
├── internal/api/system.go
├── internal/api/workspaces.go
├── internal/api/tasks.go
├── internal/api/events.go           # SSE
├── internal/api/respond.go
├── internal/config/config.go
├── internal/config/defaults.go
├── internal/config/validate.go
├── internal/workspace/model.go
├── internal/workspace/manager.go
├── internal/workspace/state.go
├── internal/workspace/ports.go
├── internal/workspace/delete.go
├── internal/store/file.go
├── internal/store/lock_unix.go
├── internal/store/lock_windows.go
├── internal/id/ulid.go
├── internal/logging/logging.go
├── internal/logging/redact.go
├── internal/httpx/errors.go
├── internal/httpx/validate.go
├── internal/task/manager.go
├── internal/task/model.go
├── internal/secret/store.go
├── internal/platform/platform.go
├── internal/platform/linux/platform.go
├── internal/platform/darwin/platform.go
├── internal/platform/windows/platform.go
├── internal/migrate/migrate.go
├── internal/version/version.go
└── web/...
```

## 附录 B：M1 验收演示脚本（草稿）

```bash
cd D:/work/h2x/hxxDiscourseDevKit/src
go build -o ./bin/devkitd.exe ./cmd/devkitd
go build -o ./bin/hxxddk.exe ./cmd/hxxddk

rm -rf ./.devkit
./bin/devkitd.exe --root ./.devkit --foreground &
./bin/hxxddk.exe --root ./.devkit workspace list
./bin/hxxddk.exe --root ./.devkit workspace create Alpha
./bin/hxxddk.exe --root ./.devkit workspace create Beta
./bin/hxxddk.exe --root ./.devkit workspace inspect Alpha
./bin/hxxddk.exe --root ./.devkit workspace delete Alpha --dry-run
```

预期：`Default` 自动存在；Alpha/Beta 端口与数据库名唯一；删除预览列出资源；无服务依赖即可完成全部步骤。

---

*本计划为 v0.1，确认后随实现进展更新。*
