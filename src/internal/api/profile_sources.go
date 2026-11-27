package api

import (
	"context"
	"net/http"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
)

type profileSourceListResponse struct {
	Items []profile.Source `json:"items"`
	Total int              `json:"total"`
}

func (s *Server) profileSourcesReady(w http.ResponseWriter, r *http.Request) bool {
	if s.opts.Profiles == nil || !s.opts.Profiles.SourcesEnabled() {
		s.fail(w, r, errFeatureUnavailable("第三方 Project Profile 来源"))
		return false
	}
	return true
}

func (s *Server) handleListProfileSources(w http.ResponseWriter, r *http.Request) {
	if !s.profileSourcesReady(w, r) {
		return
	}
	items := s.opts.Profiles.ListSources()
	httpx.WriteJSON(w, http.StatusOK, profileSourceListResponse{Items: items, Total: len(items)})
}

func (s *Server) handleAddProfileSource(w http.ResponseWriter, r *http.Request) {
	if !s.profileSourcesReady(w, r) {
		return
	}
	var req profile.SourceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	source, err := s.opts.Profiles.AddSource(ctx, req)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, source)
}

func (s *Server) handleRefreshProfileSource(w http.ResponseWriter, r *http.Request) {
	if !s.profileSourcesReady(w, r) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	source, err := s.opts.Profiles.RefreshSource(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, source)
}

func (s *Server) handleRemoveProfileSource(w http.ResponseWriter, r *http.Request) {
	if !s.profileSourcesReady(w, r) {
		return
	}
	if err := s.opts.Profiles.RemoveSource(r.PathValue("id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
