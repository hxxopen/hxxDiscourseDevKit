import { useMemo, useState } from 'react'
import { useAudit, useWorkspaces } from '../api/hooks'
import type { AuditEvent, AuditResult } from '../api/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorNotice,
  Skeleton,
  Spinner,
  inputClass,
} from '../components/ui'
import { formatDate } from '../lib/format'

const actionLabels: Record<string, string> = {
  'workspace.create': '创建 Workspace',
  'workspace.update': '更新 Workspace',
  'workspace.activate': '切换活动 Workspace',
  'workspace.provision': '初始化 Workspace 数据库',
  'workspace.delete.preview': '预览删除 Workspace',
  'workspace.delete': '删除 Workspace',
  'project.source.update': '更新源码配置',
  'project.clone': '克隆源码',
  'project.fetch': '获取远程更新',
  'project.pull': '拉取更新',
  'project.checkout': '切换版本',
  'project.init': '初始化 Discourse',
  'project.start': '启动项目',
  'project.stop': '停止项目',
  'project.restart': '重启项目',
  'plugin.create': '创建插件',
  'plugin.test': '运行插件测试',
  'plugin.package': '打包插件',
  'plugin.enable': '启用插件',
  'plugin.disable': '禁用插件',
  'theme.create': '创建主题',
  'theme.package': '打包主题',
  'theme.build': '构建主题',
  'theme.preview': '预览主题',
  'backup.create': '创建备份',
  'backup.delete': '删除备份',
  'backup.restore.preview': '预览恢复',
  'backup.restore': '恢复备份',
  'task.cancel': '取消任务',
  'settings.update': '更新设置',
  'settings.theme-preview.set': '保存主题预览 Key',
  'settings.theme-preview.clear': '清除主题预览 Key',
  'service.update': '更新服务配置',
  'service.test': '测试服务连接',
  'service.start': '启动服务',
  'service.stop': '停止服务',
  'service.restart': '重启服务',
  'runtime.verify': '验证运行时',
  'profile-source.add': '添加模板来源',
  'profile-source.refresh': '刷新模板来源',
  'profile-source.remove': '移除模板来源',
}

const actionOptions = Object.entries(actionLabels).sort((a, b) =>
  a[1].localeCompare(b[1], 'zh-CN'),
)

const resultMeta: Record<AuditResult, { label: string; tone: 'emerald' | 'cyan' | 'rose' }> = {
  success: { label: '成功', tone: 'emerald' },
  accepted: { label: '已受理', tone: 'cyan' },
  failure: { label: '失败', tone: 'rose' },
}

const clientLabels: Record<string, string> = {
  ui: '界面',
  cli: 'CLI',
  api: 'API',
}

const limitOptions = [50, 100, 200, 500]

