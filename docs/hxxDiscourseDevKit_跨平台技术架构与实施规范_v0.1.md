# hxxDiscourseDevKit

## 跨平台 Discourse 开发环境与工具链设计规范 v0.1

-   **项目名称**：hxxDiscourseDevKit
-   **文档版本**：0.1.0
-   **文档状态**：架构设计基线
-   **开源协议**：MIT（仅适用于 DevKit 自身代码）
-   **主要分发渠道**：GitHub Releases
-   **主要发行形式**：Full Installer
-   **目标平台**：Windows、macOS、Linux，以及经过专项适配的国产 Linux
    发行版
-   **核心定位**：面向 Discourse 开发者、插件作者、主题开发者和
    Discourse 二次开发项目的跨平台一体化开发环境

------------------------------------------------------------------------

## 1. 项目概述

### 1.1 产品定位

hxxDiscourseDevKit 是一个独立、通用、跨平台的 Discourse
开发环境发行产品。它为开发者自动准备 Discourse
开发所需的运行环境、基础服务和管理工具，降低
Ruby/Rails、Node.js、PostgreSQL、Redis 等依赖的安装与配置门槛。

DevKit 面向整个 Discourse 生态，不是 HXX AgentForge
专属工具。任何开发者均可使用它开发官方 Discourse、Discourse
插件、Discourse 主题，或基于 Discourse 开展二次开发。

### 1.2 与 HXX AgentForge 的关系

HXX AgentForge 是基于 Discourse 的独立分支项目，是 hxxDiscourseDevKit
的一个下游项目/可选项目模板。

两者边界如下：

-   hxxDiscourseDevKit 提供通用运行环境、Workspace
    管理、源码管理、插件/主题开发辅助、诊断和维护能力。
-   HXX AgentForge 的业务功能、业务插件、业务数据模型和商业逻辑由 HXX
    AgentForge 自身维护。
-   DevKit 核心不得依赖 HXX AgentForge，也不得将 AgentForge
    的业务逻辑作为通用功能。
-   DevKit 可以提供 `hxx-agentforge`
    项目模板，但该模板是可选扩展，不影响 DevKit 独立使用。

### 1.3 设计目标

1.  下载一个对应平台的完整安装包，即可安装 DevKit 和基础开发依赖。
2.  不要求用户预先安装
    Ruby、Rails、Node.js、Yarn、PostgreSQL、Redis、Git、Docker、Homebrew
    等开发依赖。
3.  默认安装基础开发运行环境和 PostgreSQL、Redis、Git。
4.  默认不下载 Discourse
    源码；用户可以在安装向导中选择安装，也可以在安装后创建项目。
5.  第一版即支持多个相互隔离的 Workspace。
6.  安装完成后自动创建名为 `Default` 的默认 Workspace。
7.  提供图形化环境管理页面，允许用户配置、启动、停止、诊断和维护环境。
8.  采用平台适配架构，兼顾 Windows、macOS、普通 Linux 和国产 Linux。
9.  以本地开发为主，尽量减少云端依赖和 DevKit 服务端负担。
10. 支持在线安装包和完整离线安装包；Full Installer 是主要分发形式。

### 1.4 非目标

第一版不以以下事项为目标：

-   替代 Discourse 官方生产部署方案。
-   提供面向生产环境的集群编排、生产级高可用或自动扩缩容。
-   将 Discourse 改写为 Go 服务。
-   强制使用 Docker 或 Docker Desktop。
-   将 HXX AgentForge 的业务能力内置到 DevKit。
-   自动修改用户的业务代码或无提示覆盖本地源码。
-   提供完整 IDE；DevKit 与 VS Code、JetBrains 等开发工具协作。

------------------------------------------------------------------------

## 2. 产品原则与关键决策

  项目                          决策
  ----------------------------- --------------------------------------------
  产品名称                      hxxDiscourseDevKit
  开源协议                      MIT，适用于 DevKit 自身代码
  主要分发渠道                  GitHub Releases
  主要发行形式                  Full Installer
  在线安装器                    可提供，但不是主要发行形式
  PostgreSQL                    默认安装
  Redis                         默认安装
  Git                           默认安装
  Ruby/Rails 运行环境           默认安装并配置
  Node.js/Yarn 等前端构建环境   默认安装并配置
  Discourse 源码                默认不安装，用户可选
  Git 仓库                      可配置 GitHub 或其他 Git 仓库
  分支/Tag/Commit               支持配置
  Workspace                     第一版必须支持多 Workspace
  默认 Workspace                `Default`，安装后自动创建
  数据服务                      默认由 DevKit 统一管理，可按需连接外部服务
  Docker                        不作为必需依赖
  管理界面                      本机图形化管理页面
  CLI                           提供统一命令行入口，作为 GUI 的补充

------------------------------------------------------------------------

## 3. 总体架构

### 3.1 分层架构

