package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
)

// ManifestFileName is the file a third-party profile source must provide at its
// root. The manifest is data only: DevKit never executes scripts from it.
const ManifestFileName = "devkit-profiles.json"

const (
	sourceStoreSchemaVersion = 1
	manifestSchemaVersion    = 1
	maxManifestBytes         = 1 << 20
	maxSourceRegistryBytes   = 1 << 20
	maxSources               = 20
	maxProfilesPerSource     = 50
	maxOperations            = 20
	maxPermissions           = 20
)

// Source kinds.
const (
	SourceKindLocal = "local"
	SourceKindGit   = "git"
)

// Profile source errors.
var (
	ErrSourcesDisabled   = errors.New("profile sources are not enabled")
	ErrSourceNotFound    = errors.New("profile source not found")
	ErrSourceExists      = errors.New("profile source is already registered")
	ErrSourceConflict    = errors.New("profile source conflicts with an existing profile")
	ErrSourceInvalid     = errors.New("profile source manifest is invalid")
	ErrSourceUnavailable = errors.New("profile source is unavailable")
	ErrGitUnavailable    = errors.New("git is unavailable for profile sources")
)

// GitFetcher clones one profile source repository. Implementations must not run
// code from the repository; cloning data is the only allowed operation.
type GitFetcher interface {
	Fetch(ctx context.Context, repoURL, branch, destDir string) (commit string, err error)
}

// Source is one registered third-party template repository.
type Source struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Name         string     `json:"name"`
	Location     string     `json:"location"`
	Branch       string     `json:"branch,omitempty"`
	Version      string     `json:"version,omitempty"`
	Commit       string     `json:"commit,omitempty"`
	ProfileIDs   []string   `json:"profile_ids"`
	AddedAt      time.Time  `json:"added_at"`
	LastLoadedAt *time.Time `json:"last_loaded_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

// SourceRequest registers a local directory or Git repository.
type SourceRequest struct {
	Location string `json:"location"`
	Branch   string `json:"branch,omitempty"`
}

type sourceEntry struct {
	source   Source
	profiles []Definition
}

type sourceStoreFile struct {
	SchemaVersion int      `json:"schema_version"`
	Sources       []Source `json:"sources"`
}

type manifestFile struct {
	SchemaVersion int               `json:"schema_version"`
	Name          string            `json:"name"`
	Version       string            `json:"version,omitempty"`
	Description   string            `json:"description,omitempty"`
	Profiles      []manifestProfile `json:"profiles"`
}

type manifestProfile struct {
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`
	Description          string       `json:"description"`
	Category             string       `json:"category,omitempty"`
	ProjectType          string       `json:"project_type"`
	DefaultRepositoryURL string       `json:"default_repository_url,omitempty"`
	DefaultBranch        string       `json:"default_branch,omitempty"`
	SourceRequired       *bool        `json:"source_required,omitempty"`
	ExtensionKind        string       `json:"extension_kind,omitempty"`
	RuntimeComponents    []string     `json:"runtime_components,omitempty"`
	Services             []string     `json:"services,omitempty"`
	Operations           []Operation  `json:"operations"`
	Permissions          []Permission `json:"permissions"`
}

type manifestMeta struct {
	Name       string
	Version    string
	ProfileIDs []string
}

var (
	profileIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	actionIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	projectTypePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	scpRemotePattern   = regexp.MustCompile(`^[^@\s]+@[^:\s]+:.+$`)
)

var (
	allowedRuntimeComponents = map[string]bool{"ruby": true, "node": true, "git": true}
	allowedServices          = map[string]bool{"postgresql": true, "redis": true}
	allowedExtensionKinds    = map[string]bool{"": true, "plugin": true, "theme": true}
	allowedRisks             = map[string]bool{"low": true, "medium": true, "high": true}
)

