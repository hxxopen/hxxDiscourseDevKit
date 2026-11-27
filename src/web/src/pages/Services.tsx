import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import {
  useRuntimeComponents,
  useServiceLogs,
  useServices,
} from '../api/hooks'
import type {
  RuntimeComponent,
  ServiceConfigInput,
  ServiceName,
  ServiceStatus,
  ServiceTestResult,
} from '../api/types'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  EmptyState,
  ErrorNotice,
  Field,
  Modal,
  Skeleton,
  Spinner,
  inputClass,
} from '../components/ui'
import {
  errorMessage,
  formatDate,
  serviceStateLabel,
  serviceStateTone,
} from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

const serviceMeta: Record<
  ServiceName,
  { title: string; subtitle: string; databases: boolean }
> = {
  postgresql: {
    title: 'PostgreSQL',
    subtitle: '为每个 Workspace 提供独立数据库与角色',
    databases: true,
  },
  redis: {
    title: 'Redis',
    subtitle: '为每个 Workspace 分配独立的逻辑 DB',
    databases: false,
  },
}

const serviceOrder: ServiceName[] = ['postgresql', 'redis']

type ServiceAction = 'start' | 'stop' | 'restart'

function runServiceAction(name: ServiceName, action: ServiceAction) {
  switch (action) {
    case 'start':
      return api.startService(name)
    case 'stop':
      return api.stopService(name)
    case 'restart':
      return api.restartService(name)
  }
}