``` text
+------------------------------------------------------+
|                 hxxDiscourseDevKit                   |
+------------------------------------------------------+
| User Interfaces                                      |
|  - Desktop Launcher (optional shell)                 |
|  - Local Web Management UI                           |
|  - CLI                                                |
+------------------------------------------------------+
| DevKit Core (Go)                                     |
|  - API Server                                        |
|  - Workspace Manager                                 |
|  - Project/Source Manager                            |
|  - Runtime Manager                                   |
|  - Service Manager                                   |
|  - Configuration Manager                             |
|  - Task/Process Manager                               |
|  - Diagnostics & Logs                                |
|  - Backup & Restore                                  |
|  - Package/Update Manager                            |
+------------------------------------------------------+
| Platform Runtime Adapters                            |
|  - Windows / WSL2                                    |
|  - macOS                                              |
|  - Linux                                              |
|  - Kylin / UOS / openEuler profiles                   |
+------------------------------------------------------+
| Managed Runtime Components                           |
|  - Ruby / Bundler / Rails                             |
|  - Node.js / package manager                          |
|  - PostgreSQL                                         |
|  - Redis                                              |
|  - Git                                                |
|  - Native libraries / build tools                     |
+------------------------------------------------------+
| Projects / Workspaces                                |
|  - Official Discourse                                |
|  - Plugin development                                |
|  - Theme development                                 |
|  - Custom Discourse forks                            |
|  - HXX AgentForge (optional project template)        |
+------------------------------------------------------+
```

### 3.2 核心模块

  模块                  职责
  --------------------- -----------------------------------------------
  DevKit Core           管理器核心、生命周期、统一 API
  Runtime Manager       安装、版本锁定、更新和卸载 Runtime
  Service Manager       PostgreSQL、Redis 等服务管理
  Workspace Manager     创建、切换、隔离、备份和删除 Workspace
  Project Manager       项目初始化、仓库配置、源码更新
  Plugin Developer      插件创建、依赖检查、测试和打包辅助
  Theme Developer       主题创建、预览、构建和调试辅助
  Environment Console   图形化配置和运行状态管理
  Diagnostics           依赖、端口、数据库、Redis、源码和构建环境诊断
  Package Manager       平台组件包、校验、升级和回滚

### 3.3 技术选型建议

  层次             建议技术
  ---------------- ------------------------------------------------------
  DevKit Core      Go
  本地管理 API     Go HTTP Server
  管理 UI          React + TypeScript
  UI 访问方式      本机浏览器为基础入口；桌面壳可选
  配置格式         JSON，必要时支持 YAML 导入/导出
  进程管理         Go 子进程管理与平台 Adapter
  Windows 控制     WSL2、`wsl.exe`、PowerShell
  macOS 服务管理   launchd 或用户级进程管理
  Linux 服务管理   systemd 或用户级进程管理
  安装包           Windows EXE/MSI、macOS DMG/PKG、Linux DEB/RPM/TAR.GZ
  组件仓库         GitHub Releases 或独立静态组件仓库

不要求使用 Electron。管理 UI
可以通过本机浏览器访问；如未来增加桌面壳，应保持其为轻量可选层，不改变
Go Core 和 Web UI 的主体架构。

------------------------------------------------------------------------

## 4. 安装与发行体系

### 4.1 Full Installer First

Full Installer 是主要发行形式。用户从 GitHub Releases
下载与操作系统、CPU 架构匹配的安装包，安装后即可获得完整的基础开发环境。

Full Installer
应尽量包含或携带经过验证的运行组件，不应把系统包管理器、Homebrew 或
Docker 作为安装成功的前提。

安装过程可以执行平台必要的系统配置，但必须清楚告知用户需要的权限、系统重启或虚拟化条件。

### 4.2 安装内容

**默认安装：**

-   hxxDiscourseDevKit Core
-   管理 UI 及本地管理服务
-   Git
-   PostgreSQL
-   Redis
-   Ruby 及对应 Bundler/Rails 开发运行环境
-   Node.js 及 Discourse 所需前端包管理工具
-   编译工具链和必要原生库
-   日志、诊断、备份等管理组件
-   默认 `Default` Workspace

**默认不安装：**

-   Discourse 源码
-   用户自定义 Fork 源码
-   可选插件和主题
-   HXX AgentForge 业务扩展

### 4.3 安装向导：Discourse 源码选项

安装向导提供"是否同时安装 Discourse 源码"的可选项：

-   默认选中状态：不安装源码。
-   用户勾选后：安装向导从指定 Git 仓库获取源码。
-   默认仓库建议为 `https://github.com/discourse/discourse.git`。
-   仓库地址允许修改。
-   支持 Branch、Tag、Commit 选择。
-   如果 Commit 已填写，则以 Commit 为精确检出目标。
-   如果 Tag 已填写，则检出指定 Tag。
-   否则检出所选 Branch 的最新提交。
-   默认分支应通过仓库元数据识别，不应永久硬编码某个分支名称。
-   安装完成后，仍可通过 Workspace Manager 添加其他项目。

源码安装失败不应导致基础 Runtime
和数据库服务安装结果被破坏；安装器应报告失败原因，并允许用户稍后重试。

### 4.4 安装包命名建议

示例命名：

``` text
hxxDiscourseDevKit-windows-amd64-<version>.exe
hxxDiscourseDevKit-windows-arm64-<version>.exe

hxxDiscourseDevKit-darwin-amd64-<version>.dmg
hxxDiscourseDevKit-darwin-arm64-<version>.dmg

hxxDiscourseDevKit-linux-amd64-<version>.tar.gz
hxxDiscourseDevKit-linux-arm64-<version>.tar.gz
hxxDiscourseDevKit-linux-amd64-<version>.deb
hxxDiscourseDevKit-linux-arm64-<version>.deb
```

