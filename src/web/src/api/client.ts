import type {
  ActivateResponse,
  Backup,
  BackupCreateInput,
  BackupList,
  CreateWorkspaceInput,
  DatabasesResponse,
  DeletePlan,
  DiagnosticsReport,
  Info,
  LogContent,
  LogSourcesResponse,
  ProjectCheckoutInput,
  ProjectInitInput,
  ProjectLogs,
  ProjectResponse,
  ProjectSourceInput,
  ProjectStatus,
  RestorePreview,
  RuntimeComponent,
  RuntimeComponentsResponse,
  RuntimeMatrixResponse,
  ServiceActionResponse,
  ServiceConfigInput,
  ServiceList,
  ServiceLogs,
  ServiceName,
  ServiceStatus,
  ServiceTestResult,
  SessionResponse,
  SettingsInput,
  SettingsResponse,
  Task,
  TaskAccepted,
  Workspace,
  WorkspaceList,
  WorkspaceStatusResponse,
} from './types'

const API_BASE = '/api/v1'

let csrfToken = ''

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId?: string

  constructor(status: number, code: string, message: string, requestId?: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
  }
}

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
    request_id?: string
  }
}

async function parsePayload(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) {
    return null
  }
  try {
    return JSON.parse(text) as unknown
  } catch {
    return null
  }
}

export async function ensureSession(): Promise<SessionResponse> {
  const response = await fetch(`${API_BASE}/system/session`, {
    method: 'GET',
    headers: { Accept: 'application/json' },
    credentials: 'same-origin',
  })
  const payload = await parsePayload(response)
  if (!response.ok) {
    const envelope = payload as ErrorEnvelope | null
    throw new ApiError(
      response.status,
      envelope?.error?.code ?? 'SESSION_FAILED',
      envelope?.error?.message ?? '无法建立管理会话',
      envelope?.error?.request_id,
    )
  }
  const session = payload as SessionResponse
  csrfToken = session.csrf_token
  return session
}