// EnableSources loads the persisted third-party source registry and exposes its
// profiles through List/Get/Check. A missing registry is not an error. A
// corrupt registry is reported and leaves source management disabled so the
// user can repair or remove the file deliberately.
func (c *Catalog) EnableSources(storePath, sourcesDir string, fetcher GitFetcher) error {
	if strings.TrimSpace(storePath) == "" || strings.TrimSpace(sourcesDir) == "" {
		return errors.New("profile source paths are required")
	}
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	if err := os.MkdirAll(sourcesDir, 0o755); err != nil {
		return fmt.Errorf("create profile source directory: %w", err)
	}
	c.sourceStore = storePath
	c.sourcesDir = sourcesDir
	c.fetcher = fetcher

	persisted, err := readSourceStore(storePath)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	entries := make([]sourceEntry, 0, len(persisted))
	for _, src := range persisted {
		src.ProfileIDs = []string{}
		if !id.Valid(src.ID) {
			src.LastError = "来源 ID 无效"
			entries = append(entries, sourceEntry{source: src})
			continue
		}
		if src.Kind != SourceKindLocal && src.Kind != SourceKindGit {
			src.LastError = fmt.Sprintf("未知来源类型 %q", src.Kind)
			entries = append(entries, sourceEntry{source: src})
			continue
		}
		dir, err := c.sourceContentDir(src)
		if err != nil {
			src.LastError = err.Error()
			entries = append(entries, sourceEntry{source: src})
			continue
		}
		profiles, meta, err := loadManifestFromDir(dir, src)
		if err != nil {
			src.LastError = err.Error()
			entries = append(entries, sourceEntry{source: src})
			continue
		}
		if err := findProfileConflict(profiles, c.builtins, entries, src.ID); err != nil {
			src.LastError = err.Error()
			entries = append(entries, sourceEntry{source: src})
			continue
		}
		src.Name = meta.Name
		src.Version = meta.Version
		src.ProfileIDs = meta.ProfileIDs
		src.LastLoadedAt = &now
		src.LastError = ""
		entries = append(entries, sourceEntry{source: src, profiles: profiles})
	}

	c.mu.Lock()
	c.entries = entries
	c.rebuildDefsLocked()
	c.mu.Unlock()
	c.sourcesEnabled.Store(true)

	sources := make([]Source, 0, len(entries))
	for _, entry := range entries {
		sources = append(sources, cloneSource(entry.source))
	}
	if err := writeSourceStore(storePath, sources); err != nil {
		return fmt.Errorf("write profile source registry: %w", err)
	}
	return nil
}

// SourcesEnabled reports whether third-party source management is available.
func (c *Catalog) SourcesEnabled() bool { return c.sourcesEnabled.Load() }

// ListSources returns a defensive copy of the registered sources.
func (c *Catalog) ListSources() []Source {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Source, 0, len(c.entries))
	for _, entry := range c.entries {
		out = append(out, cloneSource(entry.source))
	}
	return out
}

