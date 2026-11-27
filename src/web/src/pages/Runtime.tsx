import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../api/client'
import { useRuntimeComponents, useRuntimeMatrix, useSettings } from '../api/hooks'
import type { RuntimeComponent, RuntimeComponentSpec } from '../api/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorNotice,
  Skeleton,
  Spinner,
} from '../components/ui'
import { errorMessage, formatDate } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

export function Runtime() {
  const queryClient = useQueryClient()
  const components = useRuntimeComponents()
  const matrix = useRuntimeMatrix()
  const settings = useSettings()
  const [notice, setNotice] = useState<Notice | null>(null)

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['runtime-components'] })
    void queryClient.invalidateQueries({ queryKey: ['diagnostics'] })
    void queryClient.invalidateQueries({ queryKey: ['settings'] })
  }

  const verifyAll = useMutation({
    mutationFn: api.verifyAllRuntime,
    onSuccess: (result) => {
      const failed = result.items.filter((item) => item.required && !item.verified)
      setNotice(
        failed.length === 0
          ? { tone: 'success', text: `全部 ${result.items.length} 个组件验证通过` }
          : {
              tone: 'error',
              text: `${failed.length} 个必需组件未通过：${failed.map((item) => item.name).join('、')}`,
            },
      )
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const verifyOne = useMutation({
    mutationFn: (name: string) => api.verifyRuntime(name),
    onSuccess: (item) => {
      setNotice(
        item.verified
          ? { tone: 'success', text: `${item.name} 验证通过` }
          : { tone: 'error', text: `${item.name} 未通过：${item.message || '版本或路径不可用'}` },
      )
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const items = components.data?.items ?? []
  const required = items.filter((item) => item.required)
  const verifiedRequired = required.filter((item) => item.verified).length

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Runtime
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">运行时组件</h1>
          <p className="mt-1 text-sm text-slate-400">
            对照版本矩阵检查 Ruby、Node.js、Git、PostgreSQL、Redis 等组件，并显示实际执行环境。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="secondary"
            onClick={() => void components.refetch()}
            disabled={components.isFetching}
          >
            {components.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新状态
          </Button>
          <Button onClick={() => verifyAll.mutate()} disabled={verifyAll.isPending}>
            {verifyAll.isPending ? <Spinner /> : null}
            重新验证全部
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

      {components.error ? (
        <ErrorNotice error={components.error} onRetry={() => void components.refetch()} />
      ) : null}
      {matrix.error ? (
        <ErrorNotice error={matrix.error} onRetry={() => void matrix.refetch()} />
      ) : null}

      <div className="grid gap-4 md:grid-cols-3">
        <Card className="p-5">
          <p className="text-[11px] uppercase tracking-wide text-slate-500">Profile</p>
          <p className="mt-2 text-xl font-semibold text-slate-100">
            {matrix.data?.matrix.profile ?? 'default'}
          </p>
          <p className="mt-1 text-xs text-slate-400">
            {matrix.data?.builtin ? '内置矩阵' : '自定义矩阵'}
            {matrix.data?.matrix.devkit_version_range
              ? ` · DevKit ${matrix.data.matrix.devkit_version_range}`
              : ''}
          </p>
        </Card>
        <Card className="p-5">
          <p className="text-[11px] uppercase tracking-wide text-slate-500">必需组件</p>
          <p className="mt-2 text-xl font-semibold text-slate-100">
            {verifiedRequired} / {required.length}
          </p>
          <p
            className={`mt-1 text-xs ${
              required.length > 0 && verifiedRequired === required.length
                ? 'text-emerald-300'
                : 'text-amber-300'
            }`}
          >
            {required.length === 0
              ? '矩阵未标记必需组件'
              : verifiedRequired === required.length
                ? '全部必需组件可用'
                : '存在未通过验证的必需组件'}
          </p>
        </Card>
        <Card className="p-5">
          <p className="text-[11px] uppercase tracking-wide text-slate-500">矩阵文件</p>
          <p className="mt-2 truncate font-mono text-xs text-slate-200" title={settings.data?.config.runtime.matrix_file}>
            {settings.data?.config.runtime.matrix_file ?? 'config/runtime-matrix.json'}
          </p>
          <p className="mt-1 text-xs text-slate-400">
            最近检查：
            {items.length === 0 ? '-' : formatDate(items[0].checked_at)}
          </p>
        </Card>
      </div>

      <Card>
        <CardHeader
          title="组件状态"
          subtitle="绿色表示版本兼容且路径可用；可选组件缺失不会阻止启动。"
          action={
            <Badge tone={verifiedRequired === required.length && required.length > 0 ? 'emerald' : 'amber'}>
              {verifiedRequired}/{required.length} 必需组件
            </Badge>
          }
        />
        {components.isLoading ? (
          <div className="space-y-3 p-5">
            {Array.from({ length: 5 }).map((_, index) => (
              <Skeleton key={index} className="h-10 w-full" />
            ))}
          </div>
        ) : items.length === 0 ? (
          <EmptyState
            title="没有运行时组件"
            description="检查 config/runtime-matrix.json 是否存在且格式正确。"
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[980px] border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                  <th className="px-5 py-3 font-medium">组件</th>
                  <th className="px-4 py-3 font-medium">期望版本</th>
                  <th className="px-4 py-3 font-medium">实际版本</th>
                  <th className="px-4 py-3 font-medium">执行环境</th>
                  <th className="px-4 py-3 font-medium">路径</th>
                  <th className="px-4 py-3 font-medium">SHA-256</th>
                  <th className="px-5 py-3 text-right font-medium">状态</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/70">
                {items.map((component) => (
                  <ComponentRow
                    key={component.name}
                    component={component}
                    verifying={verifyOne.isPending && verifyOne.variables === component.name}
                    onVerify={() => verifyOne.mutate(component.name)}
                  />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card>
        <CardHeader
          title="版本矩阵"
          subtitle="组件的期望版本、来源与哈希定义；自定义矩阵放在 config/runtime-matrix.json。"
        />
        {matrix.isLoading ? (
          <div className="space-y-3 p-5">
            <Skeleton className="h-8 w-full" />
            <Skeleton className="h-8 w-full" />
          </div>
        ) : (matrix.data?.matrix.components ?? {}) &&
          Object.keys(matrix.data?.matrix.components ?? {}).length === 0 ? (
          <EmptyState title="矩阵为空" description="内置矩阵没有定义任何组件。" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[760px] border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                  <th className="px-5 py-3 font-medium">组件</th>
                  <th className="px-4 py-3 font-medium">版本</th>
                  <th className="px-4 py-3 font-medium">来源</th>
                  <th className="px-4 py-3 font-medium">必需</th>
                  <th className="px-5 py-3 font-medium">校验和 / 可执行文件</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/70">
                {Object.entries(matrix.data?.matrix.components ?? {}).map(([name, spec]) => (
                  <MatrixRow key={name} name={name} spec={spec} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card className="p-5">
        <h2 className="text-sm font-semibold text-slate-100">执行环境说明</h2>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          原生模式下组件直接在本机执行；Windows 上未安装原生组件时，DevKit 可通过 WSL2
          分发版执行。执行环境列会显示 <code className="text-slate-300">native</code> 或{' '}
          <code className="text-slate-300">wsl:&lt;发行版&gt;</code>。可在设置页指定 WSL 发行版与运行时矩阵文件。
        </p>
      </Card>
    </div>
  )
}

function ComponentRow({
  component,
  verifying,
  onVerify,
}: {
  component: RuntimeComponent
  verifying: boolean
  onVerify: () => void
}) {
  const tone = component.verified ? 'emerald' : component.required ? 'rose' : 'amber'
  const label = component.verified ? '可用' : component.required ? '必需但不可用' : '可选未通过'
  return (
    <tr className="align-top text-slate-300">
      <td className="px-5 py-3">
        <p className="font-medium text-slate-100">{component.name}</p>
        <p className="text-[11px] text-slate-500">{component.required ? '必需组件' : '可选组件'}</p>
      </td>
      <td className="px-4 py-3 font-mono text-xs">{component.expected_version || '-'}</td>
      <td className="px-4 py-3 font-mono text-xs">{component.actual_version || '-'}</td>
      <td className="px-4 py-3 font-mono text-xs">{component.execution || '-'}</td>
      <td className="max-w-[260px] px-4 py-3">
        <p className="truncate font-mono text-xs" title={component.path}>
          {component.path || '-'}
        </p>
      </td>
      <td className="px-4 py-3 text-xs">
        {component.sha256 ? (
          <span className={component.hash_verified ? 'text-emerald-300' : 'text-amber-300'}>
            {component.hash_verified ? '已校验' : '未校验'}
          </span>
        ) : (
          <span className="text-slate-500">未定义</span>
        )}
      </td>
      <td className="px-5 py-3">
        <div className="flex flex-col items-end gap-2">
          <Badge tone={tone}>{label}</Badge>
          {!component.verified && component.message ? (
            <p className="max-w-[220px] text-right text-[11px] text-slate-500">
              {component.message}
            </p>
          ) : null}
          <Button variant="ghost" size="sm" onClick={onVerify} disabled={verifying}>
            {verifying ? <Spinner className="h-3 w-3" /> : null}
            验证
          </Button>
        </div>
      </td>
    </tr>
  )
}

function MatrixRow({ name, spec }: { name: string; spec: RuntimeComponentSpec }) {
  return (
    <tr className="align-top text-slate-300">
      <td className="px-5 py-3 font-medium text-slate-100">{name}</td>
      <td className="px-4 py-3 font-mono text-xs">{spec.version || '-'}</td>
      <td className="max-w-[260px] px-4 py-3 text-xs">
        <p className="truncate" title={spec.source}>
          {spec.source || '-'}
        </p>
      </td>
      <td className="px-4 py-3 text-xs">
        {spec.required ? <Badge tone="cyan">必需</Badge> : <Badge tone="slate">可选</Badge>}
      </td>
      <td className="px-5 py-3 text-xs">
        <p className="truncate font-mono" title={spec.sha256}>
          {spec.sha256 || '未定义哈希'}
        </p>
        {spec.binary ? (
          <p className="mt-1 truncate font-mono text-slate-500" title={spec.binary}>
            可执行文件：{spec.binary}
          </p>
        ) : null}
      </td>
    </tr>
  )
}