国产 Linux 可以根据具体发行版、ABI
和架构提供独立包或离线包。文件名中的平台标识只是发行标识，最终兼容性必须由实际构建和测试矩阵确认。

### 4.5 Online 与 Offline

-   **Full Offline Installer**：包含目标平台所需
    Runtime、基础服务、安装器和必要依赖缓存，适用于内网或无外网环境。
-   **Full Installer with Online Source Option**：基础 Runtime
    完整随包分发；用户选择安装 Discourse 源码时，从配置的 Git
    仓库获取源码。
-   可额外提供源码缓存包或完整源码离线包，供无外网开发环境使用。

"完整安装包"应明确说明是否包含 Discourse 源码。默认发行包不包含
Discourse 源码，以控制包体积并保持用户选择权。

### 4.6 第三方许可证

MIT 仅适用于 hxxDiscourseDevKit
自身代码。PostgreSQL、Redis、Ruby、Node.js、Git、系统库及其他第三方组件继续遵守各自许可证。

发行包必须提供：

-   `THIRD_PARTY_NOTICES`
-   第三方许可证文本
-   组件名称、版本、来源
-   必要的版权声明
-   对需要提供源代码或修改声明的组件，按其许可证履行义务

------------------------------------------------------------------------

## 5. Workspace 多项目管理

### 5.1 基本原则

多项目 Workspace 是第一版正式功能，不是未来扩展项。

安装完成时自动创建 `Default` Workspace。Workspace 与 Discourse
项目不是同一个概念：

-   Workspace 是一个独立的开发环境容器/项目工作区。
-   Workspace 可以尚未安装或关联 Discourse 源码。
-   用户可以在 Workspace 中初始化官方 Discourse、克隆
    Fork、开发插件或主题。
-   一个 Workspace 默认关联一个主要 Discourse
    源码项目；插件、主题可作为该项目的扩展资源管理。
-   需要测试不同 Discourse 版本或不同项目时，创建独立 Workspace。

### 5.2 Workspace 功能范围

第一版必须支持：

-   自动创建 `Default`
-   创建 Workspace
-   修改名称和描述
-   设置当前活动 Workspace
-   初始化项目
-   配置 Git 仓库、Branch、Tag、Commit
-   克隆、更新和检出源码
-   独立启动、停止、重启
-   独立端口配置
-   独立数据库和 Redis 命名空间配置
-   独立日志和配置
-   环境诊断
-   备份和恢复
-   删除 Workspace
-   删除时选择是否同时删除源码和数据

Workspace 的数量不设置固定产品限制，但实际可运行数量受本机
CPU、内存、磁盘和端口资源约束。

### 5.3 Workspace 目录布局

目录根路径由平台 Adapter 决定。建议逻辑结构如下：

``` text
hxxDiscourseDevKit/
├── bin/
├── runtime/
│   ├── ruby/
│   ├── node/
│   ├── git/
│   └── tools/
├── services/
│   ├── postgresql/
│   └── redis/
├── workspaces/
│   ├── Default/
│   │   ├── workspace.json
│   │   ├── source/
│   │   ├── config/
│   │   ├── logs/
│   │   ├── data/
│   │   ├── backups/
│   │   └── tmp/
│   ├── ProjectA/
│   │   ├── workspace.json
│   │   ├── source/
│   │   ├── config/
│   │   ├── logs/
│   │   ├── data/
│   │   ├── backups/
│   │   └── tmp/
│   └── ProjectB/
├── config/
│   └── devkit.json
└── logs/
```

实际实现中，安装程序、Runtime、服务数据、用户项目数据应根据平台规范分开放置。例如
macOS/Linux 使用用户级应用数据目录；Windows 的 WSL2 项目数据放在 WSL2
Linux 文件系统中，而不是 `/mnt/c`。

### 5.4 Workspace 元数据

建议 `workspace.json` 至少包含：

``` json
{
  "workspace_id": "01J...",
  "name": "Default",
  "description": "Default Discourse development workspace",
  "workspace_schema_version": 1,
  "project_type": "discourse",
  "source": {
    "installed": false,
    "repository_url": "",
    "branch": "",
    "tag": "",
    "commit": "",
    "path": "source"
  },
  "runtime_profile": "default",
  "database": {
    "mode": "managed",
    "database_name": "discourse_default",
    "username": "discourse_default"
  },
  "redis": {
    "mode": "managed",
    "key_prefix": "hxxddk:default:"
  },
  "network": {
    "host": "127.0.0.1",
    "http_port": 3000
  },
  "created_at": "2026-01-01T00:00:00Z",
  "updated_at": "2026-01-01T00:00:00Z"
}
```

以上仅为结构示例。真实实现中应使用随机或安全生成的 Workspace
ID、数据库凭据，并由配置管理器写入时间字段。不得把示例密码或固定凭据用于实际部署。

### 5.5 Default Workspace 初始化

安装完成后：