// AddSource registers a local directory or clones a Git repository, validates
// its manifest and publishes its profiles. Fetching the manifest never executes
// repository code.
func (c *Catalog) AddSource(ctx context.Context, req SourceRequest) (Source, error) {
	if !c.SourcesEnabled() {
		return Source{}, ErrSourcesDisabled
	}
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	location, branch, kind, err := normalizeSourceRequest(req)
	if err != nil {
		return Source{}, err
	}

	c.mu.RLock()
	sources := c.sourceSnapshotsLocked()
	entries := append([]sourceEntry(nil), c.entries...)
	c.mu.RUnlock()

	if len(sources) >= maxSources {
		return Source{}, fmt.Errorf("%w: 最多只能注册 %d 个模板来源", ErrSourceInvalid, maxSources)
	}
	for _, existing := range sources {
		if existing.Kind == kind && sameSourceLocation(existing.Kind, existing.Location, location) {
			return Source{}, fmt.Errorf("%w: %s", ErrSourceExists, location)
		}
	}

	now := time.Now().UTC()
	src := Source{
		ID:         id.MustNew(),
		Kind:       kind,
		Location:   location,
		Branch:     branch,
		ProfileIDs: []string{},
		AddedAt:    now,
	}

	contentDir := location
	cleanupDir := ""
	if kind == SourceKindGit {
		if c.fetcher == nil {
			return Source{}, ErrGitUnavailable
		}
		contentDir = filepath.Join(c.sourcesDir, ".tmp-"+src.ID)
		_ = os.RemoveAll(contentDir)
		cleanupDir = contentDir
		commit, err := c.fetcher.Fetch(ctx, location, branch, contentDir)
		if err != nil {
			_ = os.RemoveAll(contentDir)
			return Source{}, fmt.Errorf("%w: %v", ErrSourceUnavailable, err)
		}
		src.Commit = commit
	}

	profiles, meta, err := loadManifestFromDir(contentDir, src)
	if err != nil {
		if cleanupDir != "" {
			_ = os.RemoveAll(cleanupDir)
		}
		return Source{}, err
	}
	if err := findProfileConflict(profiles, c.builtins, entries, ""); err != nil {
		if cleanupDir != "" {
			_ = os.RemoveAll(cleanupDir)
		}
		return Source{}, err
	}

	if kind == SourceKindGit {
		finalDir := c.sourceContentDirUnchecked(src)
		if err := os.Rename(contentDir, finalDir); err != nil {
			_ = os.RemoveAll(contentDir)
			return Source{}, fmt.Errorf("%w: 保存来源检出目录: %v", ErrSourceUnavailable, err)
		}
	}

	src.Name = meta.Name
	src.Version = meta.Version
	src.ProfileIDs = meta.ProfileIDs
	src.LastLoadedAt = &now
	entry := sourceEntry{source: src, profiles: profiles}

	nextSources := append(sources, cloneSource(src))
	if err := writeSourceStore(c.sourceStore, nextSources); err != nil {
		if kind == SourceKindGit {
			_ = os.RemoveAll(c.sourceContentDirUnchecked(src))
		}
		return Source{}, fmt.Errorf("write profile source registry: %w", err)
	}

	c.mu.Lock()
	c.entries = append(entries, entry)
	c.rebuildDefsLocked()
	c.mu.Unlock()
	return cloneSource(src), nil
}

// RefreshSource reloads a local manifest or re-clones a Git source checkout.
// On failure the previously loaded profiles remain active.
func (c *Catalog) RefreshSource(ctx context.Context, sourceID string) (Source, error) {
	if !c.SourcesEnabled() {
		return Source{}, ErrSourcesDisabled
	}
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	c.mu.RLock()
	entries := append([]sourceEntry(nil), c.entries...)
	c.mu.RUnlock()

	index := -1
	for i, entry := range entries {
		if entry.source.ID == sourceID {
			index = i
			break
		}
	}
	if index < 0 {
		return Source{}, fmt.Errorf("%w: %s", ErrSourceNotFound, sourceID)
	}
	src := entries[index].source

	var profiles []Definition
	var meta manifestMeta
	if src.Kind == SourceKindGit {
		if c.fetcher == nil {
			return Source{}, ErrGitUnavailable
		}
		tmp := filepath.Join(c.sourcesDir, ".refresh-"+src.ID+"-"+id.MustNew())
		_ = os.RemoveAll(tmp)
		commit, err := c.fetcher.Fetch(ctx, src.Location, src.Branch, tmp)
		if err != nil {
			_ = os.RemoveAll(tmp)
			return Source{}, fmt.Errorf("%w: %v", ErrSourceUnavailable, err)
		}
		profiles, meta, err = loadManifestFromDir(tmp, src)
		if err != nil {
			_ = os.RemoveAll(tmp)
			return Source{}, err
		}
		if err := findProfileConflict(profiles, c.builtins, entries, src.ID); err != nil {
			_ = os.RemoveAll(tmp)
			return Source{}, err
		}
		if err := replaceSourceCheckout(c.sourceContentDirUnchecked(src), tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return Source{}, fmt.Errorf("%w: 替换来源检出目录: %v", ErrSourceUnavailable, err)
		}
		src.Commit = commit
	} else {
		var err error
		profiles, meta, err = loadManifestFromDir(src.Location, src)
		if err != nil {
			return Source{}, err
		}
		if err := findProfileConflict(profiles, c.builtins, entries, src.ID); err != nil {
			return Source{}, err
		}
	}

	now := time.Now().UTC()
	src.Name = meta.Name
	src.Version = meta.Version
	src.ProfileIDs = meta.ProfileIDs
	src.LastLoadedAt = &now
	src.LastError = ""

	nextEntries := append([]sourceEntry(nil), entries...)
	nextEntries[index] = sourceEntry{source: src, profiles: profiles}
	nextSources := make([]Source, 0, len(nextEntries))
	for _, entry := range nextEntries {
		nextSources = append(nextSources, cloneSource(entry.source))
	}
	if err := writeSourceStore(c.sourceStore, nextSources); err != nil {
		return Source{}, fmt.Errorf("write profile source registry: %w", err)
	}

	c.mu.Lock()
	c.entries = nextEntries
	c.rebuildDefsLocked()
	c.mu.Unlock()
	return cloneSource(src), nil
}