function isMutating(method: string): boolean {
  return method !== 'GET' && method !== 'HEAD' && method !== 'OPTIONS'
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  retried = false,
): Promise<T> {
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
  }
  if (isMutating(method) && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }

  const response = await fetch(`${API_BASE}${path}`, {
    method,
    headers,
    credentials: 'same-origin',
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  const payload = await parsePayload(response)

  if (response.status === 401 && !retried) {
    await ensureSession()
    return request<T>(method, path, body, true)
  }
  if (!response.ok) {
    const envelope = payload as ErrorEnvelope | null
    throw new ApiError(
      response.status,
      envelope?.error?.code ?? 'HTTP_ERROR',
      envelope?.error?.message ?? response.statusText ?? '请求失败',
      envelope?.error?.request_id,
    )
  }
  return payload as T
}

function workspacePath(id: string): string {
  return `/workspaces/${encodeURIComponent(id)}`
}

function servicePath(name: ServiceName): string {
  return `/services/${encodeURIComponent(name)}`
}

export const api = {
  info: () => request<Info>('GET', '/system/info'),

  diagnostics: () => request<DiagnosticsReport>('GET', '/diagnostics'),

  settings: () => request<SettingsResponse>('GET', '/settings'),

  updateSettings: (input: SettingsInput) =>
    request<SettingsResponse>('PATCH', '/settings', input),

  logSources: () => request<LogSourcesResponse>('GET', '/logs/sources'),

  log: (source: string, tail = 200) =>
    request<LogContent>(
      'GET',
      `/logs?source=${encodeURIComponent(source)}&tail=${tail}`,
    ),

  workspaces: () => request<WorkspaceList>('GET', '/workspaces'),

  createWorkspace: (input: CreateWorkspaceInput) =>
    request<Workspace>('POST', '/workspaces', input),

  activateWorkspace: (id: string) =>
    request<ActivateResponse>('POST', `${workspacePath(id)}/activate`),

  workspaceStatus: (id: string) =>
    request<WorkspaceStatusResponse>('GET', `${workspacePath(id)}/status`),

  provisionWorkspace: (id: string) =>
    request<Workspace>('POST', `${workspacePath(id)}/provision`),

  previewDelete: (id: string) =>
    request<DeletePlan>('DELETE', `${workspacePath(id)}/preview`),

  deleteWorkspace: (id: string, confirmToken: string, mode: 'record' | 'purge') => {
    const params = new URLSearchParams({ confirm_token: confirmToken })
    if (mode === 'purge') {
      params.set('mode', 'purge')
    }
    return request<TaskAccepted>('DELETE', `${workspacePath(id)}?${params.toString()}`)
  },

  project: (id: string) =>
    request<ProjectResponse>('GET', `${workspacePath(id)}/project`),

  projectStatus: (id: string) =>
    request<ProjectStatus>('GET', `${workspacePath(id)}/project/status`),

  updateProjectSource: (id: string, input: ProjectSourceInput) =>
    request<Workspace>('PUT', `${workspacePath(id)}/project/source`, input),

  cloneProject: (id: string) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/project/clone`, {}),

  fetchProject: (id: string) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/project/fetch`),

  pullProject: (id: string, force = false) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/project/pull`, { force }),

  checkoutProject: (id: string, input: ProjectCheckoutInput) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/project/checkout`, input),

  initProject: (id: string, input: ProjectInitInput) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/project/init`, input),

  startProject: (id: string) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/start`),

  stopProject: (id: string) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/stop`),

  restartProject: (id: string) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/restart`),

  projectLogs: (id: string, source = 'project', tail = 200) =>
    request<ProjectLogs>(
      'GET',
      `${workspacePath(id)}/logs?source=${encodeURIComponent(source)}&tail=${tail}`,
    ),

  backups: (id: string) =>
    request<BackupList>('GET', `${workspacePath(id)}/backups`),

  createBackup: (id: string, input: BackupCreateInput) =>
    request<TaskAccepted>('POST', `${workspacePath(id)}/backups`, input),

  backup: (id: string, backupId: string, verify = false) =>
    request<Backup>(
      'GET',
      `${workspacePath(id)}/backups/${encodeURIComponent(backupId)}${verify ? '?verify=true' : ''}`,
    ),

  deleteBackup: (id: string, backupId: string) =>
    request<null>('DELETE', `${workspacePath(id)}/backups/${encodeURIComponent(backupId)}`),

  previewRestore: (id: string, backupId: string) =>
    request<RestorePreview>(
      'POST',
      `${workspacePath(id)}/backups/${encodeURIComponent(backupId)}/restore-preview`,
    ),

  restoreBackup: (id: string, backupId: string, confirmToken: string) =>
    request<TaskAccepted>(
      'POST',
      `${workspacePath(id)}/backups/${encodeURIComponent(backupId)}/restore`,
      { confirm_token: confirmToken },
    ),

  task: (id: string) => request<Task>('GET', `/tasks/${encodeURIComponent(id)}`),

  cancelTask: (id: string) =>
    request<Task>('POST', `/tasks/${encodeURIComponent(id)}/cancel`),

  runtimeMatrix: () => request<RuntimeMatrixResponse>('GET', '/runtime/matrix'),

  runtimeComponents: () =>
    request<RuntimeComponentsResponse>('GET', '/runtime/components'),

  verifyRuntime: (name: string) =>
    request<RuntimeComponent>('GET', `/runtime/components/${encodeURIComponent(name)}`),

  verifyAllRuntime: () =>
    request<RuntimeComponentsResponse>('POST', '/runtime/verify'),

  services: () => request<ServiceList>('GET', '/services'),

  service: (name: ServiceName) => request<ServiceStatus>('GET', servicePath(name)),

  startService: (name: ServiceName) =>
    request<ServiceActionResponse>('POST', `${servicePath(name)}/start`),

  stopService: (name: ServiceName) =>
    request<ServiceActionResponse>('POST', `${servicePath(name)}/stop`),

  restartService: (name: ServiceName) =>
    request<ServiceActionResponse>('POST', `${servicePath(name)}/restart`),

  testService: (name: ServiceName, input?: ServiceConfigInput) =>
    request<ServiceTestResult>('POST', `${servicePath(name)}/test`, input),

  updateService: (name: ServiceName, input: ServiceConfigInput) =>
    request<ServiceStatus>('PATCH', servicePath(name), input),

  serviceLogs: (name: ServiceName, tail = 200) =>
    request<ServiceLogs>('GET', `${servicePath(name)}/logs?tail=${tail}`),

  serviceDatabases: () => request<DatabasesResponse>('GET', '/services/postgresql/databases'),
}
