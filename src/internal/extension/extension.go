// Package extension manages Discourse plugin and theme scaffolds that belong
// to a workspace.
package extension

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// Kind identifies a supported extension type.
type Kind string

const (
	KindPlugin Kind = "plugin"
	KindTheme  Kind = "theme"
)

var (
	ErrValidation     = errors.New("extension validation failed")
	ErrExists         = errors.New("extension already exists")
	ErrNotFound       = errors.New("extension not found")
	ErrSourceMissing  = errors.New("Discourse source is not installed")
	ErrProjectRunning = errors.New("project must be stopped")
	ErrStateConflict  = errors.New("extension state conflict")
	ErrNotInitialized = errors.New("project is not initialized")
	errUnknownKind    = errors.New("unknown extension kind")
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

	discourseMajorPattern  = regexp.MustCompile(`(?m)\bMAJOR\s*=\s*(\d+)`)
	discourseMinorPattern  = regexp.MustCompile(`(?m)\bMINOR\s*=\s*(\d+)`)
	discourseTinyPattern   = regexp.MustCompile(`(?m)\bTINY\s*=\s*(\d+)`)
	discoursePrePattern    = regexp.MustCompile(`(?m)\bPRE\s*=\s*(?:nil|"([^"]*)"|'([^']*)')`)
	discourseStringPattern = regexp.MustCompile(`(?m)\b(?:STRING|VERSION)\s*=\s*["']([^"']+)["']`)
	versionPattern         = regexp.MustCompile(`^[vV]?(\d+)(?:\.(\d+))?(?:\.(\d+))?(?:[.\-+~]?([0-9A-Za-z][0-9A-Za-z.\-+~]*))?$`)
)

// WorkspaceResolver resolves a workspace by ID or name.
type WorkspaceResolver interface {
	Get(ctx context.Context, ref string) (*workspace.Workspace, error)
}

// Check is one prerequisite or discovery result shown by the UI.
type Check struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

// Item describes one plugin or theme directory.
type Item struct {
	Kind        Kind      `json:"kind"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	About       string    `json:"about"`
	Version     string    `json:"version"`
	Authors     string    `json:"authors"`
	URL         string    `json:"url,omitempty"`
	Path        string    `json:"path"`
	Enabled     bool      `json:"enabled"`
	HasMetadata bool      `json:"has_metadata"`
	FileCount   int       `json:"file_count"`
	ModifiedAt  time.Time `json:"modified_at"`
}

// Listing is the discovery result for one workspace and extension kind.
type Listing struct {
	WorkspaceID     string  `json:"workspace_id"`
	Kind            Kind    `json:"kind"`
	Root            string  `json:"root"`
	SourceInstalled bool    `json:"source_installed"`
	Checks          []Check `json:"checks"`
	Items           []Item  `json:"items"`
	Total           int     `json:"total"`
}

// CreateRequest is the user-supplied metadata for a new scaffold.
type CreateRequest struct {
	Name    string `json:"name"`
	Slug    string `json:"slug"`
	About   string `json:"about"`
	Version string `json:"version"`
	Authors string `json:"authors"`
}

// Package is a distributable ZIP archive created from a plugin or theme.
type Package struct {
	Kind      Kind      `json:"kind"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	FileName  string    `json:"file_name"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"size_bytes"`
	SHA256    string    `json:"sha256"`
	FileCount int       `json:"file_count"`
	CreatedAt time.Time `json:"created_at"`
}

// BuildResult is the outcome of a theme build. A theme with blocking check
// errors is not packaged, so Package is only populated when Ready is true.
type BuildResult struct {
	Ready   bool         `json:"ready"`
	Message string       `json:"message"`
	Check   *CheckReport `json:"check"`
	Package *Package     `json:"package,omitempty"`
}

// Finding statuses used by CheckReport.
const (
	SeverityOK      = "ok"
	SeverityWarning = "warning"
	SeverityError   = "error"
	SeveritySkipped = "skipped"
)

// Finding is one metadata, dependency or compatibility assertion.
type Finding struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Required bool   `json:"required"`
	Fix      string `json:"fix,omitempty"`
}

// CheckReport is the result of validating one plugin or theme against the
// workspace's Discourse source tree. It is advisory: the check never changes
// files, and Ready only reports whether any blocking error was found.
type CheckReport struct {
	WorkspaceID      string    `json:"workspace_id"`
	Kind             Kind      `json:"kind"`
	Slug             string    `json:"slug"`
	Name             string    `json:"name"`
	Path             string    `json:"path"`
	Enabled          bool      `json:"enabled"`
	Ready            bool      `json:"ready"`
	SourceInstalled  bool      `json:"source_installed"`
	DiscourseVersion string    `json:"discourse_version,omitempty"`
	RequiredVersion  string    `json:"required_version,omitempty"`
	MaximumVersion   string    `json:"maximum_version,omitempty"`
	ErrorCount       int       `json:"error_count"`
	WarningCount     int       `json:"warning_count"`
	Findings         []Finding `json:"findings"`
	CheckedAt        time.Time `json:"checked_at"`
}