1.  创建 `Default` Workspace。
2.  生成唯一 Workspace ID。
3.  创建配置、日志、数据、备份和临时目录。
4.  创建 Workspace 元数据。
5.  关联默认 Runtime Profile。
6.  关联 DevKit 管理的 PostgreSQL 和 Redis 服务。
7.  如果安装时选择了 Discourse 源码，则将源码放入 `Default/source`
    并执行项目初始化。
8.  如果没有选择源码，则保留空项目状态，提示用户可稍后初始化。

创建 Workspace 不代表必须立即创建 Discourse 数据库或启动 Rails
服务。数据库初始化应在项目源码准备好并执行项目初始化时进行。

### 5.6 多 Workspace 隔离模型

  -----------------------------------------------------------------------
  资源                                隔离方式
  ----------------------------------- -----------------------------------
  源码                                每个 Workspace 独立目录

  项目配置                            每个 Workspace 独立

  日志                                每个 Workspace 独立

  上传文件/开发数据                   每个 Workspace 独立

  PostgreSQL                          默认共享服务实例，每个 Workspace
                                      独立 Database 和专用数据库用户

  Redis                               默认共享服务实例，每个 Workspace
                                      使用独立 Key Prefix；可选独立
                                      DB/实例

  HTTP 端口                           每个 Workspace 独立分配

  Runtime                             默认共享版本化 Runtime；允许
                                      Workspace 锁定兼容版本

  进程                                每个 Workspace 独立管理

  备份                                每个 Workspace 独立
  -----------------------------------------------------------------------

PostgreSQL 共享实例不代表共享数据库。每个 Workspace
必须使用独立数据库和独立数据库用户，不能让一个 Workspace
的数据库凭据访问其他 Workspace 的数据库。

Redis Key Prefix
只能提供命名空间隔离，不等同于强安全隔离。对于需要更强隔离的场景，提供独立
Redis DB 或独立实例配置。

### 5.7 Workspace 生命周期

建议状态：

-   `EMPTY`：已创建，尚无项目源码。
-   `INITIALIZING`：正在初始化项目。
-   `READY`：项目和依赖准备完成，未运行。
-   `STARTING`：正在启动。
-   `RUNNING`：运行中。
-   `STOPPING`：正在停止。
-   `STOPPED`：已停止。
-   `ERROR`：发生错误，需要诊断。

状态应由进程和服务的真实检查结果产生，不能仅根据上一次命令是否成功推断。

### 5.8 删除和清理保护

删除 Workspace 必须提供明确选项：

-   仅从 DevKit 中移除 Workspace 管理记录。
-   删除 Workspace 配置和日志。
-   删除源码。
-   删除 Workspace 数据目录。
-   删除对应 PostgreSQL Database 和专用用户。
-   清理 Redis 对应命名空间（仅在确认安全时执行）。

默认采用保守策略：删除前展示将删除的资源，要求用户确认；数据删除前建议自动备份。不得因删除
Workspace 而误删共享 PostgreSQL/Redis 服务或其他 Workspace 数据。

------------------------------------------------------------------------

## 6. Runtime 与基础服务管理

### 6.1 默认安装组件

  ------------------------------------------------------------------------------
  组件                        用途                    管理要求
  --------------------------- ----------------------- --------------------------
  Ruby                        Discourse 后端运行环境  版本锁定、Gem 管理

  Bundler                     Ruby 依赖管理           与 Ruby 版本兼容

  Rails                       Discourse 后端框架依赖  以 Discourse 源码依赖为准

  Node.js                     前端构建                版本锁定

  Yarn/官方要求的包管理工具   前端依赖管理            以目标 Discourse
                                                      版本要求为准

  PostgreSQL                  关系型数据库            初始化、备份、恢复、日志

  Redis                       缓存和队列等            启停、内存和连接状态

  Git                         源码版本管理            仓库、分支、Tag、Commit

  Native Build Tools          原生扩展编译            按平台和 ABI 配套
  ------------------------------------------------------------------------------

具体 Ruby、Node.js、PostgreSQL、Redis 版本不得在设计文档中永久固定。每个
DevKit Release 应维护经过验证的组件版本矩阵，并确保与所支持的 Discourse
版本兼容。

### 6.2 共享服务与按需启动

PostgreSQL 和 Redis 默认由 DevKit 统一管理，以减少多 Workspace
重复占用资源。

-   安装完成后完成安装和基础初始化。
-   不要求所有服务一直常驻。
-   DevKit 根据 Workspace 启停策略启动所需服务。
-   当没有活动 Workspace 时，可以按配置停止开发服务。
-   停止服务前检查是否仍有 Workspace 或任务依赖。
-   支持用户配置外部 PostgreSQL/Redis，并执行连接测试。

### 6.3 外部服务配置

PostgreSQL 和 Redis 均支持两种模式：

-   `managed`：由 DevKit 安装和管理。
-   `external`：使用用户提供的外部服务。

图形界面至少提供：

-   Host
-   Port
-   Database / Redis DB
-   Username
-   Password
-   TLS/SSL（服务支持时）
-   连接测试
-   保存配置
-   恢复为 DevKit 管理服务

密码应使用操作系统凭据存储或受保护的本地存储机制，不应以明文写入普通配置文件或日志。

