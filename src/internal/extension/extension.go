// Package extension manages Discourse plugin and theme scaffolds that belong
// to a workspace.
package extension

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	ErrValidation    = errors.New("extension validation failed")
	ErrExists        = errors.New("extension already exists")
	ErrSourceMissing = errors.New("Discourse source is not installed")
	errUnknownKind   = errors.New("unknown extension kind")
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

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
		listing.Items = append(listing.Items, *item)
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

func (m *Manager) root(ws *workspace.Workspace, kind Kind) (string, error) {
	workspaceDir := m.paths.WorkspaceDir(ws.WorkspaceID)
	switch kind {
	case KindPlugin:
		value := ws.Source.Path
		if value == "" {
			value = "source"
		}
		if !filepath.IsLocal(value) {
			return "", fmt.Errorf("%w: 源码路径必须是 Workspace 内的相对路径", ErrValidation)
		}
		return filepath.Join(workspaceDir, value, "plugins"), nil
	case KindTheme:
		return filepath.Join(workspaceDir, "data", "themes"), nil
	default:
		return "", errUnknownKind
	}
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

type pluginMetadata struct {
	name    string
	about   string
	version string
	authors string
	url     string
}

func readPluginMetadata(dir string) (pluginMetadata, bool) {
	file, err := os.Open(filepath.Join(dir, "plugin.rb"))
	if err != nil {
		return pluginMetadata{}, false
	}
	defer func() { _ = file.Close() }()

	var meta pluginMetadata
	scanner := bufio.NewScanner(file)
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
		}
	}
	ok := meta.name != "" || meta.about != "" || meta.version != "" || meta.authors != ""
	return meta, ok
}

type themeMetadata struct {
	Name    string          `json:"name"`
	About   string          `json:"about"`
	Version string          `json:"version"`
	Authors json.RawMessage `json:"authors"`
	URL     string          `json:"url"`
}

func readThemeMetadata(dir string) (themeMetadata, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "about.json"))
	if err != nil || len(data) > 256*1024 {
		return themeMetadata{}, false
	}
	var raw themeMetadata
	if err := json.Unmarshal(data, &raw); err != nil {
		return themeMetadata{}, false
	}
	raw.Authors = json.RawMessage(parseAuthors(raw.Authors))
	return raw, true
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