// PluginTestPlan describes a validated RSpec invocation for one plugin. It is
// produced by PreparePluginTest and executed by the project manager, which owns
// Runtime resolution and test database lifecycle.
type PluginTestPlan struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceName string `json:"workspace_name"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	PluginPath    string `json:"plugin_path"`
	SourceRoot    string `json:"source_root"`
	SpecPath      string `json:"spec_path"`
	SpecFiles     int    `json:"spec_files"`
	TestDatabase  string `json:"test_database"`
	Command       string `json:"command"`
}

// Manager discovers and creates plugin/theme scaffolds.
type Manager struct {
	paths      *config.Paths
	workspaces WorkspaceResolver
	log        *slog.Logger
}

// New creates an extension manager.
func New(paths *config.Paths, workspaces WorkspaceResolver, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{paths: paths, workspaces: workspaces, log: logger}
}

// ListPlugins lists plugins under the workspace's Discourse source tree.
func (m *Manager) ListPlugins(ctx context.Context, ref string) (*Listing, error) {
	return m.list(ctx, ref, KindPlugin)
}

// ListThemes lists theme scaffolds stored in the workspace data directory.
func (m *Manager) ListThemes(ctx context.Context, ref string) (*Listing, error) {
	return m.list(ctx, ref, KindTheme)
}

// CreatePlugin creates a plugin scaffold inside <source>/plugins/<slug>.
func (m *Manager) CreatePlugin(ctx context.Context, ref string, req CreateRequest) (*Item, error) {
	return m.create(ctx, ref, KindPlugin, req)
}

// CreateTheme creates a theme scaffold inside <workspace>/data/themes/<slug>.
func (m *Manager) CreateTheme(ctx context.Context, ref string, req CreateRequest) (*Item, error) {
	return m.create(ctx, ref, KindTheme, req)
}

// PackagePlugin creates a ZIP archive from an installed plugin.
func (m *Manager) PackagePlugin(ctx context.Context, ref, slug string) (*Package, error) {
	return m.packageItem(ctx, ref, KindPlugin, slug)
}

// PackageTheme creates a ZIP archive from a theme scaffold.
func (m *Manager) PackageTheme(ctx context.Context, ref, slug string) (*Package, error) {
	return m.packageItem(ctx, ref, KindTheme, slug)
}

// EnablePlugin moves a disabled plugin back under the Discourse source tree.
func (m *Manager) EnablePlugin(ctx context.Context, ref, slug string) (*Item, error) {
	return m.setPluginEnabled(ctx, ref, slug, true)
}

// DisablePlugin moves a plugin out of the Discourse source tree while keeping
// its files inside the workspace.
func (m *Manager) DisablePlugin(ctx context.Context, ref, slug string) (*Item, error) {
	return m.setPluginEnabled(ctx, ref, slug, false)
}

// CheckPlugin validates a plugin's metadata, declared Discourse requirement
// and optional frontend dependency manifest against the installed source.
func (m *Manager) CheckPlugin(ctx context.Context, ref, slug string) (*CheckReport, error) {
	return m.check(ctx, ref, KindPlugin, slug)
}

// CheckTheme validates a theme's about.json, structure and declared Discourse
// version range against the installed source.
func (m *Manager) CheckTheme(ctx context.Context, ref, slug string) (*CheckReport, error) {
	return m.check(ctx, ref, KindTheme, slug)
}

// BuildTheme validates a theme and only creates a distributable ZIP when the
// check reports no blocking errors. A failed build still returns its report so
// callers can show the user what needs to be fixed.
func (m *Manager) BuildTheme(ctx context.Context, ref, slug string) (*BuildResult, error) {
	report, err := m.CheckTheme(ctx, ref, slug)
	if err != nil {
		return nil, err
	}
	result := &BuildResult{Ready: report.Ready, Check: report}
	if !report.Ready {
		result.Message = fmt.Sprintf("依赖检查发现 %d 个错误，未生成分发包。", report.ErrorCount)
		return result, nil
	}
	pkg, err := m.PackageTheme(ctx, ref, slug)
	if err != nil {
		return nil, err
	}
	result.Package = pkg
	result.Message = "构建完成，已生成可导入 Discourse 的分发包。"
	return result, nil
}

// PreparePluginTest validates that a plugin can run its RSpec suite and
// returns the immutable plan consumed by the project manager. It never starts
// a process or modifies the database.
func (m *Manager) PreparePluginTest(ctx context.Context, ref, slug string) (*PluginTestPlan, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("%w: 目录名不合法", ErrValidation)
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	item, err := m.locateItem(ws, KindPlugin, slug)
	if err != nil {
		return nil, err
	}
	if !item.Enabled {
		return nil, fmt.Errorf("%w: 插件已禁用，请先启用后再运行测试", ErrStateConflict)
	}
	if !ws.Source.Installed {
		return nil, fmt.Errorf("%w: 请先在项目页克隆或初始化 Discourse 源码", ErrSourceMissing)
	}
	sourceRoot, err := m.sourceRoot(ws)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(sourceRoot); statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 源码目录 %s 不存在", ErrSourceMissing, sourceRoot)
	}
	if !ws.Project.Initialized {
		return nil, fmt.Errorf("%w: 请先在项目页初始化 Ruby 依赖与数据库", ErrNotInitialized)
	}
	info, statErr := os.Lstat(item.Path)
	if statErr != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: 插件目录 %s 不可访问", ErrValidation, item.Path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: 插件目录是符号链接，DevKit 不会执行链接目标", ErrValidation)
	}
	specDir := filepath.Join(item.Path, "spec")
	specInfo, specErr := os.Lstat(specDir)
	switch {
	case specErr != nil:
		return nil, fmt.Errorf("%w: 插件缺少 spec/ 目录", ErrValidation)
	case specInfo.Mode()&os.ModeSymlink != 0:
		return nil, fmt.Errorf("%w: spec/ 目录是符号链接，DevKit 不会执行链接目标", ErrValidation)
	case !specInfo.IsDir():
		return nil, fmt.Errorf("%w: spec/ 不是目录", ErrValidation)
	}
	files, err := countSpecFiles(specDir)
	if err != nil {
		return nil, err
	}
	if files == 0 {
		return nil, fmt.Errorf("%w: spec/ 中没有 *_spec.rb 测试文件", ErrValidation)
	}
	database := ws.TestDatabaseName()
	if database == "" {
		return nil, fmt.Errorf("%w: Workspace 没有可用于测试的数据库名", ErrValidation)
	}
	relativeSpec := filepath.ToSlash(filepath.Join("plugins", item.Slug, "spec"))
	command := "RAILS_ENV=test LOAD_PLUGINS=1 RAILS_DB=" + database +
		" bundle exec rspec " + relativeSpec
	return &PluginTestPlan{
		WorkspaceID:   ws.WorkspaceID,
		WorkspaceName: ws.Name,
		Slug:          item.Slug,
		Name:          item.Name,
		PluginPath:    item.Path,
		SourceRoot:    sourceRoot,
		SpecPath:      specDir,
		SpecFiles:     files,
		TestDatabase:  database,
		Command:       command,
	}, nil
}

func (m *Manager) setPluginEnabled(ctx context.Context, ref, slug string, enabled bool) (*Item, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("%w: 目录名不合法", ErrValidation)
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if enabled && !ws.Source.Installed {
		return nil, fmt.Errorf("%w: 请先在项目页克隆或初始化 Discourse 源码", ErrSourceMissing)
	}
	if err := ensureProjectStopped(ws); err != nil {
		return nil, err
	}
	enabledRoot, err := m.root(ws, KindPlugin)
	if err != nil {
		return nil, err
	}
	sourceRoot, targetRoot := enabledRoot, m.disabledPluginsRoot(ws)
	if enabled {
		sourceRoot, targetRoot = targetRoot, enabledRoot
	}
	sourceDir := filepath.Join(sourceRoot, slug)
	targetDir := filepath.Join(targetRoot, slug)

	info, statErr := os.Lstat(sourceDir)
	switch {
	case statErr == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("%w: 不支持符号链接形式的插件目录", ErrValidation)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%w: %s 不是插件目录", ErrValidation, slug)
		}
	case errors.Is(statErr, fs.ErrNotExist):
		exists, existsErr := pathExists(targetDir)
		if existsErr != nil {
			return nil, existsErr
		}
		if exists {
			if enabled {
				return nil, fmt.Errorf("%w: 插件已启用", ErrStateConflict)
			}
			return nil, fmt.Errorf("%w: 插件已禁用", ErrStateConflict)
		}
		return nil, fmt.Errorf("%w: %s 不存在", ErrNotFound, slug)
	default:
		return nil, statErr
	}
	exists, err := pathExists(targetDir)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: 目标目录 %s 已存在", ErrStateConflict, targetDir)
	}
	if enabled {
		sourceTree := filepath.Dir(enabledRoot)
		if tree, treeErr := os.Stat(sourceTree); treeErr != nil || !tree.IsDir() {
			return nil, fmt.Errorf("%w: 源码目录 %s 不存在", ErrSourceMissing, sourceTree)
		}
	}
	if err := os.MkdirAll(targetRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create plugin root: %w", err)
	}
	if err := os.Rename(sourceDir, targetDir); err != nil {
		return nil, fmt.Errorf("move plugin directory: %w", err)
	}
	item, err := readItem(KindPlugin, targetRoot, slug)
	if err != nil {
		_ = os.Rename(targetDir, sourceDir)
		return nil, err
	}
	item.Enabled = enabled
	m.log.Info("changed plugin state",
		"workspace", ws.WorkspaceID, "slug", slug, "enabled", enabled, "path", item.Path)
	return item, nil
}

func (m *Manager) check(ctx context.Context, ref string, kind Kind, slug string) (*CheckReport, error) {
	if kind != KindPlugin && kind != KindTheme {
		return nil, errUnknownKind
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("%w: 目录名不合法", ErrValidation)
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	item, err := m.locateItem(ws, kind, slug)
	if err != nil {
		return nil, err
	}
	report := &CheckReport{
		WorkspaceID: ws.WorkspaceID,
		Kind:        kind,
		Slug:        item.Slug,
		Name:        item.Name,
		Path:        item.Path,
		Enabled:     item.Enabled,
		Findings:    make([]Finding, 0, 10),
		CheckedAt:   time.Now().UTC(),
	}

	m.checkDirectory(report, item)
	if kind == KindPlugin {
		if item.Enabled {
			report.add(Finding{
				ID: "state", Title: "插件状态", Status: SeverityOK,
				Message: "插件位于源码 plugins/ 目录，Discourse 重启后会加载。",
			})
		} else {
			report.add(Finding{
				ID: "state", Title: "插件状态", Status: SeverityWarning,
				Message: "插件位于 data/disabled-plugins/，当前不会随 Discourse 加载。",
				Fix:     "运行 hxxddk plugin enable " + item.Slug + "，然后重启 Rails。",
			})
		}
	}

	sourceRoot, sourceReady := m.checkSource(report, ws)
	if sourceReady {
		if version, ok := readDiscourseVersion(sourceRoot); ok {
			report.DiscourseVersion = version
			report.add(Finding{
				ID: "discourse_version", Title: "Discourse 版本", Status: SeverityOK,
				Message: "识别到源码版本 " + version + "。",
			})
		} else {
			report.add(Finding{
				ID: "discourse_version", Title: "Discourse 版本", Status: SeverityWarning,
				Message: "无法从 lib/version.rb 读取 Discourse 版本，版本兼容性将无法校验。",
				Fix:     "确认源码完整，或手动检查扩展对当前 Discourse 版本的兼容性。",
			})
		}
	}

	switch kind {
	case KindPlugin:
		m.checkPluginManifest(report, item.Path)
	case KindTheme:
		m.checkThemeManifest(report, item.Path)
	}
	m.checkCompatibility(report)

	report.ErrorCount, report.WarningCount = 0, 0
	for _, finding := range report.Findings {
		switch finding.Status {
		case SeverityError:
			report.ErrorCount++
		case SeverityWarning:
			report.WarningCount++
		}
	}
	report.Ready = report.ErrorCount == 0
	return report, nil
}

func (report *CheckReport) add(finding Finding) {
	report.Findings = append(report.Findings, finding)
}

func (m *Manager) checkDirectory(report *CheckReport, item *Item) {
	info, err := os.Lstat(item.Path)
	switch {
	case err != nil:
		report.add(Finding{
			ID: "directory", Title: "扩展目录", Status: SeverityError, Required: true,
			Message: "无法访问扩展目录：" + err.Error(),
		})
	case info.Mode()&os.ModeSymlink != 0:
		report.add(Finding{
			ID: "directory", Title: "扩展目录", Status: SeverityError, Required: true,
			Message: "扩展目录是符号链接，DevKit 不会管理链接目标。",
			Fix:     "把插件或主题移动到 Workspace 内的真实目录。",
		})
	case !info.IsDir():
		report.add(Finding{
			ID: "directory", Title: "扩展目录", Status: SeverityError, Required: true,
			Message: item.Path + " 不是目录。",
		})
	default:
		report.add(Finding{
			ID: "directory", Title: "扩展目录", Status: SeverityOK,
			Message: item.Path,
		})
	}
}

func (m *Manager) checkSource(report *CheckReport, ws *workspace.Workspace) (string, bool) {
	root, err := m.sourceRoot(ws)
	if err != nil {
		report.add(Finding{
			ID: "source", Title: "Discourse 源码", Status: SeverityError, Required: true,
			Message: err.Error(),
			Fix:     "在 Workspace 设置中把源码路径改为 Workspace 内的相对路径。",
		})
		return "", false
	}
	required := report.Kind == KindPlugin
	if !ws.Source.Installed {
		status := SeverityWarning
		if required {
			status = SeverityError
		}
		report.add(Finding{
			ID: "source", Title: "Discourse 源码", Status: status, Required: required,
			Message: "当前 Workspace 尚未安装 Discourse 源码。",
			Fix:     "在项目页克隆或初始化 Discourse 源码后重新检查。",
		})
		return root, false
	}
	info, statErr := os.Stat(root)
	if statErr != nil || !info.IsDir() {
		status := SeverityWarning
		if required {
			status = SeverityError
		}
		message := "源码目录不可访问：" + root
		if statErr != nil {
			message += "（" + statErr.Error() + "）"
		} else {
			message += "（不是目录）"
		}
		report.add(Finding{
			ID: "source", Title: "Discourse 源码", Status: status, Required: required,
			Message: message,
			Fix:     "在项目页重新克隆或修复源码目录。",
		})
		return root, false
	}
	report.add(Finding{
		ID: "source", Title: "Discourse 源码", Status: SeverityOK,
		Message: "源码可用：" + root,
	})
	return root, true
}

func (m *Manager) checkPluginManifest(report *CheckReport, dir string) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.rb"))
	switch {
	case err != nil:
		report.add(Finding{
			ID: "manifest", Title: "plugin.rb", Status: SeverityError, Required: true,
			Message: "缺少可读取的 plugin.rb：" + err.Error(),
			Fix:     "补全 plugin.rb 后重新检查。",
		})
	case len(bytes.TrimSpace(data)) == 0:
		report.add(Finding{
			ID: "manifest", Title: "plugin.rb", Status: SeverityError, Required: true,
			Message: "plugin.rb 为空，Discourse 无法加载该插件。",
		})
	default:
		report.add(Finding{
			ID: "manifest", Title: "plugin.rb", Status: SeverityOK,
			Message: fmt.Sprintf("plugin.rb 可读取（%d 字节）。", len(data)),
		})
		meta, ok := parsePluginMetadata(data)
		if !ok {
			report.add(Finding{
				ID: "metadata", Title: "插件元数据", Status: SeverityWarning,
				Message: "plugin.rb 未包含标准注释元数据（# name:、# about:、# version: 等）。",
				Fix:     "在 plugin.rb 顶部补充注释元数据。",
			})
			break
		}
		report.RequiredVersion = meta.requiredVersion
		missing := missingFields(map[string]string{
			"name":    meta.name,
			"about":   meta.about,
			"version": meta.version,
			"authors": meta.authors,
		}, []string{"name", "about", "version", "authors"})
		if len(missing) == 0 {
			report.add(Finding{
				ID: "metadata", Title: "插件元数据", Status: SeverityOK,
				Message: "name、about、version 与 authors 已提供。",
			})
		} else {
			status := SeverityWarning
			message := "缺少元数据字段：" + strings.Join(missing, "、") + "。"
			if meta.name == "" {
				message = "未声明插件名称（# name:）；" + message
			}
			report.add(Finding{
				ID: "metadata", Title: "插件元数据", Status: status,
				Message: message,
				Fix:     "在 plugin.rb 顶部补充对应注释字段。",
			})
		}
	}
	m.checkPackageJSON(report, dir)
}

func (m *Manager) checkPackageJSON(report *CheckReport, dir string) {
	const maxManifestSize = 256 * 1024
	path := filepath.Join(dir, "package.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		report.add(Finding{
			ID: "frontend_dependencies", Title: "前端依赖", Status: SeveritySkipped,
			Message: "未提供 package.json，使用 Discourse 核心前端依赖。",
		})
		return
	}
	if err != nil {
		report.add(Finding{
			ID: "frontend_dependencies", Title: "前端依赖", Status: SeverityWarning,
			Message: "package.json 不可读取：" + err.Error(),
		})
		return
	}
	if len(data) > maxManifestSize {
		report.add(Finding{
			ID: "frontend_dependencies", Title: "前端依赖", Status: SeverityWarning,
			Message: "package.json 超过 256 KiB，已跳过解析。",
		})
		return
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		report.add(Finding{
			ID: "frontend_dependencies", Title: "前端依赖", Status: SeverityWarning,
			Message: "package.json 不是有效 JSON：" + err.Error(),
			Fix:     "修复 package.json 后重新检查。",
		})
		return
	}
	total := len(manifest.Dependencies) + len(manifest.DevDependencies)
	message := fmt.Sprintf("package.json 可解析：dependencies %d 个，devDependencies %d 个。",
		len(manifest.Dependencies), len(manifest.DevDependencies))
	fix := ""
	if total > 0 {
		fix = "在插件目录运行包管理器安装前端依赖；DevKit 不会自动安装第三方依赖。"
	}
	report.add(Finding{
		ID: "frontend_dependencies", Title: "前端依赖", Status: SeverityOK,
		Message: message, Fix: fix,
	})
}

func (m *Manager) checkThemeManifest(report *CheckReport, dir string) {
	meta, err := parseThemeMetadata(dir)
	if err != nil {
		message := "about.json 不可读取：" + err.Error()
		fix := "补全有效的 about.json 后重新检查。"
		if errors.Is(err, fs.ErrNotExist) {
			message = "缺少 about.json，Discourse 无法识别该主题。"
		}
		report.add(Finding{
			ID: "manifest", Title: "about.json", Status: SeverityError, Required: true,
			Message: message, Fix: fix,
		})
	} else {
		report.RequiredVersion = meta.MinimumDiscourseVersion
		report.MaximumVersion = meta.MaximumDiscourseVersion
		report.add(Finding{
			ID: "manifest", Title: "about.json", Status: SeverityOK,
			Message: "about.json 可解析。",
		})
		authors := strings.TrimSpace(string(meta.Authors))
		missing := missingFields(map[string]string{
			"name":          meta.Name,
			"about":         meta.About,
			"authors":       authors,
			"license":       meta.License,
			"theme_version": meta.ThemeVersion,
		}, []string{"name", "about", "authors", "license", "theme_version"})
		if len(missing) == 0 {
			report.add(Finding{
				ID: "metadata", Title: "主题元数据", Status: SeverityOK,
				Message: "name、about、authors、license 与 theme_version 已提供。",
			})
		} else {
			message := "缺少元数据字段：" + strings.Join(missing, "、") + "。"
			if meta.Name == "" {
				message = "未声明主题名称（name）；" + message
			}
			report.add(Finding{
				ID: "metadata", Title: "主题元数据", Status: SeverityWarning,
				Message: message,
				Fix:     "在 about.json 中补充对应字段。",
			})
		}
	}

	var found []string
	for _, name := range []string{"common", "desktop", "mobile", "assets", "javascripts", "locales"} {
		info, statErr := os.Stat(filepath.Join(dir, name))
		if statErr == nil && info.IsDir() {
			found = append(found, name)
		}
	}
	if len(found) == 0 {
		report.add(Finding{
			ID: "structure", Title: "主题目录结构", Status: SeverityWarning,
			Message: "未找到 common/desktop/mobile/assets/javascripts/locales 等主题目录。",
			Fix:     "至少创建一个样式或资源目录，或确认该主题只包含远程资源。",
		})
	} else {
		report.add(Finding{
			ID: "structure", Title: "主题目录结构", Status: SeverityOK,
			Message: "包含目录：" + strings.Join(found, "、") + "。",
		})
	}
}

func (m *Manager) checkCompatibility(report *CheckReport) {
	required := strings.TrimSpace(report.RequiredVersion)
	maximum := strings.TrimSpace(report.MaximumVersion)
	if required == "" && maximum == "" {
		field := "minimum_discourse_version"
		if report.Kind == KindPlugin {
			field = "required_version"
		}
		report.add(Finding{
			ID: "compatibility", Title: "版本兼容性", Status: SeveritySkipped,
			Message: "未声明 " + field + "，跳过版本约束检查。",
		})
		return
	}
	if report.DiscourseVersion == "" {
		report.add(Finding{
			ID: "compatibility", Title: "版本兼容性", Status: SeverityWarning,
			Message: "已声明版本约束，但未识别当前 Discourse 版本，无法比较。",
			Fix:     "确认源码中的 lib/version.rb 存在且可解析。",
		})
		return
	}
	installed, ok := parseVersion(report.DiscourseVersion)
	if !ok {
		report.add(Finding{
			ID: "compatibility", Title: "版本兼容性", Status: SeverityWarning,
			Message: "无法解析当前 Discourse 版本 " + report.DiscourseVersion + "。",
		})
		return
	}

	var problems, warnings []string
	if required != "" {
		want, ok := parseLowerBound(required)
		if !ok {
			warnings = append(warnings, "无法解析最低版本 "+required)
		} else if compareVersions(installed, want) < 0 {
			problems = append(problems, fmt.Sprintf("要求 Discourse >= %s，当前为 %s", required, report.DiscourseVersion))
		}
	}
	if maximum != "" {
		want, ok := parseUpperBound(maximum)
		if !ok {
			warnings = append(warnings, "无法解析最高版本 "+maximum)
		} else if compareVersions(installed, want) > 0 {
			problems = append(problems, fmt.Sprintf("要求 Discourse <= %s，当前为 %s", maximum, report.DiscourseVersion))
		}
	}
	if len(warnings) > 0 {
		report.add(Finding{
			ID: "compatibility_warning", Title: "版本约束格式", Status: SeverityWarning,
			Message: strings.Join(warnings, "；") + "。",
			Fix:     "使用形如 3.0.0 或 >= 3.0.0 的版本约束。",
		})
	}
	if len(problems) > 0 {
		report.add(Finding{
			ID: "compatibility", Title: "版本兼容性", Status: SeverityError, Required: true,
			Message: strings.Join(problems, "；") + "。",
			Fix:     "切换到这个扩展支持的 Discourse 分支/版本，或更新扩展声明的版本约束。",
		})
		return
	}
	if len(warnings) == 0 {
		report.add(Finding{
			ID: "compatibility", Title: "版本兼容性", Status: SeverityOK,
			Message: fmt.Sprintf("当前 Discourse %s 满足声明的版本约束。", report.DiscourseVersion),
		})
	}
}

func missingFields(values map[string]string, order []string) []string {
	var missing []string
	for _, name := range order {
		if strings.TrimSpace(values[name]) == "" {
			missing = append(missing, name)
		}
	}
	return missing
}

func (m *Manager) list(ctx context.Context, ref string, kind Kind) (*Listing, error) {
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	root, err := m.root(ws, kind)
	if err != nil {
		return nil, err
	}
	listing := &Listing{
		WorkspaceID:     ws.WorkspaceID,
		Kind:            kind,
		Root:            root,
		SourceInstalled: ws.Source.Installed,
		Items:           []Item{},
	}
	listing.Checks = append(listing.Checks, Check{
		Name: "source", OK: ws.Source.Installed, Message: sourceMessage(ws),
	})

	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s directory: %w", kind, err)
	}
	listing.Checks = append(listing.Checks, Check{
		Name: "directory", OK: err == nil, Message: directoryMessage(root, err),
	})

	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		item, itemErr := readItem(kind, root, name)
		if itemErr != nil {
			m.log.Warn("could not read extension directory",
				"kind", kind, "slug", name, "error", itemErr)
			continue
		}
		seen[name] = true
		listing.Items = append(listing.Items, *item)
	}
	if kind == KindPlugin {
		disabledRoot := m.disabledPluginsRoot(ws)
		for _, item := range m.scan(kind, disabledRoot, false) {
			if seen[item.Slug] {
				m.log.Warn("plugin exists in both enabled and disabled directories",
					"slug", item.Slug, "disabled_path", item.Path)
				continue
			}
			listing.Items = append(listing.Items, item)
		}
	}
	sort.Slice(listing.Items, func(i, j int) bool {
		left, right := listing.Items[i], listing.Items[j]
		if left.Name != right.Name {
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		}
		return left.Slug < right.Slug
	})
	listing.Total = len(listing.Items)
	return listing, nil
}

func (m *Manager) create(ctx context.Context, ref string, kind Kind, req CreateRequest) (*Item, error) {
	if kind != KindPlugin && kind != KindTheme {
		return nil, errUnknownKind
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if kind == KindPlugin && !ws.Source.Installed {
		return nil, fmt.Errorf("%w: 请先在项目页克隆或初始化 Discourse 源码", ErrSourceMissing)
	}
	root, err := m.root(ws, kind)
	if err != nil {
		return nil, err
	}
	if kind == KindPlugin {
		sourceDir := filepath.Dir(root)
		info, statErr := os.Stat(sourceDir)
		if statErr != nil || !info.IsDir() {
			return nil, fmt.Errorf("%w: 源码目录 %s 不存在", ErrSourceMissing, sourceDir)
		}
	}

	name, slug, about, version, authors, err := normalizeRequest(req, kind)
	if err != nil {
		return nil, err
	}
	target := filepath.Join(root, slug)
	if _, statErr := os.Lstat(target); statErr == nil {
		return nil, fmt.Errorf("%w: %s 已存在", ErrExists, slug)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return nil, statErr
	}
	if kind == KindPlugin {
		disabled := filepath.Join(m.disabledPluginsRoot(ws), slug)
		if _, statErr := os.Lstat(disabled); statErr == nil {
			return nil, fmt.Errorf("%w: %s 已存在于禁用目录，请先启用或删除", ErrExists, slug)
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, statErr
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return nil, fmt.Errorf("create %s directory: %w", kind, err)
	}
	files, err := scaffoldFiles(kind, name, slug, about, version, authors)
	if err != nil {
		_ = os.RemoveAll(target)
		return nil, err
	}
	for relative, content := range files {
		full := filepath.Join(target, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			_ = os.RemoveAll(target)
			return nil, err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			_ = os.RemoveAll(target)
			return nil, fmt.Errorf("write %s: %w", relative, err)
		}
	}
	item, err := readItem(kind, root, slug)
	if err != nil {
		_ = os.RemoveAll(target)
		return nil, err
	}
	return item, nil
}

func (m *Manager) packageItem(ctx context.Context, ref string, kind Kind, slug string) (*Package, error) {
	if kind != KindPlugin && kind != KindTheme {
		return nil, errUnknownKind
	}
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !slugPattern.MatchString(slug) {
		return nil, fmt.Errorf("%w: 目录名不合法", ErrValidation)
	}
	ws, err := m.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	item, err := m.locateItem(ws, kind, slug)
	if err != nil {
		return nil, err
	}

	exportDir := filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "data", "exports", string(kind)+"s")
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return nil, fmt.Errorf("create export directory: %w", err)
	}
	version := packageVersion(item.Version)
	fileName := slug + "-" + version + ".zip"
	target := filepath.Join(exportDir, fileName)

	temp, err := os.CreateTemp(exportDir, "."+slug+"-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create package file: %w", err)
	}
	tempPath := temp.Name()
	discard := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	fileCount, err := writePackage(temp, item.Path)
	if err != nil {
		discard()
		return nil, fmt.Errorf("create package: %w", err)
	}
	if err := temp.Sync(); err != nil {
		discard()
		return nil, fmt.Errorf("sync package: %w", err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("close package: %w", err)
	}

	sum, err := fileSHA256(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}
	if _, statErr := os.Stat(target); statErr == nil {
		if err := os.Remove(target); err != nil {
			_ = os.Remove(tempPath)
			return nil, fmt.Errorf("replace package: %w", err)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		_ = os.Remove(tempPath)
		return nil, statErr
	}
	if err := os.Rename(tempPath, target); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("publish package: %w", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return nil, fmt.Errorf("stat package: %w", err)
	}
	result := &Package{
		Kind:      kind,
		Slug:      slug,
		Name:      item.Name,
		Version:   version,
		FileName:  fileName,
		Path:      target,
		SizeBytes: info.Size(),
		SHA256:    sum,
		FileCount: fileCount,
		CreatedAt: time.Now().UTC(),
	}
	m.log.Info("packaged extension",
		"kind", kind, "slug", slug, "files", fileCount, "path", target)
	return result, nil
}

// writePackage writes a ZIP archive of dir to w. Git metadata, installed
// dependencies and symlinks are excluded so the archive is portable.
func writePackage(w io.Writer, dir string) (int, error) {
	zw := zip.NewWriter(w)
	var files int
	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules":
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if !filepath.IsLocal(relative) {
			return fmt.Errorf("package path escapes root: %s", relative)
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Method = zip.Deflate
		dest, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(dest, source)
		closeErr := source.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		files++
		return nil
	})
	if walkErr != nil {
		_ = zw.Close()
		return files, walkErr
	}
	return files, zw.Close()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("hash package: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash package: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func packageVersion(value string) string {
	var builder strings.Builder
	dash := false
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			builder.WriteRune(r)
			dash = false
		default:
			if !dash && builder.Len() > 0 {
				builder.WriteByte('-')
				dash = true
			}
		}
	}
	version := strings.Trim(builder.String(), "-._")
	if len(version) > 32 {
		version = strings.TrimRight(version[:32], "-._")
	}
	if version == "" {
		return "0.0.0"
	}
	return version
}

func (m *Manager) disabledPluginsRoot(ws *workspace.Workspace) string {
	return filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), "data", "disabled-plugins")
}

func (m *Manager) locateItem(ws *workspace.Workspace, kind Kind, slug string) (*Item, error) {
	root, err := m.root(ws, kind)
	if err != nil {
		return nil, err
	}
	item, err := readItem(kind, root, slug)
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if kind == KindPlugin {
		item, err = readItem(kind, m.disabledPluginsRoot(ws), slug)
		if err == nil {
			item.Enabled = false
			return item, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: %s 不存在", ErrNotFound, slug)
}

func (m *Manager) scan(kind Kind, root string, enabled bool) []Item {
	entries, err := os.ReadDir(root)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			m.log.Warn("could not read extension directory",
				"kind", kind, "root", root, "error", err)
		}
		return nil
	}
	items := make([]Item, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}
		item, itemErr := readItem(kind, root, name)
		if itemErr != nil {
			m.log.Warn("could not read extension directory",
				"kind", kind, "slug", name, "error", itemErr)
			continue
		}
		item.Enabled = enabled
		items = append(items, *item)
	}
	return items
}

func ensureProjectStopped(ws *workspace.Workspace) error {
	switch ws.State {
	case workspace.StateInitializing, workspace.StateStarting,
		workspace.StateRunning, workspace.StateStopping:
		return fmt.Errorf("%w: 请先停止当前 Workspace 的项目再启用或禁用插件", ErrProjectRunning)
	}
	switch ws.Project.ProcessState {
	case "STARTING", "RUNNING", "STOPPING":
		return fmt.Errorf("%w: 请先停止当前 Workspace 的项目再启用或禁用插件", ErrProjectRunning)
	}
	return nil
}

func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (m *Manager) root(ws *workspace.Workspace, kind Kind) (string, error) {
	workspaceDir := m.paths.WorkspaceDir(ws.WorkspaceID)
	switch kind {
	case KindPlugin:
		sourceRoot, err := m.sourceRoot(ws)
		if err != nil {
			return "", err
		}
		return filepath.Join(sourceRoot, "plugins"), nil
	case KindTheme:
		return filepath.Join(workspaceDir, "data", "themes"), nil
	default:
		return "", errUnknownKind
	}
}

func (m *Manager) sourceRoot(ws *workspace.Workspace) (string, error) {
	value := ws.Source.Path
	if value == "" {
		value = "source"
	}
	if !filepath.IsLocal(value) {
		return "", fmt.Errorf("%w: 源码路径必须是 Workspace 内的相对路径", ErrValidation)
	}
	return filepath.Join(m.paths.WorkspaceDir(ws.WorkspaceID), value), nil
}

func normalizeRequest(req CreateRequest, kind Kind) (name, slug, about, version, authors string, err error) {
	name = strings.TrimSpace(req.Name)
	if err := validateName(name); err != nil {
		return "", "", "", "", "", err
	}
	slug = strings.ToLower(strings.TrimSpace(req.Slug))
	if slug == "" {
		slug = Slugify(name)
	}
	if !slugPattern.MatchString(slug) {
		return "", "", "", "", "", fmt.Errorf(
			"%w: slug 必须以字母或数字开头，只能包含小写字母、数字、点、下划线和连字符", ErrValidation)
	}
	about = strings.TrimSpace(req.About)
	if utf8.RuneCountInString(about) > 500 {
		return "", "", "", "", "", fmt.Errorf("%w: 描述最多 500 个字符", ErrValidation)
	}
	if about == "" {
		if kind == KindPlugin {
			about = "A Discourse plugin created with hxxDiscourseDevKit."
		} else {
			about = "A Discourse theme created with hxxDiscourseDevKit."
		}
	}
	version = strings.TrimSpace(req.Version)
	if version == "" {
		version = "0.1.0"
	}
	if utf8.RuneCountInString(version) > 32 {
		return "", "", "", "", "", fmt.Errorf("%w: 版本最多 32 个字符", ErrValidation)
	}
	authors = strings.TrimSpace(req.Authors)
	if authors == "" {
		authors = "DevKit"
	}
	if utf8.RuneCountInString(authors) > 128 {
		return "", "", "", "", "", fmt.Errorf("%w: 作者最多 128 个字符", ErrValidation)
	}
	for _, value := range []string{slug, about, version, authors} {
		if hasControl(value) {
			return "", "", "", "", "", fmt.Errorf("%w: 包含非法控制字符", ErrValidation)
		}
	}
	return name, slug, about, version, authors, nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: 名称不能为空", ErrValidation)
	}
	if utf8.RuneCountInString(name) > 64 {
		return fmt.Errorf("%w: 名称最多 64 个字符", ErrValidation)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: 名称不合法", ErrValidation)
	}
	if hasControl(name) {
		return fmt.Errorf("%w: 名称包含非法控制字符", ErrValidation)
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf("%w: 名称不能包含 / \\ : * ? \" < > |", ErrValidation)
	}
	return nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// Slugify converts a display name into a safe directory name.
func Slugify(value string) string {
	var builder strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			dash = false
		default:
			if !dash && builder.Len() > 0 {
				builder.WriteByte('-')
				dash = true
			}
		}
	}
	result := strings.Trim(builder.String(), "-")
	if len(result) > 64 {
		result = strings.TrimRight(result[:64], "-")
	}
	return result
}

func readItem(kind Kind, root, slug string) (*Item, error) {
	dir := filepath.Join(root, slug)
	if !filepath.IsLocal(slug) {
		return nil, fmt.Errorf("%w: invalid slug", ErrValidation)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("not a directory")
	}
	item := &Item{
		Kind:       kind,
		Slug:       slug,
		Name:       slug,
		Path:       dir,
		Enabled:    true,
		ModifiedAt: info.ModTime().UTC(),
	}
	var files int
	var newest time.Time
	_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		files++
		if fileInfo, statErr := entry.Info(); statErr == nil && fileInfo.ModTime().After(newest) {
			newest = fileInfo.ModTime()
		}
		return nil
	})
	item.FileCount = files
	if !newest.IsZero() {
		item.ModifiedAt = newest.UTC()
	}

	switch kind {
	case KindPlugin:
		meta, ok := readPluginMetadata(dir)
		item.HasMetadata = ok
		if ok {
			if meta.name != "" {
				item.Name = meta.name
			}
			item.About = meta.about
			item.Version = meta.version
			item.Authors = meta.authors
			item.URL = meta.url
		}
	case KindTheme:
		meta, ok := readThemeMetadata(dir)
		item.HasMetadata = ok
		if ok {
			if meta.Name != "" {
				item.Name = meta.Name
			}
			item.About = meta.About
			item.Version = meta.Version
			item.Authors = string(meta.Authors)
			item.URL = meta.URL
		}
	}
	return item, nil
}

func countSpecFiles(dir string) (int, error) {
	count := 0
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if strings.HasSuffix(entry.Name(), "_spec.rb") {
			count++
		}
		return nil
	})
	return count, err
}

type pluginMetadata struct {
	name            string
	about           string
	version         string
	authors         string
	url             string
	requiredVersion string
}

func readPluginMetadata(dir string) (pluginMetadata, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "plugin.rb"))
	if err != nil || len(data) > 256*1024 {
		return pluginMetadata{}, false
	}
	return parsePluginMetadata(data)
}

func parsePluginMetadata(data []byte) (pluginMetadata, bool) {
	var meta pluginMetadata
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 64*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			break
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		key, value, ok := strings.Cut(body, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case "name":
			meta.name = value
		case "about":
			meta.about = value
		case "version":
			meta.version = value
		case "authors":
			meta.authors = value
		case "url":
			meta.url = value
		case "required_version":
			meta.requiredVersion = value
		}
	}
	ok := meta.name != "" || meta.about != "" || meta.version != "" ||
		meta.authors != "" || meta.requiredVersion != ""
	return meta, ok
}

type themeMetadata struct {
	Name                    string          `json:"name"`
	About                   string          `json:"about"`
	Version                 string          `json:"version"`
	Authors                 json.RawMessage `json:"authors"`
	URL                     string          `json:"url"`
	License                 string          `json:"license"`
	ThemeVersion            string          `json:"theme_version"`
	MinimumDiscourseVersion string          `json:"minimum_discourse_version"`
	MaximumDiscourseVersion string          `json:"maximum_discourse_version"`
}

func readThemeMetadata(dir string) (themeMetadata, bool) {
	meta, err := parseThemeMetadata(dir)
	return meta, err == nil
}

func parseThemeMetadata(dir string) (themeMetadata, error) {
	const maxMetadataSize = 256 * 1024
	data, err := os.ReadFile(filepath.Join(dir, "about.json"))
	if err != nil {
		return themeMetadata{}, err
	}
	if len(data) > maxMetadataSize {
		return themeMetadata{}, fmt.Errorf("about.json 超过 256 KiB")
	}
	var raw themeMetadata
	if err := json.Unmarshal(data, &raw); err != nil {
		return themeMetadata{}, fmt.Errorf("about.json 不是有效 JSON: %w", err)
	}
	raw.Authors = json.RawMessage(parseAuthors(raw.Authors))
	return raw, nil
}

func parseAuthors(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		return single
	}
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return strings.Join(list, ", ")
	}
	return ""
}

// readDiscourseVersion reads lib/version.rb from a Discourse source tree. It
// supports the MAJOR/MINOR/TINY/PRE constants and a literal STRING/VERSION.
func readDiscourseVersion(root string) (string, bool) {
	const maxVersionFile = 64 * 1024
	path := filepath.Join(root, "lib", "version.rb")
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxVersionFile {
		return "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	text := string(data)
	if match := discourseMajorPattern.FindStringSubmatch(text); match != nil {
		parts := []string{match[1]}
		if minor := discourseMinorPattern.FindStringSubmatch(text); minor != nil {
			parts = append(parts, minor[1])
		}
		if tiny := discourseTinyPattern.FindStringSubmatch(text); tiny != nil {
			parts = append(parts, tiny[1])
		}
		version := strings.Join(parts, ".")
		if pre := discoursePrePattern.FindStringSubmatch(text); pre != nil {
			value := pre[1]
			if value == "" {
				value = pre[2]
			}
			if value != "" {
				version += "." + value
			}
		}
		return version, true
	}
	if match := discourseStringPattern.FindStringSubmatch(text); match != nil {
		value := strings.TrimSpace(match[1])
		if value != "" {
			return value, true
		}
	}
	return "", false
}

type prePart struct {
	text    string
	number  int
	numeric bool
}

type parsedVersion struct {
	major int
	minor int
	patch int
	pre   []prePart
}

func parseVersion(value string) (parsedVersion, bool) {
	value = strings.TrimSpace(value)
	match := versionPattern.FindStringSubmatch(value)
	if match == nil {
		return parsedVersion{}, false
	}
	major, err := strconv.Atoi(match[1])
	if err != nil {
		return parsedVersion{}, false
	}
	var result parsedVersion
	result.major = major
	if match[2] != "" {
		result.minor, _ = strconv.Atoi(match[2])
	}
	if match[3] != "" {
		result.patch, _ = strconv.Atoi(match[3])
	}
	if match[4] != "" {
		result.pre = splitPre(match[4])
	}
	return result, true
}

func splitPre(value string) []prePart {
	parts := make([]prePart, 0, 4)
	var buffer strings.Builder
	numeric := false
	flush := func() {
		if buffer.Len() == 0 {
			return
		}
		text := strings.ToLower(buffer.String())
		if number, err := strconv.Atoi(text); err == nil {
			parts = append(parts, prePart{number: number, numeric: true})
		} else {
			parts = append(parts, prePart{text: text})
		}
		buffer.Reset()
	}
	for _, r := range value {
		digit := r >= '0' && r <= '9'
		if buffer.Len() > 0 && digit != numeric {
			flush()
		}
		numeric = digit
		buffer.WriteRune(r)
	}
	flush()
	return parts
}

func compareVersions(left, right parsedVersion) int {
	if result := compareInt(left.major, right.major); result != 0 {
		return result
	}
	if result := compareInt(left.minor, right.minor); result != 0 {
		return result
	}
	if result := compareInt(left.patch, right.patch); result != 0 {
		return result
	}
	return comparePre(left.pre, right.pre)
}

func comparePre(left, right []prePart) int {
	if len(left) == 0 && len(right) == 0 {
		return 0
	}
	// A stable release sorts after any pre-release.
	if len(left) == 0 {
		return 1
	}
	if len(right) == 0 {
		return -1
	}
	for i := 0; i < len(left) && i < len(right); i++ {
		a, b := left[i], right[i]
		switch {
		case a.numeric && b.numeric:
			if result := compareInt(a.number, b.number); result != 0 {
				return result
			}
		case a.numeric != b.numeric:
			if a.numeric {
				return 1
			}
			return -1
		default:
			if a.text != b.text {
				if a.text < b.text {
					return -1
				}
				return 1
			}
		}
	}
	return compareInt(len(left), len(right))
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func parseLowerBound(spec string) (parsedVersion, bool) {
	spec = strings.TrimSpace(spec)
	for _, prefix := range []string{">=", "~>", "==", "="} {
		if strings.HasPrefix(spec, prefix) {
			spec = strings.TrimSpace(strings.TrimPrefix(spec, prefix))
			break
		}
	}
	return parseVersion(spec)
}

func parseUpperBound(spec string) (parsedVersion, bool) {
	spec = strings.TrimSpace(spec)
	for _, prefix := range []string{"<=", "==", "="} {
		if strings.HasPrefix(spec, prefix) {
			spec = strings.TrimSpace(strings.TrimPrefix(spec, prefix))
			break
		}
	}
	return parseVersion(spec)
}

func sourceMessage(ws *workspace.Workspace) string {
	if ws.Source.Installed {
		return "Discourse 源码已安装"
	}
	return "Discourse 源码尚未安装；插件创建需要先克隆或初始化源码"
}

func directoryMessage(root string, err error) string {
	if err == nil {
		return root
	}
	if errors.Is(err, fs.ErrNotExist) {
		return "目录尚未创建：" + root
	}
	return err.Error()
}

func scaffoldFiles(kind Kind, name, slug, about, version, authors string) (map[string]string, error) {
	switch kind {
	case KindPlugin:
		return map[string]string{
			"plugin.rb": fmt.Sprintf(`# frozen_string_literal: true