------------------------------------------------------------------------

## 7. 跨平台 Runtime Adapter

### 7.1 Windows

Windows 使用 WSL2 作为 Discourse 开发运行环境，DevKit
负责检测、部署和管理 WSL2 Runtime。

要求：

-   不要求用户自行安装和配置 Ubuntu、Ruby、Node、PostgreSQL、Redis 等。
-   不依赖 Docker Desktop。
-   通过 Windows 安装器检测 WSL2、虚拟化能力和系统版本。
-   尽可能自动启用和部署所需 WSL2 组件。
-   对需要管理员权限、系统重启或组织策略授权的步骤，提供明确说明和恢复流程。
-   在 WSL2 Linux 文件系统中保存源码、依赖和 Workspace
    数据，避免将活跃项目放在 `/mnt/c`。
-   提供 Windows 侧管理界面，通过受控接口调用 WSL2 内的 DevKit Runtime。
-   支持从 Windows IDE 访问 WSL2 项目。

不能承诺在所有 Windows
Home/Pro、固件、企业策略和虚拟化状态下完全无交互安装。安装器必须先检测并清楚报告阻塞条件。

### 7.2 macOS

-   分别构建 Intel（x86_64）和 Apple Silicon（arm64）发行包。
-   优先使用原生 Runtime。
-   使用用户级目录保存 Runtime 和 Workspace。
-   使用 launchd 或 DevKit 自身的用户级进程管理能力。
-   不要求用户安装 Homebrew。
-   安装、升级和卸载过程应遵守 macOS 权限、签名和公证要求。

### 7.3 普通 Linux

-   提供 DEB、RPM 或 TAR.GZ 发行形式。
-   优先使用用户目录安装 Runtime，日常开发尽量不需要 root。
-   可利用 systemd 用户服务；没有用户级 systemd 时使用 DevKit
    进程管理能力。
-   不要求用户预先安装 apt、dnf/yum 管理的开发依赖。
-   对 glibc、系统库和原生扩展进行兼容性检测。

### 7.4 国产 Linux

重点考虑麒麟、统信 UOS、openEuler 等系统，但不能仅凭发行版名称判定兼容。

必须按以下维度建立验证矩阵：

-   发行版及版本
-   CPU 架构：x86_64、aarch64，以及实际需要支持的国产 CPU 平台
-   glibc 版本
-   系统 ABI 和基础系统库
-   包格式及系统服务机制
-   原生扩展编译能力
-   图形环境和 WebView 依赖（如使用桌面壳）

对鲲鹏、飞腾等平台，应以实际 CPU 指令集、ABI
和发行版测试结果确定支持状态，不应仅凭 CPU 品牌推断二进制兼容。

提供国产 Linux
专项离线包和测试报告。若某个发行版/架构尚未通过完整测试，应明确标记为实验性或未验证，而不是宣称全面支持。

### 7.5 平台能力抽象

Go Core 通过统一接口调用平台 Adapter。建议抽象能力包括：

-   Runtime 安装和定位
-   服务启动、停止、状态检查
-   进程创建和终止
-   环境变量和路径管理
-   端口检测
-   文件权限和目录管理
-   系统服务注册
-   系统依赖检测
-   日志收集
-   安装、升级和卸载

平台 Adapter
可以采用不同底层实现，不要求各平台强行使用相同的服务管理机制。

------------------------------------------------------------------------

## 8. Git 与项目源码管理

### 8.1 Git Source 配置

项目源码配置至少包括：

  字段             说明
  ---------------- ----------------------
  Repository URL   Git 仓库地址
  Branch           分支
  Tag              标签
  Commit           精确提交
  Authentication   私有仓库认证
  Shallow Clone    是否浅克隆
  Submodules       是否初始化子模块
  Clone Path       Workspace 内源码目录

### 8.2 检出优先级

建议规则：

1.  如果指定 Commit，检出指定 Commit。
2.  否则如果指定 Tag，检出指定 Tag。
3.  否则检出指定 Branch 的最新提交。
4.  未指定 Branch 时，读取仓库默认分支。
5.  仓库地址为空时，Workspace 保持空项目状态。

### 8.3 源码安全

-   更新前检查工作区是否存在未提交修改。
-   默认不自动覆盖用户本地修改。
-   对 Reset、Clean、强制切换分支等破坏性操作要求二次确认。
-   更新失败时保留原源码状态并记录日志。
-   私有仓库凭据不得出现在命令日志、进程参数日志或普通配置文件中。
-   支持 GitHub，也允许用户配置其他兼容 Git 的仓库地址。

------------------------------------------------------------------------

## 9. 图形化管理界面

### 9.1 访问方式

提供本机管理页面，例如：

`http://127.0.0.1:17860`

管理器启动 Go 本地 HTTP
服务。用户可以通过浏览器打开，也可以从安装器或桌面快捷方式打开。

服务默认只监听回环地址，不应默认暴露到局域网或公网。

### 9.2 页面结构

1.  **Dashboard**
    -   DevKit 版本
    -   当前 Workspace
    -   PostgreSQL/Redis 状态
    -   当前项目运行状态
    -   一键启动、停止和重启
