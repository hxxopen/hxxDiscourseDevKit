import type { TaskStatus } from '../api/types'

export function taskTone(
  status: TaskStatus,
): 'slate' | 'cyan' | 'emerald' | 'rose' | 'amber' {
  switch (status) {
    case 'succeeded':
      return 'emerald'
    case 'failed':
      return 'rose'
    case 'canceled':
      return 'amber'
    case 'running':
      return 'cyan'
    default:
      return 'slate'
  }
}

export const taskLabels: Record<TaskStatus, string> = {
  queued: '排队中',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
  canceled: '已取消',
}
