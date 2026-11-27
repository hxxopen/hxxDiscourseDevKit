import { useState } from 'react'
import { useDiagnostics } from '../api/hooks'
import type { DiagnosticCheck } from '../api/types'
import { Badge, Button, Card, EmptyState, ErrorNotice, Skeleton, Spinner } from '../components/ui'
import { formatDate } from '../lib/format'

type Severity = DiagnosticCheck['severity']
type Filter = 'all' | Severity

const severityMeta: Record<
  Severity,
  { tone: 'rose' | 'amber' | 'sky'; label: string; icon: string; border: string }
> = {
  error: {
    tone: 'rose',
    label: '错误',
    icon: '✕',
    border: 'border-rose-500/25 bg-rose-500/[0.03]',
  },
  warning: {
    tone: 'amber',
    label: '警告',
    icon: '!',
    border: 'border-amber-400/25 bg-amber-400/[0.03]',
  },
  info: {
    tone: 'sky',
    label: '信息',
    icon: 'i',
    border: 'border-slate-800 bg-slate-900/40',
  },
}

const filters: { id: Filter; label: string }[] = [
  { id: 'all', label: '全部' },
  { id: 'error', label: '错误' },
  { id: 'warning', label: '警告' },
  { id: 'info', label: '信息' },
]

function detailValue(value: unknown): string {
  if (value === null || value === undefined) {
    return '-'
  }
  if (typeof value === 'string') {
    return value
  }
  if (typeof value === 'number' || typeof value === 'boolean') {
    return String(value)
  }
  try {
    return JSON.stringify(value) ?? String(value)
  } catch {
    return String(value)
  }
}

