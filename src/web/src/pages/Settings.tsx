import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState, type FormEvent, type ReactNode } from 'react'
import { api } from '../api/client'
import { useSettings } from '../api/hooks'
import type { DevKitConfig, SettingsInput } from '../api/types'
import { ProfileSourcesCard } from '../components/ProfileSources'
import { StorageMigrationCard } from '../components/StorageMigrationCard'
import {
  Badge,
  Button,
  Card,
  CardHeader,
  ErrorNotice,
  Field,
  Skeleton,
  Spinner,
  inputClass,
} from '../components/ui'
import { errorMessage } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

interface FormState {
  apiPort: string
  defaultProfile: string
  matrixFile: string
  wslDistro: string
  repositoryURL: string
  defaultBranch: string
  startTimeout: string
  stopTimeout: string
  logLevel: string
  retentionDays: string
  maxFileMB: string
  updatesChannel: string
  checkOnStart: boolean
  autoInstall: boolean
}

const restartLabels: Record<string, string> = {
  'storage.data_root': '数据目录',
  'network.host': '网络监听地址',
  'network.api_port': '管理 API 端口',
  'project.start_timeout_seconds': '项目启动超时',
  'project.stop_timeout_seconds': '项目停止超时',
  'logging.retention_days': '日志保留天数',
  'logging.max_file_mb': '单个日志文件大小上限',
}

function formFromConfig(config: DevKitConfig): FormState {
  return {
    apiPort: String(config.network.api_port),
    defaultProfile: config.runtime.default_profile,
    matrixFile: config.runtime.matrix_file,
    wslDistro: config.runtime.wsl_distro ?? '',
    repositoryURL: config.project.default_repository_url,
    defaultBranch: config.project.default_branch,
    startTimeout: String(config.project.start_timeout_seconds),
    stopTimeout: String(config.project.stop_timeout_seconds),
    logLevel: config.logging.level,
    retentionDays: String(config.logging.retention_days),
    maxFileMB: String(config.logging.max_file_mb),
    updatesChannel: config.updates.channel,
    checkOnStart: config.updates.check_on_start,
    autoInstall: Boolean(config.updates.auto_install),
  }
}

