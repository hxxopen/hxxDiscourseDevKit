package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/process"
)

var devkitLogName = regexp.MustCompile(`^devkit-\d{8}(-\d{6}\.\d{3})?\.jsonl$`)

// logSource describes one discoverable log file. IDs are opaque to clients and
// are validated by exact match against the server-built source list, so clients
// can never request an arbitrary path.
type logSource struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Category    string     `json:"category"`
	Kind        string     `json:"kind"`
	WorkspaceID string     `json:"workspace_id,omitempty"`
	Path        string     `json:"path"`
	Exists      bool       `json:"exists"`
	SizeBytes   int64      `json:"size_bytes"`
	ModifiedAt  *time.Time `json:"modified_at,omitempty"`
}

type logSourcesResponse struct {
	Items []logSource `json:"items"`
	Total int         `json:"total"`
}

type logContentResponse struct {
	Source     string     `json:"source"`
	Path       string     `json:"path"`
	Lines      []string   `json:"lines"`
	Total      int        `json:"total"`
	SizeBytes  int64      `json:"size_bytes"`
	ModifiedAt *time.Time `json:"modified_at,omitempty"`
}

func (s *Server) handleListLogSources(w http.ResponseWriter, r *http.Request) {
	items := s.logSources(r)
	httpx.WriteJSON(w, http.StatusOK, logSourcesResponse{Items: items, Total: len(items)})
}

func (s *Server) handleReadLog(w http.ResponseWriter, r *http.Request) {
	sourceID := strings.TrimSpace(r.URL.Query().Get("source"))
	if sourceID == "" {
		httpx.WriteError(w, r, httpx.BadRequest("VALIDATION_FAILED", "source 参数不能为空"), s.log)
		return
	}
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 2000 {
			httpx.WriteError(w, r, httpx.BadRequest("VALIDATION_FAILED", "tail 必须是 1–2000 之间的整数"), s.log)
			return
		}
		tail = parsed
	}

	var match *logSource
	for _, item := range s.logSources(r) {
		if item.ID == sourceID {
			found := item
			match = &found
			break
		}
	}
	if match == nil {
		httpx.WriteError(w, r, httpx.NotFound("LOG_NOT_FOUND", "日志来源不存在"), s.log)
		return
	}

	lines, err := process.TailFile(match.Path, tail)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, logContentResponse{
		Source:     match.ID,
		Path:       match.Path,
		Lines:      lines,
		Total:      len(lines),
		SizeBytes:  match.SizeBytes,
		ModifiedAt: match.ModifiedAt,
	})
}

// logSources builds the complete server-side allowlist for log reads.
func (s *Server) logSources(r *http.Request) []logSource {
	var items []logSource

	items = append(items, s.devkitLogSources()...)
	items = append(items,
		newLogSource("startup", "守护进程启动日志", "devkit", "startup", "",
			filepath.Join(s.opts.Paths.LogsDir, "devkitd-startup.log")),
		newLogSource("tasks", "任务历史", "devkit", "tasks", "",
			filepath.Join(s.opts.Paths.LogsDir, "tasks.jsonl")),
	)

	if s.opts.Services != nil {
		for _, service := range []struct {
			name  string
			label string
		}{
			{"postgresql", "PostgreSQL 服务日志"},
			{"redis", "Redis 服务日志"},
		} {
			items = append(items, newLogSource(
				"service:"+service.name, service.label, "service", service.name, "",
				filepath.Join(s.opts.Paths.ServiceDir(service.name), service.name+".log"),
			))
		}
	}

	if s.opts.Workspaces != nil {
		if list, err := s.opts.Workspaces.List(r.Context()); err == nil {
			sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
			for _, ws := range list {
				dir := filepath.Join(s.opts.Paths.WorkspaceDir(ws.WorkspaceID), "logs")
				for _, kind := range []struct {
					name  string
					label string
				}{
					{"project", "Rails 进程日志"},
					{"init", "初始化日志"},
					{"git", "Git 操作日志"},
				} {
					items = append(items, newLogSource(
						fmt.Sprintf("workspace:%s:%s", ws.WorkspaceID, kind.name),
						fmt.Sprintf("%s · %s", ws.Name, kind.label),
						"workspace", kind.name, ws.WorkspaceID,
						filepath.Join(dir, kind.name+".log"),
					))
				}
			}
		}
	}
	return items
}

func (s *Server) devkitLogSources() []logSource {
	dir := s.opts.Paths.LogsDir
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	latest := make([]logSource, 0, len(entries))
	var others []logSource
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !devkitLogName.MatchString(name) {
			continue
		}
		source := newLogSource("devkit:"+name, "DevKit 主日志 · "+name, "devkit", "devkit", "",
			filepath.Join(dir, name))
		if source.Exists {
			latest = append(latest, source)
		} else {
			others = append(others, source)
		}
	}
	sort.SliceStable(latest, func(i, j int) bool {
		if latest[i].ModifiedAt == nil || latest[j].ModifiedAt == nil {
			return latest[i].ID < latest[j].ID
		}
		return latest[i].ModifiedAt.After(*latest[j].ModifiedAt)
	})
	return append(latest, others...)
}

func newLogSource(id, label, category, kind, workspaceID, path string) logSource {
	source := logSource{
		ID:          id,
		Label:       label,
		Category:    category,
		Kind:        kind,
		WorkspaceID: workspaceID,
		Path:        path,
	}
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		modified := info.ModTime().UTC()
		source.Exists = true
		source.SizeBytes = info.Size()
		source.ModifiedAt = &modified
	}
	return source
}