// RemoveSource unregisters a source. DevKit-owned Git checkouts are deleted;
// local directories supplied by the user are never touched.
func (c *Catalog) RemoveSource(sourceID string) error {
	if !c.SourcesEnabled() {
		return ErrSourcesDisabled
	}
	c.sourceMu.Lock()
	defer c.sourceMu.Unlock()

	c.mu.RLock()
	entries := append([]sourceEntry(nil), c.entries...)
	c.mu.RUnlock()

	index := -1
	for i, entry := range entries {
		if entry.source.ID == sourceID {
			index = i
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("%w: %s", ErrSourceNotFound, sourceID)
	}
	removed := entries[index].source

	nextEntries := append([]sourceEntry(nil), entries[:index]...)
	nextEntries = append(nextEntries, entries[index+1:]...)
	nextSources := make([]Source, 0, len(nextEntries))
	for _, entry := range nextEntries {
		nextSources = append(nextSources, cloneSource(entry.source))
	}
	if err := writeSourceStore(c.sourceStore, nextSources); err != nil {
		return fmt.Errorf("write profile source registry: %w", err)
	}

	c.mu.Lock()
	c.entries = nextEntries
	c.rebuildDefsLocked()
	c.mu.Unlock()

	if removed.Kind == SourceKindGit {
		if dir, err := c.sourceContentDir(removed); err == nil {
			_ = os.RemoveAll(dir)
		}
	}
	return nil
}

func (c *Catalog) sourceSnapshotsLocked() []Source {
	out := make([]Source, 0, len(c.entries))
	for _, entry := range c.entries {
		out = append(out, cloneSource(entry.source))
	}
	return out
}

func (c *Catalog) rebuildDefsLocked() {
	defs := cloneDefinitions(c.builtins)
	for _, entry := range c.entries {
		defs = append(defs, cloneDefinitions(entry.profiles)...)
	}
	c.defs = defs
}

func (c *Catalog) sourceContentDir(src Source) (string, error) {
	if src.Kind == SourceKindLocal {
		if !filepath.IsAbs(src.Location) {
			return "", fmt.Errorf("%w: 本地来源必须使用绝对路径", ErrSourceInvalid)
		}
		return filepath.Clean(src.Location), nil
	}
	if !id.Valid(src.ID) {
		return "", fmt.Errorf("%w: 来源 ID 无效", ErrSourceInvalid)
	}
	return c.sourceContentDirUnchecked(src), nil
}

func (c *Catalog) sourceContentDirUnchecked(src Source) string {
	return filepath.Join(c.sourcesDir, src.ID)
}

func cloneSource(src Source) Source {
	src.ProfileIDs = append([]string(nil), src.ProfileIDs...)
	return src
}

func normalizeSourceRequest(req SourceRequest) (location, branch, kind string, err error) {
	location = strings.TrimSpace(req.Location)
	branch = strings.TrimSpace(req.Branch)
	if location == "" {
		return "", "", "", fmt.Errorf("%w: 来源地址不能为空", ErrSourceInvalid)
	}
	if len(location) > 2048 {
		return "", "", "", fmt.Errorf("%w: 来源地址过长", ErrSourceInvalid)
	}
	if strings.ContainsAny(location, "\x00\r\n") {
		return "", "", "", fmt.Errorf("%w: 来源地址包含非法字符", ErrSourceInvalid)
	}

	lower := strings.ToLower(location)
	switch {
	case strings.HasPrefix(lower, "file://"):
		path, err := localPathFromFileURL(location)
		if err != nil {
			return "", "", "", err
		}
		location = path
		kind = SourceKindLocal
	case strings.Contains(location, "://") || scpRemotePattern.MatchString(location):
		if err := project.ValidateRepositoryURL(location); err != nil {
			return "", "", "", fmt.Errorf("%w: %v", ErrSourceInvalid, err)
		}
		if err := project.ValidateBranch(branch); err != nil {
			return "", "", "", fmt.Errorf("%w: %v", ErrSourceInvalid, err)
		}
		kind = SourceKindGit
	default:
		kind = SourceKindLocal
	}

	if kind == SourceKindLocal {
		if branch != "" {
			return "", "", "", fmt.Errorf("%w: 本地目录来源不接受分支参数", ErrSourceInvalid)
		}
		abs, err := filepath.Abs(location)
		if err != nil {
			return "", "", "", fmt.Errorf("%w: 解析本地路径: %v", ErrSourceInvalid, err)
		}
		location = filepath.Clean(abs)
		info, err := os.Stat(location)
		if err != nil {
			return "", "", "", fmt.Errorf("%w: 无法读取本地来源 %s: %v", ErrSourceInvalid, location, err)
		}
		if !info.IsDir() {
			return "", "", "", fmt.Errorf("%w: 本地来源必须是目录", ErrSourceInvalid)
		}
	}
	return location, branch, kind, nil
}

func localPathFromFileURL(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("%w: file URL 无效", ErrSourceInvalid)
	}
	if parsed.Host != "" && !strings.EqualFold(parsed.Host, "localhost") {
		return "", fmt.Errorf("%w: 不支持带主机的 file URL", ErrSourceInvalid)
	}
	path := parsed.Path
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%w: file URL 缺少路径", ErrSourceInvalid)
	}
	return filepath.FromSlash(path), nil
}

