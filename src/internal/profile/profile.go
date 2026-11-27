// Package profile implements built-in project profiles (templates) and their
// dependency preflight checks.
package profile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
)

// ErrNotFound is returned when a profile ID is unknown.
var ErrNotFound = errors.New("project profile not found")

// Check statuses.
const (
	StatusOK      = "ok"
	StatusInfo    = "info"
	StatusWarning = "warning"
	StatusError   = "error"
)

// Origin describes where a profile definition came from. Third-party profiles
// must populate it before they may be executed.
type Origin struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	URL      string `json:"url,omitempty"`
	Version  string `json:"version,omitempty"`
	SourceID string `json:"source_id,omitempty"`
	Verified bool   `json:"verified"`
}

// Operation is one action a profile performs when a workspace is created.
type Operation struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Destructive bool   `json:"destructive"`
}

// Permission is one local capability a profile needs.
type Permission struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Risk        string `json:"risk"`
}

// Definition is a project template. It controls workspace defaults and
// dependency preflight checks; it never changes the core architecture.
type Definition struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	Category             string       `json:"category"`
	ProjectType          string       `json:"project_type"`
	DefaultRepositoryURL string       `json:"default_repository_url,omitempty"`
	DefaultBranch        string       `json:"default_branch,omitempty"`
	SourceRequired       bool         `json:"source_required"`
	ExtensionKind        string       `json:"extension_kind,omitempty"`
	RuntimeComponents    []string     `json:"runtime_components"`
	Services             []string     `json:"services"`
	Operations           []Operation  `json:"operations"`
	Permissions          []Permission `json:"permissions"`
	Origin               Origin       `json:"origin"`
}

// RuntimeChecker verifies one runtime component. *runtime.Manager satisfies it.
type RuntimeChecker interface {
	Verify(ctx context.Context, name string) (runtime.ComponentStatus, error)
}

// ServiceChecker reports one shared service. *service.Manager satisfies it.
type ServiceChecker interface {
	Status(ctx context.Context, name service.Name) (service.Status, error)
}

// Check is one preflight assertion.
type Check struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Required  bool   `json:"required"`
	Message   string `json:"message"`
	FixAction string `json:"fix_action,omitempty"`
}

// CheckReport is the result of a profile preflight check.
type CheckReport struct {
	ProfileID    string    `json:"profile_id"`
	ProfileName  string    `json:"profile_name"`
	GeneratedAt  time.Time `json:"generated_at"`
	Checks       []Check   `json:"checks"`
	Ready        bool      `json:"ready"`
	ErrorCount   int       `json:"error_count"`
	WarningCount int       `json:"warning_count"`
}

// Catalog holds the built-in profiles, third-party source profiles and the live
// dependency checkers.
type Catalog struct {
	mu       sync.RWMutex
	builtins []Definition
	defs     []Definition
	entries  []sourceEntry

	runtime  RuntimeChecker
	services ServiceChecker

	sourceMu       sync.Mutex
	sourceStore    string
	sourcesDir     string
	fetcher        GitFetcher
	sourcesEnabled atomic.Bool
}

// New builds the built-in catalog. Checkers may be nil, in which case checks
// report warnings instead of failing. Third-party sources are disabled until
// EnableSources is called.
func New(runtimeChecker RuntimeChecker, serviceChecker ServiceChecker) *Catalog {
	builtins := builtinDefinitions()
	return &Catalog{
		builtins: builtins,
		defs:     cloneDefinitions(builtins),
		runtime:  runtimeChecker,
		services: serviceChecker,
	}
}

// List returns a defensive copy of every profile in display order.
func (c *Catalog) List() []Definition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return cloneDefinitions(c.defs)
}

// Get returns one profile by ID. IDs are matched case-insensitively.
func (c *Catalog) Get(id string) (Definition, error) {
	id = strings.ToLower(strings.TrimSpace(id))
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, def := range c.defs {
		if def.ID == id {
			return cloneDefinition(def), nil
		}
	}
	return Definition{}, fmt.Errorf("%w: %s", ErrNotFound, id)
}

func cloneDefinitions(defs []Definition) []Definition {
	out := make([]Definition, 0, len(defs))
	for _, def := range defs {
		out = append(out, cloneDefinition(def))
	}
	return out
}