export function Services() {
  const queryClient = useQueryClient()
  const services = useServices()
  const runtime = useRuntimeComponents()
  const [notice, setNotice] = useState<Notice | null>(null)
  const [configTarget, setConfigTarget] = useState<ServiceStatus | null>(null)
  const [logsTarget, setLogsTarget] = useState<ServiceName | null>(null)
  const [databasesOpen, setDatabasesOpen] = useState(false)
  const [testResults, setTestResults] = useState<
    Partial<Record<ServiceName, ServiceTestResult>>
  >({})

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: ['services'] })
    void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    void queryClient.invalidateQueries({ queryKey: ['info'] })
    void queryClient.invalidateQueries({ queryKey: ['diagnostics'] })
  }

  const actionMutation = useMutation({
    mutationFn: ({ name, action }: { name: ServiceName; action: ServiceAction }) =>
      runServiceAction(name, action),
    onSuccess: (result, variables) => {
      setNotice({
        tone: 'success',
        text: `${serviceMeta[variables.name].title} ${actionLabel(variables.action)}完成（${serviceStateLabel(result.status.state)}）`,
      })
      invalidateAll()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const testMutation = useMutation({
    mutationFn: (name: ServiceName) => api.testService(name),
    onSuccess: (result, name) => {
      setTestResults((current) => ({ ...current, [name]: result }))
      setNotice(
        result.reachable && result.authenticated
          ? { tone: 'success', text: `${serviceMeta[name].title} 连接测试通过（${result.latency_ms} ms）` }
          : { tone: 'error', text: `${serviceMeta[name].title} 连接测试失败：${result.message || '无法连接'}` },
      )
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const verifyMutation = useMutation({
    mutationFn: api.verifyAllRuntime,
    onSuccess: (result) => {
      const failed = result.items.filter((item) => item.required && !item.verified)
      setNotice(
        failed.length === 0
          ? { tone: 'success', text: '所有必需组件验证通过' }
          : { tone: 'error', text: `仍有 ${failed.length} 个必需组件未就绪` },
      )
      void queryClient.invalidateQueries({ queryKey: ['runtime-components'] })
      void queryClient.invalidateQueries({ queryKey: ['diagnostics'] })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const items = services.data?.items ?? []

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Services
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">
            服务与运行时
          </h1>
          <p className="mt-1 text-sm text-slate-400">
            管理共享 PostgreSQL / Redis、外部连接与运行时组件验证。
          </p>
        </div>
        <Button variant="secondary" onClick={() => void services.refetch()} disabled={services.isFetching}>
          {services.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
          刷新状态
        </Button>
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

      {services.error ? (
        <ErrorNotice error={services.error} onRetry={() => void services.refetch()} />
      ) : null}

      <div className="grid gap-5 xl:grid-cols-2">
        {services.isLoading ? (
          <>
            <Skeleton className="h-[300px]" />
            <Skeleton className="h-[300px]" />
          </>
        ) : (
          serviceOrder.map((name) => {
            const status = items.find((item) => item.name === name)
            return (
              <ServiceCard
                key={name}
                name={name}
                status={status}
                testResult={testResults[name]}
                pendingAction={
                  actionMutation.isPending && actionMutation.variables?.name === name
                    ? actionMutation.variables.action
                    : null
                }
                testing={testMutation.isPending && testMutation.variables === name}
                onAction={(action) => actionMutation.mutate({ name, action })}
                onTest={() => testMutation.mutate(name)}
                onConfigure={() => status && setConfigTarget(status)}
                onLogs={() => setLogsTarget(name)}
                onDatabases={() => setDatabasesOpen(true)}
              />
            )
          })
        )}
      </div>

      <Card>
        <CardHeader
          title="运行时组件"
          subtitle={
            runtime.data
              ? `Profile ${runtime.data.profile ?? 'default'}${runtime.data.builtin ? ' · 内置矩阵' : ''}`
              : '验证 Ruby、Node、Git、PostgreSQL 与 Redis 工具链'
          }
          action={
            <Button
              variant="secondary"
              size="sm"
              onClick={() => verifyMutation.mutate()}
              disabled={verifyMutation.isPending}
            >
              {verifyMutation.isPending ? <Spinner className="h-3 w-3" /> : null}
              重新验证
            </Button>
          }
        />
        {runtime.isLoading ? (
          <div className="space-y-3 p-5">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : runtime.error ? (
          <div className="p-5">
            <ErrorNotice error={runtime.error} onRetry={() => void runtime.refetch()} />
          </div>
        ) : (runtime.data?.items.length ?? 0) === 0 ? (
          <EmptyState title="没有运行时组件" description="检查运行时矩阵配置后重试。" />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[820px] border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                  <th className="px-5 py-3 font-medium">组件</th>
                  <th className="px-4 py-3 font-medium">期望版本</th>
                  <th className="px-4 py-3 font-medium">实际版本</th>
                  <th className="px-4 py-3 font-medium">执行环境</th>
                  <th className="px-4 py-3 font-medium">路径</th>
                  <th className="px-5 py-3 text-right font-medium">状态</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/70">
                {(runtime.data?.items ?? []).map((component) => (
                  <RuntimeRow key={component.name} component={component} />
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <Card className="p-5">
        <h2 className="text-sm font-semibold text-slate-100">说明</h2>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          Managed 模式下 DevKit 使用 <code className="text-slate-300">initdb</code> 与{' '}
          <code className="text-slate-300">pg_ctl</code>、<code className="text-slate-300">redis-server</code>{' '}
          启动本地实例，数据与日志保存在实例目录内；管理员与 Workspace 凭据由 DevKit 自动生成并保存在本机密钥存储中。
          External 模式下可在对应卡片的「配置」中填写已有服务的用户名与密码，凭据只写入受保护的本机密钥存储，不会出现在配置或日志中。
          停止服务前会检查正在运行的 Workspace，存在依赖时会拒绝停止。Discourse 仅使用 PostgreSQL，不支持
          MySQL/MariaDB。
        </p>
      </Card>

      {configTarget ? (
        <ServiceConfigModal
          key={configTarget.name}
          status={configTarget}
          onClose={() => setConfigTarget(null)}
          onSaved={(status) => {
            setConfigTarget(null)
            setNotice({
              tone: 'success',
              text: `${serviceMeta[status.name as ServiceName].title} 配置已更新（${serviceStateLabel(status.state)}）`,
            })
            invalidateAll()
          }}
        />
      ) : null}

      {logsTarget ? (
        <ServiceLogsModal name={logsTarget} onClose={() => setLogsTarget(null)} />
      ) : null}

      {databasesOpen ? <DatabasesModal onClose={() => setDatabasesOpen(false)} /> : null}
    </div>
  )
}

function actionLabel(action: ServiceAction): string {
  switch (action) {
    case 'start':
      return '启动'
    case 'stop':
      return '停止'
    case 'restart':
      return '重启'
  }
}

function ServiceCard({
  name,
  status,
  testResult,
  pendingAction,
  testing,
  onAction,
  onTest,
  onConfigure,
  onLogs,
  onDatabases,
}: {
  name: ServiceName
  status?: ServiceStatus
  testResult?: ServiceTestResult
  pendingAction: ServiceAction | null
  testing: boolean
  onAction: (action: ServiceAction) => void
  onTest: () => void
  onConfigure: () => void
  onLogs: () => void
  onDatabases: () => void
}) {
  const meta = serviceMeta[name]
  const state = status?.state ?? 'ERROR'
  const external = status?.mode === 'external'
  const running = state === 'RUNNING'
  const notInstalled = state === 'NOT_INSTALLED'
  const busy = pendingAction !== null
  const canLogs = !external

  return (
    <Card className="flex flex-col">
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            {meta.title}
            <Badge tone={serviceStateTone(state)}>{serviceStateLabel(state)}</Badge>
            {status ? <Badge tone="slate">{status.mode}</Badge> : null}
          </span>
        }
        subtitle={meta.subtitle}
        action={
          <Button variant="ghost" size="sm" onClick={onConfigure} disabled={!status}>
            配置
          </Button>
        }
      />

      {!status ? (
        <EmptyState title="无法读取服务状态" description="请确认 devkitd 正常运行。" />
      ) : (
        <div className="flex flex-1 flex-col gap-4 p-5">
          <dl className="grid gap-3 sm:grid-cols-2">
            <Info label="地址" value={status.host ? `${status.host}:${status.port}` : `:${status.port}`} mono />
            <Info label="版本" value={status.version || '-'} />
            <Info label="执行环境" value={status.execution || (external ? 'external' : '-')} />
            <Info label="检查时间" value={formatDate(status.checked_at)} />
            {status.pid ? <Info label="进程 PID" value={String(status.pid)} mono /> : null}
            {status.data_dir ? <Info label="数据目录" value={status.data_dir} mono /> : null}
          </dl>

          {status.dependencies && status.dependencies.length > 0 ? (
            <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-xs text-amber-200">
              依赖此服务的运行中 Workspace：{status.dependencies.join('、')}
            </div>
          ) : null}

          {notInstalled ? (
            <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-xs leading-5 text-amber-100">
              {meta.title} 尚未安装。到{' '}
              <a href="#/runtime" className="font-semibold underline underline-offset-2">
                运行时页面
              </a>{' '}
              的 WSL2 卡片中安装组件，安装完成后返回此页刷新状态。
            </div>
          ) : null}

          {status.message ? (
            <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs leading-5 text-slate-400">
              {status.message}
            </div>
          ) : null}

          {testResult ? (
            <div
              className={`rounded-xl border px-4 py-3 text-xs leading-5 ${
                testResult.reachable && testResult.authenticated
                  ? 'border-emerald-400/25 bg-emerald-400/5 text-emerald-200'
                  : 'border-rose-400/25 bg-rose-400/5 text-rose-200'
              }`}
            >
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={testResult.reachable ? 'emerald' : 'rose'}>
                  {testResult.reachable ? '可达' : '不可达'}
                </Badge>
                <Badge tone={testResult.authenticated ? 'emerald' : 'rose'}>
                  {testResult.authenticated ? '已认证' : '认证失败'}
                </Badge>
                <span>{testResult.latency_ms} ms</span>
                {testResult.server_version ? <span>v{testResult.server_version}</span> : null}
              </div>
              {testResult.message ? <p className="mt-1.5">{testResult.message}</p> : null}
            </div>
          ) : null}

          <div className="mt-auto flex flex-wrap gap-2">
            {!external ? (
              <>
                <Button
                  size="sm"
                  onClick={() => onAction('start')}
                  disabled={busy || running || notInstalled}
                >
                  {pendingAction === 'start' ? <Spinner className="h-3 w-3" /> : null}
                  启动
                </Button>
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => onAction('stop')}
                  disabled={busy || !running}
                >
                  {pendingAction === 'stop' ? <Spinner className="h-3 w-3" /> : null}
                  停止
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => onAction('restart')}
                  disabled={busy || !running}
                >
                  {pendingAction === 'restart' ? <Spinner className="h-3 w-3" /> : null}
                  重启
                </Button>
              </>
            ) : (
              <Badge tone="sky">外部服务由用户维护</Badge>
            )}
            <Button variant="secondary" size="sm" onClick={onTest} disabled={testing}>
              {testing ? <Spinner className="h-3 w-3" /> : null}
              测试连接
            </Button>
            {canLogs ? (
              <Button variant="ghost" size="sm" onClick={onLogs}>
                日志
              </Button>
            ) : null}
            {meta.databases ? (
              <Button variant="ghost" size="sm" onClick={onDatabases}>
                数据库
              </Button>
            ) : null}
          </div>
        </div>
      )}
    </Card>
  )
}

function Info({ label, value, mono = false }: { label: string; value: ReactNode; mono?: boolean }) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
      <dt className="text-[11px] uppercase tracking-wide text-slate-500">{label}</dt>
      <dd
        className={`mt-1 break-all text-sm text-slate-200 ${mono ? 'font-mono text-xs' : ''}`}
        title={typeof value === 'string' ? value : undefined}
      >
        {value}
      </dd>
    </div>
  )
}

function RuntimeRow({ component }: { component: RuntimeComponent }) {
  const tone = component.verified
    ? 'emerald'
    : component.required
      ? 'rose'
      : 'amber'
  return (
    <tr className="align-top">
      <td className="px-5 py-3">
        <div className="flex items-center gap-2">
          <span className="font-medium text-slate-100">{component.name}</span>
          {component.required ? <Badge tone="slate">必需</Badge> : <Badge tone="slate">可选</Badge>}
        </div>
        {component.message ? (
          <p className="mt-1 max-w-[280px] text-[11px] leading-4 text-slate-500">{component.message}</p>
        ) : null}
      </td>
      <td className="px-4 py-3 font-mono text-xs text-slate-300">{component.expected_version}</td>
      <td className="px-4 py-3 font-mono text-xs text-slate-300">
        {component.actual_version || '-'}
      </td>
      <td className="px-4 py-3 text-xs text-slate-400">{component.execution || '-'}</td>
      <td className="max-w-[260px] px-4 py-3">
        <p className="truncate font-mono text-[11px] text-slate-400" title={component.path}>
          {component.path || '-'}
        </p>
      </td>
      <td className="px-5 py-3 text-right">
        <Badge tone={tone}>
          {component.verified ? '已验证' : component.required ? '未就绪' : '未验证'}
        </Badge>
      </td>
    </tr>
  )
}

function ServiceConfigModal({
  status,
  onClose,
  onSaved,
}: {
  status: ServiceStatus
  onClose: () => void
  onSaved: (status: ServiceStatus) => void
}) {
  const name = status.name as ServiceName
  const external = status.mode === 'external'
  const [mode, setMode] = useState<'managed' | 'external'>(external ? 'external' : 'managed')
  const [host, setHost] = useState(status.host || '127.0.0.1')
  const [port, setPort] = useState(String(status.port || ''))
  const [adminUser, setAdminUser] = useState('postgres')
  const [username, setUsername] = useState('')
  const [database, setDatabase] = useState('postgres')
  const [password, setPassword] = useState('')
  const [tls, setTls] = useState(false)

  useEffect(() => {
    setMode(status.mode === 'external' ? 'external' : 'managed')
    setHost(status.host || '127.0.0.1')
    setPort(String(status.port || ''))
  }, [status])

  const mutation = useMutation({
    mutationFn: (input: ServiceConfigInput) => api.updateService(name, input),
    onSuccess: onSaved,
  })

  const numericPort = Number.parseInt(port, 10)
  const portError =
    Number.isFinite(numericPort) && numericPort >= 1 && numericPort <= 65535
      ? ''
      : '端口必须在 1-65535 之间'
  const hostError =
    mode === 'external' && host.trim() === '' ? '外部服务必须填写主机名' : ''

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (portError || hostError) {
      return
    }
    mutation.mutate({
      mode,
      host: host.trim() || '127.0.0.1',
      port: numericPort,
      admin_user: mode === 'managed' && name === 'postgresql' ? adminUser.trim() : undefined,
      username: mode === 'external' ? username.trim() || undefined : undefined,
      password: mode === 'external' && password !== '' ? password : undefined,
      database: mode === 'external' && name === 'postgresql' ? database.trim() || undefined : undefined,
      tls: mode === 'external' ? tls : undefined,
    })
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`配置 ${serviceMeta[name].title}`}
      description="Managed 使用 DevKit 管理的本地实例；External 连接用户自行维护的服务。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            取消
          </Button>
          <Button
            type="submit"
            form="service-config-form"
            disabled={mutation.isPending || Boolean(portError) || Boolean(hostError)}
          >
            {mutation.isPending ? <Spinner /> : null}
            保存
          </Button>
        </>
      }
    >
      <form id="service-config-form" onSubmit={submit} className="space-y-5">
        <div className="grid gap-3 sm:grid-cols-2">
          <label
            className={`flex cursor-pointer gap-3 rounded-xl border px-4 py-3 transition ${
              mode === 'managed'
                ? 'border-cyan-400/40 bg-cyan-400/5'
                : 'border-slate-800 bg-slate-950/40 hover:border-slate-700'
            }`}
          >
            <input
              type="radio"
              name="service-mode"
              className="mt-1 accent-cyan-400"
              checked={mode === 'managed'}
              onChange={() => setMode('managed')}
            />
            <span>
              <span className="block text-sm font-medium text-slate-200">Managed</span>
              <span className="mt-1 block text-xs leading-5 text-slate-400">
                DevKit 启动并管理本地实例
              </span>
            </span>
          </label>
          <label
            className={`flex cursor-pointer gap-3 rounded-xl border px-4 py-3 transition ${
              mode === 'external'
                ? 'border-cyan-400/40 bg-cyan-400/5'
                : 'border-slate-800 bg-slate-950/40 hover:border-slate-700'
            }`}
          >
            <input
              type="radio"
              name="service-mode"
              className="mt-1 accent-cyan-400"
              checked={mode === 'external'}
              onChange={() => setMode('external')}
            />
            <span>
              <span className="block text-sm font-medium text-slate-200">External</span>
              <span className="mt-1 block text-xs leading-5 text-slate-400">
                连接已有的远程或本机服务
              </span>
            </span>
          </label>
        </div>

        <div className="grid gap-4 sm:grid-cols-[2fr_1fr]">
          <Field label="主机" htmlFor="service-host" error={hostError || undefined}>
            <input
              id="service-host"
              className={inputClass}
              value={host}
              onChange={(event) => setHost(event.target.value)}
              placeholder="127.0.0.1"
            />
          </Field>
          <Field label="端口" htmlFor="service-port" error={portError || undefined}>
            <input
              id="service-port"
              className={inputClass}
              value={port}
              onChange={(event) => setPort(event.target.value)}
              inputMode="numeric"
              placeholder={name === 'postgresql' ? '15432' : '16379'}
            />
          </Field>
        </div>

        {mode === 'managed' ? (
          name === 'postgresql' ? (
            <div className="space-y-3">
              <Field
                label="管理员用户"
                htmlFor="service-admin-user"
                hint="仅新初始化的数据目录使用；已存在的数据目录保留原管理员用户。"
              >
                <input
                  id="service-admin-user"
                  className={inputClass}
                  value={adminUser}
                  onChange={(event) => setAdminUser(event.target.value)}
                  placeholder="postgres"
                />
              </Field>
              <p className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs leading-5 text-slate-400">
                Managed PostgreSQL 的管理员密码与 Workspace 数据库用户密码由 DevKit 自动生成并保存在受保护的本机密钥存储中，不在此处显示或修改。如需连接已有数据库，请切换到 External 并填写用户名与密码。
              </p>
            </div>
          ) : (
            <p className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs leading-5 text-slate-400">
              Managed Redis 的密码自动生成并保存在受保护的本机密钥存储中。
            </p>
          )
        ) : (
          <>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="用户名" htmlFor="service-username" hint="Redis 可留空。">
                <input
                  id="service-username"
                  className={inputClass}
                  value={username}
                  onChange={(event) => setUsername(event.target.value)}
                  placeholder={name === 'postgresql' ? 'postgres' : ''}
                  autoComplete="off"
                />
              </Field>
              {name === 'postgresql' ? (
                <Field label="维护数据库" htmlFor="service-database" hint="用于管理连接的数据库。">
                  <input
                    id="service-database"
                    className={inputClass}
                    value={database}
                    onChange={(event) => setDatabase(event.target.value)}
                    placeholder="postgres"
                  />
                </Field>
              ) : null}
            </div>
            <Field
              label="密码"
              htmlFor="service-password"
              hint="留空沿用已保存的凭据；密码只写入受保护的密钥存储。"
            >
              <input
                id="service-password"
                type="password"
                className={inputClass}
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                placeholder="••••••••"
                autoComplete="new-password"
              />
            </Field>
            <label className="flex items-center gap-2.5 text-sm text-slate-300">
              <input
                type="checkbox"
                className="h-4 w-4 accent-cyan-400"
                checked={tls}
                onChange={(event) => setTls(event.target.checked)}
              />
              使用 TLS（PostgreSQL 使用 require，Redis 使用 TLS 连接）
            </label>
          </>
        )}

        {mutation.error ? <ErrorNotice error={mutation.error} /> : null}
        <p className="text-xs leading-5 text-slate-500">
          切换模式不会自动迁移已有数据。Managed 的数据库与 Redis 数据位于实例服务目录；修改端口后需重启服务生效。
        </p>
      </form>
    </Modal>
  )
}

function ServiceLogsModal({ name, onClose }: { name: ServiceName; onClose: () => void }) {
  const logs = useServiceLogs(name, 300)
  return (
    <Modal
      open
      onClose={onClose}
      title={`${serviceMeta[name].title} 日志`}
      description={logs.data?.path}
      width="max-w-4xl"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            关闭
          </Button>
          <Button variant="secondary" onClick={() => void logs.refetch()} disabled={logs.isFetching}>
            {logs.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
        </>
      }
    >
      {logs.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-5/6" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      ) : logs.error ? (
        <ErrorNotice error={logs.error} onRetry={() => void logs.refetch()} />
      ) : (logs.data?.lines.length ?? 0) === 0 ? (
        <EmptyState title="没有日志" description="服务尚未产生日志，或日志文件为空。" />
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-xl border border-slate-800 bg-slate-950/70 p-4 font-mono text-[11px] leading-5 text-slate-300">
          {(logs.data?.lines ?? []).join('\n')}
        </pre>
      )}
    </Modal>
  )
}

function DatabasesModal({ onClose }: { onClose: () => void }) {
  const databases = useQuery({
    queryKey: ['service-databases'],
    queryFn: api.serviceDatabases,
    staleTime: 0,
    gcTime: 0,
  })
  return (
    <Modal
      open
      onClose={onClose}
      title="PostgreSQL 数据库"
      description="通过管理员连接列出所有非模板数据库。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            关闭
          </Button>
          <Button
            variant="secondary"
            onClick={() => void databases.refetch()}
            disabled={databases.isFetching}
          >
            {databases.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
        </>
      }
    >
      {databases.isLoading ? (
        <div className="space-y-2">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-full" />
        </div>
      ) : databases.error ? (
        <ErrorNotice error={databases.error} onRetry={() => void databases.refetch()} />
      ) : (databases.data?.items.length ?? 0) === 0 ? (
        <EmptyState title="没有数据库" description="PostgreSQL 中还没有非模板数据库。" />
      ) : (
        <ul className="divide-y divide-slate-800/70 rounded-xl border border-slate-800">
          {(databases.data?.items ?? []).map((database) => (
            <li key={database} className="px-4 py-2.5 font-mono text-xs text-slate-300">
              {database}
            </li>
          ))}
        </ul>
      )}
    </Modal>
  )
}
