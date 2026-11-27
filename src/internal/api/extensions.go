package api

import (
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

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
