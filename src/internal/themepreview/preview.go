// Package themepreview imports built themes into the workspace's local
// Discourse instance and returns preview URLs. It only talks to the HTTP
// endpoint of the selected workspace; remote sites are intentionally out of
// scope so the administrator API key never leaves the local development host.
package themepreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/store"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

// APIKeySecret is the secret-store entry used by the Settings UI. The CLI may
// pass a key per request instead of persisting it.
const APIKeySecret = "discourse.theme_api_key"

const (
	previewStateSchemaVersion = 1
	maxResponseBytes          = 1 << 20
)

// Domain errors mapped to API responses.
var (
	ErrKeyRequired  = errors.New("theme preview API key is required")
	ErrNotRunning   = errors.New("workspace project is not running")
	ErrUnreachable  = errors.New("Discourse site is unreachable")
	ErrImportFailed = errors.New("theme import failed")
)

// WorkspaceResolver resolves a workspace by ID or name.
type WorkspaceResolver interface {
	Get(ctx context.Context, ref string) (*workspace.Workspace, error)
}

// ThemeBuilder builds a validated theme package.
type ThemeBuilder interface {
	BuildTheme(ctx context.Context, ref, slug string) (*extension.BuildResult, error)
}

// Result describes a preview attempt. Build is always populated; the preview
// URL fields are only populated after a successful import.
type Result struct {
	Build      *extension.BuildResult `json:"build"`
	SiteURL    string                 `json:"site_url,omitempty"`
	PreviewURL string                 `json:"preview_url,omitempty"`
	AdminURL   string                 `json:"admin_url,omitempty"`
	ThemeID    int                    `json:"theme_id,omitempty"`
	Action     string                 `json:"action,omitempty"`
}

// Previewer builds and imports themes for local Workspaces.
type Previewer struct {
	paths      *config.Paths
	workspaces WorkspaceResolver
	themes     ThemeBuilder
	client     *http.Client
	log        *slog.Logger
	mu         sync.Mutex
}

// New creates a previewer.
func New(paths *config.Paths, workspaces WorkspaceResolver, themes ThemeBuilder,
	logger *slog.Logger) *Previewer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Previewer{
		paths:      paths,
		workspaces: workspaces,
		themes:     themes,
		client:     &http.Client{Timeout: 60 * time.Second},
		log:        logger,
	}
}

// Preview builds the theme and imports it into the running local Discourse
// instance. A theme that fails its build check returns Result with
// Build.Ready == false and no import attempt.
func (p *Previewer) Preview(ctx context.Context, ref, slug, apiKey string) (*Result, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrKeyRequired
	}
	ws, err := p.workspaces.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	if !projectRunning(ws) {
		return nil, ErrNotRunning
	}
	site, err := localSiteURL(ws)
	if err != nil {
		return nil, err
	}
	if err := p.probe(ctx, site); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	build, err := p.themes.BuildTheme(ctx, ref, slug)
	if err != nil {
		return nil, err
	}
	result := &Result{Build: build, SiteURL: site}
	if build == nil || !build.Ready || build.Package == nil {
		return result, nil
	}

	themeID, action, err := p.importTheme(ctx, ws, site, apiKey, build.Package)
	if err != nil {
		return result, err
	}
	result.ThemeID = themeID
	result.Action = action
	result.PreviewURL = strings.TrimRight(site, "/") + "/?preview_theme_id=" + strconv.Itoa(themeID)
	result.AdminURL = strings.TrimRight(site, "/") + "/admin/customize/themes/" + strconv.Itoa(themeID)
	p.log.Info("theme preview imported",
		"workspace", ws.WorkspaceID, "slug", build.Package.Slug, "theme_id", themeID, "action", action)
	return result, nil
}

func projectRunning(ws *workspace.Workspace) bool {
	return ws.State == workspace.StateRunning || ws.Project.ProcessState == "RUNNING"
}

func localSiteURL(ws *workspace.Workspace) (string, error) {
	host := strings.TrimSpace(ws.Network.Host)
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	if ws.Network.HTTPPort < 1 || ws.Network.HTTPPort > 65535 {
		return "", fmt.Errorf("%w: Workspace 没有有效的 HTTP 端口", ErrNotRunning)
	}
	value := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(ws.Network.HTTPPort))}
	return value.String(), nil
}

// probe verifies that the worktree's Discourse server is actually reachable,
// not just recorded as running after a crash.
func (p *Previewer) probe(ctx context.Context, site string) error {
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, site+"/about.json", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /about.json 返回 HTTP %d", resp.StatusCode)
	}
	return nil
}

type previewEntry struct {
	ThemeID     int       `json:"theme_id"`
	Name        string    `json:"name,omitempty"`
	PreviewedAt time.Time `json:"previewed_at"`
}

type previewState struct {
	SchemaVersion int                     `json:"schema_version"`
	Themes        map[string]previewEntry `json:"themes"`
}