func sameSourceLocation(kind, left, right string) bool {
	if kind == SourceKindLocal && runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return left == right
}

func readSourceStore(path string) ([]Source, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read profile source registry: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("read profile source registry: %s 不是普通文件", path)
	}
	if info.Size() > maxSourceRegistryBytes {
		return nil, fmt.Errorf("read profile source registry: 文件超过 %d 字节", maxSourceRegistryBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read profile source registry: %w", err)
	}
	var file sourceStoreFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("read profile source registry: %w", err)
	}
	if file.SchemaVersion != sourceStoreSchemaVersion {
		return nil, fmt.Errorf("read profile source registry: 不支持的 schema_version %d", file.SchemaVersion)
	}
	if len(file.Sources) > maxSources {
		return nil, fmt.Errorf("read profile source registry: 来源数量超过 %d", maxSources)
	}
	seen := make(map[string]bool, len(file.Sources))
	for i := range file.Sources {
		src := &file.Sources[i]
		if seen[src.ID] {
			return nil, fmt.Errorf("read profile source registry: 重复的来源 ID %q", src.ID)
		}
		seen[src.ID] = true
	}
	return file.Sources, nil
}

func writeSourceStore(path string, sources []Source) error {
	return store.WriteJSON(path, sourceStoreFile{
		SchemaVersion: sourceStoreSchemaVersion,
		Sources:       sources,
	}, 0o600)
}

func loadManifestFromDir(dir string, src Source) ([]Definition, manifestMeta, error) {
	manifest, err := readManifestFile(dir)
	if err != nil {
		return nil, manifestMeta{}, err
	}
	return buildDefinitions(manifest, src)
}

