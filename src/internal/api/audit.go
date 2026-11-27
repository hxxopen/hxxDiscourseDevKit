package api

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/audit"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

// auditActions maps a registered route to a stable operation name. The
// fallback in auditActionFor keeps newly added mutating routes auditable even
// before they receive a friendly label.
var auditActions = map[string]string{
	"PATCH /api/v1/settings":                                          "settings.update",
	"PUT /api/v1/settings/theme-preview":                              "settings.theme-preview.set",
	"DELETE /api/v1/settings/theme-preview":                           "settings.theme-preview.clear",
	"POST /api/v1/settings/storage/migration/preview":                 "settings.storage.migration.preview",
	"POST /api/v1/settings/storage/migration":                         "settings.storage.migration.start",
	"POST /api/v1/tasks/{id}/cancel":                                  "task.cancel",
	"POST /api/v1/profile-sources":                                    "profile-source.add",
	"POST /api/v1/profile-sources/{id}/refresh":                       "profile-source.refresh",
	"DELETE /api/v1/profile-sources/{id}":                             "profile-source.remove",
	"POST /api/v1/workspaces":                                         "workspace.create",
	"PATCH /api/v1/workspaces/{id}":                                   "workspace.update",
	"POST /api/v1/workspaces/{id}/activate":                           "workspace.activate",
	"POST /api/v1/workspaces/{id}/provision":                          "workspace.provision",
	"DELETE /api/v1/workspaces/{id}/preview":                          "workspace.delete.preview",
	"DELETE /api/v1/workspaces/{id}":                                  "workspace.delete",
	"POST /api/v1/workspaces/{id}/start":                              "project.start",
	"POST /api/v1/workspaces/{id}/stop":                               "project.stop",
	"POST /api/v1/workspaces/{id}/restart":                            "project.restart",
	"PUT /api/v1/workspaces/{id}/project/source":                      "project.source.update",
	"POST /api/v1/workspaces/{id}/project/clone":                      "project.clone",
	"POST /api/v1/workspaces/{id}/project/fetch":                      "project.fetch",
	"POST /api/v1/workspaces/{id}/project/pull":                       "project.pull",
	"POST /api/v1/workspaces/{id}/project/checkout":                   "project.checkout",
	"POST /api/v1/workspaces/{id}/project/init":                       "project.init",
	"POST /api/v1/workspaces/{id}/plugins":                            "plugin.create",
	"POST /api/v1/workspaces/{id}/plugins/{slug}/test":                "plugin.test",
	"POST /api/v1/workspaces/{id}/plugins/{slug}/package":             "plugin.package",
	"POST /api/v1/workspaces/{id}/plugins/{slug}/enable":              "plugin.enable",
	"POST /api/v1/workspaces/{id}/plugins/{slug}/disable":             "plugin.disable",
	"POST /api/v1/workspaces/{id}/themes":                             "theme.create",
	"POST /api/v1/workspaces/{id}/themes/{slug}/package":              "theme.package",
	"POST /api/v1/workspaces/{id}/themes/{slug}/build":                "theme.build",
	"POST /api/v1/workspaces/{id}/themes/{slug}/preview":              "theme.preview",
	"POST /api/v1/workspaces/{id}/backups":                            "backup.create",
	"DELETE /api/v1/workspaces/{id}/backups/{backupID}":               "backup.delete",
	"POST /api/v1/workspaces/{id}/backups/{backupID}/restore-preview": "backup.restore.preview",
	"POST /api/v1/workspaces/{id}/backups/{backupID}/restore":         "backup.restore",
	"POST /api/v1/runtime/verify":                                     "runtime.verify",
	"POST /api/v1/runtime/discover":                                   "runtime.discover",
	"POST /api/v1/runtime/install-preview":                            "runtime.install.preview",
	"POST /api/v1/runtime/install":                                    "runtime.install",
	"PATCH /api/v1/services/{name}":                                   "service.update",
	"POST /api/v1/services/{name}/test":                               "service.test",
	"POST /api/v1/services/{name}/start":                              "service.start",
	"POST /api/v1/services/{name}/stop":                               "service.stop",
	"POST /api/v1/services/{name}/restart":                            "service.restart",
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	if s.opts.Audit == nil {
		s.fail(w, r, errFeatureUnavailable("操作审计"))
		return
	}

	query := r.URL.Query()
	limit := 100
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			s.fail(w, r, httpx.BadRequest("VALIDATION_FAILED", "limit 必须是 1–500 之间的整数"))
			return
		}
		limit = parsed
	}

	filter := audit.Filter{
		Limit:       limit,
		Action:      strings.TrimSpace(query.Get("action")),
		WorkspaceID: strings.TrimSpace(query.Get("workspace_id")),
		Result:      strings.TrimSpace(query.Get("result")),
	}
	if len(filter.Action) > 200 || len(filter.WorkspaceID) > 128 {
		s.fail(w, r, httpx.BadRequest("VALIDATION_FAILED", "过滤条件过长"))
		return
	}
	switch filter.Result {
	case "", "success", "accepted", "failure":
	default:
		s.fail(w, r, httpx.BadRequest("VALIDATION_FAILED", "result 必须是 success、accepted 或 failure"))
		return
	}
	if raw := strings.TrimSpace(query.Get("since")); raw != "" {
		since, err := parseAuditSince(raw)
		if err != nil {
			s.fail(w, r, httpx.BadRequest("VALIDATION_FAILED", "since 必须是 RFC3339 时间或 YYYY-MM-DD"))
			return
		}
		filter.Since = since
	}

	page, err := s.opts.Audit.List(filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func parseAuditSince(raw string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return parsed.UTC(), nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

// withAudit records mutating requests after they complete. It wraps the auth
// handler so rejected attempts (401/403) are visible as failures too. Bodies
// are never captured, which keeps passwords, API keys, and confirm tokens out
// of the audit trail.
func (s *Server) withAudit(pattern string, next http.Handler) http.Handler {
	if s.opts.Audit == nil {
		return next
	}
	method, routePath := splitRoutePattern(pattern)
	action := auditActionFor(pattern)
	if action == "" || !isMutating(method) {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder, ok := w.(*httpx.Recorder)
		if !ok {
			recorder = &httpx.Recorder{ResponseWriter: w}
		}
		next.ServeHTTP(recorder, r)

		status := recorder.Status
		if status == 0 {
			status = http.StatusOK
		}
		values := matchRouteValues(routePath, r.URL.Path)
		event := audit.Event{
			Action:     action,
			Result:     auditResult(status),
			Status:     status,
			Method:     r.Method,
			Path:       r.URL.Path,
			Target:     auditTarget(routePath, values),
			Client:     auditClient(r),
			Actor:      "local",
			RequestID:  httpx.RequestIDFrom(r.Context()),
			RemoteAddr: auditRemoteHost(r.RemoteAddr),
			DurationMS: time.Since(start).Milliseconds(),
		}
		if strings.Contains(routePath, "/workspaces/{id}") {
			event.WorkspaceID = values["id"]
		}
		if err := s.opts.Audit.Record(event); err != nil {
			s.log.Warn("audit event was not recorded",
				"action", action, "request_id", event.RequestID, "error", err)
		}
	})
}

func splitRoutePattern(pattern string) (method, routePath string) {
	method, routePath, _ = strings.Cut(pattern, " ")
	return method, routePath
}

func auditActionFor(pattern string) string {
	if action, ok := auditActions[pattern]; ok {
		return action
	}
	method, routePath := splitRoutePattern(pattern)
	if !isMutating(method) {
		return ""
	}
	return fallbackAuditAction(routePath)
}

func fallbackAuditAction(routePath string) string {
	parts := strings.Split(strings.Trim(routePath, "/"), "/")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "api" || part == "v1" || strings.HasPrefix(part, "{") {
			continue
		}
		names = append(names, part)
	}
	if len(names) == 0 {
		return "api.mutation"
	}
	return strings.Join(names, ".")
}