# name: %s
# about: %s
# version: %s
# authors: %s
# url: https://github.com/

after_initialize do
end
`, name, about, version, authors),
			"README.md": fmt.Sprintf(`# %s

%s

## Development

This plugin is loaded from `+"`plugins/%s`"+` in the Discourse source tree.
Restart the Rails development server after changing Ruby files.
`, name, about, slug),
			"config/locales/client.en.yml": fmt.Sprintf(`en:
  js:
    %s:
      title: "%s"
`, slug, strings.ReplaceAll(name, `"`, `\"`)),
			"assets/javascripts/discourse/initializers/" + slug + "-init.js": fmt.Sprintf(
				`import { withPluginApi } from "discourse/lib/plugin-api";

export default {
  name: "%s-init",
  initialize() {
    withPluginApi("1.0", () => {
      // Add client-side customizations here.
    });
  },
};
`, slug),
		}, nil
	case KindTheme:
		aboutJSON, err := json.MarshalIndent(map[string]any{
			"name":                      name,
			"about":                     about,
			"version":                   version,
			"authors":                   authors,
			"theme_version":             "2",
			"minimum_discourse_version": "3.0.0",
			"license":                   "MIT",
		}, "", "  ")
		if err != nil {
			return nil, err
		}
		return map[string]string{
			"about.json": string(aboutJSON) + "\n",
			"README.md": fmt.Sprintf(`# %s

%s

## Development

From this directory, with a running Discourse development instance:

`+"```bash"+`
discourse_theme watch .
`+"```"+`
`, name, about),
			"common/common.scss":   fmt.Sprintf("// Shared styles for %s.\n", name),
			"desktop/desktop.scss": fmt.Sprintf("// Desktop-only styles for %s.\n", name),
			"mobile/mobile.scss":   fmt.Sprintf("// Mobile-only styles for %s.\n", name),
		}, nil
	default:
		return nil, errUnknownKind
	}
}
