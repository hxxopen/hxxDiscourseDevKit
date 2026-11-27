package api

import (
	"errors"
	"net/http"

	"github.com/hxx/hxxDiscourseDevKit/internal/backup"
	"github.com/hxx/hxxDiscourseDevKit/internal/extension"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/migration"
	"github.com/hxx/hxxDiscourseDevKit/internal/profile"
	"github.com/hxx/hxxDiscourseDevKit/internal/project"
	"github.com/hxx/hxxDiscourseDevKit/internal/runtime"
	"github.com/hxx/hxxDiscourseDevKit/internal/service"
	"github.com/hxx/hxxDiscourseDevKit/internal/task"
	"github.com/hxx/hxxDiscourseDevKit/internal/themepreview"
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
	case errors.Is(err, profile.ErrNotFound):
		return httpx.NotFound("PROFILE_NOT_FOUND", err.Error())
	case errors.Is(err, profile.ErrSourcesDisabled):
		return errFeatureUnavailable("第三方 Project Profile 来源")
	case errors.Is(err, profile.ErrSourceNotFound):
		return httpx.NotFound("PROFILE_SOURCE_NOT_FOUND", err.Error())
	case errors.Is(err, profile.ErrSourceExists):
		return httpx.Conflict("PROFILE_SOURCE_EXISTS", err.Error())
	case errors.Is(err, profile.ErrSourceConflict):
		return httpx.Conflict("PROFILE_SOURCE_CONFLICT", err.Error())
	case errors.Is(err, profile.ErrGitUnavailable):
		return httpx.New(http.StatusServiceUnavailable, "PROFILE_SOURCE_GIT_UNAVAILABLE", err.Error())
	case errors.Is(err, profile.ErrSourceUnavailable):
		return httpx.New(http.StatusBadGateway, "PROFILE_SOURCE_UNAVAILABLE", err.Error())
	case errors.Is(err, profile.ErrSourceInvalid):
		return httpx.Unprocessable("PROFILE_SOURCE_INVALID", err.Error())
	case errors.Is(err, task.ErrNotFound):
		return httpx.NotFound("TASK_NOT_FOUND", "任务不存在")
	case errors.Is(err, task.ErrNotCancelable):
		return httpx.Conflict("TASK_NOT_CANCELABLE", "任务不可取消或已结束")
	case errors.Is(err, runtime.ErrUnknownComponent):
		return httpx.NotFound("RUNTIME_COMPONENT_NOT_FOUND", err.Error())
	case errors.Is(err, runtime.ErrWSLUnsupported):
		return httpx.New(http.StatusNotImplemented, "RUNTIME_INSTALL_UNSUPPORTED", err.Error())
	case errors.Is(err, runtime.ErrWSLUnavailable):
		return httpx.New(http.StatusServiceUnavailable, "RUNTIME_WSL_UNAVAILABLE", err.Error())
	case errors.Is(err, runtime.ErrUnsupportedDistro):
		return httpx.Conflict("RUNTIME_DISTRO_UNSUPPORTED", err.Error())
	case errors.Is(err, runtime.ErrInstallConfirm):
		return httpx.Unprocessable("PREVIEW_REQUIRED", err.Error())
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
	case errors.Is(err, service.ErrDataDirUnsupported):
		return httpx.Conflict("SERVICE_DATA_DIR_UNSUPPORTED", err.Error())
	case errors.Is(err, project.ErrSourceNotConfigured):
		return httpx.Conflict("SOURCE_NOT_CONFIGURED", err.Error())
	case errors.Is(err, project.ErrSourceNotEmpty):
		return httpx.Conflict("SOURCE_NOT_EMPTY", err.Error())
	case errors.Is(err, project.ErrSourceInstalled):
		return httpx.Conflict("SOURCE_INSTALLED", err.Error())
	case errors.Is(err, project.ErrSourceMissing):
		return httpx.Conflict("SOURCE_MISSING", "源码尚未安装，请先克隆源码")
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
	case errors.Is(err, migration.ErrConfirmToken):
		return httpx.Unprocessable("PREVIEW_REQUIRED", "迁移操作需要有效的预览确认令牌")
	case errors.Is(err, migration.ErrUnsupported), errors.Is(err, migration.ErrNotEligible):
		return httpx.Conflict("STORAGE_MIGRATION_NOT_ELIGIBLE", err.Error())
	case errors.Is(err, migration.ErrPendingRestart):
		return httpx.Conflict("STORAGE_RESTART_REQUIRED", err.Error())
	case errors.Is(err, migration.ErrWSLUnavailable):
		return httpx.New(http.StatusServiceUnavailable, "STORAGE_MIGRATION_WSL_UNAVAILABLE", err.Error())
	case errors.Is(err, migration.ErrTargetInvalid):
		return httpx.Unprocessable("STORAGE_TARGET_INVALID", err.Error())
	case errors.Is(err, migration.ErrBusy):
		return httpx.Conflict("STORAGE_MIGRATION_BUSY", err.Error())
	case errors.Is(err, migration.ErrMigrating):
		return httpx.Conflict("STORAGE_MIGRATION_IN_PROGRESS", err.Error())
	case errors.Is(err, migration.ErrPlanStale):
		return httpx.Conflict("STORAGE_MIGRATION_PLAN_STALE", err.Error())
	case errors.Is(err, migration.ErrBlocked), errors.Is(err, migration.ErrTargetPartial):
		return httpx.Conflict("STORAGE_MIGRATION_BLOCKED", err.Error())
	case errors.Is(err, migration.ErrSourceChanged):
		return httpx.Conflict("STORAGE_SOURCE_CHANGED", err.Error())
	case errors.Is(err, migration.ErrInsufficientSpace):
		return httpx.Conflict("INSUFFICIENT_SPACE", err.Error())
	case errors.Is(err, migration.ErrStopFailed):
		return httpx.Conflict("STORAGE_STOP_FAILED", err.Error())
	case errors.Is(err, extension.ErrExists):
		return httpx.Conflict("EXTENSION_EXISTS", err.Error())
	case errors.Is(err, extension.ErrNotFound):
		return httpx.NotFound("EXTENSION_NOT_FOUND", err.Error())
	case errors.Is(err, extension.ErrSourceMissing):
		return httpx.Conflict("SOURCE_MISSING", err.Error())
	case errors.Is(err, extension.ErrProjectRunning):
		return httpx.Conflict("PROJECT_RUNNING", err.Error())
	case errors.Is(err, extension.ErrStateConflict):
		return httpx.Conflict("EXTENSION_STATE_CONFLICT", err.Error())
	case errors.Is(err, extension.ErrNotInitialized):
		return httpx.Conflict("PROJECT_NOT_INITIALIZED", err.Error())
	case errors.Is(err, extension.ErrValidation):
		return httpx.BadRequest("EXTENSION_VALIDATION_FAILED", err.Error())
	case errors.Is(err, themepreview.ErrKeyRequired):
		return httpx.BadRequest("THEME_PREVIEW_KEY_REQUIRED", err.Error())
	case errors.Is(err, themepreview.ErrNotRunning):
		return httpx.Conflict("THEME_PREVIEW_NOT_RUNNING",
			"Workspace 项目未运行，请先在项目页启动 Discourse 后再预览主题")
	case errors.Is(err, themepreview.ErrUnreachable):
		return httpx.New(http.StatusBadGateway, "THEME_PREVIEW_UNREACHABLE",
			"无法连接 Workspace 的 Discourse 站点："+err.Error())
	case errors.Is(err, themepreview.ErrImportFailed):
		return httpx.New(http.StatusBadGateway, "THEME_PREVIEW_IMPORT_FAILED", err.Error())
	default:
		var apiErr *httpx.Error
		if errors.As(err, &apiErr) {
			return apiErr
		}
		return httpx.Internal(err)
	}
}
