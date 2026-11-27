import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
import { useSettings } from '../api/hooks'
import type { ExtensionItem, ThemeBuildResult, ThemePreviewResult } from '../api/types'
import { ExtensionCheckView } from './ExtensionCheckView'
import {
  Badge,
  Button,
  ErrorNotice,
  Field,
  Modal,
  Spinner,
  inputClass,
} from './ui'
import { formatBytes, formatDate } from '../lib/format'

type ThemeMode = 'build' | 'preview'

export function ThemeWorkbench({
  workspaceId,
  item,
  mode,
  onClose,
}: {
  workspaceId: string
  item: ExtensionItem
  mode: ThemeMode
  onClose: () => void
}) {
  const queryClient = useQueryClient()
  const settings = useSettings()
  const [buildResult, setBuildResult] = useState<ThemeBuildResult | null>(null)
  const [previewResult, setPreviewResult] = useState<ThemePreviewResult | null>(null)
  const [keyInput, setKeyInput] = useState('')
  const [keyError, setKeyError] = useState('')
  const [showKeyForm, setShowKeyForm] = useState(false)
  const started = useRef(false)

  const build = useMutation({
    mutationFn: () => api.buildTheme(workspaceId, item.slug),
    onSuccess: (result) => {
      setBuildResult(result)
      setPreviewResult(null)
    },
  })

  const preview = useMutation({
    mutationFn: () => api.previewTheme(workspaceId, item.slug),
    onSuccess: (result) => {
      setPreviewResult(result)
      setBuildResult(result.build)
    },
  })

  const saveAndPreview = useMutation({
    mutationFn: async (key: string) => {
      await api.setThemePreviewKey(key)
      await queryClient.invalidateQueries({ queryKey: ['settings'] })
      return api.previewTheme(workspaceId, item.slug)
    },
    onSuccess: (result) => {
      setPreviewResult(result)
      setBuildResult(result.build)
      setKeyInput('')
      setShowKeyForm(false)
      void queryClient.invalidateQueries({ queryKey: ['settings'] })
    },
    onError: (error) => {
      if (error instanceof ApiError && error.code === 'THEME_PREVIEW_KEY_REQUIRED') {
        setShowKeyForm(true)
      }
    },
  })

  const configured = settings.data?.theme_preview?.api_key_configured ?? false
  const busy = build.isPending || preview.isPending || saveAndPreview.isPending
  const mutationError = build.error ?? preview.error ?? saveAndPreview.error

  useEffect(() => {
    if (started.current || !settings.data) {
      return
    }
    started.current = true
    if (mode === 'build') {
      build.mutate()
    } else if (settings.data.theme_preview?.api_key_configured) {
      preview.mutate()
    } else {
      setShowKeyForm(true)
    }
  }, [mode, preview, build, settings.data])

  const startBuild = () => {
    setBuildResult(null)
    setPreviewResult(null)
    build.reset()
    preview.reset()
    saveAndPreview.reset()
    build.mutate()
  }

  const startPreview = () => {
    setBuildResult(null)
    setPreviewResult(null)
    build.reset()
    preview.reset()
    saveAndPreview.reset()
    if (!configured) {
      setShowKeyForm(true)
      return
    }
    preview.mutate()
  }

  const submitKey = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const key = keyInput.trim()
    if (key.length < 16) {
      setKeyError('API Key 长度至少 16 个字符')
      return
    }
    setKeyError('')
    setBuildResult(null)
    setPreviewResult(null)
    preview.reset()
    build.reset()
    saveAndPreview.reset()
    saveAndPreview.mutate(key)
  }

  const retry = () => {
    if (mode === 'build') {
      startBuild()
    } else if (configured) {
      startPreview()
    } else {
      setShowKeyForm(true)
    }
  }

  const title = mode === 'build' ? '构建主题' : '预览主题'
  const result = buildResult

  return (
    <Modal
      open
      onClose={() => {
        if (!busy) {
          onClose()
        }
      }}
      title={title}
      description={`${item.name}（${item.slug}）· ${item.path}`}
      width="max-w-3xl"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={busy}>
            关闭
          </Button>
          {mode === 'build' ? (
            <Button variant="secondary" onClick={startBuild} disabled={busy}>
              {build.isPending ? <Spinner /> : null}
              重新构建
            </Button>
          ) : (
            <Button
              variant="secondary"
              onClick={startPreview}
              disabled={busy || !configured}
              title={!configured ? '请先配置 Discourse 管理员 API Key' : undefined}
            >
              {preview.isPending ? <Spinner /> : null}
              重新预览
            </Button>
          )}
          {previewResult?.preview_url ? (
            <a
              href={previewResult.preview_url}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg border border-cyan-400/30 bg-cyan-400/10 px-3 py-2 text-xs font-medium text-cyan-200 transition hover:bg-cyan-400/20"
            >
              打开预览 ↗
            </a>
          ) : null}
        </>
      }
    >
      <div className="space-y-4">
        {mode === 'preview' && (!configured || showKeyForm) ? (
          <form
            onSubmit={submitKey}
            className="space-y-3 rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3"
          >
            <div>
              <p className="text-sm font-medium text-amber-100">需要 Discourse 管理员 API Key</p>
              <p className="mt-1 text-xs leading-5 text-amber-200/80">
                在 Discourse 管理后台的 API Keys 页面创建：用户级别选择 Single User，并勾选
                Global Key。Key 只保存在本机 0600 权限的 secrets.json 中。
              </p>
            </div>
            <Field label="API Key" htmlFor="theme-preview-key" hint="不会显示在页面或日志中。">
              <input
                id="theme-preview-key"
                type="password"
                autoFocus
                className={inputClass}
                value={keyInput}
                onChange={(event) => {
                  setKeyInput(event.target.value)
                  setKeyError('')
                }}
                placeholder="粘贴 Discourse API Key"
              />
            </Field>
            {keyError ? (
              <p className="text-xs text-rose-200">{keyError}</p>
            ) : null}
            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={saveAndPreview.isPending}>
                {saveAndPreview.isPending ? <Spinner /> : null}
                保存并预览
              </Button>
              {configured ? (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => {
                    setShowKeyForm(false)
                    setKeyError('')
                  }}
                  disabled={saveAndPreview.isPending}
                >
                  取消
                </Button>
              ) : null}
            </div>
          </form>
        ) : null}

        {mutationError ? (
          <ErrorNotice error={mutationError} onRetry={retry} />
        ) : null}

        {busy ? (
          <div className="flex items-center gap-3 rounded-xl border border-slate-700 bg-slate-900/60 px-4 py-6 text-sm text-slate-300">
            <Spinner />
            {mode === 'build'
              ? '正在检查元数据并构建分发包…'
              : '正在构建并导入到运行中的本地 Discourse…'}
          </div>
        ) : null}

        {result ? (
          <>
            <div
              className={`rounded-xl border px-4 py-3 text-sm ${
                result.ready
                  ? 'border-emerald-400/30 bg-emerald-400/10 text-emerald-200'
                  : 'border-rose-400/30 bg-rose-400/10 text-rose-200'
              }`}
            >
              {result.ready
                ? result.message || '构建完成。'
                : result.message || '构建未生成分发包，请先处理检查错误。'}
            </div>

            {result.ready && result.package ? (
              <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge tone="emerald">已打包</Badge>
                  <span className="text-sm font-medium text-slate-100">
                    {result.package.file_name}
                  </span>
                  <span className="text-xs text-slate-500">
                    {result.package.file_count} 个文件 · {formatBytes(result.package.size_bytes)}
                  </span>
                </div>
                <p className="mt-2 break-all font-mono text-[11px] text-slate-500">
                  {result.package.path}
                </p>
                <p className="mt-1 break-all font-mono text-[10px] text-slate-600">
                  SHA-256 {result.package.sha256}
                </p>
              </div>
            ) : null}

            {previewResult?.preview_url ? (
              <div className="space-y-3 rounded-xl border border-cyan-400/30 bg-cyan-400/10 px-4 py-3">
                <div className="flex flex-wrap items-center gap-2">
                  <Badge tone="cyan">
                    {previewResult.action === 'updated' ? '已更新预览主题' : '已创建预览主题'}
                  </Badge>
                  <span className="text-xs text-cyan-200/80">
                    主题 ID {previewResult.theme_id} · {previewResult.site_url}
                  </span>
                </div>
                <div className="grid gap-2 text-xs">
                  <LinkRow label="预览" value={previewResult.preview_url} />
                  {previewResult.admin_url ? (
                    <LinkRow label="管理" value={previewResult.admin_url} />
                  ) : null}
                </div>
                <p className="text-[11px] leading-5 text-cyan-200/70">
                  预览链接只对当前 Discourse 生效；未登录管理员账号时可能看不到未应用的样式。
                </p>
              </div>
            ) : null}

            {mode === 'preview' && result.ready && !previewResult?.preview_url && !busy ? (
              <p className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3 text-xs leading-5 text-slate-400">
                构建已完成，但尚未导入预览。确认 Discourse 正在运行后点击“重新预览”。
              </p>
            ) : null}

            <div>
              <p className="mb-2 text-xs font-medium text-slate-400">检查详情</p>
              <ExtensionCheckView report={result.check} />
            </div>

            <p className="text-[11px] text-slate-600">
              检查时间 {formatDate(result.check.checked_at)} · 重新构建会覆盖同名 ZIP。
            </p>
          </>
        ) : null}

        {configured && !showKeyForm && mode === 'preview' ? (
          <div className="flex flex-wrap items-center justify-between gap-2 border-t border-slate-800/70 pt-3">
            <p className="text-[11px] text-slate-500">
              已保存 Discourse 管理员 API Key；预览会新建或更新同名预览主题。
            </p>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setShowKeyForm(true)}
              disabled={busy}
            >
              更换 Key
            </Button>
          </div>
        ) : null}
      </div>
    </Modal>
  )
}

function LinkRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex min-w-0 items-baseline gap-2">
      <span className="w-10 shrink-0 text-cyan-200/60">{label}</span>
      <a
        href={value}
        target="_blank"
        rel="noreferrer"
        className="min-w-0 truncate font-mono text-[11px] text-cyan-100 underline decoration-cyan-400/40 underline-offset-2 hover:decoration-cyan-300"
        title={value}
      >
        {value}
      </a>
    </div>
  )
}
