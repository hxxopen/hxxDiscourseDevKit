package api

import (
	"net/http"
	"strings"

	"github.com/hxx/hxxDiscourseDevKit/internal/config"
	"github.com/hxx/hxxDiscourseDevKit/internal/httpx"
)

type settingsPaths struct {
	Root          string `json:"root"`
	ConfigFile    string `json:"config_file"`
	ConfigDir     string `json:"config_dir"`
	WorkspacesDir string `json:"workspaces_dir"`
	RuntimeDir    string `json:"runtime_dir"`
	ServicesDir   string `json:"services_dir"`
	LogsDir       string `json:"logs_dir"`
	BackupsDir    string `json:"backups_dir"`
	TmpDir        string `json:"tmp_dir"`
}

type settingsResponse struct {
	Config          config.Config `json:"config"`
	Paths           settingsPaths `json:"paths"`
	RestartRequired []string      `json:"restart_required"`
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
	return settingsResponse{
		Config: cfg,
		Paths: settingsPaths{
			Root:          s.opts.Paths.Root,
			ConfigFile:    s.opts.Paths.ConfigFile(),
			ConfigDir:     s.opts.Paths.ConfigDir,
			WorkspacesDir: s.opts.Paths.WorkspacesDir,
			RuntimeDir:    s.opts.Paths.RuntimeDir,
			ServicesDir:   s.opts.Paths.ServicesDir,
			LogsDir:       s.opts.Paths.LogsDir,
			BackupsDir:    s.opts.Paths.BackupsDir,
			TmpDir:        s.opts.Paths.TmpDir,
		},
		RestartRequired: s.restartRequired(cfg),
	}
}

// restartRequired lists settings that are persisted but only take effect after
// the daemon restarts.
func (s *Server) restartRequired(current config.Config) []string {
	var fields []string
	startup := s.startupConfig
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
