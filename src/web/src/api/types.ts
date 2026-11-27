export type WorkspaceState =
  | 'EMPTY'
  | 'INITIALIZING'
  | 'READY'
  | 'STARTING'
  | 'RUNNING'
  | 'STOPPING'
  | 'STOPPED'
  | 'ERROR'

export interface VersionInfo {
  version: string
  commit: string
  build_date: string
  go_version: string
  os: string
  arch: string
}

export interface PlatformInfo {
  os: string
  arch: string
  arch_family: string
  distro?: string
  distro_version?: string
  is_wsl: boolean
}

export interface Info {
  version: VersionInfo
  instance_id: string
  root: string
  api_addr: string
  started_at: string
  platform: PlatformInfo
  active_workspace_id: string
  workspace_count: number
  capabilities: Record<string, boolean>
}

export interface SourceConfig {
  installed: boolean
  repository_url: string
  branch: string
  tag: string
  commit: string
  path: string
  shallow: boolean
  submodules: boolean
  auth_ref: string
}

export interface DatabaseConfig {
  mode: string
  database_name: string
  username: string
  password_ref: string
  provisioned: boolean
  provisioned_at?: string | null
  provision_error?: string
}

export interface RedisConfig {
  mode: string
  db_index: number
  key_prefix: string
}

export interface NetworkConfig {
  host: string
  http_port: number
}

export interface ProjectConfig {
  initialized: boolean
  process_state: string
  pid: number
  last_started_at: string | null
}

export interface Workspace {
  workspace_schema_version: number
  workspace_id: string
  name: string
  description: string
  project_type: string
  state: WorkspaceState
  last_error: string
  created_at: string
  updated_at: string
  source: SourceConfig
  runtime_profile: string
  database: DatabaseConfig
  redis: RedisConfig
  network: NetworkConfig
  project: ProjectConfig
}

export interface WorkspaceList {
  items: Workspace[]
  total: number
  active_workspace_id: string
}

export interface ActivateResponse {
  active_workspace_id: string
  workspace: Workspace
}

export interface WorkspaceCheck {
  name: string
  ok: boolean
  message?: string
}

export interface WorkspaceStatus {
  workspace_id: string
  state: WorkspaceState
  running: boolean
  checks: WorkspaceCheck[]
}

export interface WorkspaceStatusResponse {
  workspace: Workspace
  status: WorkspaceStatus
}

export interface CreateWorkspaceInput {
  name: string
  description?: string
  project_type?: string
}

export interface DiagnosticCheck {
  id: string
  title: string
  severity: 'error' | 'warning' | 'info'
  message: string
  details?: Record<string, unknown>
  fix_action?: string
  destructive: boolean
}

export interface DiagnosticsReport {
  generated_at: string
  items: DiagnosticCheck[]
  counts: Record<string, number>
}

export type TaskStatus = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled'

export interface Task {
  task_id: string
  type: string
  workspace_id?: string
  status: TaskStatus
  progress: number
  message: string
  error_code?: string
  error_message?: string
  created_at: string
  started_at?: string | null
  finished_at?: string | null
  cancelable: boolean
}

export interface DeleteResource {
  kind: string
  path?: string
  exists: boolean
  size_bytes: number
  shared: boolean
  note?: string
}

export interface DeletePlan {
  workspace_id: string
  name: string
  running: boolean
  total_bytes: number
  resources: DeleteResource[]
  confirm_token: string
  expires_at: string
}

export interface TaskAccepted {
  task_id: string
  status_url: string
  task: Task
}

export interface SessionResponse {
  csrf_token: string
  expires_at: string
}

export type ServiceName = 'postgresql' | 'redis'

export type ServiceState =
  | 'RUNNING'
  | 'STOPPED'
  | 'NOT_INSTALLED'
  | 'EXTERNAL'
  | 'UNREACHABLE'
  | 'ERROR'

export interface ServiceStatus {
  name: string
  mode: string
  state: ServiceState
  host: string
  port: number
  reachable: boolean
  authenticated: boolean
  version?: string
  execution?: string
  path?: string
  data_dir?: string
  log_file?: string
  pid?: number
  message?: string
  dependencies?: string[]
  checked_at: string
}

export interface ServiceList {
  items: ServiceStatus[]
  total: number
}

export interface ServiceActionResponse {
  status: ServiceStatus
}

export interface ServiceTestResult {
  name: string
  mode: string
  host: string
  port: number
  reachable: boolean
  authenticated: boolean
  latency_ms: number
  server_version?: string
  message?: string
  checked_at: string
}

export interface ServiceLogs {
  lines: string[]
  path?: string
  total: number
}

export interface ServiceConfigInput {
  mode: 'managed' | 'external'
  host: string
  port: number
  admin_user?: string
  username?: string
  password?: string
  database?: string
  tls?: boolean
}

export interface DatabasesResponse {
  items: string[]
  total: number
}

export interface RuntimeComponentSpec {
  version: string
  source: string
  sha256?: string
  binary?: string
  version_args?: string[]
  required?: boolean
}

export interface RuntimeMatrix {
  profile: string
  devkit_version_range?: string
  components: Record<string, RuntimeComponentSpec>
}

export interface RuntimeMatrixResponse {
  matrix: RuntimeMatrix
  builtin: boolean
}

