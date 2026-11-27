package api

import (
	"context"
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/migration"
)

// StorageMigrator plans and reserves legacy data-root migrations.
type StorageMigrator interface {
	Preview(ctx context.Context) (*migration.Plan, error)
	Begin(ctx context.Context, confirmToken string) (migration.Runnable, error)
	Migrating() bool
	SetTaskID(taskID string)
	CurrentTaskID() string
}

func (s *Server) handlePreviewStorageMigration(w http.ResponseWriter, r *http.Request) {
	if s.opts.StorageMigration == nil {
		s.fail(w, r, errFeatureUnavailable("数据目录迁移"))
		return
	}
	plan, err := s.opts.StorageMigration.Preview(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, plan)
}

type storageMigrationInput struct {
	ConfirmToken string `json:"confirm_token"`
}

func (s *Server) handleStartStorageMigration(w http.ResponseWriter, r *http.Request) {
	if s.opts.StorageMigration == nil {
		s.fail(w, r, errFeatureUnavailable("数据目录迁移"))
		return
	}
	if s.opts.Tasks == nil {
		s.fail(w, r, errFeatureUnavailable("任务"))
		return
	}
	var input storageMigrationInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	if input.ConfirmToken == "" {
		s.fail(w, r, httpx.Unprocessable("PREVIEW_REQUIRED", "迁移操作需要有效的预览确认令牌"))
		return
	}
	runnable, err := s.opts.StorageMigration.Begin(r.Context(), input.ConfirmToken)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	submitted := s.opts.Tasks.Submit(migration.TaskType, "", true,
		func(ctx context.Context, progress func(int, string)) error {
			_, err := runnable.Execute(ctx, progress)
			return err
		})
	if submitted.Status.Terminal() {
		runnable.Abort()
		s.fail(w, r, errFeatureUnavailable("任务"))
		return
	}
	s.opts.StorageMigration.SetTaskID(submitted.TaskID)
	httpx.WriteJSON(w, http.StatusAccepted, deleteAcceptedResponse{
		TaskID:    submitted.TaskID,
		StatusURL: "/api/v1/tasks/" + submitted.TaskID,
		Task:      submitted,
	})
}

// maintenance blocks mutating requests while a data-root migration owns the
// instance. Task cancellation remains available so the user can abort.
func (s *Server) maintenance(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.opts.StorageMigration == nil ||
			!s.opts.StorageMigration.Migrating() ||
			!isMutating(r.Method) ||
			maintenanceAllowed(r) {
			next.ServeHTTP(w, r)
			return
		}
		httpx.WriteError(w, r,
			httpx.Conflict("MAINTENANCE_IN_PROGRESS", "数据目录迁移正在执行，请稍后再试"),
			s.log)
	})
}

func maintenanceAllowed(r *http.Request) bool {
	return r.Pattern == "POST /api/v1/tasks/{id}/cancel"
}
