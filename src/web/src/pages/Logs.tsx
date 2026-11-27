import { useEffect, useMemo, useState } from 'react'
import { useLog, useLogSources } from '../api/hooks'
import type { LogSource } from '../api/types'
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
import { formatBytes, formatDate } from '../lib/format'

const categoryOrder = ['devkit', 'service', 'workspace'] as const
const categoryLabel: Record<string, string> = {
  devkit: 'DevKit',
  service: '共享服务',
  workspace: 'Workspace',
}

const tailOptions = [100, 200, 500, 1000, 2000]

export function Logs() {
  const sources = useLogSources()
  const [sourceID, setSourceID] = useState('')
  const [tail, setTail] = useState(200)
  const [live, setLive] = useState(true)

  const items = sources.data?.items ?? []
  const selected = items.find((item) => item.id === sourceID)

  useEffect(() => {
    if (items.length === 0) {
      return
    }
    if (items.some((item) => item.id === sourceID)) {
      return
    }
    const preferred =
      items.find((item) => item.exists && item.category === 'devkit') ??
      items.find((item) => item.exists) ??
      items[0]
    setSourceID(preferred.id)
  }, [items, sourceID])

  const grouped = useMemo(() => {
    const groups = new Map<string, LogSource[]>()
    for (const item of items) {
      const bucket = groups.get(item.category) ?? []
      bucket.push(item)
      groups.set(item.category, bucket)
    }
    return categoryOrder
      .filter((category) => groups.has(category))
      .map((category) => ({ category, items: groups.get(category) ?? [] }))
  }, [items])

  const content = useLog(sourceID || null, tail, live)

  const refresh = () => {
    void sources.refetch()
    if (sourceID) {
      void content.refetch()
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Logs
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">日志中心</h1>
          <p className="mt-1 text-sm text-slate-400">
            查看 DevKit 守护进程、Workspace 项目与共享服务日志；内容在返回前已脱敏。
          </p>
        </div>
        <Button variant="secondary" onClick={refresh} disabled={sources.isFetching}>
          {sources.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
          刷新
        </Button>
      </div>

      {sources.error ? (
        <ErrorNotice error={sources.error} onRetry={() => void sources.refetch()} />
      ) : null}

      <Card className="p-4">
        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_150px_auto] md:items-end">
          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">日志来源</span>
            <select
              className={inputClass}
              value={sourceID}
              onChange={(event) => setSourceID(event.target.value)}
              disabled={sources.isLoading || items.length === 0}
            >
              {items.length === 0 ? <option value="">没有可用日志来源</option> : null}
              {grouped.map((group) => (
                <optgroup key={group.category} label={categoryLabel[group.category] ?? group.category}>
                  {group.items.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.label}
                      {item.exists ? '' : '（尚未生成）'}
                    </option>
                  ))}
                </optgroup>
              ))}
            </select>
          </label>

          <label className="space-y-1.5">
            <span className="block text-xs font-medium text-slate-300">显示行数</span>
            <select
              className={inputClass}
              value={tail}
              onChange={(event) => setTail(Number(event.target.value))}
            >
              {tailOptions.map((option) => (
                <option key={option} value={option}>
                  最近 {option} 行
                </option>
              ))}
            </select>
          </label>

          <label className="flex h-10 items-center gap-2 rounded-lg border border-slate-700 bg-slate-950/70 px-3 text-xs text-slate-300">
            <input
              type="checkbox"
              className="h-4 w-4 accent-cyan-400"
              checked={live}
              onChange={(event) => setLive(event.target.checked)}
            />
            自动刷新
          </label>
        </div>

        {selected ? (
          <div className="mt-4 flex flex-wrap items-center gap-2 border-t border-slate-800/70 pt-3 text-[11px] text-slate-500">
            <Badge tone={selected.exists ? 'cyan' : 'slate'}>
              {categoryLabel[selected.category] ?? selected.category}
            </Badge>
            <span className="truncate font-mono" title={selected.path}>
              {selected.path}
            </span>
            <span>·</span>
            <span>{formatBytes(selected.size_bytes)}</span>
            <span>·</span>
            <span>{selected.modified_at ? formatDate(selected.modified_at) : '未生成'}</span>
          </div>
        ) : null}
      </Card>

      <Card>
        <CardHeader
          title={selected?.label ?? '日志内容'}
          subtitle={
            live ? '每 3 秒自动刷新，仅显示选中来源的末尾内容。' : '自动刷新已暂停。'
          }
          action={
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void content.refetch()}
              disabled={!sourceID || content.isFetching}
            >
              {content.isFetching ? <Spinner className="h-3 w-3" /> : null}
              立即刷新
            </Button>
          }
        />

        {!sourceID ? (
          <EmptyState
            title="没有可查看的日志"
            description="创建 Workspace 或启动服务后，日志来源会出现在上方列表中。"
          />
        ) : content.isLoading ? (
          <div className="space-y-2 p-5">
            {Array.from({ length: 8 }).map((_, index) => (
              <Skeleton key={index} className="h-4 w-full" />
            ))}
          </div>
        ) : content.error ? (
          <div className="p-5">
            <ErrorNotice error={content.error} onRetry={() => void content.refetch()} />
          </div>
        ) : (content.data?.lines.length ?? 0) === 0 ? (
          <EmptyState
            title="日志为空"
            description={
              selected?.exists
                ? '该日志文件当前没有内容。'
                : '该日志文件尚未生成，执行相关操作后再回来查看。'
            }
          />
        ) : (
          <>
            <div className="max-h-[62vh] overflow-auto bg-slate-950/70">
              <pre
                className="min-w-full whitespace-pre-wrap break-all p-4 font-mono text-xs leading-5 text-slate-300"
                aria-label={`${selected?.label ?? '日志'}内容`}
              >
                {content.data?.lines.join('\n')}
              </pre>
            </div>
            <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-800/70 px-5 py-3 text-[11px] text-slate-500">
              <span>
                显示最近 {content.data?.total ?? 0} 行
                {content.data?.size_bytes ? ` · 文件 ${formatBytes(content.data.size_bytes)}` : ''}
              </span>
              <span>{live ? '实时刷新中' : '手动刷新模式'}</span>
            </div>
          </>
        )}
      </Card>
    </div>
  )
}