// Check evaluates a profile's runtime components, services, source defaults
// and provenance. Only runtime errors make the profile not ready; service and
// source findings are warnings that can be resolved after creation.
func (c *Catalog) Check(ctx context.Context, id string) (*CheckReport, error) {
	def, err := c.Get(id)
	if err != nil {
		return nil, err
	}
	report := &CheckReport{
		ProfileID:   def.ID,
		ProfileName: def.Name,
		GeneratedAt: time.Now().UTC(),
		Checks:      make([]Check, 0, len(def.RuntimeComponents)+len(def.Services)+3),
	}

	report.Checks = append(report.Checks, c.checkOrigin(def))
	for _, name := range def.RuntimeComponents {
		report.Checks = append(report.Checks, c.checkRuntimeComponent(ctx, name))
	}
	for _, name := range def.Services {
		report.Checks = append(report.Checks, c.checkService(ctx, name))
	}
	report.Checks = append(report.Checks, checkSource(def))
	if def.ExtensionKind != "" {
		report.Checks = append(report.Checks, Check{
			ID:       "extension.kind",
			Title:    "扩展类型",
			Status:   StatusInfo,
			Required: false,
			Message:  fmt.Sprintf("该模板面向 Discourse %s 开发。", extensionLabel(def.ExtensionKind)),
		})
	}

	for _, check := range report.Checks {
		switch check.Status {
		case StatusError:
			report.ErrorCount++
		case StatusWarning:
			report.WarningCount++
		}
	}
	report.Ready = report.ErrorCount == 0
	return report, nil
}

func (c *Catalog) checkOrigin(def Definition) Check {
	origin := def.Origin
	name := strings.TrimSpace(origin.Name)
	if name == "" {
		name = "未知来源"
	}
	status := StatusOK
	message := fmt.Sprintf("%s · %s", typeLabel(origin.Type), name)
	if origin.Version != "" {
		message += " · " + origin.Version
	}
	if origin.Verified {
		message += " · 已验证"
	} else {
		status = StatusWarning
		message += " · 未验证"
	}
	if origin.URL != "" {
		message += " · " + origin.URL
	}
	return Check{
		ID:       "origin",
		Title:    "模板来源",
		Status:   status,
		Required: true,
		Message:  message,
	}
}

func (c *Catalog) checkRuntimeComponent(ctx context.Context, name string) Check {
	title := componentLabel(name)
	check := Check{
		ID:       "runtime." + name,
		Title:    title,
		Required: true,
	}
	if c.runtime == nil {
		check.Status = StatusWarning
		check.Message = fmt.Sprintf("Runtime 检查未启用，无法验证 %s。", title)
		check.FixAction = "在 DevKit 设置中启用 Runtime 管理。"
		return check
	}
	status, err := c.runtime.Verify(ctx, name)
	if err != nil {
		check.Status = StatusWarning
		check.Message = fmt.Sprintf("无法检查 %s：%v", title, err)
		check.FixAction = "检查 Runtime 版本矩阵与执行环境设置。"
		return check
	}
	if !status.Available {
		check.Status = StatusError
		check.Message = fmt.Sprintf("未找到 %s。", title)
		if status.Message != "" {
			check.Message = fmt.Sprintf("未找到 %s：%s", title, status.Message)
		}
		check.FixAction = fmt.Sprintf("安装 %s，或在 Runtime 版本矩阵中配置可执行文件路径。", title)
		return check
	}
	if !status.VersionCompatible {
		check.Status = StatusWarning
		if status.Message != "" {
			check.Message = status.Message
		} else {
			check.Message = fmt.Sprintf("%s 版本与矩阵不匹配。", title)
		}
		check.FixAction = "安装匹配版本，或在 Runtime 版本矩阵中调整期望版本。"
		return check
	}
	check.Status = StatusOK
	check.Message = formatRuntimeMessage(title, status)
	if !status.Verified {
		check.Status = StatusWarning
		if status.Message != "" {
			check.Message = fmt.Sprintf("%s：%s", formatRuntimeMessage(title, status), status.Message)
		}
		check.FixAction = "在 Runtime 页面重新验证组件完整性。"
	}
	return check
}

