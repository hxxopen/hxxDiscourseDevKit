import type { ReactNode } from 'react'
import { useDiagnostics, useInfo, useServices, useWorkspaces } from '../api/hooks'
import type { DiagnosticCheck } from '../api/types'
import type { Page } from '../components/Layout'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorNotice,
  Skeleton,
  Spinner,
  StateBadge,
} from '../components/ui'
import {
  formatDate,
  formatUptime,
  serviceStateLabel,
  serviceStateTone,
} from '../lib/format'

function StatCard({
  label,
  value,
  hint,
  accent = 'cyan',
}: {
  label: string
  value: ReactNode
  hint: ReactNode
  accent?: 'cyan' | 'emerald' | 'amber' | 'rose' | 'slate'
}) {
  const accents: Record<string, string> = {
    cyan: 'from-cyan-400/70',
    emerald: 'from-emerald-400/70',
    amber: 'from-amber-400/70',
    rose: 'from-rose-400/70',
    slate: 'from-slate-500/70',
  }
  return (
    <Card className="relative overflow-hidden p-5">
      <div
        className={`absolute inset-x-0 top-0 h-px bg-gradient-to-r ${accents[accent]} to-transparent`}
      />
      <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-slate-500">{label}</p>
      <div className="mt-2 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight text-slate-100">{value}</span>
      </div>
      <p className="mt-1.5 text-xs text-slate-500">{hint}</p>
    </Card>
  )
}

function severityTone(severity: DiagnosticCheck['severity']): 'rose' | 'amber' | 'sky' {
  if (severity === 'error') return 'rose'
  if (severity === 'warning') return 'amber'
  return 'sky'
}

function severityLabel(severity: DiagnosticCheck['severity']): string {
  if (severity === 'error') return '错误'
  if (severity === 'warning') return '警告'
  return '信息'
}

