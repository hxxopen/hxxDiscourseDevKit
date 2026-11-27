import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { isTerminalTask, useInfo, useSettings, useTask } from '../api/hooks'
import type { StorageMigrationUnit } from '../api/types'
import { Badge, Button, ErrorNotice, Modal, ProgressBar, Spinner } from './ui'
import { formatBytes } from '../lib/format'

const refreshAfterMigration = [
  ['settings'],
  ['info'],
  ['services'],
  ['service'],
  ['workspaces'],
  ['workspace-status'],
  ['project'],
  ['diagnostics'],
  ['runtime-components'],
  ['runtime-matrix'],
  ['backups'],
]

function unitActionLabel(action: StorageMigrationUnit['action']): string {
  switch (action) {
    case 'copy':
      return '复制'
    case 'skip':
      return '跳过'
    default:
      return '阻塞'
  }
}

function unitActionTone(action: StorageMigrationUnit['action']) {
  switch (action) {
    case 'copy':
      return 'cyan' as const
    case 'skip':
      return 'slate' as const
    default:
      return 'rose' as const
  }
}

function unitKindLabel(kind: StorageMigrationUnit['kind']): string {
  switch (kind) {
    case 'workspace':
      return 'Workspace'
    case 'service':
      return '服务'
    default:
      return '备份'
  }
}

function NoticeList({
  title,
  items,
  tone,
}: {
  title: string
  items: string[]
  tone: 'amber' | 'rose'
}) {
  if (items.length === 0) {
    return null
  }
  const palette =
    tone === 'amber'
      ? 'border-amber-400/30 bg-amber-400/10 text-amber-100'
      : 'border-rose-500/30 bg-rose-500/10 text-rose-100'
  return (
    <div className={`rounded-xl border px-4 py-3 text-xs leading-6 ${palette}`}>
      <p className="font-medium">{title}</p>
      <ul className="mt-1 list-disc space-y-1 pl-5">
        {items.map((item, index) => (
          <li key={`${index}-${item}`}>{item}</li>
        ))}
      </ul>
    </div>
  )
}