func auditResult(status int) string {
	switch {
	case status == http.StatusAccepted:
		return "accepted"
	case status >= 200 && status < 300:
		return "success"
	default:
		return "failure"
	}
}

func auditClient(r *http.Request) string {
	switch client := strings.ToLower(strings.TrimSpace(r.Header.Get("X-DevKit-Client"))); client {
	case "cli", "ui", "api":
		return client
	}
	agent := r.UserAgent()
	switch {
	case strings.HasPrefix(agent, "hxxddk/"):
		return "cli"
	case strings.Contains(agent, "Mozilla/"):
		return "ui"
	default:
		return "api"
	}
}

func auditRemoteHost(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

func auditTarget(routePath string, values map[string]string) string {
	switch {
	case strings.Contains(routePath, "/plugins/{slug}"), strings.Contains(routePath, "/themes/{slug}"):
		return values["slug"]
	case strings.Contains(routePath, "/backups/{backupID}"):
		return values["backupID"]
	case strings.HasPrefix(routePath, "/api/v1/services/{name}"):
		return values["name"]
	case strings.HasPrefix(routePath, "/api/v1/profile-sources/{id}"):
		return values["id"]
	case strings.HasPrefix(routePath, "/api/v1/tasks/{id}"):
		return values["id"]
	default:
		return ""
	}
}

func matchRouteValues(pattern, path string) map[string]string {
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")
	if len(patternParts) != len(pathParts) {
		return nil
	}
	values := make(map[string]string)
	for i, part := range patternParts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			values[name] = pathParts[i]
			continue
		}
		if part != pathParts[i] {
			return nil
		}
	}
	return values
}
