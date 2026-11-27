package config

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/hxx/hxxDiscourseDevKit/internal/id"
)

// CurrentSchemaVersion is the devkit.json schema version understood here.
const CurrentSchemaVersion = 1

// Config is the persisted DevKit instance configuration. It must never contain
// plaintext secrets; secret values live in secrets.json and are referenced by
// name.
type Config struct {
	SchemaVersion         int             `json:"schema_version"`
	InstanceID            string          `json:"instance_id"`
	ActiveWorkspaceID     string          `json:"active_workspace_id"`
	WorkspacesInitialized bool            `json:"workspaces_initialized"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
	Network               NetworkConfig   `json:"network"`
	Runtime               RuntimeConfig   `json:"runtime"`
	Storage               StorageConfig   `json:"storage"`
	Project               ProjectSettings `json:"project"`
	Services              ServicesConfig  `json:"services"`
	Logging               LoggingConfig   `json:"logging"`
	Updates               UpdatesConfig   `json:"updates"`
}

// NetworkConfig controls the local management API listener.
type NetworkConfig struct {
	Host    string `json:"host"`
	APIPort int    `json:"api_port"`
}

// RuntimeConfig points at the verified component matrix.
type RuntimeConfig struct {
	DefaultProfile string `json:"default_profile"`
	MatrixFile     string `json:"matrix_file"`
	// WSLDistro selects the WSL2 distribution used to execute runtime and
	// service commands on Windows. When empty, the first installed distribution
	// is used as a fallback.
	WSLDistro string `json:"wsl_distro,omitempty"`
}

// StorageConfig controls where project and service data are stored. When
// DataRoot is empty, data lives under the instance root. On Windows a new
// instance defaults DataRoot to the WSL2 filesystem.
type StorageConfig struct {
	DataRoot string `json:"data_root,omitempty"`
}

// ProjectSettings controls source checkout and development process defaults.
type ProjectSettings struct {
	DefaultRepositoryURL string `json:"default_repository_url"`
	DefaultBranch        string `json:"default_branch"`
	StartTimeoutSeconds  int    `json:"start_timeout_seconds"`
	StopTimeoutSeconds   int    `json:"stop_timeout_seconds"`
}

// ServicesConfig groups managed development services.
type ServicesConfig struct {
	PostgreSQL PostgreSQLConfig `json:"postgresql"`
	Redis      RedisConfig      `json:"redis"`
}

// PostgreSQLConfig describes either a managed instance or an external server.
type PostgreSQLConfig struct {
	Mode             string           `json:"mode"`
	Host             string           `json:"host"`
	Port             int              `json:"port"`
	AdminUser        string           `json:"admin_user"`
	AdminPasswordRef string           `json:"admin_password_ref"`
	DataDir          string           `json:"data_dir"`
	External         ExternalEndpoint `json:"external"`
}

// RedisConfig describes either a managed instance or an external server.
type RedisConfig struct {
	Mode        string           `json:"mode"`
	Host        string           `json:"host"`
	Port        int              `json:"port"`
	PasswordRef string           `json:"password_ref"`
	DataDir     string           `json:"data_dir"`
	External    ExternalEndpoint `json:"external"`
}

// ExternalEndpoint is used when a service runs outside DevKit.
type ExternalEndpoint struct {
	Host        string `json:"host,omitempty"`
	Port        int    `json:"port,omitempty"`
	Database    string `json:"database,omitempty"`
	Username    string `json:"username,omitempty"`
	PasswordRef string `json:"password_ref,omitempty"`
	TLS         bool   `json:"tls,omitempty"`
}

// LoggingConfig controls log verbosity and retention.
type LoggingConfig struct {
	Level         string `json:"level"`
	RetentionDays int    `json:"retention_days"`
	MaxFileMB     int    `json:"max_file_mb"`
}

// UpdatesConfig controls release checking.
type UpdatesConfig struct {
	Channel      string `json:"channel"`
	CheckOnStart bool   `json:"check_on_start"`
	AutoInstall  bool   `json:"auto_install"`
}

// Default returns a fully populated configuration for a new instance.
func Default(paths *Paths) Config {
	now := time.Now().UTC()
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		InstanceID:    id.MustNew(),
		CreatedAt:     now,
		UpdatedAt:     now,
		Network: NetworkConfig{
			Host:    "127.0.0.1",
			APIPort: 17860,
		},
		Runtime: RuntimeConfig{
			DefaultProfile: "default",
			MatrixFile:     "config/runtime-matrix.json",
		},
		Project: ProjectSettings{
			DefaultRepositoryURL: "https://github.com/discourse/discourse.git",
			DefaultBranch:        "main",
			StartTimeoutSeconds:  180,
			StopTimeoutSeconds:   20,
		},
		Services: ServicesConfig{
			PostgreSQL: PostgreSQLConfig{
				Mode:             "managed",
				Host:             "127.0.0.1",
				Port:             15432,
				AdminUser:        "hxxddk_admin",
				AdminPasswordRef: "pg_admin",
				DataDir:          "services/postgresql/data",
			},
			Redis: RedisConfig{
				Mode:        "managed",
				Host:        "127.0.0.1",
				Port:        16379,
				PasswordRef: "redis_password",
				DataDir:     "services/redis/data",
			},
		},
		Logging: LoggingConfig{
			Level:         "info",
			RetentionDays: 30,
			MaxFileMB:     50,
		},
		Updates: UpdatesConfig{
			Channel:      "stable",
			CheckOnStart: true,
		},
	}
}

// ApplyDefaults fills fields omitted from a hand-edited config file.
func (c *Config) ApplyDefaults(paths *Paths) {
	def := Default(paths)
	if c.SchemaVersion == 0 {
		c.SchemaVersion = CurrentSchemaVersion
	}
	if c.InstanceID == "" {
		c.InstanceID = def.InstanceID
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = def.CreatedAt
	}
	if c.Network.Host == "" {
		c.Network.Host = def.Network.Host
	}
	if c.Network.APIPort == 0 {
		c.Network.APIPort = def.Network.APIPort
	}
	if c.Runtime.DefaultProfile == "" {
		c.Runtime.DefaultProfile = def.Runtime.DefaultProfile
	}
	if c.Runtime.MatrixFile == "" {
		c.Runtime.MatrixFile = def.Runtime.MatrixFile
	}
	if c.Project.DefaultRepositoryURL == "" {
		c.Project.DefaultRepositoryURL = def.Project.DefaultRepositoryURL
	}
	if c.Project.DefaultBranch == "" {
		c.Project.DefaultBranch = def.Project.DefaultBranch
	}
	if c.Project.StartTimeoutSeconds == 0 {
		c.Project.StartTimeoutSeconds = def.Project.StartTimeoutSeconds
	}
	if c.Project.StopTimeoutSeconds == 0 {
		c.Project.StopTimeoutSeconds = def.Project.StopTimeoutSeconds
	}
	if c.Services.PostgreSQL.Mode == "" {
		c.Services.PostgreSQL = def.Services.PostgreSQL
	}
	if c.Services.Redis.Mode == "" {
		c.Services.Redis = def.Services.Redis
	}
	// Managed services rely on the default endpoint, storage and secret refs.
	// A hand-edited config that only sets mode must still resolve the password
	// the manager generated and a usable data directory, otherwise status and
	// start fail with confusing errors.
	if c.Services.PostgreSQL.Mode == "managed" {
		pg, defPG := &c.Services.PostgreSQL, def.Services.PostgreSQL
		if pg.Host == "" {
			pg.Host = defPG.Host
		}
		if pg.Port == 0 {
			pg.Port = defPG.Port
		}
		if pg.AdminUser == "" {
			pg.AdminUser = defPG.AdminUser
		}
		if pg.AdminPasswordRef == "" {
			pg.AdminPasswordRef = defPG.AdminPasswordRef
		}
		if pg.DataDir == "" {
			pg.DataDir = defPG.DataDir
		}
	}
	if c.Services.Redis.Mode == "managed" {
		redis, defRedis := &c.Services.Redis, def.Services.Redis
		if redis.Host == "" {
			redis.Host = defRedis.Host
		}
		if redis.Port == 0 {
			redis.Port = defRedis.Port
		}
		if redis.PasswordRef == "" {
			redis.PasswordRef = defRedis.PasswordRef
		}
		if redis.DataDir == "" {
			redis.DataDir = defRedis.DataDir
		}
	}
	if c.Logging.Level == "" {
		c.Logging.Level = def.Logging.Level
	}
	if c.Logging.RetentionDays == 0 {
		c.Logging.RetentionDays = def.Logging.RetentionDays
	}
	if c.Logging.MaxFileMB == 0 {
		c.Logging.MaxFileMB = def.Logging.MaxFileMB
	}
	if c.Updates.Channel == "" {
		c.Updates.Channel = def.Updates.Channel
	}
}

// Validate checks invariants that must hold before the daemon starts.
func (c *Config) Validate() error {
	if c.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported config schema_version %d (this build supports %d)",
			c.SchemaVersion, CurrentSchemaVersion)
	}
	if !id.Valid(c.InstanceID) {
		return fmt.Errorf("config instance_id is not a valid ULID")
	}
	if !isLoopbackHost(c.Network.Host) {
		return fmt.Errorf("network.host must be a loopback address, got %q", c.Network.Host)
	}
	if c.Network.APIPort < 1 || c.Network.APIPort > 65535 {
		return fmt.Errorf("network.api_port must be between 1 and 65535")
	}
	if strings.TrimSpace(c.Runtime.DefaultProfile) == "" {
		return fmt.Errorf("runtime.default_profile must not be empty")
	}
	if !filepath.IsLocal(c.Runtime.MatrixFile) {
		return fmt.Errorf("runtime.matrix_file must be a relative path")
	}
	if strings.ContainsAny(c.Runtime.WSLDistro, "\x00\r\n") {
		return fmt.Errorf("runtime.wsl_distro must not contain control characters")
	}
	dataRoot := strings.TrimSpace(c.Storage.DataRoot)
	if strings.ContainsAny(dataRoot, "\x00\r\n") {
		return fmt.Errorf("storage.data_root must not contain control characters")
	}
	if dataRoot != "" && !isAbsoluteDataRoot(dataRoot) {
		return fmt.Errorf("storage.data_root must be an absolute path")
	}
	if strings.TrimSpace(c.Project.DefaultRepositoryURL) == "" {
		return fmt.Errorf("project.default_repository_url must not be empty")
	}
	if strings.ContainsAny(c.Project.DefaultRepositoryURL, "\x00\r\n") {
		return fmt.Errorf("project.default_repository_url must not contain control characters")
	}
	if strings.TrimSpace(c.Project.DefaultBranch) == "" {
		return fmt.Errorf("project.default_branch must not be empty")
	}
	if c.Project.StartTimeoutSeconds < 10 || c.Project.StartTimeoutSeconds > 3600 {
		return fmt.Errorf("project.start_timeout_seconds must be between 10 and 3600")
	}
	if c.Project.StopTimeoutSeconds < 3 || c.Project.StopTimeoutSeconds > 600 {
		return fmt.Errorf("project.stop_timeout_seconds must be between 3 and 600")
	}
	if err := validateService("postgresql", c.Services.PostgreSQL.Mode, c.Services.PostgreSQL.Host,
		c.Services.PostgreSQL.Port, c.Services.PostgreSQL.External); err != nil {
		return err
	}
	if err := validateService("redis", c.Services.Redis.Mode, c.Services.Redis.Host,
		c.Services.Redis.Port, c.Services.Redis.External); err != nil {
		return err
	}
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("logging.level %q is not supported", c.Logging.Level)
	}
	if c.Logging.RetentionDays < 0 {
		return fmt.Errorf("logging.retention_days must not be negative")
	}
	if c.Logging.MaxFileMB < 0 {
		return fmt.Errorf("logging.max_file_mb must not be negative")
	}
	return nil
}

func validateService(name, mode, host string, port int, external ExternalEndpoint) error {
	switch mode {
	case "managed":
		if !isLoopbackHost(host) {
			return fmt.Errorf("services.%s.host must be loopback in managed mode", name)
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("services.%s.port must be between 1 and 65535", name)
		}
	case "external":
		if strings.TrimSpace(external.Host) == "" {
			return fmt.Errorf("services.%s.external.host is required in external mode", name)
		}
		if external.Port < 1 || external.Port > 65535 {
			return fmt.Errorf("services.%s.external.port must be between 1 and 65535", name)
		}
	default:
		return fmt.Errorf("services.%s.mode must be managed or external", name)
	}
	return nil
}

func isAbsoluteDataRoot(value string) bool {
	if strings.HasPrefix(value, `\\`) {
		return true
	}
	return filepath.IsAbs(value)
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "127.0.0.1", "::1", "localhost":
		return true
	default:
		return false
	}
}
