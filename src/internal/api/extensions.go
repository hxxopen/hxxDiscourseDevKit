package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/themepreview"
)

// PluginTestRunner executes a validated plugin test plan in the background.
type PluginTestRunner interface {
	RunPluginTest(ctx context.Context, plan extension.PluginTestPlan, progress func(int, string)) error
}

func (s *Server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	s.handleListExtensions(w, r, extension.KindPlugin)
}

func (s *Server) handleCreatePlugin(w http.ResponseWriter, r *http.Request) {
	s.handleCreateExtension(w, r, extension.KindPlugin)
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	s.handleListExtensions(w, r, extension.KindTheme)
}

func (s *Server) handleCreateTheme(w http.ResponseWriter, r *http.Request) {
	s.handleCreateExtension(w, r, extension.KindTheme)
}

func (s *Server) handlePackagePlugin(w http.ResponseWriter, r *http.Request) {
	s.handlePackageExtension(w, r, extension.KindPlugin)
}

func (s *Server) handlePackageTheme(w http.ResponseWriter, r *http.Request) {
	s.handlePackageExtension(w, r, extension.KindTheme)
}

func (s *Server) handleEnablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handleSetPluginEnabled(w, r, true)
}

func (s *Server) handleDisablePlugin(w http.ResponseWriter, r *http.Request) {
	s.handleSetPluginEnabled(w, r, false)
}

func (s *Server) handleCheckPlugin(w http.ResponseWriter, r *http.Request) {
	s.handleCheckExtension(w, r, extension.KindPlugin)
}

func (s *Server) handleCheckTheme(w http.ResponseWriter, r *http.Request) {
	s.handleCheckExtension(w, r, extension.KindTheme)
}

func (s *Server) handleTestPlugin(w http.ResponseWriter, r *http.Request) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	if s.opts.PluginTests == nil {
		s.handleNotImplemented("插件测试尚未启用")(w, r)
		return
	}
	plan, err := s.opts.Extensions.PreparePluginTest(r.Context(),
		r.PathValue("id"), r.PathValue("slug"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	submitted := s.opts.Tasks.Submit("plugin.test", plan.WorkspaceID, true,
		func(ctx context.Context, progress func(int, string)) error {
			return s.opts.PluginTests.RunPluginTest(ctx, *plan, progress)
		})
	httpx.WriteJSON(w, http.StatusAccepted, deleteAcceptedResponse{
		TaskID:    submitted.TaskID,
		StatusURL: "/api/v1/tasks/" + submitted.TaskID,
		Task:      submitted,
	})
}

func (s *Server) handleCheckExtension(w http.ResponseWriter, r *http.Request, kind extension.Kind) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	var (
		report *extension.CheckReport
		err    error
	)
	switch kind {
	case extension.KindPlugin:
		report, err = s.opts.Extensions.CheckPlugin(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	case extension.KindTheme:
		report, err = s.opts.Extensions.CheckTheme(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}

func (s *Server) handleSetPluginEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	var (
		item *extension.Item
		err  error
	)
	if enabled {
		item, err = s.opts.Extensions.EnablePlugin(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	} else {
		item, err = s.opts.Extensions.DisablePlugin(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleListExtensions(w http.ResponseWriter, r *http.Request, kind extension.Kind) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	var (
		listing *extension.Listing
		err     error
	)
	switch kind {
	case extension.KindPlugin:
		listing, err = s.opts.Extensions.ListPlugins(r.Context(), r.PathValue("id"))
	case extension.KindTheme:
		listing, err = s.opts.Extensions.ListThemes(r.Context(), r.PathValue("id"))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, listing)
}

func (s *Server) handleCreateExtension(w http.ResponseWriter, r *http.Request, kind extension.Kind) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	var req extension.CreateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	var (
		item *extension.Item
		err  error
	)
	switch kind {
	case extension.KindPlugin:
		item, err = s.opts.Extensions.CreatePlugin(r.Context(), r.PathValue("id"), req)
	case extension.KindTheme:
		item, err = s.opts.Extensions.CreateTheme(r.Context(), r.PathValue("id"), req)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, item)
}

func (s *Server) handlePackageExtension(w http.ResponseWriter, r *http.Request, kind extension.Kind) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	var (
		pkg *extension.Package
		err error
	)
	switch kind {
	case extension.KindPlugin:
		pkg, err = s.opts.Extensions.PackagePlugin(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	case extension.KindTheme:
		pkg, err = s.opts.Extensions.PackageTheme(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, pkg)
}

func (s *Server) handleBuildTheme(w http.ResponseWriter, r *http.Request) {
	if s.opts.Extensions == nil {
		s.handleNotImplemented("扩展管理尚未启用")(w, r)
		return
	}
	result, err := s.opts.Extensions.BuildTheme(r.Context(), r.PathValue("id"), r.PathValue("slug"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}

func (s *Server) handlePreviewTheme(w http.ResponseWriter, r *http.Request) {
	if s.opts.ThemePreview == nil {
		s.handleNotImplemented("主题预览尚未启用")(w, r)
		return
	}
	var input struct {
		APIKey string `json:"api_key"`
	}
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	key := strings.TrimSpace(input.APIKey)
	if key == "" && s.opts.Secrets != nil {
		key, _ = s.opts.Secrets.Get(themepreview.APIKeySecret)
	}
	if key == "" {
		httpx.WriteError(w, r, httpx.BadRequest("THEME_PREVIEW_KEY_REQUIRED",
			"尚未配置 Discourse 管理员 API Key，请先在设置页保存，或通过 --api-key 提供"), s.log)
		return
	}
	result, err := s.opts.ThemePreview.Preview(r.Context(), r.PathValue("id"), r.PathValue("slug"), key)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, result)
}
