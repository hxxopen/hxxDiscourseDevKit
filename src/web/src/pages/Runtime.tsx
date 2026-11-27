import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api } from '../api/client'
import {
  isTerminalTask,
  useInfo,
  useRuntimeComponents,
  useRuntimeMatrix,
  useSettings,
  useTask,
  useWSLStatus,
} from '../api/hooks'
import type { RuntimeComponent, RuntimeComponentSpec, Task } from '../api/types'
import { StorageMigrationCard } from '../components/StorageMigrationCard'
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
} from '../components/ui'
import { errorMessage, formatDate } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

export function Runtime() {
  const queryClient = useQueryClient()
  const components = useRuntimeComponents()
  const matrix = useRuntimeMatrix()
  const settings = useSettings()
  const info = useInfo()
  const wsl = useWSLStatus()
  const [notice, setNotice] = useState<Notice | null>(null)
  const [installOpen, setInstallOpen] = useState(false)

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

  const discover = useMutation({
    mutationFn: api.discoverRuntime,
    onSuccess: (result) => {
      setNotice({
        tone: result.total > 0 ? 'success' : 'error',
        text:
          result.total > 0
            ? `已探测到 ${result.total} 个组件：${result.names.join('、')}`
            : '未探测到任何组件；请确认 WSL2 中已安装对应软件包',
      })
      invalidate()
      void wsl.refetch()
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

      {info.data?.storage.legacy ||
      info.data?.storage.data_root_pending ||
      info.data?.storage.migrating ? (
        <StorageMigrationCard compact />
      ) : null}

      <Card>
        <CardHeader
          title="WSL2 执行环境"
          subtitle="Windows 上运行时组件与服务命令通过 WSL2 执行；DevKit 只复用已安装的发行版，不安装原生 Windows 包。"
          action={
            <div className="flex items-center gap-2">
              {wsl.data ? (
                <Badge tone={wsl.data.available ? 'emerald' : 'rose'}>
                  {wsl.data.available ? '可用' : '不可用'}
                </Badge>
              ) : null}
              <Button
                variant="ghost"
                size="sm"
                onClick={() => void wsl.refetch()}
                disabled={wsl.isFetching}
              >
                {wsl.isFetching ? <Spinner className="h-3 w-3" /> : null}
                重新检测
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => discover.mutate()}
                disabled={discover.isPending}
              >
                {discover.isPending ? <Spinner className="h-3 w-3" /> : null}
                探测组件路径
              </Button>
              <Button
                size="sm"
                onClick={() => setInstallOpen(true)}
                disabled={!wsl.data?.available || !info.data?.capabilities.runtime_install}
              >
                安装运行时组件
              </Button>
            </div>
          }
        />
        {wsl.isLoading ? (
          <div className="flex items-center gap-3 p-5 text-sm text-slate-400">
            <Spinner />
            正在探测 WSL2，首次启动发行版可能需要几十秒…
          </div>
        ) : wsl.error ? (
          <div className="p-5">
            <ErrorNotice error={wsl.error} onRetry={() => void wsl.refetch()} />
          </div>
        ) : wsl.data ? (
          <div className="space-y-4 p-5">
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
              <Detail label="发行版" value={wsl.data.distro || '-'} />
              <Detail label="系统" value={wsl.data.os_name || '-'} />
              <Detail label="内核" value={wsl.data.kernel || '-'} />
              <Detail
                label="root 执行"
                value={wsl.data.root_available ? '允许' : '不允许'}
              />
              <Detail label="用户" value={wsl.data.user || '-'} />
              <Detail label="主目录" value={wsl.data.home || '-'} />
              <Detail
                label="数据目录"
                value={info.data?.storage.split ? 'WSL2 文件系统' : '实例根目录（旧布局）'}
              />
              <Detail
                label="外挂服务"
                value="仅连接与检测，不执行安装或启停"
              />
            </div>
            {wsl.data.error ? (
              <div className="rounded-xl border border-rose-400/30 bg-rose-400/10 px-4 py-3 text-sm text-rose-200">
                {wsl.data.error}
              </div>
            ) : null}
            {!wsl.data.available ? (
              <p className="text-sm leading-6 text-slate-400">
                未检测到可用的 WSL2 发行版。请先在 Windows 中执行{' '}
                <code className="text-slate-300">wsl --install</code> 并完成发行版初始化，然后点击“重新检测”。
              </p>
            ) : null}
            {!wsl.data.root_available && wsl.data.available ? (
              <p className="text-sm leading-6 text-amber-200">
                当前发行版不允许 root 执行，自动安装依赖包会失败。请检查 WSL 配置或使用具有 sudo
                权限的默认用户。
              </p>
            ) : null}
          </div>
        ) : null}
      </Card>

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

      <InstallRuntimeModal
        open={installOpen}
        onClose={() => setInstallOpen(false)}
        onFinished={() => {
          invalidate()
          void wsl.refetch()
        }}
      />
    </div>
  )
}

function InstallRuntimeModal({
  open,
  onClose,
  onFinished,
}: {
  open: boolean
  onClose: () => void
  onFinished: () => void
}) {
  const preview = useMutation({ mutationFn: () => api.previewRuntimeInstall() })
  const install = useMutation({ mutationFn: api.installRuntime })
  const [taskId, setTaskId] = useState<string | null>(null)
  const task = useTask(taskId)
  const finished = useRef(false)

  useEffect(() => {
    finished.current = false
    setTaskId(null)
    install.reset()
    preview.reset()
    if (open) {
      preview.mutate()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  useEffect(() => {
    if (task.data?.status === 'succeeded' && !finished.current) {
      finished.current = true
      onFinished()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [task.data?.status])

  const plan = preview.data
  const running = Boolean(task.data && !isTerminalTask(task.data.status))
  const succeeded = task.data?.status === 'succeeded'
  const failed = task.data?.status === 'failed'

  const installComponent = () => {
    if (!plan?.confirm_token) {
      return
    }
    install.mutate(plan.confirm_token, {
      onSuccess: (accepted) => setTaskId(accepted.task_id),
    })
  }

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="安装运行时组件到 WSL2"
      description="安装前会展示将执行的 apt 命令；确认后以后台任务执行，输出写入“运行时安装日志”。"
      width="max-w-3xl"
      footer={
        taskId ? (
          <Button onClick={onClose} disabled={running}>
            {running ? '后台安装中…' : succeeded ? '完成' : '关闭'}
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose} disabled={install.isPending}>
              取消
            </Button>
            <Button
              onClick={installComponent}
              disabled={
                !plan?.confirm_token ||
                install.isPending ||
                preview.isPending ||
                Boolean(plan && !plan.root_available)
              }
            >
              {install.isPending ? <Spinner /> : null}
              确认安装
            </Button>
          </>
        )
      }
    >
      <div className="space-y-4">
        {preview.isPending ? (
          <div className="flex items-center gap-3 rounded-xl border border-slate-800 px-4 py-6 text-sm text-slate-400">
            <Spinner />
            正在检查 WSL2 与已安装组件…
          </div>
        ) : preview.error ? (
          <ErrorNotice error={preview.error} onRetry={() => preview.mutate()} />
        ) : plan ? (
          <>
            <div className="grid gap-3 sm:grid-cols-3">
              <Detail label="发行版" value={plan.distro || '-'} />
              <Detail label="系统" value={plan.os_name || '-'} />
              <Detail
                label="root 执行"
                value={plan.root_available ? '允许' : '不允许'}
              />
            </div>
            {(plan.warnings ?? []).map((warning) => (
              <div
                key={warning}
                className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-100"
              >
                {warning}
              </div>
            ))}
            <div className="overflow-hidden rounded-xl border border-slate-800">
              <table className="w-full border-collapse text-left text-sm">
                <thead>
                  <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                    <th className="px-4 py-2 font-medium">组件</th>
                    <th className="px-4 py-2 font-medium">软件包</th>
                    <th className="px-4 py-2 font-medium">状态</th>
                    <th className="px-4 py-2 font-medium">版本</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/70">
                  {plan.components.map((component) => (
                    <tr key={component.name} className="text-slate-300">
                      <td className="px-4 py-2 font-medium text-slate-100">
                        {component.name}
                      </td>
                      <td className="px-4 py-2 font-mono text-xs">{component.package}</td>
                      <td className="px-4 py-2">
                        <Badge tone={component.installed ? 'emerald' : 'amber'}>
                          {component.installed ? '已可用' : '待安装'}
                        </Badge>
                      </td>
                      <td className="px-4 py-2 font-mono text-xs">
                        {component.version || '-'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {plan.commands.length > 0 ? (
              <div>
                <p className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500">
                  将执行的命令
                </p>
                <pre className="overflow-x-auto rounded-xl border border-slate-800 bg-slate-950/60 p-4 text-xs leading-6 text-slate-300">
                  {plan.commands.map((command) => `${command.label}:\n${command.preview}`).join('\n\n')}
                </pre>
              </div>
            ) : null}
            <RuntimeInstallTask task={task.data ?? null} failed={failed} />
            {install.error ? <ErrorNotice error={install.error} /> : null}
            {succeeded ? (
              <div className="rounded-xl border border-emerald-400/30 bg-emerald-400/10 px-4 py-3 text-sm text-emerald-200">
                安装完成，组件路径已写入发现文件，可关闭窗口后在组件状态中查看。
              </div>
            ) : null}
            <p className="text-xs leading-6 text-slate-500">
              安装日志：日志页中的 “运行时安装日志”（runtime-install）。安装过程不会写入请求体或凭据。
            </p>
          </>
        ) : null}
      </div>
    </Modal>
  )
}

function RuntimeInstallTask({ task, failed }: { task: Task | null; failed: boolean }) {
  if (!task) {
    return null
  }
  return (
    <div className="space-y-2 rounded-xl border border-slate-800 px-4 py-3">
      <div className="flex items-center justify-between text-sm">
        <span className="text-slate-200">{task.message || task.status}</span>
        <span className="font-mono text-xs text-slate-500">{task.progress}%</span>
      </div>
      <ProgressBar value={task.progress} />
      {failed ? (
        <p className="text-xs leading-6 text-rose-300">
          {task.error_message || task.message || '安装失败，请查看运行时安装日志'}
        </p>
      ) : null}
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