func readManifestFile(dir string) (manifestFile, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return manifestFile{}, fmt.Errorf("%w: 无法读取来源目录 %s: %v", ErrSourceUnavailable, dir, err)
	}
	if !info.IsDir() {
		return manifestFile{}, fmt.Errorf("%w: %s 不是目录", ErrSourceInvalid, dir)
	}
	path := filepath.Join(dir, ManifestFileName)
	info, err = os.Stat(path)
	if err != nil {
		return manifestFile{}, fmt.Errorf("%w: 缺少 %s: %v", ErrSourceInvalid, ManifestFileName, err)
	}
	if !info.Mode().IsRegular() {
		return manifestFile{}, fmt.Errorf("%w: %s 不是普通文件", ErrSourceInvalid, ManifestFileName)
	}
	if info.Size() > maxManifestBytes {
		return manifestFile{}, fmt.Errorf("%w: %s 超过 %d 字节", ErrSourceInvalid, ManifestFileName, maxManifestBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return manifestFile{}, fmt.Errorf("%w: 打开 %s: %v", ErrSourceInvalid, ManifestFileName, err)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return manifestFile{}, fmt.Errorf("%w: 读取 %s: %v", ErrSourceInvalid, ManifestFileName, err)
	}
	if len(data) > maxManifestBytes {
		return manifestFile{}, fmt.Errorf("%w: %s 超过 %d 字节", ErrSourceInvalid, ManifestFileName, maxManifestBytes)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var manifest manifestFile
	if err := dec.Decode(&manifest); err != nil {
		return manifestFile{}, fmt.Errorf("%w: 解析 %s: %v", ErrSourceInvalid, ManifestFileName, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifestFile{}, fmt.Errorf("%w: %s 只能包含一个 JSON 对象", ErrSourceInvalid, ManifestFileName)
	}
	return manifest, nil
}

func buildDefinitions(manifest manifestFile, src Source) ([]Definition, manifestMeta, error) {
	if manifest.SchemaVersion != manifestSchemaVersion {
		return nil, manifestMeta{}, fmt.Errorf("%w: 不支持的 schema_version %d", ErrSourceInvalid, manifest.SchemaVersion)
	}
	name, err := cleanText(manifest.Name, "模板来源名称", 100, true)
	if err != nil {
		return nil, manifestMeta{}, err
	}
	version, err := cleanText(manifest.Version, "模板来源版本", 64, false)
	if err != nil {
		return nil, manifestMeta{}, err
	}
	if _, err := cleanText(manifest.Description, "模板来源说明", 500, false); err != nil {
		return nil, manifestMeta{}, err
	}
	if len(manifest.Profiles) == 0 || len(manifest.Profiles) > maxProfilesPerSource {
		return nil, manifestMeta{}, fmt.Errorf("%w: profiles 数量必须在 1–%d 之间", ErrSourceInvalid, maxProfilesPerSource)
	}

	origin := Origin{
		Type:     "third-party",
		Name:     name,
		URL:      src.Location,
		Version:  version,
		SourceID: src.ID,
		Verified: false,
	}

	seen := make(map[string]bool, len(manifest.Profiles))
	defs := make([]Definition, 0, len(manifest.Profiles))
	profileIDs := make([]string, 0, len(manifest.Profiles))
	for index, item := range manifest.Profiles {
		def, err := buildDefinition(item, origin)
		if err != nil {
			if errors.Is(err, ErrSourceInvalid) {
				return nil, manifestMeta{}, fmt.Errorf("profiles[%d]: %w", index, err)
			}
			return nil, manifestMeta{}, fmt.Errorf("%w: profiles[%d]: %v", ErrSourceInvalid, index, err)
		}
		if seen[def.ID] {
			return nil, manifestMeta{}, fmt.Errorf("%w: 重复的 Profile ID %q", ErrSourceInvalid, def.ID)
		}
		seen[def.ID] = true
		defs = append(defs, def)
		profileIDs = append(profileIDs, def.ID)
	}
	return defs, manifestMeta{Name: name, Version: version, ProfileIDs: profileIDs}, nil
}

func buildDefinition(item manifestProfile, origin Origin) (Definition, error) {
	id := strings.ToLower(strings.TrimSpace(item.ID))
	if !profileIDPattern.MatchString(id) {
		return Definition{}, fmt.Errorf("Profile ID %q 无效（小写字母、数字与连字符，最多 64 字符）", item.ID)
	}
	name, err := cleanText(item.Name, "名称", 100, true)
	if err != nil {
		return Definition{}, err
	}
	description, err := cleanText(item.Description, "说明", 1000, true)
	if err != nil {
		return Definition{}, err
	}
	category := strings.TrimSpace(item.Category)
	if category == "" {
		category = "third-party"
	}
	category, err = cleanText(category, "分类", 64, true)
	if err != nil {
		return Definition{}, err
	}
	projectType := strings.TrimSpace(item.ProjectType)
	if !projectTypePattern.MatchString(projectType) {
		return Definition{}, fmt.Errorf("project_type %q 无效", item.ProjectType)
	}
	repositoryURL := strings.TrimSpace(item.DefaultRepositoryURL)
	if err := project.ValidateRepositoryURL(repositoryURL); err != nil {
		return Definition{}, fmt.Errorf("%w: %v", ErrSourceInvalid, err)
	}
	branch := strings.TrimSpace(item.DefaultBranch)
	if repositoryURL == "" && branch != "" {
		return Definition{}, fmt.Errorf("%w: default_branch 需要同时提供 default_repository_url", ErrSourceInvalid)
	}
	if err := project.ValidateBranch(branch); err != nil {
		return Definition{}, fmt.Errorf("%w: %v", ErrSourceInvalid, err)
	}

	sourceRequired := true
	if item.SourceRequired != nil {
		sourceRequired = *item.SourceRequired
	}

	extensionKind := strings.ToLower(strings.TrimSpace(item.ExtensionKind))
	if !allowedExtensionKinds[extensionKind] {
		return Definition{}, fmt.Errorf("extension_kind %q 无效", item.ExtensionKind)
	}

	components, err := normalizeComponents(item.RuntimeComponents, "runtime_components",
		[]string{"ruby", "node", "git"}, allowedRuntimeComponents, 10)
	if err != nil {
		return Definition{}, err
	}
	services, err := normalizeComponents(item.Services, "services",
		[]string{"postgresql", "redis"}, allowedServices, 10)
	if err != nil {
		return Definition{}, err
	}
	operations, err := normalizeOperations(item.Operations)
	if err != nil {
		return Definition{}, err
	}
	permissions, err := normalizePermissions(item.Permissions)
	if err != nil {
		return Definition{}, err
	}

	return Definition{
		ID:                   id,
		Name:                 name,
		Description:          description,
		Category:             category,
		ProjectType:          projectType,
		DefaultRepositoryURL: repositoryURL,
		DefaultBranch:        branch,
		SourceRequired:       sourceRequired,
		ExtensionKind:        extensionKind,
		RuntimeComponents:    components,
		Services:             services,
		Operations:           operations,
		Permissions:          permissions,
		Origin:               origin,
	}, nil
}

func normalizeComponents(values []string, field string, defaults []string, allowed map[string]bool, max int) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), defaults...), nil
	}
	if len(values) > max {
		return nil, fmt.Errorf("%w: %s 最多 %d 项", ErrSourceInvalid, field, max)
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if !allowed[value] {
			return nil, fmt.Errorf("%w: %s 包含不支持的值 %q", ErrSourceInvalid, field, value)
		}
		if seen[value] {
			return nil, fmt.Errorf("%w: %s 包含重复值 %q", ErrSourceInvalid, field, value)
		}
		seen[value] = true
		out = append(out, value)
	}
	return out, nil
}