export function Dashboard({ onNavigate }: { onNavigate: (page: Page) => void }) {
  const info = useInfo()
  const workspaces = useWorkspaces()
  const diagnostics = useDiagnostics()
  const services = useServices()

  const list = workspaces.data?.items ?? []
  const active =
    list.find((item) => item.workspace_id === workspaces.data?.active_workspace_id) ?? null
  const counts = diagnostics.data?.counts ?? {}
  const errorCount = counts.error ?? 0
  const warningCount = counts.warning ?? 0
  const infoCount = counts.info ?? 0
  const attention = (diagnostics.data?.items ?? []).filter((item) => item.severity !== 'info')
  const statusOK = Boolean(info.data) && !info.isError

  const refreshAll = () => {
    void info.refetch()
    void workspaces.refetch()
    void diagnostics.refetch()
    void services.refetch()
  }

  const serviceItems = services.data?.items ?? []
  const servicesReady = Boolean(
    info.data?.capabilities?.runtime && info.data?.capabilities?.services,
  )

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Overview
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">概览</h1>
          <p className="mt-1 text-sm text-slate-400">
            当前实例的运行状态、活动 Workspace 与环境检查结果。
          </p>
        </div>
        <Button
          variant="secondary"
          onClick={refreshAll}
          disabled={info.isFetching || workspaces.isFetching || diagnostics.isFetching}
        >
          {info.isFetching || workspaces.isFetching || diagnostics.isFetching ? (
            <Spinner />
          ) : (
            <span className="text-xs">↻</span>
          )}
          刷新状态
        </Button>
      </div>

      {info.error ? <ErrorNotice error={info.error} onRetry={() => void info.refetch()} /> : null}
      {workspaces.error ? (
        <ErrorNotice error={workspaces.error} onRetry={() => void workspaces.refetch()} />
      ) : null}

      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {workspaces.isLoading ? (
          <>
            <Skeleton className="h-[112px]" />
            <Skeleton className="h-[112px]" />
            <Skeleton className="h-[112px]" />
            <Skeleton className="h-[112px]" />
          </>
        ) : (
          <>
            <StatCard
              label="Workspaces"
              value={workspaces.data?.total ?? 0}
              hint={active ? `活动：${active.name}` : '未选择活动 Workspace'}
            />
            <StatCard
              label="运行时长"
              value={formatUptime(info.data?.started_at)}
              hint={info.data ? `启动于 ${formatDate(info.data.started_at)}` : '等待 devkitd 信息'}
              accent="emerald"
            />
            <StatCard
              label="平台"
              value={
                <span className="text-lg">
                  {info.data?.platform.os ?? '—'}
                  <span className="text-slate-500">/{info.data?.platform.arch ?? '—'}</span>
                </span>
              }
              hint={
                info.data?.platform.distro ||
                (info.data?.platform.is_wsl ? 'WSL' : info.data?.version.go_version ?? '本地运行')
              }
              accent="slate"
            />
            <StatCard
              label="环境诊断"
              value={
                <span
                  className={
                    errorCount > 0
                      ? 'text-rose-300'
                      : warningCount > 0
                        ? 'text-amber-300'
                        : 'text-emerald-300'
                  }
                >
                  {errorCount > 0
                    ? `${errorCount} 错误`
                    : warningCount > 0
                      ? `${warningCount} 警告`
                      : '正常'}
                </span>
              }
              hint={`${infoCount} 条信息 · 点击“诊断”查看详情`}
              accent={errorCount > 0 ? 'rose' : warningCount > 0 ? 'amber' : 'emerald'}
            />
          </>
        )}
      </div>

      <div className="grid gap-6 xl:grid-cols-[1.35fr_1fr]">
        <div className="space-y-6">
          <Card>
            <CardHeader
              title="活动 Workspace"
              subtitle="CLI 与后续的项目启动命令默认作用于此 Workspace"
              action={
                <Button variant="ghost" size="sm" onClick={() => onNavigate('workspaces')}>
                  全部 Workspaces →
                </Button>
              }
            />
            {workspaces.isLoading ? (
              <div className="space-y-3 p-5">
                <Skeleton className="h-6 w-40" />
                <Skeleton className="h-4 w-64" />
                <Skeleton className="h-20 w-full" />
              </div>
            ) : active ? (
              <div className="p-5">
                <div className="flex flex-wrap items-center gap-3">
                  <h3 className="text-lg font-semibold text-slate-100">{active.name}</h3>
                  <StateBadge state={active.state} />
                  {active.workspace_id === workspaces.data?.active_workspace_id ? (
                    <Badge tone="cyan">活动</Badge>
                  ) : null}
                </div>
                <p className="mt-1 text-sm text-slate-400">
                  {active.description || '未填写描述'}
                </p>
                <dl className="mt-5 grid gap-4 sm:grid-cols-2">
                  <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                    <dt className="text-[11px] uppercase tracking-wide text-slate-500">
                      开发服务器
                    </dt>
                    <dd className="mt-1 font-mono text-sm text-slate-200">
                      http://{active.network.host}:{active.network.http_port}
                    </dd>
                  </div>
                  <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                    <dt className="text-[11px] uppercase tracking-wide text-slate-500">数据库</dt>
                    <dd className="mt-1 flex items-center gap-2">
                      <span className="truncate font-mono text-sm text-slate-200">
                        {active.database.database_name}
                      </span>
                      <Badge tone={active.database.provisioned ? 'emerald' : 'amber'}>
                        {active.database.provisioned ? '已就绪' : '待创建'}
                      </Badge>
                    </dd>
                  </div>
                  <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                    <dt className="text-[11px] uppercase tracking-wide text-slate-500">Redis</dt>
                    <dd className="mt-1 font-mono text-sm text-slate-200">
                      DB {active.redis.db_index}
                    </dd>
                  </div>
                  <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                    <dt className="text-[11px] uppercase tracking-wide text-slate-500">创建时间</dt>
                    <dd className="mt-1 text-sm text-slate-200">{formatDate(active.created_at)}</dd>
                  </div>
                </dl>
                <div className="mt-5 flex flex-wrap gap-2">
                  <Button onClick={() => onNavigate('workspaces')}>管理 Workspace</Button>
                  <Button variant="ghost" onClick={() => onNavigate('diagnostics')}>
                    查看诊断
                  </Button>
                </div>
              </div>
            ) : (
              <EmptyState
                title="尚未选择活动 Workspace"
                description="创建或选择一个 Workspace 后，这里会展示它的端口、数据库与运行状态。"
                action={<Button onClick={() => onNavigate('workspaces')}>前往 Workspaces</Button>}
              />
            )}
          </Card>

          <Card>
            <CardHeader
              title="最近的工作区"
              subtitle={`共 ${workspaces.data?.total ?? 0} 个 Workspace`}
              action={
                <Button variant="ghost" size="sm" onClick={() => onNavigate('workspaces')}>
                  查看全部
                </Button>
              }
            />
            {list.length === 0 && !workspaces.isLoading ? (
              <EmptyState
                title="还没有 Workspace"
                description="Default Workspace 会在首次启动时自动创建。"
              />
            ) : (
              <div className="divide-y divide-slate-800/70">
                {list.slice(0, 5).map((workspace) => (
                  <div
                    key={workspace.workspace_id}
                    className="flex flex-wrap items-center justify-between gap-3 px-5 py-3"
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="truncate text-sm font-medium text-slate-200">
                          {workspace.name}
                        </span>
                        {workspace.workspace_id === workspaces.data?.active_workspace_id ? (
                          <Badge tone="cyan">活动</Badge>
                        ) : null}
                      </div>
                      <p className="mt-0.5 truncate font-mono text-[11px] text-slate-500">
                        {workspace.workspace_id}
                      </p>
                    </div>
                    <div className="flex items-center gap-4">
                      <span className="hidden text-xs text-slate-500 sm:inline">
                        :{workspace.network.http_port}
                      </span>
                      <StateBadge state={workspace.state} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>

        <div className="space-y-6">
          <Card>
            <CardHeader
              title="共享服务"
              subtitle="PostgreSQL 与 Redis"
              action={
                <Button variant="ghost" size="sm" onClick={() => onNavigate('services')}>
                  管理
                </Button>
              }
            />
            {services.isLoading ? (
              <div className="space-y-3 p-5">
                <Skeleton className="h-5 w-full" />
                <Skeleton className="h-5 w-4/5" />
              </div>
            ) : services.error ? (
              <div className="p-5">
                <ErrorNotice error={services.error} onRetry={() => void services.refetch()} />
              </div>
            ) : serviceItems.length === 0 ? (
              <EmptyState title="没有服务状态" description="检查 devkitd 服务管理是否启用。" />
            ) : (
              <div className="divide-y divide-slate-800/70">
                {serviceItems.map((service) => (
                  <div key={service.name} className="flex items-center justify-between gap-3 px-5 py-3">
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-slate-200">
                        {service.name === 'postgresql' ? 'PostgreSQL' : 'Redis'}
                      </p>
                      <p className="mt-0.5 truncate font-mono text-[11px] text-slate-500">
                        {service.host}:{service.port} · {service.mode}
                      </p>
                    </div>
                    <Badge tone={serviceStateTone(service.state)}>
                      {serviceStateLabel(service.state)}
                    </Badge>
                  </div>
                ))}
              </div>
            )}
          </Card>

          <Card>
            <CardHeader
              title="环境诊断"
              subtitle={
                diagnostics.data
                  ? `生成于 ${formatDate(diagnostics.data.generated_at)}`
                  : '检查实例配置与 Workspace 元数据'
              }
              action={
                <Button variant="ghost" size="sm" onClick={() => onNavigate('diagnostics')}>
                  详情
                </Button>
              }
            />
            <div className="p-5">
              {diagnostics.isLoading ? (
                <div className="space-y-2">
                  <Skeleton className="h-5 w-full" />
                  <Skeleton className="h-5 w-4/5" />
                  <Skeleton className="h-5 w-3/5" />
                </div>
              ) : diagnostics.error ? (
                <ErrorNotice
                  error={diagnostics.error}
                  onRetry={() => void diagnostics.refetch()}
                />
              ) : (
                <>
                  <div className="flex flex-wrap gap-2">
                    <Badge tone={errorCount > 0 ? 'rose' : 'slate'}>{errorCount} 错误</Badge>
                    <Badge tone={warningCount > 0 ? 'amber' : 'slate'}>{warningCount} 警告</Badge>
                    <Badge tone="sky">{infoCount} 信息</Badge>
                  </div>
                  <div className="mt-4 space-y-2">
                    {attention.length === 0 ? (
                      <div className="rounded-xl border border-emerald-400/20 bg-emerald-400/5 px-4 py-3 text-sm text-emerald-200">
                        所有检查均已通过。
                      </div>
                    ) : (
                      attention.slice(0, 3).map((check) => (
                        <div
                          key={check.id}
                          className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3"
                        >
                          <div className="flex items-center justify-between gap-2">
                            <p className="text-sm font-medium text-slate-200">{check.title}</p>
                            <Badge tone={severityTone(check.severity)}>
                              {severityLabel(check.severity)}
                            </Badge>
                          </div>
                          <p className="mt-1 text-xs leading-5 text-slate-400">{check.message}</p>
                        </div>
                      ))
                    )}
                    {attention.length > 3 ? (
                      <p className="text-xs text-slate-500">还有 {attention.length - 3} 项待查看…</p>
                    ) : null}
                  </div>
                </>
              )}
            </div>
          </Card>

          <Card>
            <CardHeader title="当前能力" subtitle="按里程碑逐步开放" />
            <div className="space-y-3 p-5">
              {[
                { title: '多 Workspace 隔离', ready: true, note: '独立端口、数据库与 Redis DB' },
                { title: '本地 API 与会话保护', ready: true, note: '回环地址 + CSRF' },
                {
                  title: 'Runtime 与服务管理',
                  ready: servicesReady,
                  note: servicesReady ? 'Managed/External 服务、WSL2 与组件验证' : 'M2',
                },
                { title: 'Git / Discourse 初始化', ready: false, note: 'M3' },
              ].map((item) => (
                <div key={item.title} className="flex items-center justify-between gap-3">
                  <div className="flex items-center gap-2.5">
                    <span
                      className={`flex h-5 w-5 items-center justify-center rounded-full text-[10px] ${
                        item.ready
                          ? 'bg-emerald-400/15 text-emerald-300'
                          : 'bg-slate-800 text-slate-500'
                      }`}
                    >
                      {item.ready ? '✓' : '·'}
                    </span>
                    <div>
                      <p className="text-sm text-slate-200">{item.title}</p>
                      <p className="text-[11px] text-slate-500">{item.note}</p>
                    </div>
                  </div>
                  <Badge tone={item.ready ? 'emerald' : 'slate'}>
                    {item.ready ? '可用' : '计划中'}
                  </Badge>
                </div>
              ))}
            </div>
          </Card>

          {statusOK ? (
            <div className="rounded-2xl border border-slate-800 bg-slate-900/40 px-5 py-4">
              <p className="text-[11px] uppercase tracking-[0.16em] text-slate-500">实例</p>
              <p className="mt-2 truncate font-mono text-xs text-slate-400" title={info.data?.root}>
                {info.data?.root}
              </p>
              <div className="mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[11px] text-slate-500">
                <span>ID {info.data?.instance_id.slice(0, 10)}…</span>
                <span>v{info.data?.version.version}</span>
                <span>{info.data?.version.go_version}</span>
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