export function StorageMigrationCard({ compact = false }: { compact?: boolean }) {
  const queryClient = useQueryClient()
  const info = useInfo()
  const settings = useSettings()
  const [open, setOpen] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const [activeTaskId, setActiveTaskId] = useState<string | null>(null)

  const pending = Boolean(
    info.data?.storage.data_root_pending || settings.data?.paths.data_root_pending,
  )
  const legacy = Boolean(
    info.data?.storage.legacy || settings.data?.paths.data_root_legacy,
  )

  const taskId = activeTaskId ?? info.data?.storage.migration_task_id ?? null
  const task = useTask(taskId)
  const taskStatus = task.data?.status
  const taskRunning = Boolean(taskId) && !(taskStatus && isTerminalTask(taskStatus))
  const busy = taskRunning || Boolean(info.data?.storage.migrating)

  const preview = useQuery({
    queryKey: ['storage-migration-preview'],
    queryFn: api.previewStorageMigration,
    enabled: open && !busy,
    staleTime: 0,
    gcTime: 0,
    retry: false,
    refetchOnMount: 'always',
  })

  const start = useMutation({
    mutationFn: (token: string) => api.startStorageMigration(token),
    onSuccess: (accepted) => {
      setActiveTaskId(accepted.task_id)
      setConfirmed(false)
      void queryClient.invalidateQueries({ queryKey: ['info'] })
      void queryClient.invalidateQueries({ queryKey: ['tasks'] })
    },
  })

  const cancel = useMutation({
    mutationFn: (id: string) => api.cancelTask(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['task', taskId] })
      void queryClient.invalidateQueries({ queryKey: ['info'] })
    },
  })

  const handledTask = useRef('')
  useEffect(() => {
    if (!task.data || !isTerminalTask(task.data.status)) {
      return
    }
    if (handledTask.current === task.data.task_id) {
      return
    }
    handledTask.current = task.data.task_id
    for (const queryKey of refreshAfterMigration) {
      void queryClient.invalidateQueries({ queryKey })
    }
    void info.refetch()
    void settings.refetch()
  }, [task.data, queryClient])

  const close = () => {
    if (busy) {
      return
    }
    setOpen(false)
    setConfirmed(false)
    setActiveTaskId(null)
  }

  const openDialog = () => {
    setConfirmed(false)
    setActiveTaskId(null)
    start.reset()
    setOpen(true)
  }

  if (!pending && !legacy && !busy) {
    return null
  }

  const plan = preview.data
  const target = settings.data?.config.storage?.data_root || plan?.target_root || ''
  const blockers = plan?.blockers ?? []
  const canSubmit = Boolean(
    plan?.eligible &&
      plan.confirm_token &&
      blockers.length === 0 &&
      confirmed &&
      !busy &&
      !start.isPending,
  )

  const taskFailed =
    !pending &&
    taskStatus !== undefined &&
    (taskStatus === 'failed' || taskStatus === 'canceled')

  const actionLabel = busy ? '迁移进行中' : '一键迁移到 WSL2 数据目录'

  return (
    <div
      data-testid="storage-migration-card"
      className={`rounded-xl border px-4 py-3 text-sm leading-6 ${
        pending
          ? 'border-emerald-400/30 bg-emerald-400/10 text-emerald-100'
          : 'border-amber-400/30 bg-amber-400/10 text-amber-100'
      }`}
    >
      {pending ? (
        <>
          <p data-testid="storage-migration-pending" className="font-medium">
            数据目录迁移已完成，请重启 devkitd
          </p>
          <p className="mt-1 text-xs leading-6 text-emerald-100/90">
            重启后实例将切换到 WSL2 数据目录
            {target ? (
              <>
                ：<code className="break-all">{target}</code>
              </>
            ) : null}
            。旧数据仍保留在原位置，确认新目录工作正常后可自行清理。重启后再启动 PostgreSQL /
            Redis 服务。
          </p>
        </>
      ) : (
        <>
          <p className="font-medium">当前实例仍使用旧的 Windows 数据目录</p>
          <p className="mt-1 text-xs leading-6 text-amber-100/90">
            托管 PostgreSQL 需要在 WSL2 文件系统中初始化数据目录。可以一键把
            <code className="mx-1">workspaces/</code>、
            <code className="mx-1">services/</code>、
            <code className="mx-1">backups/</code>
            迁移到 WSL2 数据目录；迁移前会停止运行中的 Workspace 与托管服务，旧数据不会自动删除。
          </p>
          <p className="mt-1 text-xs leading-6 text-amber-100/90">
            迁移完成后需要手动重启 devkitd 才会切换数据目录。
          </p>
        </>
      )}

      {busy ? (
        <div className="mt-3 space-y-2" data-testid="storage-migration-progress">
          <div className="flex items-center gap-2 text-xs">
            <Badge tone="amber">迁移中</Badge>
            <span>{task.data?.message || '正在执行迁移任务…'}</span>
          </div>
          <ProgressBar value={task.data?.progress ?? 0} />
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs text-amber-100/80">
              进度 {task.data?.progress ?? 0}%
            </span>
            {taskId ? (
              <Button
                variant="secondary"
                size="sm"
                data-testid="storage-migration-cancel"
                onClick={() => cancel.mutate(taskId)}
                disabled={cancel.isPending || taskStatus === 'canceled'}
              >
                {cancel.isPending ? <Spinner className="h-3 w-3" /> : null}
                取消迁移
              </Button>
            ) : null}
          </div>
          {cancel.error ? <ErrorNotice error={cancel.error} /> : null}
        </div>
      ) : null}

      {!busy && !pending ? (
        <div className="mt-3 flex flex-wrap items-center gap-2">
          <Button
            variant="primary"
            size="sm"
            data-testid="storage-migration-open"
            onClick={openDialog}
            disabled={!info.data?.capabilities?.storage_migration}
          >
            一键迁移到 WSL2 数据目录
          </Button>
          {info.data && !info.data.capabilities?.storage_migration ? (
            <span className="text-xs text-amber-100/80">当前构建未启用数据目录迁移</span>
          ) : null}
        </div>
      ) : null}

      <Modal
        open={open}
        onClose={close}
        title="迁移到 WSL2 数据目录"
        description={
          compact
            ? '迁移会停止运行中的 Workspace 和托管服务，复制完成后需要重启 devkitd。'
            : '先预览迁移计划，确认后停止服务并复制数据。旧数据保留，不会自动删除。'
        }
        width="max-w-3xl"
        footer={
          <>
            {busy ? (
              <Button variant="secondary" onClick={close} disabled>
                迁移进行中
              </Button>
            ) : (
              <Button variant="ghost" onClick={close}>
                关闭
              </Button>
            )}
            {busy && taskId ? (
              <Button
                variant="danger"
                data-testid="storage-migration-cancel"
                onClick={() => cancel.mutate(taskId)}
                disabled={cancel.isPending || taskStatus === 'canceled'}
              >
                {cancel.isPending ? <Spinner /> : null}
                取消迁移
              </Button>
            ) : null}
            {!busy && !pending ? (
              <Button
                variant="primary"
                data-testid="storage-migration-submit"
                onClick={() => {
                  if (plan?.confirm_token) {
                    start.mutate(plan.confirm_token)
                  }
                }}
                disabled={!canSubmit}
              >
                {start.isPending ? <Spinner /> : null}
                {actionLabel}
              </Button>
            ) : null}
          </>
        }
      >
        <div data-testid="storage-migration-dialog" className="space-y-4">
          {pending ? (
            <div
              data-testid="storage-migration-success"
              className="rounded-xl border border-emerald-400/30 bg-emerald-400/10 px-4 py-4 text-sm leading-6 text-emerald-100"
            >
              <p className="font-medium">迁移已完成，配置已更新</p>
              <p className="mt-2 text-xs leading-6">
                新的数据目录：
                <code className="break-all">{target || '见设置页'}</code>
              </p>
              <p className="mt-2 text-xs leading-6">
                现在请手动重启 devkitd。重启后实例会使用 WSL2 数据目录，旧目录不会被自动删除；确认无误后可自行清理。
              </p>
            </div>
          ) : null}

          {busy ? (
            <div className="space-y-3">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone="amber">迁移中</Badge>
                <span className="text-sm text-slate-200">
                  {task.data?.message || '正在执行迁移任务…'}
                </span>
              </div>
              <ProgressBar value={task.data?.progress ?? 0} />
              <p className="text-xs text-slate-400">
                {task.data?.progress ?? 0}% · 迁移期间其他写操作会被暂停，可随时取消。
              </p>
              {task.data?.error_message ? (
                <ErrorNotice error={new Error(task.data.error_message)} />
              ) : null}
            </div>
          ) : null}

          {!busy && !pending ? (
            <>
              {preview.isPending ? (
                <div className="flex items-center gap-2 text-sm text-slate-300">
                  <Spinner />
                  正在生成迁移计划…
                </div>
              ) : null}

              {preview.error ? (
                <ErrorNotice error={preview.error} onRetry={() => void preview.refetch()} />
              ) : null}

              {start.error ? <ErrorNotice error={start.error} /> : null}

              {taskFailed ? (
                <div className="space-y-3">
                  <ErrorNotice
                    error={
                      new Error(
                        task.data?.error_message ||
                          task.data?.message ||
                          (taskStatus === 'canceled' ? '迁移已取消' : '迁移任务失败'),
                      )
                    }
                  />
                  <Button variant="secondary" size="sm" onClick={() => void preview.refetch()}>
                    重新预览
                  </Button>
                </div>
              ) : null}

              {plan && !plan.eligible ? (
                <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100">
                  <p className="font-medium">当前实例暂不可迁移</p>
                  <p className="mt-1 text-xs leading-6">{plan.reason || '迁移条件不满足'}</p>
                </div>
              ) : null}

              {plan?.eligible ? (
                <>
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-wide text-slate-500">源数据目录</p>
                      <p className="mt-1 break-all font-mono text-xs text-slate-200">
                        {plan.source_data_root}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-wide text-slate-500">目标数据目录</p>
                      <p className="mt-1 break-all font-mono text-xs text-slate-200">
                        {plan.target_root}
                      </p>
                    </div>
                    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-wide text-slate-500">WSL 发行版</p>
                      <p className="mt-1 text-sm text-slate-200">{plan.distro || '-'}</p>
                    </div>
                    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                      <p className="text-[11px] uppercase tracking-wide text-slate-500">预计复制</p>
                      <p className="mt-1 text-sm text-slate-200">{formatBytes(plan.total_bytes)}</p>
                    </div>
                  </div>

                  <div>
                    <p className="text-xs font-medium text-slate-300">迁移单元</p>
                    {plan.units.length === 0 ? (
                      <p className="mt-2 text-xs text-slate-500">没有需要复制的数据。</p>
                    ) : (
                      <div className="mt-2 space-y-2">
                        {plan.units.map((unit) => (
                          <div
                            key={unit.id}
                            data-testid="storage-migration-unit"
                            className="flex flex-wrap items-center justify-between gap-2 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-2"
                          >
                            <div className="min-w-0">
                              <p className="truncate text-xs text-slate-200">
                                <span className="text-slate-500">{unitKindLabel(unit.kind)}</span>{' '}
                                {unit.name}
                              </p>
                              {unit.reason ? (
                                <p className="mt-0.5 text-[11px] text-slate-500">{unit.reason}</p>
                              ) : null}
                            </div>
                            <div className="flex items-center gap-2">
                              <span className="text-[11px] text-slate-500">
                                {unit.action === 'copy' ? formatBytes(unit.size_bytes) : ''}
                              </span>
                              <Badge tone={unitActionTone(unit.action)}>
                                {unitActionLabel(unit.action)}
                              </Badge>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>

                  {plan.workspaces.length > 0 || plan.services.length > 0 ? (
                    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                      <p className="text-xs font-medium text-slate-300">迁移前停止</p>
                      <div className="mt-1 space-y-1 text-xs text-slate-300">
                        {plan.workspaces.map((ws) => (
                          <p key={ws.workspace_id}>
                            Workspace <span className="text-slate-100">{ws.name}</span>
                            <span className="text-slate-500">（{ws.state}）</span>
                          </p>
                        ))}
                        {plan.services
                          .filter((svc) => svc.action === 'stop')
                          .map((svc) => (
                            <p key={svc.name}>
                              服务 <span className="text-slate-100">{svc.name}</span>
                              <span className="text-slate-500">（{svc.state || svc.mode}）</span>
                            </p>
                          ))}
                      </div>
                    </div>
                  ) : null}

                  <NoticeList title="提示" items={plan.warnings} tone="amber" />
                  <NoticeList
                    title="阻塞项（需要先处理）"
                    items={blockers.map((blocker) =>
                      blocker.path ? `${blocker.message}（${blocker.path}）` : blocker.message,
                    )}
                    tone="rose"
                  />

                  <label className="flex items-start gap-2 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs leading-5 text-slate-300">
                    <input
                      type="checkbox"
                      data-testid="storage-migration-confirm"
                      className="mt-0.5 h-4 w-4 rounded border-slate-600 bg-slate-900 text-cyan-400"
                      checked={confirmed}
                      onChange={(event) => setConfirmed(event.target.checked)}
                    />
                    <span>
                      我已了解迁移会停止运行中的 Workspace 和托管服务，复制期间其他写操作暂停；旧数据保留，不会自动删除；迁移完成后需要手动重启 devkitd。
                    </span>
                  </label>
                </>
              ) : null}
            </>
          ) : null}
        </div>
      </Modal>
    </div>
  )
}
