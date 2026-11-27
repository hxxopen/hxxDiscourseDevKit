import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from '../api/client'
import {
  isTerminalTask,
  useProject,
  useProjectLogs,
  useTask,
  useWorkspaces,
} from '../api/hooks'
import type {
  ProjectInitInput,
  ProjectSourceInput,
  ProjectStatus,
  Workspace,
} from '../api/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  Detail,
  EmptyState,
  ErrorNotice,
  Field,
  ProgressBar,
  Skeleton,
  Spinner,
  StateBadge,
  inputClass,
} from '../components/ui'
import { errorMessage, formatDate } from '../lib/format'
import { taskLabels, taskTone } from '../lib/tasks'

type Notice = { tone: 'success' | 'error'; text: string }

type ProjectAction =
  | { kind: 'clone' }
  | { kind: 'fetch' }
  | { kind: 'pull'; force: boolean }
  | { kind: 'checkout'; ref: string; force: boolean }
  | { kind: 'init'; options: ProjectInitInput }
  | { kind: 'start' }
  | { kind: 'stop' }
  | { kind: 'restart' }

const actionLabels: Record<ProjectAction['kind'], string> = {
  clone: '克隆源码',
  fetch: '获取更新',
  pull: '拉取更新',
  checkout: '切换版本',
  init: '初始化项目',
  start: '启动项目',
  stop: '停止项目',
  restart: '重启项目',
}

