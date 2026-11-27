package api

import (
	"errors"
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/workspace"
)

func errFeatureUnavailable(feature string) error {
	return httpx.New(http.StatusServiceUnavailable, "FEATURE_UNAVAILABLE", feature+" 管理尚未启用")
}

func mapError(err error) error {
	switch {
	case errors.Is(err, workspace.ErrNotFound):

		return httpx.NotFound("WORKSPACE_NOT_FOUND", "Workspace 不存在")
	case errors.Is(err, workspace.ErrNameConflict):
		return httpx.Conflict("WORKSPACE_NAME_CONFLICT", err.Error())
	case errors.Is(err, workspace.ErrInvalidName), errors.Is(err, workspace.ErrValidation):
		return httpx.BadRequest("VALIDATION_FAILED", err.Error())
	case errors.Is(err, workspace.ErrRunning):
		return httpx.Conflict("WORKSPACE_BUSY", "Workspace 正在运行，请先停止")
	case errors.Is(err, workspace.ErrNoPort):
		return httpx.Conflict("PORT_EXHAUSTED", "没有可用的 Workspace HTTP 端口")
	case errors.Is(err, workspace.ErrNoRedisDB):
		return httpx.Conflict("REDIS_DB_EXHAUSTED", "没有可用的 Redis DB 索引（上限 15 个 Workspace）")
	case errors.Is(err, workspace.ErrConfirmToken):
		return httpx.Unprocessable("PREVIEW_REQUIRED", "删除操作需要有效的预览确认令牌")
	case errors.Is(err, task.ErrNotFound):
		return httpx.NotFound("TASK_NOT_FOUND", "任务不存在")
	case errors.Is(err, task.ErrNotCancelable):
		return httpx.Conflict("TASK_NOT_CANCELABLE", "任务不可取消或已结束")
	case errors.Is(err, runtime.ErrUnknownComponent):
		return httpx.NotFound("RUNTIME_COMPONENT_NOT_FOUND", err.Error())
	case errors.Is(err, service.ErrUnknownService):
		return httpx.NotFound("SERVICE_NOT_FOUND", err.Error())
	case errors.Is(err, service.ErrExternal):
		return httpx.Conflict("SERVICE_EXTERNAL", err.Error())
	case errors.Is(err, service.ErrServiceInUse):
		return httpx.Conflict("SERVICE_IN_USE", err.Error())
	case errors.Is(err, service.ErrNotInstalled):
		return httpx.Conflict("SERVICE_NOT_INSTALLED", err.Error())
	case errors.Is(err, service.ErrNotRunning):
		return httpx.Conflict("SERVICE_NOT_RUNNING", err.Error())
	case errors.Is(err, service.ErrValidation):
		return httpx.BadRequest("SERVICE_VALIDATION_FAILED", err.Error())
	case errors.Is(err, project.ErrSourceNotConfigured):
		return httpx.Conflict("SOURCE_NOT_CONFIGURED", err.Error())
	case errors.Is(err, project.ErrSourceNotEmpty):
		return httpx.Conflict("SOURCE_NOT_EMPTY", err.Error())
	case errors.Is(err, project.ErrSourceInstalled):
		return httpx.Conflict("SOURCE_INSTALLED", err.Error())
	case errors.Is(err, project.ErrSourceMissing):
		return httpx.Conflict("SOURCE_MISSING", err.Error())
	case errors.Is(err, project.ErrNotGitRepository):
		return httpx.Conflict("SOURCE_NOT_GIT", err.Error())
	case errors.Is(err, project.ErrNotInitialized):
		return httpx.Conflict("PROJECT_NOT_INITIALIZED", err.Error())
	case errors.Is(err, project.ErrAlreadyRunning):
		return httpx.Conflict("PROJECT_RUNNING", "项目正在运行，请先停止")
	case errors.Is(err, project.ErrNotRunning):
		return httpx.Conflict("PROJECT_NOT_RUNNING", "项目未在运行")
	case errors.Is(err, project.ErrDirtyWorktree):
		return httpx.Conflict("WORKTREE_DIRTY", err.Error())
	case errors.Is(err, project.ErrInvalidSource), errors.Is(err, project.ErrInvalidRef),
		errors.Is(err, project.ErrValidation):
		return httpx.BadRequest("PROJECT_VALIDATION_FAILED", err.Error())
	case errors.Is(err, backup.ErrNotFound):
		return httpx.NotFound("BACKUP_NOT_FOUND", "备份不存在")
	case errors.Is(err, backup.ErrInvalidArchive):
		return httpx.Unprocessable("BACKUP_INVALID", err.Error())
	case errors.Is(err, backup.ErrUnsafePath):
		return httpx.Unprocessable("BACKUP_UNSAFE_PATH", err.Error())
	case errors.Is(err, backup.ErrWorkspaceRunning):
		return httpx.Conflict("WORKSPACE_BUSY", "Workspace 正在运行，请先停止")
	case errors.Is(err, backup.ErrConfirmToken):
		return httpx.Unprocessable("PREVIEW_REQUIRED", "恢复操作需要有效的预览确认令牌")
	case errors.Is(err, backup.ErrValidation):
		return httpx.BadRequest("BACKUP_VALIDATION_FAILED", err.Error())
	case errors.Is(err, backup.ErrInsufficientSpace):
		return httpx.Conflict("INSUFFICIENT_SPACE", err.Error())
	case errors.Is(err, backup.ErrWorkspaceMismatch):
		return httpx.Conflict("BACKUP_WORKSPACE_MISMATCH", err.Error())
	case errors.Is(err, extension.ErrExists):
		return httpx.Conflict("EXTENSION_EXISTS", err.Error())
	case errors.Is(err, extension.ErrSourceMissing):
		return httpx.Conflict("SOURCE_MISSING", err.Error())
	case errors.Is(err, extension.ErrValidation):
		return httpx.BadRequest("EXTENSION_VALIDATION_FAILED", err.Error())
	default:
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return httpx.Internal(err)
	}
}
