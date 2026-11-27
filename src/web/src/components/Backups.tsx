import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import { isTerminalTask, useTask } from '../api/hooks'
import type { Backup, BackupCreateInput } from '../api/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  Detail,
  EmptyState,
  ErrorNotice,
  Modal,
  ProgressBar,
  Skeleton,
  Spinner,
} from './ui'
import { errorMessage, formatBytes, formatDate } from '../lib/format'
import { taskLabels, taskTone } from '../lib/tasks'

type Notice = { tone: 'success' | 'error'; text: string }

export function BackupsCard({
  workspaceId,
  items,
  loading,
  error,
  running,
  onChanged,
  onNotice,
}: {
  workspaceId: string
  items: Backup[]
  loading: boolean
  error: unknown
  running: boolean
  onChanged: () => void
  onNotice: (notice: Notice) => void
}) {
  const queryClient = useQueryClient()
  const [includeSource, setIncludeSource] = useState(true)
  const [includeDatabase, setIncludeDatabase] = useState(true)
  const [restoreTarget, setRestoreTarget] = useState<Backup | null>(null)
  const [inspectTarget, setInspectTarget] = useState<Backup | null>(null)
  const [taskId, setTaskId] = useState<string | null>(null)
  const taskQuery = useTask(taskId)
  const task = taskQuery.data
  const taskRunning = Boolean(task && !isTerminalTask(task.status))

  const backupDone = useRef(false)
  useEffect(() => {
    if (task?.status === 'succeeded' && !backupDone.current) {
      backupDone.current = true
      onChanged()
      void queryClient.invalidateQueries({ queryKey: ['backups', workspaceId] })
    }
  }, [task?.status, onChanged, queryClient, workspaceId])

  const createMutation = useMutation({
    mutationFn: (input: BackupCreateInput) => api.createBackup(workspaceId, input),
    onSuccess: (accepted) => {
      backupDone.current = false
      setTaskId(accepted.task_id)
      onNotice({ tone: 'success', text: '已提交备份任务' })
    },
    onError: (createError) => onNotice({ tone: 'error', text: errorMessage(createError) }),
  })

  const deleteMutation = useMutation({
    mutationFn: (backupId: string) => api.deleteBackup(workspaceId, backupId),
    onSuccess: () => {
      onNotice({ tone: 'success', text: '备份已删除' })
      onChanged()
    },
    onError: (deleteError) => onNotice({ tone: 'error', text: errorMessage(deleteError) }),
  })

  return (
    <Card>
      <CardHeader
        title="备份与恢复"
        subtitle="备份仅在项目停止时创建；恢复需要先预览并二次确认。"
        action={
          <Badge tone={running ? 'amber' : 'slate'}>
            {running ? '运行中无法备份' : '可备份'}
          </Badge>
        }
      />
      <div className="space-y-4 px-5 py-4">
        <div className="flex flex-wrap items-center gap-4 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs text-slate-400">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4 accent-cyan-400"
              checked={includeSource}
              onChange={(event) => setIncludeSource(event.target.checked)}
            />
            包含源码
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4 accent-cyan-400"
              checked={includeDatabase}
              onChange={(event) => setIncludeDatabase(event.target.checked)}
            />
            包含数据库
          </label>
          <div className="ml-auto">
            <Button
              size="sm"
              onClick={() =>
                createMutation.mutate({
                  include_source: includeSource,
                  include_database: includeDatabase,
                })
              }
              disabled={running || createMutation.isPending || taskRunning}
            >
              {createMutation.isPending ? <Spinner /> : null}
              创建备份
            </Button>
          </div>
        </div>

        {taskId ? (
          <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
            <div className="flex items-center justify-between gap-3">
              <span className="text-sm font-medium text-slate-200">备份任务</span>
              <Badge tone={task ? taskTone(task.status) : 'slate'}>
                {task ? taskLabels[task.status] : '载入中'}
              </Badge>
            </div>
            <div className="mt-3">
              <ProgressBar value={task?.progress ?? 0} />
            </div>
            <p className="mt-2 text-xs text-slate-400">{task?.message ?? '正在提交任务…'}</p>
            {task?.error_message ? (
              <p className="mt-2 text-xs text-rose-300">{task.error_message}</p>
            ) : null}
          </div>
        ) : null}

        {error ? (
          <ErrorNotice error={error} />
        ) : loading ? (
          <Skeleton className="h-24 w-full" />
        ) : items.length === 0 ? (
          <EmptyState
            title="还没有备份"
            description="创建备份可以保存 Workspace 配置、数据，并可选包含源码与数据库。"
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                  <th className="px-3 py-2 font-medium">ID</th>
                  <th className="px-3 py-2 font-medium">创建时间</th>
                  <th className="px-3 py-2 font-medium">大小</th>
                  <th className="px-3 py-2 font-medium">内容</th>
                  <th className="px-3 py-2 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/70">
                {items.map((backup) => (
                  <tr key={backup.id}>
                    <td className="px-3 py-3 font-mono text-xs text-slate-300">{backup.id}</td>
                    <td className="whitespace-nowrap px-3 py-3 text-xs text-slate-400">
                      {formatDate(backup.created_at)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-3 text-xs text-slate-300">
                      {formatBytes(backup.size_bytes)}
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex flex-wrap gap-1.5">
                        <Badge tone="slate">配置/数据</Badge>
                        {backup.manifest?.includes_source ? <Badge tone="cyan">源码</Badge> : null}
                        {backup.manifest?.includes_database ? (
                          <Badge tone="emerald">数据库</Badge>
                        ) : null}
                      </div>
                    </td>
                    <td className="px-3 py-3">
                      <div className="flex items-center justify-end gap-1">
                        <Button variant="ghost" size="sm" onClick={() => setInspectTarget(backup)}>
                          详情
                        </Button>
                        <Button
                          variant="secondary"
                          size="sm"
                          onClick={() => setRestoreTarget(backup)}
                          disabled={running || taskRunning}
                        >
                          恢复
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-rose-300 hover:bg-rose-500/10 hover:text-rose-200"
                          disabled={deleteMutation.isPending}
                          onClick={() => {
                            if (window.confirm(`确认删除备份 ${backup.id}？此操作不可撤销。`)) {
                              deleteMutation.mutate(backup.id)
                            }
                          }}
                        >
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {restoreTarget ? (
        <RestoreModal
          workspaceId={workspaceId}
          backup={restoreTarget}
          onClose={() => setRestoreTarget(null)}
          onRestored={() => {
            setRestoreTarget(null)
            onNotice({ tone: 'success', text: `备份 ${restoreTarget.id} 已恢复` })
            onChanged()
          }}
        />
      ) : null}
      {inspectTarget ? (
        <InspectBackupModal
          workspaceId={workspaceId}
          backup={inspectTarget}
          onClose={() => setInspectTarget(null)}
        />
      ) : null}
    </Card>
  )
}

function RestoreModal({
  workspaceId,
  backup,
  onClose,
  onRestored,
}: {
  workspaceId: string
  backup: Backup
  onClose: () => void
  onRestored: () => void
}) {
  const [confirmed, setConfirmed] = useState(false)
  const [taskId, setTaskId] = useState<string | null>(null)
  const notified = useRef(false)

  const preview = useQuery({
    queryKey: ['restore-preview', workspaceId, backup.id],
    queryFn: () => api.previewRestore(workspaceId, backup.id),
    retry: false,
    staleTime: 0,
    gcTime: 0,
  })
  const taskQuery = useTask(taskId)
  const task = taskQuery.data
  const taskRunning = Boolean(task && !isTerminalTask(task.status))

  const mutation = useMutation({
    mutationFn: (token: string) => api.restoreBackup(workspaceId, backup.id, token),
    onSuccess: (accepted) => setTaskId(accepted.task_id),
  })

  const succeeded = task?.status === 'succeeded'
  useEffect(() => {
    if (succeeded && !notified.current) {
      notified.current = true
      onRestored()
    }
  }, [succeeded, onRestored])

  const data = preview.data
  const canRestore = Boolean(data) && !data?.running && confirmed && !mutation.isPending && !taskRunning

  return (
    <Modal
      open
      onClose={onClose}
      title={`恢复备份 ${backup.id}`}
      description="恢复会在临时目录中校验归档，失败时保留原始配置与数据。"
      width="max-w-3xl"
      footer={
        taskId ? (
          <Button onClick={onClose} disabled={taskRunning}>
            {succeeded ? '完成' : '关闭'}
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
              取消
            </Button>
            <Button
              variant="danger"
              onClick={() => data && mutation.mutate(data.confirm_token)}
              disabled={!canRestore}
            >
              {mutation.isPending ? <Spinner /> : null}
              确认恢复
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        <div className="rounded-xl border border-rose-500/25 bg-rose-500/5 px-4 py-3 text-sm leading-6 text-rose-200">
          恢复会替换当前 Workspace 的配置、数据与归档中包含的源码；数据库存在时也会被覆盖。
        </div>
        {preview.isLoading ? (
          <div className="flex items-center gap-3 rounded-xl border border-slate-800 px-4 py-6 text-sm text-slate-400">
            <Spinner />
            正在校验归档…
          </div>
        ) : preview.error ? (
          <ErrorNotice error={preview.error} onRetry={() => void preview.refetch()} />
        ) : data ? (
          <>
            <div className="grid gap-3 sm:grid-cols-3">
              <Detail label="解压大小" value={formatBytes(data.uncompressed_size)} />
              <Detail label="需要空间" value={formatBytes(data.required_bytes)} />
              <Detail label="可用空间" value={formatBytes(data.free_bytes)} />
            </div>
            {data.running ? (
              <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-200">
                Workspace 正在运行，请先停止项目后再恢复。
              </div>
            ) : null}
            {data.will_replace.length > 0 ? (
              <div className="overflow-hidden rounded-xl border border-slate-800">
                <table className="w-full text-left text-sm">
                  <thead className="bg-slate-950/60 text-[11px] uppercase tracking-wide text-slate-500">
                    <tr>
                      <th className="px-4 py-2.5 font-medium">类型</th>
                      <th className="px-4 py-2.5 font-medium">路径</th>
                      <th className="px-4 py-2.5 font-medium">大小</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-slate-800/70">
                    {data.will_replace.map((action) => (
                      <tr key={`${action.kind}-${action.path}`}>
                        <td className="whitespace-nowrap px-4 py-2.5 text-slate-200">{action.kind}</td>
                        <td className="max-w-[320px] truncate px-4 py-2.5 font-mono text-[11px] text-slate-400">
                          {action.path}
                        </td>
                        <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-slate-400">
                          {formatBytes(action.size_bytes)}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : null}
            {(data.warnings ?? []).map((warning) => (
              <p
                key={warning}
                className="rounded-lg border border-amber-400/25 bg-amber-400/5 px-3 py-2 text-xs text-amber-200"
              >
                {warning}
              </p>
            ))}
            <label className="flex items-start gap-3 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
              <input
                type="checkbox"
                className="mt-0.5 h-4 w-4 accent-rose-500"
                checked={confirmed}
                onChange={(event) => setConfirmed(event.target.checked)}
                disabled={data.running}
              />
              <span className="text-sm leading-5 text-slate-300">
                我确认恢复到备份 {backup.id}，并理解现有数据将被替换。
              </span>
            </label>
            {mutation.error ? <ErrorNotice error={mutation.error} /> : null}
            {taskId ? (
              <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm font-medium text-slate-200">恢复任务</span>
                  <Badge tone={task ? taskTone(task.status) : 'slate'}>
                    {task ? taskLabels[task.status] : '载入中'}
                  </Badge>
                </div>
                <div className="mt-3">
                  <ProgressBar value={task?.progress ?? 0} />
                </div>
                <p className="mt-2 text-xs text-slate-400">{task?.message ?? '正在提交任务…'}</p>
                {task?.error_message ? (
                  <p className="mt-2 text-xs text-rose-300">{task.error_message}</p>
                ) : null}
              </div>
            ) : null}
          </>
        ) : null}
      </div>
    </Modal>
  )
}

function InspectBackupModal({
  workspaceId,
  backup,
  onClose,
}: {
  workspaceId: string
  backup: Backup
  onClose: () => void
}) {
  const detail = useQuery({
    queryKey: ['backup', workspaceId, backup.id],
    queryFn: () => api.backup(workspaceId, backup.id),
  })
  const manifest = detail.data?.manifest
  return (
    <Modal
      open
      onClose={onClose}
      title={`备份 ${backup.id}`}
      description={backup.file_name}
      width="max-w-2xl"
      footer={<Button onClick={onClose}>关闭</Button>}
    >
      {detail.isLoading ? (
        <Skeleton className="h-40 w-full" />
      ) : detail.error ? (
        <ErrorNotice error={detail.error} />
      ) : manifest ? (
        <div className="space-y-4">
          <div className="grid gap-3 sm:grid-cols-2">
            <Detail label="创建时间" value={formatDate(manifest.created_at)} />
            <Detail label="DevKit 版本" value={manifest.devkit_version} />
            <Detail label="格式版本" value={String(manifest.format_version)} />
            <Detail label="文件数" value={String(manifest.file_count)} />
            <Detail label="原始大小" value={formatBytes(manifest.total_bytes)} />
            <Detail label="源码" value={manifest.includes_source ? '包含' : '不包含'} />
            <Detail label="数据库" value={manifest.includes_database ? manifest.database_name || '包含' : '不包含'} />
            <Detail label="源码提交" value={manifest.source_head_commit?.slice(0, 12) || '-'} />
          </div>
          {(manifest.warnings ?? []).map((warning) => (
            <p
              key={warning}
              className="rounded-lg border border-amber-400/25 bg-amber-400/5 px-3 py-2 text-xs text-amber-200"
            >
              {warning}
            </p>
          ))}
          <p className="text-xs text-slate-500">
            归档包含 {manifest.files?.length ?? 0} 个文件的 SHA-256 清单；恢复前会再次完整校验。
          </p>
        </div>
      ) : null}
    </Modal>
  )
}
