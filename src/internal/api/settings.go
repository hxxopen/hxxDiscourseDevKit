package api

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
	"github.com/hxx/hxxDiscourseDevKit/internal/themepreview"
)

type settingsPaths struct {
	Root               string `json:"root"`
	DataRoot           string `json:"data_root"`
	DataRootSplit      bool   `json:"data_root_split"`
	DataRootLegacy     bool   `json:"data_root_legacy"`
	DataRootPending    bool   `json:"data_root_pending"`
	ConfigFile         string `json:"config_file"`
	ConfigDir          string `json:"config_dir"`
	WorkspacesDir      string `json:"workspaces_dir"`
	ProfileSourcesFile string `json:"profile_sources_file"`
	ProfileSourcesDir  string `json:"profile_sources_dir"`
	RuntimeDir         string `json:"runtime_dir"`
	ServicesDir        string `json:"services_dir"`
	LogsDir            string `json:"logs_dir"`
	AuditDir           string `json:"audit_dir"`
	BackupsDir         string `json:"backups_dir"`
	TmpDir             string `json:"tmp_dir"`
}

type themePreviewSettings struct {
	APIKeyConfigured bool `json:"api_key_configured"`
}

type settingsResponse struct {
	Config          config.Config        `json:"config"`
	Paths           settingsPaths        `json:"paths"`
	RestartRequired []string             `json:"restart_required"`
	ThemePreview    themePreviewSettings `json:"theme_preview"`
}

type networkSettingsInput struct {
	APIPort *int `json:"api_port"`
}

type runtimeSettingsInput struct {
	DefaultProfile *string `json:"default_profile"`
	MatrixFile     *string `json:"matrix_file"`
	WSLDistro      *string `json:"wsl_distro"`
}

type projectSettingsInput struct {
	DefaultRepositoryURL *string `json:"default_repository_url"`
	DefaultBranch        *string `json:"default_branch"`
	StartTimeoutSeconds  *int    `json:"start_timeout_seconds"`
	StopTimeoutSeconds   *int    `json:"stop_timeout_seconds"`
}

type loggingSettingsInput struct {
	Level         *string `json:"level"`
	RetentionDays *int    `json:"retention_days"`
	MaxFileMB     *int    `json:"max_file_mb"`
}

type updatesSettingsInput struct {
	Channel      *string `json:"channel"`
	CheckOnStart *bool   `json:"check_on_start"`
	AutoInstall  *bool   `json:"auto_install"`
}

