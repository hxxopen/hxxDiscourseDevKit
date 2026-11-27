package api

import (
	"context"
	"net/http"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
)

type profileListResponse struct {
	Items []profile.Definition `json:"items"`
	Total int                  `json:"total"`
}

func (s *Server) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	if s.opts.Profiles == nil {
		s.fail(w, r, errFeatureUnavailable("Project Profile"))
		return
	}
	items := s.opts.Profiles.List()
	httpx.WriteJSON(w, http.StatusOK, profileListResponse{Items: items, Total: len(items)})
}

func (s *Server) handleGetProfile(w http.ResponseWriter, r *http.Request) {
	if s.opts.Profiles == nil {
		s.fail(w, r, errFeatureUnavailable("Project Profile"))
		return
	}
	def, err := s.opts.Profiles.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, def)
}

func (s *Server) handleCheckProfile(w http.ResponseWriter, r *http.Request) {
	if s.opts.Profiles == nil {
		s.fail(w, r, errFeatureUnavailable("Project Profile"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	report, err := s.opts.Profiles.Check(ctx, r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, report)
}