2.  **Workspaces**
    -   Workspace 列表
    -   创建、切换、重命名和删除
    -   初始化项目
    -   仓库和分支设置
    -   独立运行状态和端口
3.  **Runtime**
    -   Ruby、Bundler、Node.js、包管理工具版本
    -   Git 版本
    -   组件路径
    -   版本兼容检查
    -   组件修复和升级
4.  **Services**
    -   PostgreSQL 状态、数据库列表、连接信息
    -   Redis 状态、内存和连接数
    -   外部服务配置
    -   连接测试
5.  **Project**
    -   Git 仓库
    -   Branch/Tag/Commit
    -   Clone、Fetch、Pull、Checkout
    -   本地修改保护
6.  **Plugins**
    -   插件列表
    -   启用/禁用
    -   创建插件
    -   依赖检查
    -   测试和打包辅助
7.  **Themes**
    -   主题列表
    -   创建主题
    -   预览和构建辅助
8.  **Diagnostics**
    -   Runtime 检查
    -   数据库连接
    -   Redis 连接
    -   端口占用
    -   源码依赖
    -   前端资源构建
    -   插件兼容性
9.  **Logs**
    -   DevKit 日志
    -   Rails 日志
    -   PostgreSQL 日志
    -   Redis 日志
    -   前端构建日志
    -   测试日志
10. **Backup & Restore**
    -   Workspace 备份
    -   数据库备份
    -   配置备份
    -   恢复和校验
11. **Settings**
    -   Runtime 路径
    -   服务配置
    -   网络与端口
    -   存储路径
    -   组件仓库
    -   代理设置
    -   更新策略

### 9.3 本地 API 安全

-   只监听 `127.0.0.1` 或等效回环地址。
-   使用本地随机 Token 或等效会话认证。
-   校验 Origin/Host。
-   对有状态操作实施 CSRF 防护。
-   不允许任意外部网站调用本机管理 API。
-   对启动进程、删除数据、修改服务配置等敏感操作执行权限和参数校验。
-   日志中不得记录密码、Token、私有仓库凭据。

------------------------------------------------------------------------

## 10. CLI

CLI 是 GUI 的补充，便于自动化、开发调试和脚本调用。

建议命令：

``` text
hxxddk version
hxxddk doctor
hxxddk status

hxxddk workspace list
hxxddk workspace create <name>
hxxddk workspace use <name>
hxxddk workspace inspect <name>
hxxddk workspace delete <name>

hxxddk project init
hxxddk project clone
hxxddk project status
hxxddk project update

hxxddk start
hxxddk stop
hxxddk restart

hxxddk service status
hxxddk service start postgresql
hxxddk service start redis

hxxddk backup create
hxxddk backup restore
```

CLI 命令属于建议接口。正式发布前应确定命令兼容性和参数规范；CLI 与 GUI
必须调用同一套 Core API/业务逻辑，不能形成两套不一致的管理实现。

------------------------------------------------------------------------

## 11. 组件仓库、版本锁定和升级

### 11.1 组件元数据

每个 Runtime 组件包应提供：

-   组件名称
-   组件版本
-   目标 OS
-   CPU 架构
-   ABI/系统兼容要求
-   包格式
-   SHA-256
-   数字签名或可信发布校验信息
-   依赖关系
-   安装路径
-   安装/卸载步骤
-   许可证信息
-   兼容的 DevKit 版本范围

### 11.2 版本管理

-   每个 DevKit Release 固定一套经过测试的组件版本矩阵。
-   默认不自动升级核心 Runtime。
-   Workspace 可以锁定所需 Runtime Profile。
-   更新前检查项目兼容性。
-   组件升级失败时支持回滚到前一可用版本。
-   升级 DevKit 不得覆盖 Workspace 源码、数据库和用户配置。

### 11.3 GitHub Releases

GitHub Releases 作为主要分发入口，发布内容建议包括：

-   各平台 Full Installer
-   对应 SHA-256 校验文件
-   发布说明
-   第三方组件清单和许可证
-   支持平台/架构矩阵
-   已知问题
-   离线包说明
-   组件版本锁定清单

------------------------------------------------------------------------

## 12. 备份、恢复与数据安全

备份范围：

-   Workspace 元数据和配置
-   Git 仓库状态（可选打包源码，或记录远程仓库和 Commit）
-   PostgreSQL Database
-   上传文件和开发数据
-   必要的 Redis 持久化数据（如果项目确实依赖）
-   插件和主题的本地修改

要求：

-   每个 Workspace 可独立备份和恢复。
-   恢复前检查版本兼容性和磁盘空间。
-   默认不覆盖现有数据；覆盖恢复必须明确确认。
-   支持导出到用户指定目录。
-   备份文件应包含版本和完整性元数据。
-   敏感数据备份应支持加密或明确提示用户自行保护。

------------------------------------------------------------------------

## 13. 日志、诊断与可维护性

### 13.1 统一日志

日志至少包含：

-   时间
-   级别
-   模块
-   Workspace ID（适用时）
-   Task/Operation ID
-   错误码
-   可读错误说明

### 13.2 一键诊断

`doctor` 或 GUI 诊断应检查：

