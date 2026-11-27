import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { api } from '../api/client'
import { useProfileSources } from '../api/hooks'
import type { ProfileSource } from '../api/types'
import { errorMessage } from '../lib/format'
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
} from './ui'

type Notice = { tone: 'success' | 'error'; text: string }

export function ProfileSourcesCard() {
  const queryClient = useQueryClient()
  const sources = useProfileSources()
  const [location, setLocation] = useState('')
  const [branch, setBranch] = useState('')
  const [notice, setNotice] = useState<Notice | null>(null)

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['profile-sources'] })
    void queryClient.invalidateQueries({ queryKey: ['profiles'] })
    void queryClient.invalidateQueries({ queryKey: ['profile-check'] })
  }

  const add = useMutation({
    mutationFn: api.addProfileSource,
    onSuccess: (source: ProfileSource) => {
      setLocation('')
      setBranch('')
      setNotice({
        tone: 'success',
        text: `已注册模板来源「${source.name}」，加载 ${source.profile_ids.length} 个 Profile`,
      })
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const refresh = useMutation({
    mutationFn: (id: string) => api.refreshProfileSource(id),
    onSuccess: (source: ProfileSource) => {
      setNotice({
        tone: 'success',
        text: `已刷新模板来源「${source.name}」`,
      })
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const remove = useMutation({
    mutationFn: (id: string) => api.removeProfileSource(id),
    onSuccess: () => {
      setNotice({ tone: 'success', text: '已注销模板来源' })
      invalidate()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const busy = add.isPending || refresh.isPending || remove.isPending

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!location.trim() || busy) {
      return
    }
    setNotice(null)
    add.mutate({
      location: location.trim(),
      branch: branch.trim() || undefined,
    })
  }

  const confirmRemove = (source: ProfileSource) => {
    if (
      !window.confirm(
        `注销模板来源「${source.name || source.id}」？Git 来源的 DevKit 检出目录会删除，本地目录不会被删除。`,
      )
    ) {
      return
    }
    setNotice(null)
    remove.mutate(source.id)
  }

  return (
    <Card>
      <CardHeader
        title="第三方模板来源"
        subtitle="Project Profile 来源目录根需包含 devkit-profiles.json；DevKit 只读取清单，不会执行仓库中的代码。"
      />
      <div className="space-y-4 p-5">
        <form onSubmit={submit} className="grid gap-3 md:grid-cols-[minmax(0,1fr)_180px_auto] md:items-end">
          <Field
            label="来源地址"
            htmlFor="profile-source-location"
            hint="本机目录绝对路径或 Git URL（https/ssh/git）。"
          >
            <input
              id="profile-source-location"
              className={inputClass}
              value={location}
              onChange={(event) => setLocation(event.target.value)}
              placeholder="D:\\devkit-profiles 或 https://git.example.com/team/profiles.git"
              disabled={busy}
            />
          </Field>
          <Field label="分支" htmlFor="profile-source-branch" hint="仅 Git 来源；留空使用默认分支。">
            <input
              id="profile-source-branch"
              className={inputClass}
              value={branch}
              onChange={(event) => setBranch(event.target.value)}
              placeholder="main"
              disabled={busy}
            />
          </Field>
          <Button type="submit" disabled={busy || !location.trim()}>
            {add.isPending ? <Spinner /> : null}
            添加来源
          </Button>
        </form>

        {notice ? (
          <div
            className={`rounded-xl border px-4 py-3 text-sm ${
              notice.tone === 'success'
                ? 'border-emerald-400/30 bg-emerald-400/10 text-emerald-200'
                : 'border-rose-400/30 bg-rose-400/10 text-rose-200'
            }`}
          >
            {notice.text}
          </div>
        ) : null}

        {sources.isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-20" />
            <Skeleton className="h-20" />
          </div>
        ) : sources.error ? (
          <ErrorNotice error={sources.error} onRetry={() => void sources.refetch()} />
        ) : sources.data && sources.data.items.length === 0 ? (
          <p className="rounded-xl border border-dashed border-slate-800 px-4 py-6 text-center text-xs text-slate-500">
            尚未注册第三方模板来源。添加后，来源中的 Profile 会出现在新建 Workspace 的模板列表中。
          </p>
        ) : (
          <ul className="space-y-2">
            {sources.data?.items.map((source) => (
              <li
                key={source.id}
                className="rounded-xl border border-slate-800 bg-slate-950/40 p-4"
              >
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-medium text-slate-200">
                    {source.name || source.id}
                  </span>
                  <Badge tone={source.kind === 'git' ? 'cyan' : 'slate'}>
                    {source.kind === 'git' ? 'Git' : '本地'}
                  </Badge>
                  {source.version ? <Badge tone="slate">{source.version}</Badge> : null}
                  <Badge tone={source.last_error ? 'rose' : 'emerald'}>
                    {source.last_error ? '加载失败' : '已加载'}
                  </Badge>
                  <span className="text-[11px] text-slate-500">
                    {source.profile_ids.length} 个 Profile
                  </span>
                  <div className="ml-auto flex items-center gap-2">
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => {
                        setNotice(null)
                        refresh.mutate(source.id)
                      }}
                      disabled={busy}
                    >
                      {refresh.isPending && refresh.variables === source.id ? (
                        <Spinner className="h-3 w-3" />
                      ) : null}
                      刷新
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      onClick={() => confirmRemove(source)}
                      disabled={busy}
                    >
                      注销
                    </Button>
                  </div>
                </div>
                <p className="mt-2 break-all font-mono text-[11px] leading-5 text-slate-400">
                  {source.location}
                  {source.branch ? ` @ ${source.branch}` : ''}
                </p>
                {source.commit ? (
                  <p className="text-[11px] text-slate-500">提交 {source.commit}</p>
                ) : null}
                {source.last_error ? (
                  <p className="mt-1 text-xs leading-5 text-rose-300">{source.last_error}</p>
                ) : null}
                {source.profile_ids.length > 0 ? (
                  <p className="mt-1 break-all text-[11px] leading-5 text-slate-500">
                    {source.profile_ids.join(' · ')}
                  </p>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </div>
    </Card>
  )
}