func (p *Previewer) importTheme(ctx context.Context, ws *workspace.Workspace, site, apiKey string,
	pkg *extension.Package) (int, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	state := p.loadState(ws)
	entry := state.Themes[pkg.Slug]
	action := "created"
	if entry.ThemeID > 0 {
		action = "updated"
	}
	themeID, err := p.upload(ctx, site, apiKey, pkg, entry.ThemeID)
	if err != nil {
		return 0, "", err
	}
	state.Themes[pkg.Slug] = previewEntry{
		ThemeID:     themeID,
		Name:        pkg.Name,
		PreviewedAt: time.Now().UTC(),
	}
	if err := p.saveState(ws, state); err != nil {
		// The import already succeeded; failing the request would hide a
		// working preview. Keep going and let the next run create a new theme.
		p.log.Warn("could not persist theme preview mapping",
			"workspace", ws.WorkspaceID, "slug", pkg.Slug, "error", err)
	}
	return themeID, action, nil
}

func (p *Previewer) upload(ctx context.Context, site, apiKey string, pkg *extension.Package,
	themeID int) (int, error) {
	upload, err := os.CreateTemp("", "hxxddk-theme-upload-*.multipart")
	if err != nil {
		return 0, fmt.Errorf("%w: 无法创建上传临时文件：%v", ErrImportFailed, err)
	}
	uploadPath := upload.Name()
	defer func() {
		_ = upload.Close()
		_ = os.Remove(uploadPath)
	}()

	contentType, err := writeMultipart(upload, pkg.Path, pkg.FileName, themeID)
	if err != nil {
		return 0, fmt.Errorf("%w: 无法生成上传请求：%v", ErrImportFailed, err)
	}
	info, err := upload.Stat()
	if err != nil {
		return 0, fmt.Errorf("%w: 无法读取上传临时文件：%v", ErrImportFailed, err)
	}
	if _, err := upload.Seek(0, io.SeekStart); err != nil {
		return 0, fmt.Errorf("%w: 无法读取上传临时文件：%v", ErrImportFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(site, "/")+"/admin/themes/import.json", upload)
	if err != nil {
		return 0, err
	}
	req.ContentLength = info.Size()
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Api-Key", apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrImportFailed, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return 0, fmt.Errorf("%w: 读取 Discourse 响应失败：%v", ErrImportFailed, err)
	}
	if len(body) > maxResponseBytes {
		return 0, fmt.Errorf("%w: Discourse 响应超过 1 MiB", ErrImportFailed)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("%w: %s", ErrImportFailed, discourseError(resp, body))
	}

	var payload struct {
		Theme struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"theme"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return 0, fmt.Errorf("%w: Discourse 返回了无法解析的响应", ErrImportFailed)
	}
	if payload.Theme.ID <= 0 {
		return 0, fmt.Errorf("%w: Discourse 响应缺少主题 ID", ErrImportFailed)
	}
	return payload.Theme.ID, nil
}

func writeMultipart(w io.Writer, zipPath, fileName string, themeID int) (string, error) {
	file, err := os.Open(zipPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	mw := multipart.NewWriter(w)
	if themeID > 0 {
		if err := mw.WriteField("theme_id", strconv.Itoa(themeID)); err != nil {
			return "", err
		}
	}
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{
		"name": "theme", "filename": fileName,
	}))
	h.Set("Content-Type", "application/zip")
	part, err := mw.CreatePart(h)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}
	contentType := mw.FormDataContentType()
	if err := mw.Close(); err != nil {
		return "", err
	}
	return contentType, nil
}

func discourseError(resp *http.Response, body []byte) string {
	if resp.StatusCode == http.StatusForbidden {
		return "Discourse 拒绝导入（403）：API Key 需要属于管理员、类型为 Single User 并勾选 Global Key"
	}
	var payload struct {
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &payload) == nil && len(payload.Errors) > 0 {
		return strings.Join(payload.Errors, "；")
	}
	text := strings.TrimSpace(string(body))
	if len(text) > 300 {
		text = text[:300] + "…"
	}
	if text == "" {
		text = resp.Status
	}
	return text
}

func (p *Previewer) statePath(ws *workspace.Workspace) string {
	return filepath.Join(p.paths.WorkspaceDir(ws.WorkspaceID), "data", "theme-previews.json")
}

func (p *Previewer) loadState(ws *workspace.Workspace) previewState {
	state := previewState{SchemaVersion: previewStateSchemaVersion, Themes: map[string]previewEntry{}}
	err := store.ReadJSON(p.statePath(ws), &state)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return state
	case err != nil:
		p.log.Warn("could not read theme preview mapping",
			"workspace", ws.WorkspaceID, "error", err)
		return previewState{SchemaVersion: previewStateSchemaVersion, Themes: map[string]previewEntry{}}
	case state.SchemaVersion != previewStateSchemaVersion || state.Themes == nil:
		p.log.Warn("ignoring unsupported theme preview mapping",
			"workspace", ws.WorkspaceID, "schema_version", state.SchemaVersion)
		return previewState{SchemaVersion: previewStateSchemaVersion, Themes: map[string]previewEntry{}}
	default:
		return state
	}
}

func (p *Previewer) saveState(ws *workspace.Workspace, state previewState) error {
	state.SchemaVersion = previewStateSchemaVersion
	if state.Themes == nil {
		state.Themes = map[string]previewEntry{}
	}
	return store.WriteJSON(p.statePath(ws), state, 0o644)
}
