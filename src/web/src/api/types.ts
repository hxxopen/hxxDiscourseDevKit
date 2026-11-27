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

export interface StorageInfo {
  data_root: string
  configured: boolean
  split: boolean
  legacy: boolean
  data_root_pending: boolean
  migrating: boolean
  migration_task_id?: string
}

export interface StorageMigrationUnit {
  id: string
  kind: 'workspace' | 'service' | 'backup'
  name: string
  source: string
  destination: string
  action: 'copy' | 'skip' | 'blocked'
  reason?: string
  size_bytes: number
}

export interface StorageMigrationBlocker {
  code: string
  message: string
  path?: string
}

export interface StorageWorkspaceStop {
  workspace_id: string
  name: string
  state: string
}

export interface StorageServiceStop {
  name: string
  mode: string
  state: string
  action: string
  reason?: string
}

export interface StorageMigrationPlan {
  eligible: boolean
  reason?: string
  source_root: string
  source_data_root: string
  target_root: string
  distro?: string
  legacy: boolean
  units: StorageMigrationUnit[]
  workspaces: StorageWorkspaceStop[]
  services: StorageServiceStop[]
  warnings: string[]
  blockers: StorageMigrationBlocker[]
  total_bytes: number
  checked_at: string
  confirm_token?: string
  expires_at?: string
}

export interface Info {
  version: VersionInfo
  instance_id: string
  root: string
  storage: StorageInfo
  api_addr: string
  started_at: string
  platform: PlatformInfo
  active_workspace_id: string
  workspace_count: number
  capabilities: Record<string, boolean>
}

export interface ProfileOrigin {
  type: string
  name: string
  url?: string
  version?: string
  source_id?: string
  verified: boolean
}

export interface ProfileOperation {
  id: string
  title: string
  description: string
  destructive: boolean
}

export interface ProfilePermission {
  id: string
  title: string
  description: string
  risk: string
}

export interface ProjectProfile {
  id: string
  name: string
  description: string
  category: string
  project_type: string
  default_repository_url?: string
  default_branch?: string
  source_required: boolean
  extension_kind?: string
  runtime_components: string[]
  services: string[]
  operations: ProfileOperation[]
  permissions: ProfilePermission[]
  origin: ProfileOrigin
}

export interface ProfileList {
  items: ProjectProfile[]
  total: number
}

export interface ProfileSource {
  id: string
  kind: 'local' | 'git'
  name: string
  location: string
  branch?: string
  version?: string
  commit?: string
  profile_ids: string[]
  added_at: string
  last_loaded_at?: string | null
  last_error?: string
}

export interface ProfileSourceList {
  items: ProfileSource[]
  total: number
}

export interface ProfileSourceInput {
  location: string
  branch?: string
}

export type ProfileCheckStatus = 'ok' | 'info' | 'warning' | 'error'

export interface ProfileCheck {
  id: string
  title: string
  status: ProfileCheckStatus
  required: boolean
  message: string
  fix_action?: string
}

export interface ProfileCheckReport {
  profile_id: string
  profile_name: string
  generated_at: string
  checks: ProfileCheck[]
  ready: boolean
  error_count: number
  warning_count: number
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
  profile_id?: string
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
  profile_id?: string
  repository_url?: string
  branch?: string
  accept_profile?: boolean
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

export interface WSLStatus {
  supported: boolean
  available: boolean
  distro?: string
  distros?: string[]
  kernel?: string
  os_name?: string
  os_id?: string
  os_like?: string
  os_version?: string
  user?: string
  home?: string
  root_available: boolean
  error?: string
  checked_at: string
}

export interface RuntimeInstallComponent {
  name: string
  package: string
  required: boolean
  installed: boolean
  version?: string
  execution?: string
  message?: string
}

export interface RuntimeInstallCommand {
  label: string
  preview: string
}

export interface RuntimeInstallPlan {
  supported: boolean
  available: boolean
  distro?: string
  os_name?: string
  os_id?: string
  os_version?: string
  root_available: boolean
  components: RuntimeInstallComponent[]
  packages: string[]
  commands: RuntimeInstallCommand[]
  warnings?: string[]
  confirm_token?: string
  expires_at?: string
  checked_at: string
}

export interface RuntimeDiscoverResponse {
  components: Record<string, { path: string; execution: string; version?: string; detected_at: string }>
  names: string[]
  total: number
  updated_at: string
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

export type ExtensionKind = 'plugin' | 'theme'

export interface ExtensionCheck {
  name: string
  ok: boolean
  message?: string
}

export interface ExtensionItem {
  kind: ExtensionKind
  slug: string
  name: string
  about: string
  version: string
  authors: string
  url?: string
  path: string
  enabled: boolean
  has_metadata: boolean
  file_count: number
  modified_at: string
}

export interface ExtensionListing {
  workspace_id: string
  kind: ExtensionKind
  root: string
  source_installed: boolean
  checks: ExtensionCheck[]
  items: ExtensionItem[]
  total: number
}

export interface ExtensionCreateInput {
  name: string
  slug?: string
  about?: string
  version?: string
  authors?: string
}

export interface ExtensionPackage {
  kind: ExtensionKind
  slug: string
  name: string
  version: string
  file_name: string
  path: string
  size_bytes: number
  sha256: string
  file_count: number
  created_at: string
}

export type ExtensionFindingStatus = 'ok' | 'warning' | 'error' | 'skipped'

export interface ExtensionFinding {
  id: string
  title: string
  status: ExtensionFindingStatus
  message: string
  required: boolean
  fix?: string
}

export interface ExtensionCheckReport {
  workspace_id: string
  kind: ExtensionKind
  slug: string
  name: string
  path: string
  enabled: boolean
  ready: boolean
  source_installed: boolean
  discourse_version?: string
  required_version?: string
  maximum_version?: string
  error_count: number
  warning_count: number
  findings: ExtensionFinding[]
  checked_at: string
}

export interface ThemeBuildResult {
  ready: boolean
  message: string
  check: ExtensionCheckReport
  package?: ExtensionPackage
}

export interface ThemePreviewResult {
  build: ThemeBuildResult
  site_url?: string
  preview_url?: string
  admin_url?: string
  theme_id?: number
  action?: 'created' | 'updated'
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
  storage: {
    data_root?: string
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
  data_root: string
  data_root_split: boolean
  data_root_legacy: boolean
  data_root_pending: boolean
  config_file: string
  config_dir: string
  workspaces_dir: string
  profile_sources_file: string
  profile_sources_dir: string
  runtime_dir: string
  services_dir: string
  logs_dir: string
  audit_dir: string
  backups_dir: string
  tmp_dir: string
}

export type AuditResult = 'success' | 'accepted' | 'failure'

export interface AuditEvent {
  id: string
  timestamp: string
  action: string
  result: AuditResult
  status: number
  method: string
  path: string
  workspace_id?: string
  target?: string
  client: string
  actor: string
  request_id?: string
  remote_addr?: string
  duration_ms: number
}

export interface AuditList {
  items: AuditEvent[]
  total: number
  limit: number
}

export interface AuditFilter {
  limit?: number
  action?: string
  workspaceId?: string
  result?: AuditResult | ''
  since?: string
}

export interface SettingsResponse {
  config: DevKitConfig
  paths: SettingsPaths
  restart_required: string[]
  theme_preview: {
    api_key_configured: boolean
  }
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
