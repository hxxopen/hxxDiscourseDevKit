package api

import (
	"context"
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

type backupListResponse struct {
	Items []backup.Backup `json:"items"`
	Total int             `json:"total"`
}

func (s *Server) backupsReady(w http.ResponseWriter, r *http.Request) bool {
	if s.opts.Backups == nil {
		s.fail(w, r, errFeatureUnavailable("backups"))
		return false
	}
	return true
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	items, err := s.opts.Backups.List(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, backupListResponse{Items: items, Total: len(items)})
}

func (s *Server) handleGetBackup(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	verify := boolQuery(r.URL.Query(), "verify")
	item, err := s.opts.Backups.Get(r.Context(), r.PathValue("id"),
		r.PathValue("backupID"), verify)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, item)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	var opts backup.CreateOptions
	if err := decodeOptionalBody(w, r, &opts); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	s.submitWorkspaceTask(w, r, "backup.create", true,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			_, err := s.opts.Backups.Create(ctx, workspaceID, opts, progress)
			return err
		})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	if err := s.opts.Backups.Delete(r.Context(), r.PathValue("id"), r.PathValue("backupID")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePreviewRestore(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	preview, err := s.opts.Backups.PreviewRestore(r.Context(), r.PathValue("id"),
		r.PathValue("backupID"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, preview)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	if !s.backupsReady(w, r) {
		return
	}
	var input struct {
		ConfirmToken string `json:"confirm_token"`
	}
	if err := decodeOptionalBody(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if input.ConfirmToken == "" {
		input.ConfirmToken = r.URL.Query().Get("confirm_token")
	}
	backupID := r.PathValue("backupID")
	token := input.ConfirmToken
	s.submitWorkspaceTask(w, r, "backup.restore", false,
		func(ctx context.Context, workspaceID string, progress func(int, string)) error {
			_, err := s.opts.Backups.Restore(ctx, workspaceID, backupID, token, progress)
			return err
		})
}