-   DevKit 自身文件完整性
-   Runtime 是否存在且版本正确
-   Ruby/Bundler 依赖
-   Node.js 和前端包管理工具
-   PostgreSQL 服务和数据库连接
-   Redis 服务和连接
-   Git 可用性
-   Workspace 配置完整性
-   端口占用
-   文件权限和磁盘空间
-   Discourse 依赖安装状态
-   插件/主题依赖状态

诊断结果应区分错误、警告和建议。自动修复前展示修复动作；涉及数据删除或覆盖的操作不得静默执行。

------------------------------------------------------------------------

## 14. 安装、运行和卸载流程

### 14.1 安装流程

``` text
下载对应平台 Full Installer
          |
          v
检查 OS / CPU / ABI / 权限
          |
          v
部署平台 Runtime Adapter
          |
          v
安装 Git / Ruby / Node / PostgreSQL / Redis
          |
          v
初始化 DevKit Core 与管理 UI
          |
          v
创建 Default Workspace
          |
          v
用户是否选择安装 Discourse 源码？
          |
      +---+---+
      |       |
     否       是
      |       |
      |   获取仓库并检出版本
      |       |
      |   初始化项目依赖
      |       |
      +---+---+
          |
          v
安装完成，打开管理界面
```

### 14.2 日常运行

-   用户从 GUI 或 CLI 选择 Workspace。
-   DevKit 检查该 Workspace 所需 Runtime 和服务。
-   按需启动 PostgreSQL、Redis。
-   检查数据库和 Redis 连接。
-   启动 Discourse 开发服务。
-   显示本地访问 URL、日志和进程状态。
-   停止 Workspace 时只停止该 Workspace 的项目进程；共享服务根据其他
    Workspace 的使用情况决定是否停止。

### 14.3 卸载

卸载时必须区分：

1.  卸载 DevKit 管理程序。
2.  卸载 DevKit 管理的 Runtime。
3.  删除或保留 PostgreSQL/Redis 数据。
4.  删除或保留 Workspace。
5.  删除或保留用户源码和备份。

默认卸载策略应保留用户数据，并在卸载前展示数据位置和清理选项。用户明确确认后才执行彻底清理。

------------------------------------------------------------------------

## 15. 测试与兼容性策略

### 15.1 测试维度

-   安装、升级、卸载
-   首次初始化
-   Default Workspace 自动创建
-   空 Workspace 创建和项目初始化
-   多 Workspace 并行和切换
-   数据库/Redis 隔离
-   端口分配和冲突处理
-   Git 仓库、分支、Tag、Commit
-   本地未提交修改保护
-   插件和主题开发基本流程
-   备份与恢复
-   断网和离线安装
-   权限不足和安装中断恢复
-   Runtime 损坏后的诊断与修复

### 15.2 平台矩阵

第一阶段至少覆盖：

  --------------------------------------------------------------------------
  平台                    架构                    测试重点
  ----------------------- ----------------------- --------------------------
  Windows 10/11（满足     x64                     WSL2
  WSL2 条件）                                     部署、文件系统、端口转发

  Windows 11              ARM64（如纳入支持）     WSL2 架构兼容性

  macOS                   Apple Silicon           原生 Runtime、签名、公证

  macOS                   Intel                   原生 Runtime、旧架构兼容

  Ubuntu/Debian           x86_64                  DEB/TAR、glibc

  Ubuntu/Debian           ARM64                   ARM Runtime 和原生扩展

  麒麟/UOS                x86_64、ARM64           发行版
                                                  ABI、系统库、安装流程

  openEuler               x86_64、aarch64         RPM、系统库和服务管理
  --------------------------------------------------------------------------

具体 OS 版本和 CPU 平台支持列表由每个 Release
的测试结果确定。没有完成测试的组合不得标记为正式支持。

------------------------------------------------------------------------

## 16. 安全与权限原则

-   DevKit 默认仅提供本机开发服务，不默认对公网开放。
-   本地管理 API 必须有认证和来源校验。
-   不将用户数据库密码、仓库 Token 写入普通日志。
-   Workspace 之间数据库凭据隔离。
-   Git 更新和数据删除等破坏性操作需要用户确认。
-   Runtime 和安装包必须校验来源、哈希和签名。
-   升级不得静默覆盖用户代码。
-   Windows WSL2、macOS 和 Linux 的提权操作必须遵循各自系统权限模型。
-   外部数据库/Redis 连接应支持安全传输配置。
-   DevKit
    不负责管理生产环境密钥，不应鼓励用户把生产凭据放入本地开发配置。

------------------------------------------------------------------------

## 17. 开发实施阶段

### Phase 1：Core 与平台基础

-   Go Core
-   配置模型
-   Workspace 数据模型
-   Default Workspace 自动创建
-   进程和 Task 管理
-   本地管理 API
-   Runtime Adapter 接口

### Phase 2：Runtime 与服务管理

-   Git
-   Ruby/Bundler/Rails Runtime
-   Node.js/前端工具链
-   PostgreSQL
-   Redis
-   外部数据库/Redis 配置
-   版本矩阵和组件包管理

### Phase 3：项目与 Workspace

-   Git 仓库配置
-   Branch/Tag/Commit 检出
-   多 Workspace 管理
-   数据库与 Redis 隔离
-   端口管理
-   项目初始化和启动/停止

