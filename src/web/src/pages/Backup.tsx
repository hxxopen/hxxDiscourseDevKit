import { useEffect, useMemo, useState } from 'react'
import { useBackups, useSettings, useWorkspaceStatus, useWorkspaces } from '../api/hooks'
import { BackupsCard } from '../components/Backups'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  Skeleton,
  Spinner,
  inputClass,
} from '../components/ui'
import { formatBytes, formatDate } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

export function Backup() {
  const workspaces = useWorkspaces()
  const settings = useSettings()
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)

  const list = workspaces.data?.items ?? []
  const activeId = workspaces.data?.active_workspace_id ?? ''
  const currentId = selectedId || activeId || list[0]?.workspace_id || ''

  useEffect(() => {
    if (!selectedId && activeId) {
      setSelectedId(activeId)
    }
  }, [activeId, selectedId])

  const backups = useBackups(currentId || null)
  const status = useWorkspaceStatus(currentId)
  const items = useMemo(
    () => [...(backups.data?.items ?? [])].sort((left, right) => right.created_at.localeCompare(left.created_at)),
    [backups.data],
  )
  const totalBytes = items.reduce((sum, item) => sum + item.size_bytes, 0)
  const latest = items[0]
  const running = status.data?.status.running ?? false
  const workspace = list.find((item) => item.workspace_id === currentId)

  const refresh = () => {
    void backups.refetch()
    void status.refetch()
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Backup &amp; Restore
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">备份与恢复</h1>
          <p className="mt-1 text-sm text-slate-400">
            为 Workspace 保存配置、数据与可选源码/数据库快照；恢复前会校验归档并二次确认。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            className={`${inputClass} w-56`}
            value={currentId}
            onChange={(event) => {
              setSelectedId(event.target.value)
              setNotice(null)
            }}
          >
            {list.length === 0 ? <option value="">（没有 Workspace）</option> : null}
            {list.map((item) => (
              <option key={item.workspace_id} value={item.workspace_id}>
                {item.name}
                {item.workspace_id === activeId ? '（活动）' : ''}
              </option>
            ))}
          </select>
          <Button variant="secondary" onClick={refresh} disabled={!currentId || backups.isFetching}>
            {backups.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
        </div>
      </div>

      {notice ? (
        <div
          className={`flex items-center justify-between gap-3 rounded-xl border px-4 py-3 text-sm ${
            notice.tone === 'success'
              ? 'border-emerald-400/30 bg-emerald-400/10 text-emerald-200'
              : 'border-rose-400/30 bg-rose-400/10 text-rose-200'
          }`}
        >
          <span>{notice.text}</span>
          <button
            type="button"
            onClick={() => setNotice(null)}
            className="rounded px-1 text-xs opacity-70 hover:opacity-100"
            aria-label="关闭提示"
          >
            ✕
          </button>
        </div>
      ) : null}

      {!currentId ? (
        <Card>
          <EmptyState
            title="没有可备份的 Workspace"
            description="先在 Workspaces 页面创建 Workspace，完成初始化后再创建备份。"
          />
        </Card>
      ) : (
        <>
          <div className="grid gap-4 md:grid-cols-3">
            <Card className="p-5">
              <p className="text-[11px] uppercase tracking-wide text-slate-500">Workspace</p>
              <p className="mt-2 truncate text-xl font-semibold text-slate-100">
                {workspace?.name ?? '—'}
              </p>
              <p className="mt-1 text-xs text-slate-400">
                {running ? '项目运行中，无法创建或恢复备份' : '项目已停止，可以创建或恢复备份'}
              </p>
            </Card>
            <Card className="p-5">
              <p className="text-[11px] uppercase tracking-wide text-slate-500">备份数量</p>
              <p className="mt-2 text-xl font-semibold text-slate-100">
                {backups.isLoading ? '—' : items.length}
              </p>
              <p className="mt-1 text-xs text-slate-400">合计 {formatBytes(totalBytes)}</p>
            </Card>
            <Card className="p-5">
              <p className="text-[11px] uppercase tracking-wide text-slate-500">最近备份</p>
              <p className="mt-2 text-sm font-medium text-slate-100">
                {latest ? latest.id : '尚无备份'}
              </p>
              <p className="mt-1 text-xs text-slate-400">
                {latest ? formatDate(latest.created_at) : '创建后会显示在这里'}
              </p>
            </Card>
          </div>

          {status.isLoading && backups.isLoading ? (
            <Skeleton className="h-64 w-full" />
          ) : (
            <BackupsCard
              workspaceId={currentId}
              items={items}
              loading={backups.isLoading}
              error={backups.error}
              running={running}
              onChanged={() => {
                void backups.refetch()
                void status.refetch()
              }}
              onNotice={setNotice}
            />
          )}

          <Card>
            <CardHeader
              title="备份策略"
              subtitle="归档包含 manifest 与每个文件的 SHA-256；恢复失败会回滚到临时目录之前的校验结果。"
              action={<Badge tone={running ? 'amber' : 'slate'}>{running ? '运行中' : '可操作'}</Badge>}
            />
            <div className="space-y-3 px-5 py-4 text-sm leading-6 text-slate-400">
              <p>
                备份保存位置：
                <code className="ml-1 break-all font-mono text-xs text-slate-300">
                  {settings.data
                    ? `${settings.data.paths.workspaces_dir}/${currentId}/backups`
                    : `workspaces/<ID>/backups/`}
                </code>
              </p>
              <p>
                备份默认包含 Workspace 配置与数据；勾选“包含源码”会把已安装的源码目录一并归档，勾选“包含数据库”会在项目停止时导出
                PostgreSQL 数据库。恢复需要先在详情中查看清单，再经过预览与一次性确认令牌。
              </p>
            </div>
          </Card>
        </>
      )}
    </div>
  )
}