func (c *Catalog) checkService(ctx context.Context, name string) Check {
	title := componentLabel(name)
	check := Check{
		ID:       "service." + name,
		Title:    title,
		Required: true,
	}
	if c.services == nil {
		check.Status = StatusWarning
		check.Message = fmt.Sprintf("服务管理未启用，无法验证 %s。", title)
		return check
	}
	status, err := c.services.Status(ctx, service.Name(name))
	if err != nil {
		check.Status = StatusWarning
		check.Message = fmt.Sprintf("无法检查 %s：%v", title, err)
		return check
	}
	switch status.State {
	case service.StateRunning:
		if status.Authenticated {
			check.Status = StatusOK
			check.Message = serviceMessage(title, status, "正在运行")
		} else {
			check.Status = StatusWarning
			check.Message = serviceMessage(title, status, "正在运行，但管理连接未通过认证")
			check.FixAction = "在「服务」页面运行连接测试并检查凭据。"
		}
	case service.StateExternal:
		if status.Reachable && status.Authenticated {
			check.Status = StatusOK
			check.Message = serviceMessage(title, status, "外部服务可连接")
		} else if status.Reachable {
			check.Status = StatusWarning
			check.Message = serviceMessage(title, status, "外部服务可连接，但认证未通过")
			check.FixAction = "检查外部服务用户名、密码与数据库配置。"
		} else {
			check.Status = StatusWarning
			check.Message = serviceMessage(title, status, "外部服务不可连接")
			check.FixAction = "检查外部服务地址、凭据与网络。"
		}
	case service.StateStopped:
		check.Status = StatusWarning
		check.Message = serviceMessage(title, status, "尚未启动")
		check.FixAction = fmt.Sprintf("在「服务」页面启动 %s；创建后也可随时重试。", title)
	case service.StateNotInstalled:
		check.Status = StatusWarning
		check.Message = serviceMessage(title, status, "尚未安装")
		check.FixAction = fmt.Sprintf("安装 %s，或切换到外部服务。", title)
	case service.StateUnreachable:
		check.Status = StatusWarning
		check.Message = serviceMessage(title, status, "无法连接")
		check.FixAction = "检查服务状态与连接配置。"
	default:
		check.Status = StatusWarning
		check.Message = serviceMessage(title, status, "状态异常")
		check.FixAction = "在「服务」页面查看诊断信息。"
	}
	return check
}

func checkSource(def Definition) Check {
	check := Check{
		ID:       "source.repository",
		Title:    "源码仓库",
		Required: def.SourceRequired,
	}
	if !def.SourceRequired {
		check.Status = StatusInfo
		check.Message = "该模板不要求 Git 源码。"
		return check
	}
	if strings.TrimSpace(def.DefaultRepositoryURL) == "" {
		check.Status = StatusWarning
		check.Message = "该模板未内置源码仓库，创建 Workspace 时必须提供仓库地址。"
		check.FixAction = "在创建 Workspace 时填写仓库地址与分支。"
		return check
	}
	check.Status = StatusOK
	check.Message = def.DefaultRepositoryURL
	if def.DefaultBranch != "" {
		check.Message += " @ " + def.DefaultBranch
	}
	return check
}

func formatRuntimeMessage(title string, status runtime.ComponentStatus) string {
	parts := []string{title}
	if status.ActualVersion != "" {
		parts = append(parts, status.ActualVersion)
	}
	if status.Execution != "" {
		parts = append(parts, status.Execution)
	}
	return strings.Join(parts, " · ")
}

func serviceMessage(title string, status service.Status, summary string) string {
	message := fmt.Sprintf("%s %s", title, summary)
	if status.Version != "" {
		message += " · " + status.Version
	}
	if status.Host != "" && status.Port > 0 {
		message += fmt.Sprintf(" · %s:%d", status.Host, status.Port)
	}
	if status.Message != "" && status.State != service.StateRunning && status.State != service.StateExternal {
		message += " · " + status.Message
	}
	return message
}

func typeLabel(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "builtin":
		return "内置"
	case "official":
		return "官方"
	case "third-party", "third_party":
		return "第三方"
	default:
		if t == "" {
			return "未知来源"
		}
		return t
	}
}

func extensionLabel(kind string) string {
	switch kind {
	case "plugin":
		return "插件"
	case "theme":
		return "主题"
	default:
		return kind
	}
}

