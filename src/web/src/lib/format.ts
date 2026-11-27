import type { ServiceState, WorkspaceState } from '../api/types'

export function formatDate(value?: string | null): string {
  if (!value) {
    return '-'
  }
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(date)
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) {
    return '0 B'
  }
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1
  return `${value.toFixed(digits)} ${units[unit]}`
}

export function formatUptime(startedAt?: string): string {
  if (!startedAt) {
    return '-'
  }
  const start = new Date(startedAt).getTime()
  if (Number.isNaN(start)) {
    return '-'
  }
  const seconds = Math.max(0, Math.floor((Date.now() - start) / 1000))
  if (seconds < 60) {
    return `${seconds} 秒`
  }
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) {
    return `${minutes} 分钟`
  }
  const hours = Math.floor(minutes / 60)
  if (hours < 24) {
    return `${hours} 小时 ${minutes % 60} 分钟`
  }
  const days = Math.floor(hours / 24)
  return `${days} 天 ${hours % 24} 小时`
}

const stateLabels: Record<WorkspaceState, string> = {
  EMPTY: '待初始化',
  INITIALIZING: '初始化中',
  READY: '就绪',
  STARTING: '启动中',
  RUNNING: '运行中',
  STOPPING: '停止中',
  STOPPED: '已停止',
  ERROR: '错误',
}

export function stateLabel(state: WorkspaceState): string {
  return stateLabels[state] ?? state
}

export function stateClasses(state: WorkspaceState): string {
  switch (state) {
    case 'READY':
      return 'border-emerald-400/30 bg-emerald-400/10 text-emerald-300'
    case 'RUNNING':
      return 'border-cyan-400/30 bg-cyan-400/10 text-cyan-300'
    case 'STARTING':
    case 'STOPPING':
    case 'INITIALIZING':
      return 'border-amber-400/30 bg-amber-400/10 text-amber-300'
    case 'ERROR':
      return 'border-rose-400/30 bg-rose-400/10 text-rose-300'
    case 'STOPPED':
      return 'border-slate-500/30 bg-slate-500/10 text-slate-300'
    default:
      return 'border-slate-600/40 bg-slate-700/20 text-slate-300'
  }
}

const serviceStateLabels: Record<ServiceState, string> = {
  RUNNING: '运行中',
  STOPPED: '已停止',
  NOT_INSTALLED: '未安装',
  EXTERNAL: '外部服务',
  UNREACHABLE: '不可达',
  ERROR: '错误',
}

export function serviceStateLabel(state: ServiceState): string {
  return serviceStateLabels[state] ?? state
}

export function serviceStateTone(
  state: ServiceState,
): 'slate' | 'cyan' | 'emerald' | 'amber' | 'rose' | 'sky' {
  switch (state) {
    case 'RUNNING':
      return 'emerald'
    case 'EXTERNAL':
      return 'sky'
    case 'STOPPED':
      return 'slate'
    case 'UNREACHABLE':
      return 'amber'
    case 'NOT_INSTALLED':
    case 'ERROR':
      return 'rose'
    default:
      return 'slate'
  }
}

export function errorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message
  }
  if (typeof error === 'string') {
    return error
  }
  return '发生未知错误'
}