type settingsPatch struct {
	Network *networkSettingsInput `json:"network"`
	Runtime *runtimeSettingsInput `json:"runtime"`
	Project *projectSettingsInput `json:"project"`
	Logging *loggingSettingsInput `json:"logging"`
	Updates *updatesSettingsInput `json:"updates"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var input settingsPatch
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		return
	}

	levelChanged := false
	level := ""
	err := s.opts.Config.Update(func(next *config.Config) error {
		if input.Network != nil && input.Network.APIPort != nil {
			next.Network.APIPort = *input.Network.APIPort
		}
		if input.Runtime != nil {
			if input.Runtime.DefaultProfile != nil {
				next.Runtime.DefaultProfile = strings.TrimSpace(*input.Runtime.DefaultProfile)
			}
			if input.Runtime.MatrixFile != nil {
				next.Runtime.MatrixFile = strings.TrimSpace(*input.Runtime.MatrixFile)
			}
			if input.Runtime.WSLDistro != nil {
				next.Runtime.WSLDistro = strings.TrimSpace(*input.Runtime.WSLDistro)
			}
		}
		if input.Project != nil {
			if input.Project.DefaultRepositoryURL != nil {
				next.Project.DefaultRepositoryURL = strings.TrimSpace(*input.Project.DefaultRepositoryURL)
			}
			if input.Project.DefaultBranch != nil {
				next.Project.DefaultBranch = strings.TrimSpace(*input.Project.DefaultBranch)
			}
			if input.Project.StartTimeoutSeconds != nil {
				next.Project.StartTimeoutSeconds = *input.Project.StartTimeoutSeconds
			}
			if input.Project.StopTimeoutSeconds != nil {
				next.Project.StopTimeoutSeconds = *input.Project.StopTimeoutSeconds
			}
		}
		if input.Logging != nil {
			if input.Logging.Level != nil {
				candidate := strings.ToLower(strings.TrimSpace(*input.Logging.Level))
				if candidate != next.Logging.Level {
					levelChanged = true
					level = candidate
				}
				next.Logging.Level = candidate
			}
			if input.Logging.RetentionDays != nil {
				next.Logging.RetentionDays = *input.Logging.RetentionDays
			}
			if input.Logging.MaxFileMB != nil {
				next.Logging.MaxFileMB = *input.Logging.MaxFileMB
			}
		}
		if input.Updates != nil {
			if input.Updates.Channel != nil {
				next.Updates.Channel = strings.TrimSpace(*input.Updates.Channel)
			}
			if input.Updates.CheckOnStart != nil {
				next.Updates.CheckOnStart = *input.Updates.CheckOnStart
			}
			if input.Updates.AutoInstall != nil {
				next.Updates.AutoInstall = *input.Updates.AutoInstall
			}
		}
		return nil
	})
	if err != nil {
		httpx.WriteError(w, r, httpx.BadRequest("SETTINGS_INVALID", err.Error()), s.log)
		return
	}
	if levelChanged && s.opts.SetLogLevel != nil {
		s.opts.SetLogLevel(level)
	}
	httpx.WriteJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) settingsSnapshot() settingsResponse {
	cfg := s.opts.Config.Get()
	keyConfigured := false
	if s.opts.Secrets != nil {
		_, keyConfigured = s.opts.Secrets.Get(themepreview.APIKeySecret)
	}
	return settingsResponse{
		Config: cfg,
		Paths: settingsPaths{
			Root:               s.opts.Paths.Root,
			DataRoot:           s.opts.Paths.DataRoot,
			DataRootSplit:      s.opts.Paths.UsesSplitDataRoot(),
			DataRootLegacy:     s.opts.Paths.LegacyDataRoot,
			DataRootPending:    s.opts.Paths.DataRootPending(cfg.Storage.DataRoot),
			ConfigFile:         s.opts.Paths.ConfigFile(),
			ConfigDir:          s.opts.Paths.ConfigDir,
			WorkspacesDir:      s.opts.Paths.WorkspacesDir,
			ProfileSourcesFile: s.opts.Paths.ProfileSourcesFile(),
			ProfileSourcesDir:  s.opts.Paths.ProfileSourcesDir,
			RuntimeDir:         s.opts.Paths.RuntimeDir,
			ServicesDir:        s.opts.Paths.ServicesDir,
			LogsDir:            s.opts.Paths.LogsDir,
			AuditDir:           s.opts.Paths.AuditDir,
			BackupsDir:         s.opts.Paths.BackupsDir,
			TmpDir:             s.opts.Paths.TmpDir,
		},
		RestartRequired: s.restartRequired(cfg),
		ThemePreview: themePreviewSettings{
			APIKeyConfigured: keyConfigured,
		},
	}
}

type themePreviewKeyInput struct {
	APIKey string `json:"api_key"`
}

func (s *Server) handleSetThemePreviewKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.Secrets == nil {
		s.handleNotImplemented("主题预览凭据")(w, r)
		return
	}
	var input themePreviewKeyInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, r, err, s.log)
		return
	}
	key := strings.TrimSpace(input.APIKey)
	if err := validateThemePreviewKey(key); err != nil {
		httpx.WriteError(w, r, httpx.BadRequest("THEME_PREVIEW_KEY_INVALID", err.Error()), s.log)
		return
	}
	if err := s.opts.Secrets.Set(themepreview.APIKeySecret, key); err != nil {
		httpx.WriteError(w, r, httpx.Internal(err), s.log)
		return
	}
	s.log.Info("theme preview API key updated")
	httpx.WriteJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleClearThemePreviewKey(w http.ResponseWriter, r *http.Request) {
	if s.opts.Secrets == nil {
		s.handleNotImplemented("主题预览凭据")(w, r)
		return
	}
	if err := s.opts.Secrets.Delete(themepreview.APIKeySecret); err != nil {
		httpx.WriteError(w, r, httpx.Internal(err), s.log)
		return
	}
	s.log.Info("theme preview API key cleared")
	httpx.WriteJSON(w, http.StatusOK, s.settingsSnapshot())
}

func validateThemePreviewKey(key string) error {
	if len(key) < 16 || len(key) > 512 {
		return fmt.Errorf("Discourse API Key 长度必须在 16–512 个字符之间")
	}
	for _, r := range key {
		if r < 0x21 || r > 0x7e {
			return fmt.Errorf("Discourse API Key 只能包含可打印 ASCII 字符，不能包含空白")
		}
	}
	return nil
}

// restartRequired lists settings that are persisted but only take effect after
// the daemon restarts.
func (s *Server) restartRequired(current config.Config) []string {
	fields := make([]string, 0, 7)
	startup := s.startupConfig
	if !config.SamePath(
		effectiveDataRoot(current.Storage.DataRoot, s.opts.Paths.Root),
		effectiveDataRoot(startup.Storage.DataRoot, s.opts.Paths.Root),
	) {
		fields = append(fields, "storage.data_root")
	}
	if current.Network.Host != startup.Network.Host {
		fields = append(fields, "network.host")
	}
	livePort := s.opts.APIPort
	if livePort == 0 {
		livePort = startup.Network.APIPort
	}
	if current.Network.APIPort != livePort {
		fields = append(fields, "network.api_port")
	}
	if current.Project.StartTimeoutSeconds != startup.Project.StartTimeoutSeconds {
		fields = append(fields, "project.start_timeout_seconds")
	}
	if current.Project.StopTimeoutSeconds != startup.Project.StopTimeoutSeconds {
		fields = append(fields, "project.stop_timeout_seconds")
	}
	if current.Logging.RetentionDays != startup.Logging.RetentionDays {
		fields = append(fields, "logging.retention_days")
	}
	if current.Logging.MaxFileMB != startup.Logging.MaxFileMB {
		fields = append(fields, "logging.max_file_mb")
	}
	return fields
}

func effectiveDataRoot(value, root string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return root
	}
	return value
}