func componentLabel(name string) string {
	switch name {
	case "ruby":
		return "Ruby"
	case "node":
		return "Node.js"
	case "git":
		return "Git"
	case "postgresql":
		return "PostgreSQL"
	case "redis":
		return "Redis"
	default:
		return name
	}
}

func cloneDefinition(def Definition) Definition {
	def.RuntimeComponents = append([]string(nil), def.RuntimeComponents...)
	def.Services = append([]string(nil), def.Services...)
	def.Operations = append([]Operation(nil), def.Operations...)
	def.Permissions = append([]Permission(nil), def.Permissions...)
	return def
}

const discourseRepositoryURL = "https://github.com/discourse/discourse.git"

func builtinOrigin() Origin {
	return Origin{
		Type:     "builtin",
		Name:     "hxxDiscourseDevKit",
		Version:  "v0.1",
		Verified: true,
	}
}

func baseOperations() []Operation {
	return []Operation{
		{
			ID:          "workspace.create",
			Title:       "创建 Workspace",
			Description: "在实例目录下建立独立的配置、日志、数据与源码目录，并分配开发端口与 Redis DB。",
		},
		{
			ID:          "database.provision",
			Title:       "准备隔离数据库",
			Description: "PostgreSQL 可用时，为该 Workspace 创建独立的数据库与角色。不会修改其他 Workspace 的数据。",
		},
		{
			ID:          "source.configure",
			Title:       "写入源码配置",
			Description: "只记录仓库地址与分支，不自动克隆，也不会执行远程代码。",
		},
	}
}

func basePermissions() []Permission {
	return []Permission{
		{
			ID:          "workspace.write",
			Title:       "写入实例目录",
			Description: "在 DevKit 实例的 workspaces/ 下创建该 Workspace 的目录与元数据。",
			Risk:        "low",
		},
		{
			ID:          "database.create",
			Title:       "创建隔离数据库",
			Description: "在托管或外部 PostgreSQL 中创建专用数据库与角色。",
			Risk:        "medium",
		},
		{
			ID:          "secret.write",
			Title:       "写入本机凭据",
			Description: "生成并保存数据库密码到本机受保护的 secrets.json，不写入源码或日志。",
			Risk:        "medium",
		},
		{
			ID:          "network.port",
			Title:       "分配本机端口",
			Description: "在 127.0.0.1 上为开发服务器分配一个空闲端口。",
			Risk:        "low",
		},
	}
}

func builtinDefinitions() []Definition {
	base := func(id, name, category, projectType, description string) Definition {
		return Definition{
			ID:                   id,
			Name:                 name,
			Description:          description,
			Category:             category,
			ProjectType:          projectType,
			DefaultRepositoryURL: discourseRepositoryURL,
			DefaultBranch:        "main",
			SourceRequired:       true,
			RuntimeComponents:    []string{"ruby", "node", "git"},
			Services:             []string{"postgresql", "redis"},
			Operations:           baseOperations(),
			Permissions:          basePermissions(),
			Origin:               builtinOrigin(),
		}
	}

	official := base("discourse-official", "官方 Discourse 开发", "official", "discourse",
		"完整 Discourse 站点开发：使用官方仓库，后续可安装依赖、初始化数据库并启动 Rails。")
	plugin := base("discourse-plugin", "Discourse 插件开发", "plugin", "discourse-plugin",
		"以官方 Discourse 为宿主开发插件；插件位于源码 plugins/ 目录，可启用、禁用与打包分发。")
	plugin.ExtensionKind = "plugin"

	theme := base("discourse-theme", "Discourse 主题开发", "theme", "discourse-theme",
		"开发 Discourse 主题；主题保存在 Workspace 的 data/themes 目录，可打包或使用 discourse_theme 预览。")
	theme.ExtensionKind = "theme"

	custom := base("discourse-custom", "自定义 Discourse 二次开发", "custom", "discourse-custom",
		"基于自定义 Git 仓库的二次开发。模板不内置仓库地址，创建时必须提供自己的源码仓库。")
	custom.DefaultRepositoryURL = ""

	agentforge := base("hxx-agentforge", "HXX AgentForge 下游模板", "downstream", "hxx-agentforge",
		"面向 HXX AgentForge 下游集成的预留模板；需要提供内部仓库地址，DevKit 不会自动访问外部服务。")
	agentforge.DefaultRepositoryURL = ""

	return []Definition{official, plugin, theme, custom, agentforge}
}