export function Audit() {
  const workspaces = useWorkspaces()
  const [workspaceId, setWorkspaceId] = useState('')
  const [action, setAction] = useState('')
  const [result, setResult] = useState<AuditResult | ''>('')
  const [limit, setLimit] = useState(100)
  const [live, setLive] = useState(true)

  const filter = useMemo(
    () => ({
      limit,
      action: action || undefined,
      workspaceId: workspaceId || undefined,
      result,
    }),
    [limit, action, workspaceId, result],
  )
  const audit = useAudit(filter, live)

  const workspaceNames = useMemo(() => {
    const names = new Map<string, string>()
    for (const item of workspaces.data?.items ?? []) {
      names.set(item.workspace_id, item.name)
    }
    return names
  }, [workspaces.data])

  const items = audit.data?.items ?? []
  const hasFilter = Boolean(workspaceId || action || result)

  const reset = () => {
    setWorkspaceId('')
    setAction('')
    setResult('')
    setLimit(100)
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Audit
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">操作审计</h1>
          <p className="mt-1 max-w-3xl text-sm text-slate-400">
            记录本机管理 API 的写操作与拒绝请求。只保存请求元数据，不保存请求体，因此不会包含密码、API Key
            或确认令牌。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            onClick={() => void audit.refetch()}
            disabled={audit.isFetching}
          >
            {audit.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
        </div>
      </div>

      {audit.error ? (
        <ErrorNotice error={audit.error} onRetry={() => void audit.refetch()} />
      ) : null}

      <Card className="p-4">
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_150px_130px_auto] xl:items-end">
          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">Workspace</span>
            <select
              className={inputClass}
              value={workspaceId}
              onChange={(event) => setWorkspaceId(event.target.value)}
            >
              <option value="">全部 Workspace</option>
              {workspaces.data?.items.map((item) => (
                <option key={item.workspace_id} value={item.workspace_id}>
                  {item.name}
                </option>
              ))}
            </select>
          </label>

          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">操作</span>
            <select
              className={inputClass}
              value={action}
              onChange={(event) => setAction(event.target.value)}
            >
              <option value="">全部操作</option>
              {actionOptions.map(([id, label]) => (
                <option key={id} value={id}>
                  {label}
                </option>
              ))}
            </select>
          </label>

          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">结果</span>
            <select
              className={inputClass}
              value={result}
              onChange={(event) => setResult(event.target.value as AuditResult | '')}
            >
              <option value="">全部结果</option>
              <option value="success">成功</option>
              <option value="accepted">已受理</option>
              <option value="failure">失败</option>
            </select>
          </label>

          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">显示条数</span>
            <select
              className={inputClass}
              value={limit}
              onChange={(event) => setLimit(Number(event.target.value))}
            >
              {limitOptions.map((option) => (
                <option key={option} value={option}>
                  最近 {option} 条
                </option>
              ))}
            </select>
          </label>

          <div className="flex items-center gap-2">
            <label className="flex h-10 items-center gap-2 rounded-lg border border-slate-700 bg-slate-950/70 px-3 text-xs text-slate-300">
              <input
                type="checkbox"
                className="h-4 w-4 accent-cyan-400"
                checked={live}
                onChange={(event) => setLive(event.target.checked)}
              />
              自动刷新
            </label>
            {hasFilter ? (
              <Button variant="ghost" size="sm" onClick={reset}>
                清除筛选
              </Button>
            ) : null}
          </div>
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-slate-800/70 pt-3 text-[11px] text-slate-500">
          <span>
            共 {audit.data?.total ?? 0} 条匹配记录，当前显示 {items.length} 条
          </span>
          <span>·</span>
          <span>保留天数跟随日志保留设置</span>
          {live ? (
            <>
              <span>·</span>
              <span>每 10 秒自动刷新</span>
            </>
          ) : null}
        </div>
      </Card>

      <Card>
        <CardHeader
          title="审计记录"
          subtitle="按时间倒序；accepted 表示后台任务已受理，最终结果见任务历史或对应日志。"
        />

        {audit.isLoading ? (
          <div className="space-y-2 p-5">
            {Array.from({ length: 8 }).map((_, index) => (
              <Skeleton key={index} className="h-5 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            title={hasFilter ? '没有匹配的审计记录' : '还没有审计记录'}
            description={
              hasFilter
                ? '尝试清除筛选条件，或执行一次写操作后再查看。'
                : '创建 Workspace、启停项目、修改设置等写操作会自动记录在这里。'
            }
            action={
              hasFilter ? (
                <Button variant="secondary" size="sm" onClick={reset}>
                  清除筛选
                </Button>
              ) : undefined
            }
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] text-left text-xs">
              <thead className="border-b border-slate-800/70 text-[11px] uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="px-5 py-3 font-medium">时间</th>
                  <th className="px-5 py-3 font-medium">操作</th>
                  <th className="px-5 py-3 font-medium">Workspace / 目标</th>
                  <th className="px-5 py-3 font-medium">来源</th>
                  <th className="px-5 py-3 font-medium">结果</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60">
                {items.map((event) => (
                  <AuditRow
                    key={event.id}
                    event={event}
                    workspaceName={workspaceNames.get(event.workspace_id ?? '')}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>
    </div>
  )
}

function AuditRow({
  event,
  workspaceName,
}: {
  event: AuditEvent
  workspaceName?: string
}) {
  const result = resultMeta[event.result] ?? resultMeta.failure
  return (
    <tr data-testid="audit-row" className="align-top text-slate-300 hover:bg-slate-800/30">
      <td className="whitespace-nowrap px-5 py-3">
        <p className="text-slate-200">{formatDate(event.timestamp)}</p>
        <p className="mt-1 font-mono text-[10px] text-slate-500" title={event.id}>
          {event.id.slice(-8)}
        </p>
      </td>
      <td className="px-5 py-3">
        <p className="font-medium text-slate-200">
          {actionLabels[event.action] ?? event.action}
        </p>
        <p className="mt-1 font-mono text-[10px] text-slate-500">
          {event.action} · {event.method} {event.path}
        </p>
      </td>
      <td className="px-5 py-3">
        <p>{workspaceName ?? event.workspace_id ?? '-'}</p>
        {event.target ? (
          <p className="mt-1 font-mono text-[10px] text-slate-500">目标: {event.target}</p>
        ) : null}
      </td>
      <td className="px-5 py-3">
        <Badge tone={event.client === 'cli' ? 'sky' : 'slate'}>
          {clientLabels[event.client] ?? event.client}
        </Badge>
        <p className="mt-1 font-mono text-[10px] text-slate-500">{event.remote_addr ?? '-'}</p>
      </td>
      <td className="px-5 py-3">
        <Badge tone={result.tone}>{result.label}</Badge>
        <p className="mt-1 text-slate-500">
          HTTP {event.status} · {event.duration_ms} ms
        </p>
        {event.request_id ? (
          <p
            className="mt-1 max-w-[160px] truncate font-mono text-[10px] text-slate-600"
            title={event.request_id}
          >
            {event.request_id}
          </p>
        ) : null}
      </td>
    </tr>
  )
}