### Phase 4：管理 UI

-   Dashboard
-   Workspace Manager
-   Runtime/Services
-   Project/Git
-   Logs/Diagnostics
-   Backup/Restore
-   Settings

### Phase 5：平台安装包

-   Windows + WSL2
-   macOS Intel/ARM64
-   Linux x86_64/ARM64
-   国产 Linux 专项适配
-   Full Offline Installer
-   安装升级卸载测试

### Phase 6：开发者体验

-   Plugin Developer
-   Theme Developer
-   CLI
-   项目模板机制
-   第三方模板扩展接口

------------------------------------------------------------------------

## 18. 项目模板机制

DevKit 应支持可选 Project Profile，帮助开发者快速创建不同类型项目。

初始模板建议：

  Profile                用途
  ---------------------- -------------------------
  `discourse-official`   官方 Discourse 开发
  `discourse-plugin`     Discourse 插件开发
  `discourse-theme`      Discourse 主题开发
  `discourse-custom`     自定义 Discourse Fork
  `hxx-agentforge`       HXX AgentForge 可选模板

模板只负责项目初始化参数、源码来源、依赖检查和可选扩展，不应改变 DevKit
核心架构。

第三方可以提供自己的模板仓库。DevKit
应在执行模板操作前展示其来源、将执行的操作和所需权限。

------------------------------------------------------------------------

## 19. 第一版验收标准

第一版发布前至少满足：

1.  用户可从 GitHub 下载对应平台安装包。
2.  安装器可以完成 DevKit 和基础 Runtime 安装。
3.  不要求用户预装 Ruby、Node.js、PostgreSQL、Redis、Git、Docker 或
    Homebrew。
4.  PostgreSQL、Redis、Git 默认安装并可由 DevKit 管理。
5.  Discourse 源码默认不安装。
6.  安装向导允许用户配置仓库地址和
    Branch/Tag/Commit，并选择是否安装源码。
7.  安装完成后自动创建 `Default` Workspace。
8.  用户可以创建多个 Workspace。
9.  不同 Workspace 的源码、配置、数据库、日志和端口正确隔离。
10. PostgreSQL 默认共享服务实例，但每个 Workspace 使用独立 Database
    和数据库用户。
11. Redis 默认共享服务实例，并使用独立 Key Prefix；可配置更强隔离方式。
12. 用户可以在安装后为任意 Workspace 添加 Discourse 源码。
13. 用户可以通过 GUI 查看状态、启动、停止、重启和诊断项目。
14. 用户可以配置外部 PostgreSQL 和 Redis 并测试连接。
15. 删除 Workspace 时不会误删其他 Workspace 或共享服务数据。
16. 安装包和第三方组件具备许可证及校验信息。
17. 已发布平台/架构均有可复现的安装测试记录。

------------------------------------------------------------------------

## 20. 待后续详细设计的事项

以下内容不改变本文已确定的产品定位和核心原则，但需要在详细设计阶段确定：

-   各 Release 对应的 Ruby、Node.js、PostgreSQL、Redis 具体版本矩阵。
-   Windows WSL2 发行版选择、导入方式、更新和恢复策略。
-   macOS 签名、公证和自动更新机制。
-   国产 Linux 的具体发行版版本、CPU 和 ABI 支持清单。
-   PostgreSQL 共享实例的初始化、权限和备份实现。
-   Redis Key Prefix 与独立 DB/实例的切换策略。
-   Workspace 端口分配、冲突处理和释放策略。
-   组件仓库元数据格式、签名和升级协议。
-   桌面壳是否作为可选发行组件提供。
-   插件/主题管理功能的自动化程度和兼容性检测边界。
-   Full Installer 的体积、分卷和离线源码缓存策略。

------------------------------------------------------------------------

## 附录 A：术语

  术语                定义
  ------------------- ---------------------------------------
  DevKit              hxxDiscourseDevKit 产品本身
  Runtime             Ruby、Node.js 等开发运行环境
  Service             PostgreSQL、Redis 等基础服务
  Workspace           一套隔离的项目开发工作区
  Project             Workspace 中实际开发的 Discourse 项目
  Project Profile     初始化项目时使用的模板
  Full Installer      携带目标平台基础运行组件的完整安装包
  Platform Adapter    针对不同操作系统和 ABI 的底层适配实现
  Default Workspace   安装后自动创建的默认工作区

## 附录 B：核心架构结论

hxxDiscourseDevKit 是独立、MIT 开源、跨平台、以 GitHub Full Installer
为主要交付方式的通用 Discourse 开发环境工具。

默认安装 Git、PostgreSQL、Redis、Ruby/Rails 和 Node.js
等开发依赖；Discourse
源码默认不安装，用户可以在安装时或安装后配置仓库地址、分支、Tag 和
Commit。

多项目 Workspace 从第一版开始支持，安装后自动创建 `Default`。Workspace
之间隔离源码、配置、数据库、日志和端口；PostgreSQL/Redis
默认采用共享服务实例加逻辑隔离的资源管理方式，并保留外部服务及更强隔离配置能力。

HXX AgentForge 是 DevKit 的一个可选下游项目模板，不是 DevKit
的依赖或产品边界。