export function Project() {
  const queryClient = useQueryClient()
  const workspaces = useWorkspaces()
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)
  const [taskId, setTaskId] = useState<string | null>(null)
  const [taskTitle, setTaskTitle] = useState('')

  const list = workspaces.data?.items ?? []
  const activeId = workspaces.data?.active_workspace_id ?? ''
  const currentId = selectedId || activeId || list[0]?.workspace_id || ''

  useEffect(() => {
    if (!selectedId && activeId) {
      setSelectedId(activeId)
    }
  }, [activeId, selectedId])

  const project = useProject(currentId || null)
  const taskQuery = useTask(taskId)
  const task = taskQuery.data
  const taskRunning = Boolean(task && !isTerminalTask(task.status))

  const workspace = project.data?.workspace
  const status = project.data?.project

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['project', currentId] })
    void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    void queryClient.invalidateQueries({ queryKey: ['workspace-status'] })
  }

  const taskSucceeded = task?.status === 'succeeded'
  const taskFailed = task?.status === 'failed'
  const taskCanceled = task?.status === 'canceled'
  useEffect(() => {
    if (taskSucceeded || taskFailed || taskCanceled) {
      invalidate()
    }
    // Invalidate once per terminal transition.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [taskSucceeded, taskFailed, taskCanceled])

  const actionMutation = useMutation({
    mutationFn: (action: ProjectAction) => {
      const id = currentId
      switch (action.kind) {
        case 'clone':
          return api.cloneProject(id)
        case 'fetch':
          return api.fetchProject(id)
        case 'pull':
          return api.pullProject(id, action.force)
        case 'checkout':
          return api.checkoutProject(id, { ref: action.ref, force: action.force })
        case 'init':
          return api.initProject(id, action.options)
        case 'start':
          return api.startProject(id)
        case 'stop':
          return api.stopProject(id)
        case 'restart':
          return api.restartProject(id)
      }
    },
    onSuccess: (accepted, action) => {
      setTaskId(accepted.task_id)
      setTaskTitle(actionLabels[action.kind])
      setNotice({ tone: 'success', text: `已提交「${actionLabels[action.kind]}」任务` })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const [sourceDraft, setSourceDraft] = useState<ProjectSourceInput>({})
  const [checkoutRef, setCheckoutRef] = useState('')
  const [checkoutForce, setCheckoutForce] = useState(false)
  const [initDeps, setInitDeps] = useState(true)
  const [initDB, setInitDB] = useState(true)
  const [logSource, setLogSource] = useState('project')
  const logs = useProjectLogs(currentId || null, logSource, 300)

  useEffect(() => {
    if (!workspace) {
      return
    }
    setSourceDraft({
      repository_url: workspace.source.repository_url,
      branch: workspace.source.branch,
      tag: workspace.source.tag,
      commit: workspace.source.commit,
      shallow: workspace.source.shallow,
      submodules: workspace.source.submodules,
    })
  }, [workspace?.workspace_id])

  const sourceMutation = useMutation({
    mutationFn: (input: ProjectSourceInput) => api.updateProjectSource(currentId, input),
    onSuccess: () => {
      setNotice({ tone: 'success', text: '源码配置已保存' })
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const disabled = !currentId || taskRunning || actionMutation.isPending
  const installed = status?.source.installed ?? false
  const alive = status?.process.alive ?? false
  const initialized = status?.process.initialized ?? false

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Project
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">项目</h1>
          <p className="mt-1 text-sm text-slate-400">
            管理 Discourse 源码、依赖初始化与 Rails 进程；备份与恢复在独立页面。
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            className={`${inputClass} w-56`}
            value={currentId}
            onChange={(event) => {
              setSelectedId(event.target.value)
              setTaskId(null)
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
          <Button
            variant="secondary"
            onClick={() => void project.refetch()}
            disabled={!currentId}
          >
            {project.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
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
            title="没有可管理的 Workspace"
            description="先在 Workspaces 页面创建 Workspace，然后配置源码并初始化 Discourse。"
          />
        </Card>
      ) : project.error ? (
        <ErrorNotice error={project.error} onRetry={() => void project.refetch()} />
      ) : project.isLoading || !workspace || !status ? (
        <div className="space-y-3">
          <Skeleton className="h-36 w-full" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : (
        <>
          <StatusCard workspace={workspace} status={status} />

          <Card>
            <CardHeader
              title="项目操作"
              subtitle={
                taskRunning
                  ? `${taskTitle} 正在执行，完成前不能提交新的操作。`
                  : 'Git 操作会保护未提交修改；启动前会自动检查共享服务与数据库。'
              }
              action={
                task ? (
                  <Badge tone={taskTone(task.status)}>{taskLabels[task.status]}</Badge>
                ) : null
              }
            />
            <div className="space-y-4 px-5 py-4">
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  size="sm"
                  disabled={disabled || installed}
                  onClick={() => actionMutation.mutate({ kind: 'clone' })}
                >
                  克隆源码
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !installed}
                  onClick={() => actionMutation.mutate({ kind: 'fetch' })}
                >
                  获取更新
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !installed}
                  onClick={() => actionMutation.mutate({ kind: 'pull', force: false })}
                >
                  拉取更新
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !initialized}
                  onClick={() =>
                    actionMutation.mutate({
                      kind: 'init',
                      options: {
                        install_dependencies: initDeps,
                        prepare_database: initDB,
                      },
                    })
                  }
                >
                  初始化项目
                </Button>
                <span className="mx-1 h-5 w-px bg-slate-800" />
                <Button
                  size="sm"
                  disabled={disabled || !initialized || alive}
                  onClick={() => actionMutation.mutate({ kind: 'start' })}
                >
                  启动
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !alive}
                  onClick={() => actionMutation.mutate({ kind: 'stop' })}
                >
                  停止
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !initialized}
                  onClick={() => actionMutation.mutate({ kind: 'restart' })}
                >
                  重启
                </Button>
              </div>

              <div className="flex flex-wrap items-end gap-3 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                <Field label="切换版本" htmlFor="checkout-ref" hint="分支、标签或提交 SHA">
                  <input
                    id="checkout-ref"
                    className={`${inputClass} w-56`}
                    value={checkoutRef}
                    onChange={(event) => setCheckoutRef(event.target.value)}
                    placeholder="main 或 v3.3.0"
                  />
                </Field>
                <label className="flex items-center gap-2 pb-2 text-xs text-slate-400">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-cyan-400"
                    checked={checkoutForce}
                    onChange={(event) => setCheckoutForce(event.target.checked)}
                  />
                  丢弃本地修改
                </label>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={disabled || !checkoutRef.trim()}
                  onClick={() =>
                    actionMutation.mutate({
                      kind: 'checkout',
                      ref: checkoutRef.trim(),
                      force: checkoutForce,
                    })
                  }
                >
                  切换
                </Button>
              </div>

              <div className="flex flex-wrap items-center gap-4 text-xs text-slate-400">
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-cyan-400"
                    checked={initDeps}
                    onChange={(event) => setInitDeps(event.target.checked)}
                  />
                  初始化时安装依赖
                </label>
                <label className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    className="h-4 w-4 accent-cyan-400"
                    checked={initDB}
                    onChange={(event) => setInitDB(event.target.checked)}
                  />
                  初始化时准备数据库
                </label>
              </div>

              {taskId ? (
                <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
                  <div className="flex items-center justify-between gap-3">
                    <span className="text-sm font-medium text-slate-200">{taskTitle}</span>
                    <Badge tone={task ? taskTone(task.status) : 'slate'}>
                      {task ? taskLabels[task.status] : '载入中'}
                    </Badge>
                  </div>
                  <div className="mt-3">
                    <ProgressBar value={task?.progress ?? 0} />
                  </div>
                  <p className="mt-2 text-xs text-slate-400">
                    {task?.message ?? '正在提交任务…'}
                  </p>
                  {task?.error_message ? (
                    <p className="mt-2 text-xs text-rose-300">{task.error_message}</p>
                  ) : null}
                  {task?.status === 'succeeded' ? (
                    <p className="mt-2 text-xs text-emerald-300">{task.message}</p>
                  ) : null}
                </div>
              ) : null}
            </div>
          </Card>

          <SourceCard
            draft={sourceDraft}
            onChange={setSourceDraft}
            onSubmit={() => sourceMutation.mutate(sourceDraft)}
            pending={sourceMutation.isPending}
            disabled={taskRunning}
            error={sourceMutation.error}
          />

          <LogsCard
            source={logSource}
            onSourceChange={setLogSource}
            lines={logs.data?.lines ?? []}
            path={logs.data?.path}
            loading={logs.isLoading}
            fetching={logs.isFetching}
            error={logs.error}
            onRefresh={() => void logs.refetch()}
          />

        </>
      )}
    </div>
  )
}

function StatusCard({
  workspace,
  status,
}: {
  workspace: Workspace
  status: ProjectStatus
}) {
  const source = status.source
  const process = status.process
  return (
    <Card>
      <CardHeader
        title={`Workspace「${workspace.name}」`}
        subtitle={`${workspace.workspace_id} · HTTP ${workspace.network.host}:${workspace.network.http_port}`}
        action={<StateBadge state={workspace.state} />}
      />
      <div className="space-y-4 px-5 py-4">
        <div className="flex flex-wrap items-center gap-2">
          <Badge tone={process.alive ? 'emerald' : 'slate'}>
            进程 {process.process_state || 'STOPPED'}
          </Badge>
          <Badge tone={process.initialized ? 'cyan' : 'amber'}>
            {process.initialized ? '已初始化' : '未初始化'}
          </Badge>
          <Badge tone={source.installed ? 'cyan' : 'slate'}>
            {source.installed ? '源码已检出' : '源码未检出'}
          </Badge>
          {source.dirty ? <Badge tone="amber">{source.changed_files} 个未提交修改</Badge> : null}
          {process.alive ? (
            <a
              href={process.http_url}
              target="_blank"
              rel="noreferrer"
              className="text-xs font-medium text-cyan-300 underline-offset-4 hover:underline"
            >
              打开 {process.http_url}
            </a>
          ) : null}
        </div>

        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Detail label="仓库" value={source.repository_url || '未配置'} mono />
          <Detail
            label="版本"
            value={source.current_branch || source.tag || source.commit || source.branch || '-'}
            mono
          />
          <Detail label="HEAD" value={source.head_commit ? source.head_commit.slice(0, 12) : '-'} mono />
          <Detail label="上次同步" value={source.last_sync_at ? formatDate(source.last_sync_at) : '-'} />
        </div>

        {source.error ? (
          <p className="rounded-lg border border-amber-400/25 bg-amber-400/5 px-3 py-2 text-xs text-amber-200">
            {source.error}
          </p>
        ) : null}
        {process.last_error ? (
          <p className="rounded-lg border border-rose-400/25 bg-rose-400/5 px-3 py-2 text-xs text-rose-200">
            {process.last_error}
          </p>
        ) : null}

        {status.checks.length > 0 ? (
          <div className="grid gap-2 sm:grid-cols-2">
            {status.checks.map((check) => (
              <div
                key={check.name}
                className="flex items-start gap-2 rounded-lg border border-slate-800 bg-slate-950/40 px-3 py-2"
              >
                <span
                  className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full text-[9px] ${
                    check.ok
                      ? 'bg-emerald-400/15 text-emerald-300'
                      : 'bg-rose-400/15 text-rose-300'
                  }`}
                >
                  {check.ok ? '✓' : '✕'}
                </span>
                <div className="min-w-0">
                  <p className="text-xs text-slate-200">{check.name}</p>
                  {check.message ? (
                    <p className="mt-0.5 break-all text-[11px] text-slate-500">{check.message}</p>
                  ) : null}
                </div>
              </div>
            ))}
          </div>
        ) : null}
      </div>
    </Card>
  )
}

function SourceCard({
  draft,
  onChange,
  onSubmit,
  pending,
  disabled,
  error,
}: {
  draft: ProjectSourceInput
  onChange: (draft: ProjectSourceInput) => void
  onSubmit: () => void
  pending: boolean
  disabled: boolean
  error: unknown
}) {
  const update = (patch: Partial<ProjectSourceInput>) => onChange({ ...draft, ...patch })
  const selectionCount = [draft.branch, draft.tag, draft.commit].filter(
    (value) => Boolean(value && value.trim()),
  ).length
  return (
    <Card>
      <CardHeader
        title="源码配置"
        subtitle="仓库地址与版本选择保存在 Workspace 中；分支、标签、提交按优先级生效。"
      />
      <div className="space-y-4 px-5 py-4">
        <Field label="仓库地址" htmlFor="project-repo" hint="必须使用 HTTPS 或本地路径；不接受包含凭据的 URL。">
          <input
            id="project-repo"
            className={inputClass}
            value={draft.repository_url ?? ''}
            onChange={(event) => update({ repository_url: event.target.value })}
            placeholder="https://github.com/discourse/discourse.git"
          />
        </Field>
        <div className="grid gap-3 sm:grid-cols-3">
          <Field label="分支" htmlFor="project-branch">
            <input
              id="project-branch"
              className={inputClass}
              value={draft.branch ?? ''}
              onChange={(event) => update({ branch: event.target.value, tag: '', commit: '' })}
              placeholder="main"
            />
          </Field>
          <Field label="标签" htmlFor="project-tag">
            <input
              id="project-tag"
              className={inputClass}
              value={draft.tag ?? ''}
              onChange={(event) => update({ tag: event.target.value, branch: '', commit: '' })}
              placeholder="v3.3.0"
            />
          </Field>
          <Field label="提交" htmlFor="project-commit">
            <input
              id="project-commit"
              className={inputClass}
              value={draft.commit ?? ''}
              onChange={(event) => update({ commit: event.target.value, branch: '', tag: '' })}
              placeholder="完整或短 SHA"
            />
          </Field>
        </div>
        {selectionCount > 1 ? (
          <p className="text-xs text-amber-300">已填写多个版本字段，提交将优先于标签，标签优先于分支。</p>
        ) : null}
        <div className="flex flex-wrap items-center gap-4 text-xs text-slate-400">
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4 accent-cyan-400"
              checked={Boolean(draft.shallow)}
              onChange={(event) => update({ shallow: event.target.checked })}
            />
            浅克隆
          </label>
          <label className="flex items-center gap-2">
            <input
              type="checkbox"
              className="h-4 w-4 accent-cyan-400"
              checked={Boolean(draft.submodules)}
              onChange={(event) => update({ submodules: event.target.checked })}
            />
            包含子模块
          </label>
          <div className="ml-auto">
            <Button size="sm" onClick={onSubmit} disabled={pending || disabled}>
              {pending ? <Spinner /> : null}
              保存配置
            </Button>
          </div>
        </div>
        {error ? <ErrorNotice error={error} /> : null}
      </div>
    </Card>
  )
}

function LogsCard({
  source,
  onSourceChange,
  lines,
  path,
  loading,
  fetching,
  error,
  onRefresh,
}: {
  source: string
  onSourceChange: (source: string) => void
  lines: string[]
  path?: string
  loading: boolean
  fetching: boolean
  error: unknown
  onRefresh: () => void
}) {
  return (
    <Card>
      <CardHeader
        title="项目日志"
        subtitle={path ?? '项目运行日志会写入 Workspace 的 logs 目录，敏感信息已脱敏。'}
        action={
          <div className="flex items-center gap-2">
            <select
              className={`${inputClass} w-28`}
              value={source}
              onChange={(event) => onSourceChange(event.target.value)}
            >
              <option value="project">运行日志</option>
              <option value="init">初始化日志</option>
              <option value="git">Git 日志</option>
            </select>
            <Button size="sm" variant="secondary" onClick={onRefresh} disabled={fetching}>
              {fetching ? <Spinner /> : '刷新'}
            </Button>
          </div>
        }
      />
      <div className="px-5 py-4">
        {error ? (
          <ErrorNotice error={error} onRetry={onRefresh} />
        ) : loading ? (
          <Skeleton className="h-40 w-full" />
        ) : lines.length === 0 ? (
          <p className="rounded-lg border border-slate-800 bg-slate-950/60 px-4 py-6 text-center text-xs text-slate-500">
            暂无日志
          </p>
        ) : (
          <pre className="max-h-80 overflow-auto rounded-lg border border-slate-800 bg-slate-950/80 px-4 py-3 font-mono text-[11px] leading-5 text-slate-300">
            {lines.join('\n')}
          </pre>
        )}
      </div>
    </Card>
  )
}
