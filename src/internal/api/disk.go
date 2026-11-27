package api

import (
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

func (s *Server) handleDiskUsage(w http.ResponseWriter, r *http.Request) {
	if s.opts.DiskUsage == nil {
		s.fail(w, r, errFeatureUnavailable("磁盘使用情况"))
		return
	}
	httpx.WriteJSON(w, http.StatusOK, s.opts.DiskUsage.Report(r.Context()))
}
