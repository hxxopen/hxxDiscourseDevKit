import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { useExtensions, useWorkspaces } from '../api/hooks'
import type { ExtensionCreateInput, ExtensionKind } from '../api/types'
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
import { errorMessage, formatDate } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

interface FormState {
  name: string
  slug: string
  about: string
  version: string
  authors: string
}

const emptyForm: FormState = {
  name: '',
  slug: '',
  about: '',
  version: '',
  authors: '',
}

const copy: Record<
  ExtensionKind,
  {
    eyebrow: string
    title: string
    description: string
    create: string
    directory: string
    singular: string
  }
> = {
  plugin: {
    eyebrow: 'Plugins',
    title: '插件',
    description:
      '管理 Workspace 主 Discourse 源码 plugins/ 目录中的开发插件，并创建标准脚手架。',
    create: '新建插件',
    directory: '插件目录',
    singular: '插件',
  },
  theme: {
    eyebrow: 'Themes',
    title: '主题',
    description:
      '为 Workspace 创建主题脚手架，配合 discourse_theme 预览与构建；文件位于 data/themes/。',
    create: '新建主题',
    directory: '主题目录',
    singular: '主题',
  },
}

export function ExtensionsPage({ kind }: { kind: ExtensionKind }) {
  const queryClient = useQueryClient()
  const workspaces = useWorkspaces()
  const [selectedId, setSelectedId] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState<FormState>(emptyForm)
  const [formError, setFormError] = useState('')

  const list = workspaces.data?.items ?? []
  const activeId = workspaces.data?.active_workspace_id ?? ''
  const currentId = selectedId || activeId || list[0]?.workspace_id || ''
  const listing = useExtensions(kind, currentId || null)
  const texts = copy[kind]
  const sourceReady = listing.data?.source_installed ?? false
  const canCreate = Boolean(currentId) && (kind === 'theme' || sourceReady)

  useEffect(() => {
    if (!selectedId && activeId) {
      setSelectedId(activeId)
    }
  }, [activeId, selectedId])

  const create = useMutation({
    mutationFn: (input: ExtensionCreateInput) => api.createExtension(kind, currentId, input),
    onSuccess: (created) => {
      setOpen(false)
      setForm(emptyForm)
      setFormError('')
      setNotice({ tone: 'success', text: `已创建${texts.singular} ${created.name}（${created.slug}）` })
      void queryClient.invalidateQueries({ queryKey: ['extensions', kind, currentId] })
      void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    },
    onError: (error) => setFormError(errorMessage(error)),
  })

  const openModal = () => {
    setForm(emptyForm)
    setFormError('')
    setOpen(true)
  }

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    try {
      const name = requiredField(form.name, '名称')
      const slug = form.slug.trim().toLowerCase()
      if (slug && !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(slug)) {
        throw new Error('Slug 必须以字母或数字开头，只能包含小写字母、数字、点、下划线和连字符')
      }
      if (form.about.length > 500) {
        throw new Error('描述最多 500 个字符')
      }
      create.mutate({
        name,
        slug: slug || undefined,
        about: form.about.trim(),
        version: form.version.trim(),
        authors: form.authors.trim(),
      })
    } catch (error) {
      setFormError(errorMessage(error))
    }
  }

  const refresh = () => {
    void listing.refetch()
    void workspaces.refetch()
  }

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            {texts.eyebrow}
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">{texts.title}</h1>
          <p className="mt-1 text-sm text-slate-400">{texts.description}</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <select
            className={`${inputClass} w-56`}
            value={currentId}
            onChange={(event) => {
              setSelectedId(event.target.value)
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
          <Button variant="secondary" onClick={refresh} disabled={!currentId || listing.isFetching}>
            {listing.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
          <Button
            onClick={openModal}
            disabled={!canCreate}
            title={
              kind === 'plugin' && !sourceReady
                ? '需要先在当前 Workspace 安装 Discourse 源码'
                : undefined
            }
          >
            {texts.create}
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
            description="先在 Workspaces 页面创建 Workspace，再回来创建插件或主题。"
          />
        </Card>
      ) : listing.isLoading ? (
        <div className="space-y-4">
          <Skeleton className="h-24 w-full" />
          <div className="grid gap-4 md:grid-cols-2">
            <Skeleton className="h-40 w-full" />
            <Skeleton className="h-40 w-full" />
          </div>
        </div>
      ) : listing.error ? (
        <div className="space-y-4">
          <ErrorNotice error={listing.error} onRetry={() => void listing.refetch()} />
        </div>
      ) : (
        <>
          {!sourceReady ? (
            <div
              className={`rounded-xl border px-4 py-3 text-sm ${
                kind === 'plugin'
                  ? 'border-amber-400/30 bg-amber-400/10 text-amber-200'
                  : 'border-slate-700 bg-slate-900/50 text-slate-300'
              }`}
            >
              {kind === 'plugin'
                ? '当前 Workspace 尚未安装 Discourse 源码。请先在项目页克隆或初始化源码，再创建插件。'
                : '当前 Workspace 尚无 Discourse 源码。可以创建主题脚手架，但预览与构建需要可用的 Discourse 开发环境。'}
            </div>
          ) : null}

          <Card>
            <CardHeader
              title={texts.directory}
              subtitle={listing.data?.root}
              action={<Badge tone="slate">{listing.data?.total ?? 0} 个</Badge>}
            />
            <div className="flex flex-wrap gap-2 px-5 py-3">
              {(listing.data?.checks ?? []).map((check) => (
                <Badge key={check.name} tone={check.ok ? 'emerald' : 'amber'}>
                  {check.name}：{check.ok ? '正常' : '未就绪'}
                </Badge>
              ))}
            </div>
          </Card>

          {(listing.data?.items ?? []).length === 0 ? (
            <Card>
              <EmptyState
                title={`还没有${texts.singular}`}
                description={
                  kind === 'plugin'
                    ? '创建后会生成 plugin.rb、语言文件与客户端初始化器，并直接放在源码的 plugins/ 目录。'
                    : '创建后会生成 about.json、SCSS 目录与 README，可用于 discourse_theme 工作流。'
                }
                action={
                  <Button onClick={openModal} disabled={!canCreate}>
                    {texts.create}
                  </Button>
                }
              />
            </Card>
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {(listing.data?.items ?? []).map((item) => (
                <Card key={item.slug} className="flex flex-col p-5">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="truncate text-base font-semibold text-slate-100">
                          {item.name}
                        </h3>
                        <Badge tone="cyan">{item.slug}</Badge>
                        {item.version ? <Badge tone="slate">v{item.version}</Badge> : null}
                        <Badge tone={item.has_metadata ? 'emerald' : 'amber'}>
                          {item.has_metadata ? '元数据已解析' : '缺少元数据'}
                        </Badge>
                      </div>
                      <p className="mt-2 text-sm leading-6 text-slate-400">
                        {item.about || '未填写描述'}
                      </p>
                    </div>
                  </div>

                  <dl className="mt-4 grid gap-2 text-xs sm:grid-cols-2">
                    <MetaRow label="作者" value={item.authors || '—'} />
                    <MetaRow label="文件数" value={String(item.file_count)} />
                    <MetaRow label="更新时间" value={formatDate(item.modified_at)} />
                    <MetaRow label="目录" value={item.path} mono title={item.path} />
                  </dl>

                  <p className="mt-4 border-t border-slate-800/70 pt-3 text-[11px] leading-5 text-slate-500">
                    {kind === 'plugin'
                      ? '修改插件后需重启 Rails 开发服务器；Discourse 会自动加载 plugins/ 下的目录。'
                      : '在主题目录中运行 discourse_theme watch . 可将本地修改同步到运行中的 Discourse。'}
                  </p>
                </Card>
              ))}
            </div>
          )}
        </>
      )}

      <Modal
        open={open}
        onClose={() => {
          if (!create.isPending) {
            setOpen(false)
          }
        }}
        title={texts.create}
        description={
          kind === 'plugin'
            ? '在源码 plugins/ 目录生成插件脚手架；创建后需要重启 Discourse 才会加载。'
            : '在 Workspace 的 data/themes/ 目录生成主题脚手架。'
        }
        footer={
          <>
            <Button
              variant="ghost"
              onClick={() => setOpen(false)}
              disabled={create.isPending}
            >
              取消
            </Button>
            <Button type="submit" form="extension-form" disabled={create.isPending}>
              {create.isPending ? <Spinner /> : null}
              创建{texts.singular}
            </Button>
          </>
        }
      >
        <form id="extension-form" className="space-y-4" onSubmit={submit}>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="名称" htmlFor="extension-name" hint="显示名称，最多 64 个字符。">
              <input
                id="extension-name"
                className={inputClass}
                value={form.name}
                autoFocus
                onChange={(event) => {
                  setForm((current) => ({ ...current, name: event.target.value }))
                  setFormError('')
                }}
                placeholder={kind === 'plugin' ? '我的 Discourse 插件' : '我的 Discourse 主题'}
              />
            </Field>
            <Field
              label="目录名（可选）"
              htmlFor="extension-slug"
              hint="留空时根据名称生成；只能使用小写字母、数字、点、下划线和连字符。"
            >
              <input
                id="extension-slug"
                className={inputClass}
                value={form.slug}
                onChange={(event) => {
                  setForm((current) => ({ ...current, slug: event.target.value }))
                  setFormError('')
                }}
                placeholder="my-extension"
              />
            </Field>
            <Field label="版本" htmlFor="extension-version" hint="默认 0.1.0。">
              <input
                id="extension-version"
                className={inputClass}
                value={form.version}
                onChange={(event) =>
                  setForm((current) => ({ ...current, version: event.target.value }))
                }
                placeholder="0.1.0"
              />
            </Field>
            <Field label="作者" htmlFor="extension-authors" hint="默认 DevKit。">
              <input
                id="extension-authors"
                className={inputClass}
                value={form.authors}
                onChange={(event) =>
                  setForm((current) => ({ ...current, authors: event.target.value }))
                }
                placeholder="Your Name"
              />
            </Field>
          </div>
          <Field label="描述" htmlFor="extension-about" hint="最多 500 个字符，写入插件/主题元数据。">
            <textarea
              id="extension-about"
              className={`${inputClass} min-h-24`}
              value={form.about}
              onChange={(event) => {
                setForm((current) => ({ ...current, about: event.target.value }))
                setFormError('')
              }}
              placeholder={
                kind === 'plugin' ? '这个插件提供什么能力？' : '这个主题的视觉与交互目标？'
              }
            />
          </Field>
          {formError ? (
            <div className="rounded-lg border border-rose-400/30 bg-rose-400/10 px-3 py-2 text-xs text-rose-200">
              {formError}
            </div>
          ) : null}
        </form>
      </Modal>
    </div>
  )
}

function MetaRow({
  label,
  value,
  mono = false,
  title,
}: {
  label: string
  value: string
  mono?: boolean
  title?: string
}) {
  return (
    <div className="flex min-w-0 items-baseline gap-2">
      <dt className="w-14 shrink-0 text-slate-500">{label}</dt>
      <dd
        className={`min-w-0 truncate ${mono ? 'font-mono text-[11px]' : ''} text-slate-300`}
        title={title}
      >
        {value}
      </dd>
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