func normalizeOperations(values []Operation) ([]Operation, error) {
	if len(values) == 0 || len(values) > maxOperations {
		return nil, fmt.Errorf("%w: operations 数量必须在 1–%d 之间", ErrSourceInvalid, maxOperations)
	}
	seen := make(map[string]bool, len(values))
	out := make([]Operation, 0, len(values))
	for index, item := range values {
		id := strings.TrimSpace(item.ID)
		if !actionIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: operations[%d].id %q 无效", ErrSourceInvalid, index, item.ID)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: 重复的 operation ID %q", ErrSourceInvalid, id)
		}
		seen[id] = true
		title, err := cleanText(item.Title, "operation.title", 100, true)
		if err != nil {
			return nil, err
		}
		description, err := cleanText(item.Description, "operation.description", 500, true)
		if err != nil {
			return nil, err
		}
		out = append(out, Operation{ID: id, Title: title, Description: description, Destructive: item.Destructive})
	}
	return out, nil
}

func normalizePermissions(values []Permission) ([]Permission, error) {
	if len(values) == 0 || len(values) > maxPermissions {
		return nil, fmt.Errorf("%w: permissions 数量必须在 1–%d 之间", ErrSourceInvalid, maxPermissions)
	}
	seen := make(map[string]bool, len(values))
	out := make([]Permission, 0, len(values))
	for index, item := range values {
		id := strings.TrimSpace(item.ID)
		if !actionIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%w: permissions[%d].id %q 无效", ErrSourceInvalid, index, item.ID)
		}
		if seen[id] {
			return nil, fmt.Errorf("%w: 重复的 permission ID %q", ErrSourceInvalid, id)
		}
		seen[id] = true
		title, err := cleanText(item.Title, "permission.title", 100, true)
		if err != nil {
			return nil, err
		}
		description, err := cleanText(item.Description, "permission.description", 500, true)
		if err != nil {
			return nil, err
		}
		risk := strings.ToLower(strings.TrimSpace(item.Risk))
		if !allowedRisks[risk] {
			return nil, fmt.Errorf("%w: permission.risk %q 必须是 low、medium 或 high", ErrSourceInvalid, item.Risk)
		}
		out = append(out, Permission{ID: id, Title: title, Description: description, Risk: risk})
	}
	return out, nil
}