export interface RuntimeComponent {
  name: string
  required: boolean
  expected_version: string
  source: string
  actual_version?: string
  path?: string
  execution?: string
  available: boolean
  version_compatible: boolean
  sha256?: string
  hash_verified: boolean
  verified: boolean
  message?: string
  checked_at: string
}

export interface RuntimeComponentsResponse {
  items: RuntimeComponent[]
  total: number
  profile?: string
  builtin?: boolean
}

export interface ProjectSourceStatus {
  configured: boolean
  installed: boolean
  is_git: boolean
  path: string
  repository_url?: string
  branch?: string
  tag?: string
  commit?: string
  head_commit?: string
  current_branch?: string
  shallow: boolean
  submodules: boolean
  dirty: boolean
  changed_files: number
  ahead: number
  behind: number
  last_sync_at?: string | null
  error?: string
  git_description?: string
}

export interface ProjectProcessStatus {
  initialized: boolean
  initialized_at?: string | null
  process_state: string
  pid: number
  alive: boolean
  http_url: string
  env_file: string
  log_file: string
  last_started_at?: string | null
  last_error?: string
}

export interface ProjectCheck {
  name: string
  ok: boolean
  message?: string
}

export interface ProjectStatus {
  workspace_id: string
  name: string
  source: ProjectSourceStatus
  process: ProjectProcessStatus
  checks: ProjectCheck[]
}

export interface ProjectResponse {
  workspace: Workspace
  project: ProjectStatus
}

export interface ProjectLogs {
  lines: string[]
  path?: string
  total: number
}

export interface ProjectSourceInput {
  repository_url?: string
  branch?: string
  tag?: string
  commit?: string
  shallow?: boolean
  submodules?: boolean
}

export interface ProjectCheckoutInput {
  ref: string
  force?: boolean
}

export interface ProjectInitInput {
  install_dependencies?: boolean
  prepare_database?: boolean
}

export interface BackupCreateInput {
  include_source?: boolean
  include_database?: boolean
}

export interface BackupManifestFile {
  path: string
  size: number
  sha256: string
}

export interface BackupManifest {
  format_version: number
  devkit_version: string
  workspace_schema_version: number
  workspace_id: string
  workspace_name: string
  created_at: string
  platform: string
  includes_source: boolean
  includes_database: boolean
  source_head_commit?: string
  database_name?: string
  project_initialized: boolean
  total_bytes: number
  file_count: number
  skipped_symlinks?: number
  excluded?: string[]
  warnings?: string[]
  files: BackupManifestFile[]
}

export interface Backup {
  id: string
  file_name: string
  path: string
  size_bytes: number
  created_at: string
  manifest?: BackupManifest
  verify_note?: string
}

export interface BackupList {
  items: Backup[]
  total: number
}

export interface RestoreAction {
  kind: string
  path: string
  size_bytes: number
  note?: string
}

export interface RestorePreview {
  backup_id: string
  workspace_id: string
  manifest: BackupManifest
  running: boolean
  will_replace: RestoreAction[]
  restore_database: boolean
  required_bytes: number
  free_bytes: number
  warnings?: string[]
  confirm_token: string
  expires_at: string
  uncompressed_size: number
}

export interface RestoreResult {
  backup_id: string
  workspace_id: string
  restored: string[]
  database_restored: boolean
  rollback_kept: boolean
}

export interface DevKitConfig {
  schema_version: number
  instance_id: string
  active_workspace_id: string
  workspaces_initialized: boolean
  created_at: string
  updated_at: string
  network: {
    host: string
    api_port: number
  }
  runtime: {
    default_profile: string
    matrix_file: string
    wsl_distro?: string
  }
  project: {
    default_repository_url: string
    default_branch: string
    start_timeout_seconds: number
    stop_timeout_seconds: number
  }
  services: unknown
  logging: {
    level: string
    retention_days: number
    max_file_mb: number
  }
  updates: {
    channel: string
    check_on_start: boolean
    auto_install: boolean
  }
}

export interface SettingsPaths {
  root: string
  config_file: string
  config_dir: string
  workspaces_dir: string
  runtime_dir: string
  services_dir: string
  logs_dir: string
  backups_dir: string
  tmp_dir: string
}

export interface SettingsResponse {
  config: DevKitConfig
  paths: SettingsPaths
  restart_required: string[]
}

export interface SettingsInput {
  network?: { api_port?: number }
  runtime?: {
    default_profile?: string
    matrix_file?: string
    wsl_distro?: string
  }
  project?: {
    default_repository_url?: string
    default_branch?: string
    start_timeout_seconds?: number
    stop_timeout_seconds?: number
  }
  logging?: {
    level?: string
    retention_days?: number
    max_file_mb?: number
  }
  updates?: {
    channel?: string
    check_on_start?: boolean
    auto_install?: boolean
  }
}

export interface LogSource {
  id: string
  label: string
  category: 'devkit' | 'workspace' | 'service'
  kind: string
  workspace_id?: string
  path: string
  exists: boolean
  size_bytes: number
  modified_at?: string | null
}

export interface LogSourcesResponse {
  items: LogSource[]
  total: number
}

export interface LogContent {
  source: string
  path: string
  lines: string[]
  total: number
  size_bytes: number
  modified_at?: string | null
}