export function Settings() {
  const queryClient = useQueryClient()
  const settings = useSettings()
  const [form, setForm] = useState<FormState | null>(null)
  const [notice, setNotice] = useState<Notice | null>(null)
  const [formError, setFormError] = useState('')
  const [previewKey, setPreviewKey] = useState('')
  const [previewKeyError, setPreviewKeyError] = useState('')
  const [previewKeyOpen, setPreviewKeyOpen] = useState(false)

  const baseline = useMemo(
    () => (settings.data ? formFromConfig(settings.data.config) : null),
    [settings.data],
  )
  const dirty = Boolean(form && baseline && JSON.stringify(form) !== JSON.stringify(baseline))

  // Do not clobber unsaved edits when settings are refetched for unrelated reasons
  // (for example after saving the theme preview API key).
  useEffect(() => {
    if (settings.data && !dirty) {
      setForm(formFromConfig(settings.data.config))
    }
  }, [settings.data, dirty])

  const save = useMutation({
    mutationFn: (input: SettingsInput) => api.updateSettings(input),
    onSuccess: (data) => {
      queryClient.setQueryData(['settings'], data)
      setForm(formFromConfig(data.config))
      const restartRequired = data.restart_required ?? []
      setNotice({
        tone: 'success',
        text:
          restartRequired.length > 0
            ? `设置已保存；${restartRequired.length} 项需要重启 devkitd 后生效`
            : '设置已保存',
      })
      void queryClient.invalidateQueries({ queryKey: ['info'] })
      void queryClient.invalidateQueries({ queryKey: ['runtime-components'] })
      void queryClient.invalidateQueries({ queryKey: ['runtime-matrix'] })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const savePreviewKey = useMutation({
    mutationFn: (key: string) => api.setThemePreviewKey(key),
    onSuccess: (data) => {
      queryClient.setQueryData(['settings'], data)
      setPreviewKey('')
      setPreviewKeyError('')
      setPreviewKeyOpen(false)
      setNotice({ tone: 'success', text: 'Discourse 主题预览 API Key 已保存' })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const clearPreviewKey = useMutation({
    mutationFn: () => api.clearThemePreviewKey(),
    onSuccess: (data) => {
      queryClient.setQueryData(['settings'], data)
      setPreviewKey('')
      setPreviewKeyError('')
      setPreviewKeyOpen(false)
      setNotice({ tone: 'success', text: 'Discourse 主题预览 API Key 已清除' })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const submitPreviewKey = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const key = previewKey.trim()
    if (key.length < 16 || key.length > 512) {
      setPreviewKeyError('API Key 长度必须在 16–512 个字符之间')
      return
    }
    if (!/^[\x21-\x7E]+$/.test(key)) {
      setPreviewKeyError('API Key 只能包含可见 ASCII 字符，不能包含空格')
      return
    }
    setPreviewKeyError('')
    savePreviewKey.mutate(key)
  }

  const update = <K extends keyof FormState>(key: K, value: FormState[K]) => {
    setForm((current) => (current ? { ...current, [key]: value } : current))
    setFormError('')
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!form) {
      return
    }
    try {
      const input: SettingsInput = {
        network: {
          api_port: integerField(form.apiPort, '管理 API 端口', 1, 65535),
        },
        runtime: {
          default_profile: requiredField(form.defaultProfile, '默认运行时 Profile'),
          matrix_file: requiredField(form.matrixFile, '运行时矩阵文件'),
          wsl_distro: form.wslDistro.trim(),
        },
        project: {
          default_repository_url: requiredField(form.repositoryURL, '默认仓库地址'),
          default_branch: requiredField(form.defaultBranch, '默认分支'),
          start_timeout_seconds: integerField(form.startTimeout, '项目启动超时', 10, 3600),
          stop_timeout_seconds: integerField(form.stopTimeout, '项目停止超时', 3, 600),
        },
        logging: {
          level: form.logLevel,
          retention_days: integerField(form.retentionDays, '日志保留天数', 0, 36500),
          max_file_mb: integerField(form.maxFileMB, '日志文件大小上限', 0, 10240),
        },
        updates: {
          channel: form.updatesChannel,
          check_on_start: form.checkOnStart,
          auto_install: form.autoInstall,
        },
      }
      save.mutate(input)
    } catch (error) {
      setFormError(errorMessage(error))
    }
  }

  if (settings.isLoading) {
    return (
      <div className="space-y-6">
        <Skeleton className="h-16 w-72" />
        <div className="grid gap-4 lg:grid-cols-2">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-64 w-full" />
          ))}
        </div>
      </div>
    )
  }

  if (settings.error || !settings.data || !form) {
    return (
      <div className="space-y-6">
        <Header />
        <ErrorNotice error={settings.error} onRetry={() => void settings.refetch()} />
      </div>
    )
  }

  // Tolerate older daemons that encoded an empty list as null.
  const restartRequired = settings.data.restart_required ?? []
  const previewConfigured = settings.data.theme_preview?.api_key_configured ?? false

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Settings
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">设置</h1>
          <p className="mt-1 text-sm text-slate-400">
            调整运行时路径、项目默认值、日志与更新策略。敏感凭据只写入本机密钥文件，页面不会回显。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Badge tone={dirty ? 'amber' : 'slate'}>{dirty ? '有未保存修改' : '已同步'}</Badge>
          <Button type="submit" form="settings-form" disabled={!dirty || save.isPending}>
            {save.isPending ? <Spinner /> : null}
            保存设置
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

      {restartRequired.length > 0 ? (
        <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-200">
          <p className="font-medium">部分设置需要重启 devkitd 后生效</p>
          <div className="mt-2 flex flex-wrap gap-2">
            {restartRequired.map((field) => (
              <Badge key={field} tone="amber">
                {restartLabels[field] ?? field}
              </Badge>
            ))}
          </div>
          <p className="mt-2 text-xs text-amber-200/80">
            修改管理 API 端口后，请使用新端口重新打开管理界面。
          </p>
        </div>
      ) : null}

      <form id="settings-form" onSubmit={submit} className="space-y-5">
        <div className="grid gap-5 lg:grid-cols-2">
          <Card>
            <CardHeader title="网络" subtitle="管理 API 仅允许绑定回环地址。" />
            <div className="space-y-4 p-5">
              <Field label="监听地址" hint="出于安全考虑，地址只读。">
                <input className={`${inputClass} opacity-70`} value={settings.data.config.network.host} readOnly />
              </Field>
              <Field
                label="管理 API 端口"
                htmlFor="settings-api-port"
                hint="修改后需要重启 devkitd；浏览器地址也会随之变化。"
              >
                <input
                  id="settings-api-port"
                  className={inputClass}
                  type="number"
                  min={1}
                  max={65535}
                  value={form.apiPort}
                  onChange={(event) => update('apiPort', event.target.value)}
                />
              </Field>
            </div>
          </Card>

          <Card>
            <CardHeader title="运行时" subtitle="Profile 与版本矩阵决定组件期望版本。" />
            <div className="space-y-4 p-5">
              <Field label="默认 Profile" htmlFor="settings-profile" hint="创建 Workspace 时写入运行时 Profile。">
                <input
                  id="settings-profile"
                  className={inputClass}
                  value={form.defaultProfile}
                  onChange={(event) => update('defaultProfile', event.target.value)}
                />
              </Field>
              <Field
                label="矩阵文件"
                htmlFor="settings-matrix"
                hint="相对实例根目录；文件不存在时回退到内置矩阵。"
              >
                <input
                  id="settings-matrix"
                  className={inputClass}
                  value={form.matrixFile}
                  onChange={(event) => update('matrixFile', event.target.value)}
                />
              </Field>
              <Field
                label="WSL 发行版"
                htmlFor="settings-wsl"
                hint="仅 Windows 有效；留空时使用第一个已安装的发行版。"
              >
                <input
                  id="settings-wsl"
                  className={inputClass}
                  value={form.wslDistro}
                  placeholder="例如 Ubuntu-24.04"
                  onChange={(event) => update('wslDistro', event.target.value)}
                />
              </Field>
            </div>
          </Card>

          <Card>
            <CardHeader title="项目默认值" subtitle="新建 Workspace 时使用的源码与超时默认值。" />
            <div className="space-y-4 p-5">
              <Field label="默认仓库地址" htmlFor="settings-repo">
                <input
                  id="settings-repo"
                  className={inputClass}
                  value={form.repositoryURL}
                  onChange={(event) => update('repositoryURL', event.target.value)}
                />
              </Field>
              <Field label="默认分支" htmlFor="settings-branch">
                <input
                  id="settings-branch"
                  className={inputClass}
                  value={form.defaultBranch}
                  onChange={(event) => update('defaultBranch', event.target.value)}
                />
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="启动超时（秒）" htmlFor="settings-start-timeout">
                  <input
                    id="settings-start-timeout"
                    className={inputClass}
                    type="number"
                    min={10}
                    max={3600}
                    value={form.startTimeout}
                    onChange={(event) => update('startTimeout', event.target.value)}
                  />
                </Field>
                <Field label="停止超时（秒）" htmlFor="settings-stop-timeout">
                  <input
                    id="settings-stop-timeout"
                    className={inputClass}
                    type="number"
                    min={3}
                    max={600}
                    value={form.stopTimeout}
                    onChange={(event) => update('stopTimeout', event.target.value)}
                  />
                </Field>
              </div>
            </div>
          </Card>

          <Card>
            <CardHeader title="日志" subtitle="日志级别立即生效；轮转与保留策略在重启后生效。" />
            <div className="space-y-4 p-5">
              <Field label="日志级别" htmlFor="settings-log-level">
                <select
                  id="settings-log-level"
                  className={inputClass}
                  value={form.logLevel}
                  onChange={(event) => update('logLevel', event.target.value)}
                >
                  <option value="debug">debug</option>
                  <option value="info">info</option>
                  <option value="warn">warn</option>
                  <option value="error">error</option>
                </select>
              </Field>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field label="保留天数" htmlFor="settings-retention" hint="0 表示不自动清理。">
                  <input
                    id="settings-retention"
                    className={inputClass}
                    type="number"
                    min={0}
                    max={36500}
                    value={form.retentionDays}
                    onChange={(event) => update('retentionDays', event.target.value)}
                  />
                </Field>
                <Field label="单文件上限（MB）" htmlFor="settings-max-file" hint="0 表示不限制。">
                  <input
                    id="settings-max-file"
                    className={inputClass}
                    type="number"
                    min={0}
                    max={10240}
                    value={form.maxFileMB}
                    onChange={(event) => update('maxFileMB', event.target.value)}
                  />
                </Field>
              </div>
            </div>
          </Card>
        </div>

        <Card>
          <CardHeader title="更新" subtitle="当前版本仅保存更新偏好；版本检查与自动安装将在发布流水线完成后接入。" />
          <div className="grid gap-4 p-5 md:grid-cols-3">
            <Field label="更新通道" htmlFor="settings-channel">
              <select
                id="settings-channel"
                className={inputClass}
                value={form.updatesChannel}
                onChange={(event) => update('updatesChannel', event.target.value)}
              >
                <option value="stable">stable</option>
                <option value="beta">beta</option>
              </select>
            </Field>
            <Toggle
              label="启动时检查更新"
              hint="devkitd 启动后读取一次发布信息。"
              checked={form.checkOnStart}
              onChange={(checked) => update('checkOnStart', checked)}
            />
            <Toggle
              label="允许自动安装"
              hint="当前版本仅记录偏好，不会静默替换文件。"
              checked={form.autoInstall}
              onChange={(checked) => update('autoInstall', checked)}
            />
          </div>
        </Card>

        {formError ? (
          <div className="rounded-xl border border-rose-400/30 bg-rose-400/10 px-4 py-3 text-sm text-rose-200">
            {formError}
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-xs text-slate-500">
            设置写入 <code className="text-slate-400">{settings.data.paths.config_file}</code>，不包含明文凭据。
          </p>
          <div className="flex items-center gap-2">
            <Button
              variant="ghost"
              onClick={() => {
                setForm(baseline)
                setFormError('')
              }}
              disabled={!dirty || save.isPending}
            >
              放弃修改
            </Button>
            <Button type="submit" disabled={!dirty || save.isPending}>
              {save.isPending ? <Spinner /> : null}
              保存设置
            </Button>
          </div>
        </div>
      </form>

      <Card>
        <CardHeader
          title="主题预览"
          subtitle="导入本地 Discourse 时使用的管理员 API Key。"
          action={
            <Badge tone={previewConfigured ? 'emerald' : 'slate'}>
              {previewConfigured ? '已配置' : '未配置'}
            </Badge>
          }
        />
        <div className="space-y-4 p-5">
          <p className="text-xs leading-5 text-slate-400">
            在 Discourse 管理后台的 API Keys 页面创建 Key：用户级别选择 Single User，并勾选
            Global Key。Key 仅保存在本机 0600 权限的 secrets.json 中，不会写入配置文件或日志。
          </p>

          {!previewConfigured || previewKeyOpen ? (
            <form onSubmit={submitPreviewKey} className="space-y-3">
              <Field
                label={previewConfigured ? '新的 API Key' : 'API Key'}
                htmlFor="settings-theme-preview-key"
                error={previewKeyError}
                hint="至少 16 个字符；保存后不再回显。"
              >
                <input
                  id="settings-theme-preview-key"
                  type="password"
                  autoComplete="off"
                  className={inputClass}
                  value={previewKey}
                  onChange={(event) => {
                    setPreviewKey(event.target.value)
                    setPreviewKeyError('')
                  }}
                  placeholder="粘贴 Discourse 管理员 API Key"
                />
              </Field>
              <div className="flex flex-wrap items-center gap-2">
                <Button type="submit" disabled={savePreviewKey.isPending || !previewKey.trim()}>
                  {savePreviewKey.isPending ? <Spinner /> : null}
                  保存 Key
                </Button>
                {previewConfigured ? (
                  <>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setPreviewKeyOpen(false)
                        setPreviewKey('')
                        setPreviewKeyError('')
                      }}
                      disabled={savePreviewKey.isPending}
                    >
                      取消
                    </Button>
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => clearPreviewKey.mutate()}
                      disabled={clearPreviewKey.isPending}
                    >
                      {clearPreviewKey.isPending ? <Spinner /> : null}
                      清除已保存 Key
                    </Button>
                  </>
                ) : null}
              </div>
            </form>
          ) : (
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
              <p className="text-xs text-slate-300">
                已保存管理员 API Key；主题页可直接预览。
              </p>
              <div className="flex items-center gap-2">
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={() => setPreviewKeyOpen(true)}
                  disabled={clearPreviewKey.isPending}
                >
                  更换 Key
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => clearPreviewKey.mutate()}
                  disabled={clearPreviewKey.isPending}
                >
                  {clearPreviewKey.isPending ? <Spinner /> : null}
                  清除
                </Button>
              </div>
            </div>
          )}
        </div>
      </Card>

      <ProfileSourcesCard />

      <Card>
        <CardHeader
          title="存储路径"
          subtitle="配置与日志位于实例根目录；项目与服务数据可位于 WSL2 数据目录。"
        />
        {settings.data.paths.data_root_legacy || settings.data.paths.data_root_pending ? (
          <div className="mx-5 mt-4">
            <StorageMigrationCard />
          </div>
        ) : null}
        <div className="grid gap-x-8 gap-y-2 p-5 md:grid-cols-2">
          <PathRow label="实例根目录" value={settings.data.paths.root} />
          <PathRow label="数据目录" value={settings.data.paths.data_root} />
          <PathRow label="配置与日志位置" value={settings.data.paths.data_root_split ? '实例根目录' : '与数据目录相同（旧布局）'} />
          <PathRow label="配置文件" value={settings.data.paths.config_file} />
          <PathRow label="Workspaces" value={settings.data.paths.workspaces_dir} />
          <PathRow label="模板来源清单" value={settings.data.paths.profile_sources_file} />
          <PathRow label="模板来源检出" value={settings.data.paths.profile_sources_dir} />
          <PathRow label="日志" value={settings.data.paths.logs_dir} />
          <PathRow label="操作审计" value={settings.data.paths.audit_dir} />
          <PathRow label="运行时" value={settings.data.paths.runtime_dir} />
          <PathRow label="共享服务" value={settings.data.paths.services_dir} />
          <PathRow label="临时目录" value={settings.data.paths.tmp_dir} />
        </div>
      </Card>
    </div>
  )
}

function Header() {
  return (
    <div>
      <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
        Settings
      </p>
      <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">设置</h1>
    </div>
  )
}

function Toggle({
  label,
  hint,
  checked,
  onChange,
}: {
  label: string
  hint: string
  checked: boolean
  onChange: (checked: boolean) => void
}) {
  return (
    <label className="flex cursor-pointer items-start gap-3 rounded-lg border border-slate-700 bg-slate-950/50 px-3 py-3">
      <input
        type="checkbox"
        className="mt-0.5 h-4 w-4 accent-cyan-400"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>
        <span className="block text-xs font-medium text-slate-200">{label}</span>
        <span className="mt-1 block text-[11px] leading-5 text-slate-500">{hint}</span>
      </span>
    </label>
  )
}

function PathRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="flex min-w-0 items-baseline gap-3 border-b border-slate-800/50 py-1.5 text-xs last:border-b-0">
      <span className="w-24 shrink-0 text-slate-500">{label}</span>
      <span className="truncate font-mono text-slate-300" title={typeof value === 'string' ? value : undefined}>
        {value}
      </span>
    </div>
  )
}

function requiredField(value: string, label: string): string {
  const trimmed = value.trim()
  if (!trimmed) {
    throw new Error(`${label}不能为空`)
  }
  if (/[\r\n\u0000]/.test(trimmed)) {
    throw new Error(`${label}包含非法字符`)
  }
  return trimmed
}

function integerField(value: string, label: string, min: number, max: number): number {
  const parsed = Number(value)
  if (!Number.isInteger(parsed) || parsed < min || parsed > max) {
    throw new Error(`${label}必须是 ${min}–${max} 之间的整数`)
  }
  return parsed
}