func cleanText(value, field string, max int, required bool) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return "", fmt.Errorf("%w: %s 不能为空", ErrSourceInvalid, field)
		}
		return "", nil
	}
	if utf8.RuneCountInString(value) > max {
		return "", fmt.Errorf("%w: %s 超过 %d 个字符", ErrSourceInvalid, field, max)
	}
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: %s 包含控制字符", ErrSourceInvalid, field)
		}
	}
	return value, nil
}

func findProfileConflict(defs []Definition, builtins []Definition, entries []sourceEntry, excludeSourceID string) error {
	owners := make(map[string]string, len(builtins))
	for _, def := range builtins {
		owners[def.ID] = "内置模板"
	}
	for _, entry := range entries {
		if entry.source.ID == excludeSourceID {
			continue
		}
		owner := entry.source.Name
		if owner == "" {
			owner = entry.source.ID
		}
		for _, def := range entry.profiles {
			owners[def.ID] = "来源 " + owner
		}
	}
	for _, def := range defs {
		if owner, exists := owners[def.ID]; exists {
			return fmt.Errorf("%w: Profile ID %q 已被 %s 使用", ErrSourceConflict, def.ID, owner)
		}
	}
	return nil
}

func replaceSourceCheckout(finalDir, newDir string) error {
	backup := finalDir + ".old-" + id.MustNew()
	movedOld := false
	if _, err := os.Stat(finalDir); err == nil {
		if err := os.Rename(finalDir, backup); err != nil {
			return err
		}
		movedOld = true
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(newDir, finalDir); err != nil {
		if movedOld {
			_ = os.Rename(backup, finalDir)
		}
		return err
	}
	if movedOld {
		_ = os.RemoveAll(backup)
	}
	return nil
}