export function Diagnostics() {
  const diagnostics = useDiagnostics()
  const [filter, setFilter] = useState<Filter>('all')

  const report = diagnostics.data
  const items = report?.items ?? []
  const counts = report?.counts ?? {}
  const errorCount = counts.error ?? 0
  const warningCount = counts.warning ?? 0
  const infoCount = counts.info ?? 0
  const visible = filter === 'all' ? items : items.filter((item) => item.severity === filter)

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Diagnostics
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">环境诊断</h1>
          <p className="mt-1 text-sm text-slate-400">
            检查实例配置、端口占用、目录权限、Workspace 元数据、运行时组件与服务连接。
          </p>
        </div>
        <Button
          variant="secondary"
          onClick={() => void diagnostics.refetch()}
          disabled={diagnostics.isFetching}
        >
          {diagnostics.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
          重新检查
        </Button>
      </div>

      {diagnostics.error ? (
        <ErrorNotice error={diagnostics.error} onRetry={() => void diagnostics.refetch()} />
      ) : null}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        {diagnostics.isLoading ? (
          <>
            <Skeleton className="h-[82px]" />
            <Skeleton className="h-[82px]" />
            <Skeleton className="h-[82px]" />
            <Skeleton className="h-[82px]" />
          </>
        ) : (
          <>
            <CountCard
              label="错误"
              count={errorCount}
              tone="rose"
              active={filter === 'error'}
              onClick={() => setFilter(filter === 'error' ? 'all' : 'error')}
            />
            <CountCard
              label="警告"
              count={warningCount}
              tone="amber"
              active={filter === 'warning'}
              onClick={() => setFilter(filter === 'warning' ? 'all' : 'warning')}
            />
            <CountCard
              label="信息"
              count={infoCount}
              tone="sky"
              active={filter === 'info'}
              onClick={() => setFilter(filter === 'info' ? 'all' : 'info')}
            />
            <CountCard
              label="检查项"
              count={items.length}
              tone="slate"
              active={filter === 'all'}
              onClick={() => setFilter('all')}
            />
          </>
        )}
      </div>

      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap gap-1 rounded-xl border border-slate-800 bg-slate-900/50 p-1">
          {filters.map((entry) => (
            <button
              key={entry.id}
              type="button"
              onClick={() => setFilter(entry.id)}
              className={`rounded-lg px-3 py-1.5 text-xs font-medium transition ${
                filter === entry.id
                  ? 'bg-slate-800 text-slate-100'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {entry.label}
            </button>
          ))}
        </div>
        <p className="text-xs text-slate-500">
          {report ? `生成于 ${formatDate(report.generated_at)}` : '尚未生成报告'}
        </p>
      </div>

      {diagnostics.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-28 w-full" />
          <Skeleton className="h-28 w-full" />
        </div>
      ) : visible.length === 0 ? (
        <Card>
          <EmptyState
            title={items.length === 0 ? '没有检查结果' : '该分类下没有检查项'}
            description={
              items.length === 0
                ? '点击“重新检查”生成一份诊断报告。'
                : `当前筛选为「${filters.find((entry) => entry.id === filter)?.label ?? filter}」。`
            }
            action={
              items.length === 0 ? (
                <Button onClick={() => void diagnostics.refetch()} disabled={diagnostics.isFetching}>
                  生成报告
                </Button>
              ) : (
                <Button variant="ghost" onClick={() => setFilter('all')}>
                  查看全部
                </Button>
              )
            }
          />
        </Card>
      ) : (
        <div className="space-y-3">
          {visible.map((check) => {
            const meta = severityMeta[check.severity]
            const details = Object.entries(check.details ?? {})
            return (
              <article
                key={check.id}
                className={`rounded-2xl border p-5 shadow-lg shadow-slate-950/10 ${meta.border}`}
              >
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="flex min-w-0 gap-3">
                    <span
                      className={`mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-xs font-semibold ${
                        check.severity === 'error'
                          ? 'bg-rose-400/15 text-rose-300'
                          : check.severity === 'warning'
                            ? 'bg-amber-400/15 text-amber-300'
                            : 'bg-sky-400/15 text-sky-300'
                      }`}
                    >
                      {meta.icon}
                    </span>
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h2 className="text-sm font-semibold text-slate-100">{check.title}</h2>
                        <Badge tone={meta.tone}>{meta.label}</Badge>
                        {check.destructive ? <Badge tone="rose">需确认</Badge> : null}
                      </div>
                      <p className="mt-1.5 break-words text-sm leading-6 text-slate-400">
                        {check.message}
                      </p>
                    </div>
                  </div>
                  <code className="shrink-0 text-[10px] text-slate-600">{check.id}</code>
                </div>

                {check.fix_action ? (
                  <div className="mt-4 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                    <p className="text-[11px] uppercase tracking-wide text-slate-500">建议操作</p>
                    <p className="mt-1 break-words text-xs leading-5 text-slate-300">
                      {check.fix_action}
                    </p>
                  </div>
                ) : null}

                {details.length > 0 ? (
                  <dl className="mt-4 grid gap-x-6 gap-y-2 sm:grid-cols-2">
                    {details.map(([key, value]) => (
                      <div key={key} className="flex items-start justify-between gap-3">
                        <dt className="shrink-0 text-xs text-slate-500">{key}</dt>
                        <dd
                          className="break-all text-right font-mono text-[11px] text-slate-400"
                          title={detailValue(value)}
                        >
                          {detailValue(value)}
                        </dd>
                      </div>
                    ))}
                  </dl>
                ) : null}
              </article>
            )
          })}
        </div>
      )}

      <Card className="p-5">
        <h2 className="text-sm font-semibold text-slate-100">诊断范围</h2>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          当前报告覆盖配置校验、端口可用性、目录可写性、Workspace 元数据完整性、运行时矩阵与组件验证、
          PostgreSQL / Redis 连接状态以及活动 Workspace 状态。带「需确认」的修复建议属于破坏性操作，
          将在后续里程碑通过预览令牌执行，诊断本身不会修改任何文件。
        </p>
      </Card>
    </div>
  )
}

function CountCard({
  label,
  count,
  tone,
  active,
  onClick,
}: {
  label: string
  count: number
  tone: 'rose' | 'amber' | 'sky' | 'slate'
  active: boolean
  onClick: () => void
}) {
  const tones: Record<string, string> = {
    rose: 'text-rose-300',
    amber: 'text-amber-300',
    sky: 'text-sky-300',
    slate: 'text-slate-300',
  }
  return (
    <button
      type="button"
      onClick={onClick}
      className={`rounded-2xl border px-5 py-4 text-left transition ${
        active
          ? 'border-cyan-400/40 bg-cyan-400/[0.06]'
          : 'border-slate-800/80 bg-slate-900/60 hover:border-slate-700'
      }`}
    >
      <p className="text-[11px] font-medium uppercase tracking-[0.16em] text-slate-500">{label}</p>
      <p className={`mt-1 text-2xl font-semibold tracking-tight ${tones[tone]}`}>{count}</p>
    </button>
  )
}
