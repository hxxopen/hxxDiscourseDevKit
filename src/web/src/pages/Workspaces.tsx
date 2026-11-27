import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { ApiError, api } from '../api/client'
import {
  isTerminalTask,
  useProfileCheck,
  useProfiles,
  useTask,
  useWorkspaceStatus,
  useWorkspaces,
} from '../api/hooks'
import type { DeletePlan, ProfileCheckStatus, TaskStatus, Workspace } from '../api/types'
import {
  Badge,
  Button,
  Card,
  EmptyState,
  ErrorNotice,
  Field,
  Modal,
  ProgressBar,
  Skeleton,
  Spinner,
  StateBadge,
  inputClass,
} from '../components/ui'
import { errorMessage, formatBytes, formatDate } from '../lib/format'

type Notice = { tone: 'success' | 'error'; text: string }

const invalidNamePattern = /[\/\\:*?"<>|\u0000-\u001f\u007f]/

function validateName(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return '请输入 Workspace 名称'
  if (trimmed.length > 64) return '名称不能超过 64 个字符'
  if (trimmed === '.' || trimmed === '..') return '该名称为保留名称'
  if (invalidNamePattern.test(trimmed)) return '名称包含不允许的字符（/ \\ : * ? " < > |）'
  return ''
}

function taskTone(status: TaskStatus): 'slate' | 'cyan' | 'emerald' | 'rose' | 'amber' {
  switch (status) {
    case 'succeeded':
      return 'emerald'
    case 'failed':
      return 'rose'
    case 'canceled':
      return 'amber'
    case 'running':
      return 'cyan'
    default:
      return 'slate'
  }
}

const taskLabels: Record<TaskStatus, string> = {
  queued: '排队中',
  running: '执行中',
  succeeded: '已完成',
  failed: '失败',
  canceled: '已取消',
}

function profileCheckTone(status: ProfileCheckStatus): 'emerald' | 'slate' | 'amber' | 'rose' {
  switch (status) {
    case 'ok':
      return 'emerald'
    case 'warning':
      return 'amber'
    case 'error':
      return 'rose'
    default:
      return 'slate'
  }
}

const profileCheckLabels: Record<ProfileCheckStatus, string> = {
  ok: '通过',
  info: '信息',
  warning: '提醒',
  error: '错误',
}

const resourceLabels: Record<string, string> = {
  metadata: '管理记录',
  source: '源码',
  config: '配置',
  logs: '日志',
  data: '数据',
  backups: '备份',
  tmp: '临时文件',
  database: '数据库',
  redis: 'Redis',
}

export function Workspaces() {
  const queryClient = useQueryClient()
  const workspaces = useWorkspaces()
  const [createOpen, setCreateOpen] = useState(false)
  const [inspectId, setInspectId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Workspace | null>(null)
  const [notice, setNotice] = useState<Notice | null>(null)

  const invalidateAll = () => {
    void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
    void queryClient.invalidateQueries({ queryKey: ['info'] })
  }

  const activateMutation = useMutation({
    mutationFn: api.activateWorkspace,
    onSuccess: (result) => {
      setNotice({ tone: 'success', text: `活动 Workspace 已切换为「${result.workspace.name}」` })
      invalidateAll()
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const provisionMutation = useMutation({
    mutationFn: api.provisionWorkspace,
    onSuccess: (workspace) => {
      if (workspace.database.provisioned) {
        setNotice({ tone: 'success', text: `「${workspace.name}」数据库已就绪` })
      } else {
        setNotice({
          tone: 'error',
          text: `数据库创建未完成：${workspace.database.provision_error || 'PostgreSQL 服务不可用'}`,
        })
      }
      invalidateAll()
      void queryClient.invalidateQueries({ queryKey: ['workspace-status'] })
      void queryClient.invalidateQueries({ queryKey: ['diagnostics'] })
    },
    onError: (error) => setNotice({ tone: 'error', text: errorMessage(error) }),
  })

  const list = workspaces.data?.items ?? []
  const activeId = workspaces.data?.active_workspace_id ?? ''

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-[11px] font-semibold uppercase tracking-[0.22em] text-cyan-400/80">
            Workspaces
          </p>
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">工作区</h1>
          <p className="mt-1 text-sm text-slate-400">
            每个 Workspace 拥有独立的端口、数据库、Redis 命名空间与目录。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={() => void workspaces.refetch()}>
            {workspaces.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
          <Button data-testid="workspace-create" onClick={() => setCreateOpen(true)}>
            <span className="text-base leading-none">+</span>
            新建 Workspace
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

      {workspaces.error ? (
        <ErrorNotice error={workspaces.error} onRetry={() => void workspaces.refetch()} />
      ) : null}

      <Card>
        {workspaces.isLoading ? (
          <div className="space-y-3 p-5">
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
            <Skeleton className="h-12 w-full" />
          </div>
        ) : list.length === 0 ? (
          <EmptyState
            title="还没有 Workspace"
            description="创建一个 Workspace 来获得独立的开发端口、数据库与 Redis 命名空间。"
            action={<Button onClick={() => setCreateOpen(true)}>创建第一个 Workspace</Button>}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[880px] border-collapse text-left text-sm">
              <thead>
                <tr className="border-b border-slate-800 text-[11px] uppercase tracking-wide text-slate-500">
                  <th className="whitespace-nowrap px-5 py-3 font-medium">名称</th>
                  <th className="whitespace-nowrap px-4 py-3 font-medium">状态</th>
                  <th className="whitespace-nowrap px-4 py-3 font-medium">开发端口</th>
                  <th className="whitespace-nowrap px-4 py-3 font-medium">隔离资源</th>
                  <th className="whitespace-nowrap px-5 py-3 text-right font-medium">操作</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/70">
                {list.map((workspace) => {
                  const isActive = workspace.workspace_id === activeId
                  const activating =
                    activateMutation.isPending &&
                    activateMutation.variables === workspace.workspace_id
                  return (
                    <tr key={workspace.workspace_id} className="group transition hover:bg-slate-900/40">
                      <td className="max-w-[280px] px-5 py-4">
                        <div className="flex items-center gap-2">
                          <span className="truncate font-medium text-slate-100">
                            {workspace.name}
                          </span>
                          {isActive ? <Badge tone="cyan">活动</Badge> : null}
                        </div>
                        <p className="mt-0.5 truncate text-xs text-slate-500">
                          {workspace.description || '未填写描述'}
                        </p>
                        <p className="mt-1 truncate font-mono text-[10px] text-slate-600">
                          {workspace.workspace_id}
                        </p>
                      </td>
                      <td className="px-4 py-4">
                        <StateBadge state={workspace.state} />
                        {workspace.last_error ? (
                          <p
                            className="mt-1 max-w-[180px] truncate text-[11px] text-rose-300"
                            title={workspace.last_error}
                          >
                            {workspace.last_error}
                          </p>
                        ) : null}
                      </td>
                      <td className="px-4 py-4">
                        <span className="font-mono text-xs text-slate-300">
                          :{workspace.network.http_port}
                        </span>
                        <p className="mt-0.5 text-[11px] text-slate-500">
                          {workspace.network.host}
                        </p>
                      </td>
                      <td className="px-4 py-4">
                        <p className="max-w-[220px] truncate font-mono text-[11px] text-slate-300">
                          {workspace.database.database_name}
                        </p>
                        <div className="mt-1 flex flex-wrap items-center gap-1.5">
                          <Badge tone={workspace.database.provisioned ? 'emerald' : 'amber'}>
                            {workspace.database.provisioned ? 'DB 已就绪' : 'DB 待创建'}
                          </Badge>
                          <span className="text-[11px] text-slate-500">
                            Redis DB {workspace.redis.db_index} · {workspace.project_type}
                          </span>
                        </div>
                        {!workspace.database.provisioned && workspace.database.provision_error ? (
                          <p
                            className="mt-1 max-w-[220px] truncate text-[10px] text-amber-300/80"
                            title={workspace.database.provision_error}
                          >
                            {workspace.database.provision_error}
                          </p>
                        ) : null}
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex items-center justify-end gap-1">
                          {!workspace.database.provisioned ? (
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={
                                provisionMutation.isPending &&
                                provisionMutation.variables === workspace.workspace_id
                              }
                              onClick={() => provisionMutation.mutate(workspace.workspace_id)}
                            >
                              {provisionMutation.isPending &&
                              provisionMutation.variables === workspace.workspace_id ? (
                                <Spinner className="h-3 w-3" />
                              ) : null}
                              创建数据库
                            </Button>
                          ) : null}
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => setInspectId(workspace.workspace_id)}
                          >
                            详情
                          </Button>
                          {!isActive ? (
                            <Button
                              variant="secondary"
                              size="sm"
                              disabled={activating}
                              onClick={() => activateMutation.mutate(workspace.workspace_id)}
                            >
                              {activating ? <Spinner className="h-3 w-3" /> : null}
                              设为活动
                            </Button>
                          ) : null}
                          <Button
                            variant="ghost"
                            size="sm"
                            data-testid="workspace-delete"
                            className="text-rose-300 hover:bg-rose-500/10 hover:text-rose-200"
                            onClick={() => setDeleteTarget(workspace)}
                          >
                            删除
                          </Button>
                        </div>
                      </td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      <p className="text-xs leading-5 text-slate-500">
        删除 Workspace 会先展示资源预览并要求二次确认。彻底删除模式会同时删除该 Workspace 的数据库与
        Redis 逻辑 DB；仅移除记录模式会保留共享服务中的数据。
      </p>

      <CreateWorkspaceModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={(workspace) => {
          setCreateOpen(false)
          setNotice({ tone: 'success', text: `已创建 Workspace「${workspace.name}」` })
          invalidateAll()
        }}
      />

      {inspectId ? (
        <InspectWorkspaceModal workspaceId={inspectId} onClose={() => setInspectId(null)} />
      ) : null}

      {deleteTarget ? (
        <DeleteWorkspaceModal
          target={deleteTarget}
          onClose={() => setDeleteTarget(null)}
          onDeleted={() => {
            setNotice({
              tone: 'success',
              text: `Workspace「${deleteTarget.name}」已删除`,
            })
            invalidateAll()
          }}
        />
      ) : null}
    </div>
  )
}

function CreateWorkspaceModal({
  open,
  onClose,
  onCreated,
}: {
  open: boolean
  onClose: () => void
  onCreated: (workspace: Workspace) => void
}) {
  const profiles = useProfiles()
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [profileId, setProfileId] = useState('')
  const [repositoryURL, setRepositoryURL] = useState('')
  const [branch, setBranch] = useState('')
  const [acceptProfile, setAcceptProfile] = useState(false)
  const initializedProfile = useRef('')

  const selectedProfile =
    profiles.data?.items.find((item) => item.id === profileId) ?? null
  const profileCheck = useProfileCheck(selectedProfile?.id ?? null, open)

  const mutation = useMutation({
    mutationFn: api.createWorkspace,
    onSuccess: (workspace) => {
      setName('')
      setDescription('')
      onCreated(workspace)
    },
  })

  const wasOpen = useRef(false)
  useEffect(() => {
    if (open && !wasOpen.current) {
      mutation.reset()
      setProfileId('')
      setRepositoryURL('')
      setBranch('')
      setAcceptProfile(false)
      initializedProfile.current = ''
    }
    wasOpen.current = open
  }, [open])

  useEffect(() => {
    if (!open) return
    const items = profiles.data?.items
    if (!items?.length) return
    if (!items.some((item) => item.id === profileId)) {
      setProfileId(items[0].id)
    }
  }, [open, profiles.data, profileId])

  useEffect(() => {
    if (!open || !selectedProfile) return
    if (initializedProfile.current === selectedProfile.id) return
    initializedProfile.current = selectedProfile.id
    setRepositoryURL(selectedProfile.default_repository_url ?? '')
    setBranch(selectedProfile.default_branch ?? '')
  }, [open, selectedProfile])

  useEffect(() => {
    setAcceptProfile(false)
  }, [profileId])

  const selectProfile = (id: string) => {
    setProfileId(id)
    setAcceptProfile(false)
    const next = profiles.data?.items.find((item) => item.id === id)
    if (next) {
      initializedProfile.current = next.id
      setRepositoryURL(next.default_repository_url ?? '')
      setBranch(next.default_branch ?? '')
    }
  }

  const requiresProfileAck = Boolean(
    selectedProfile &&
      selectedProfile.origin.type !== 'builtin' &&
      !selectedProfile.origin.verified,
  )

  const nameError = name.length > 0 ? validateName(name) : ''
  const descriptionError =
    description.trim().length > 500 ? '描述不能超过 500 个字符' : ''
  const sourceError =
    selectedProfile?.source_required && !repositoryURL.trim()
      ? '该模板需要源码仓库地址'
      : ''
  const branchError = branch.trim().length > 255 ? '分支名称不能超过 255 个字符' : ''
  const profileError =
    !profiles.isLoading && !selectedProfile ? '请选择 Project Profile' : ''

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const error = validateName(name)
    if (
      error ||
      descriptionError ||
      sourceError ||
      branchError ||
      !selectedProfile ||
      (requiresProfileAck && !acceptProfile)
    ) {
      return
    }
    mutation.mutate({
      name: name.trim(),
      description: description.trim(),
      profile_id: selectedProfile.id,
      repository_url: repositoryURL.trim() || undefined,
      branch: branch.trim() || undefined,
      accept_profile: requiresProfileAck ? acceptProfile : undefined,
    })
  }

  const checkReport = profileCheck.data
  const checkTone = !checkReport
    ? 'slate'
    : checkReport.error_count > 0
      ? 'rose'
      : checkReport.warning_count > 0
        ? 'amber'
        : 'emerald'

  return (
    <Modal
      open={open}
      onClose={onClose}
      width="max-w-3xl"
      title="新建 Workspace"
      description="选择 Project Profile 应用默认项目类型与源码；创建后自动分配空闲端口、独立数据库名与 Redis DB。"
      footer={
        <>
          <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
            取消
          </Button>
          <Button
            type="submit"
            form="create-workspace-form"
            data-testid="workspace-create-submit"
            disabled={
              mutation.isPending ||
              !selectedProfile ||
              Boolean(validateName(name)) ||
              Boolean(descriptionError || sourceError || branchError) ||
              (requiresProfileAck && !acceptProfile)
            }
          >
            {mutation.isPending ? <Spinner /> : null}
            创建
          </Button>
        </>
      }
    >
      <form id="create-workspace-form" onSubmit={submit} className="space-y-4">
        <Field
          label="Project Profile"
          htmlFor="workspace-profile"
          error={profiles.error ? undefined : profileError || undefined}
          hint="模板决定项目类型、默认源码与依赖预检，不会自动克隆或执行远程代码。"
        >
          <select
            id="workspace-profile"
            className={inputClass}
            value={profileId}
            onChange={(event) => selectProfile(event.target.value)}
            disabled={profiles.isLoading || mutation.isPending}
          >
            {profileId === '' ? <option value="">选择 Profile…</option> : null}
            {profiles.data?.items.map((item) => (
              <option key={item.id} value={item.id}>
                {item.name}
              </option>
            ))}
          </select>
        </Field>

        {profiles.error ? (
          <ErrorNotice error={profiles.error} onRetry={() => void profiles.refetch()} />
        ) : null}

        {selectedProfile ? (
          <div className="space-y-3 rounded-xl border border-slate-800 bg-slate-950/40 p-4">
            <div className="flex flex-wrap items-center gap-2">
              <Badge tone={selectedProfile.origin.verified ? 'emerald' : 'amber'}>
                {selectedProfile.origin.verified ? '已验证模板' : '未验证模板'}
              </Badge>
              <span className="text-xs font-medium text-slate-200">
                {selectedProfile.origin.name || '未知来源'}
              </span>
              {selectedProfile.origin.version ? (
                <span className="text-[11px] text-slate-500">
                  {selectedProfile.origin.version}
                </span>
              ) : null}
              <Badge tone="slate">{selectedProfile.project_type}</Badge>
              {selectedProfile.extension_kind ? (
                <Badge tone="cyan">{selectedProfile.extension_kind}</Badge>
              ) : null}
            </div>
            {selectedProfile.origin.url ? (
              <p className="break-all text-[11px] leading-4 text-slate-500">
                来源地址：<span className="font-mono">{selectedProfile.origin.url}</span>
              </p>
            ) : null}
            <p className="text-xs leading-5 text-slate-400">{selectedProfile.description}</p>
            <div className="grid gap-3 sm:grid-cols-2">
              <div>
                <p className="text-[11px] uppercase tracking-wide text-slate-500">创建操作</p>
                <ul className="mt-1 space-y-1">
                  {selectedProfile.operations.map((operation) => (
                    <li key={operation.id} className="text-xs leading-5 text-slate-300">
                      · {operation.title}
                      {operation.destructive ? (
                        <span className="ml-1 text-rose-300">[破坏性]</span>
                      ) : null}
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <p className="text-[11px] uppercase tracking-wide text-slate-500">所需权限</p>
                <ul className="mt-1 space-y-1">
                  {selectedProfile.permissions.map((permission) => (
                    <li key={permission.id} className="text-xs leading-5 text-slate-300">
                      · {permission.title}
                      <span className="ml-1 text-[10px] text-slate-500">
                        [{permission.risk}]
                      </span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
            {requiresProfileAck ? (
              <label
                className={`flex cursor-pointer items-start gap-3 rounded-lg border px-3 py-3 ${
                  acceptProfile
                    ? 'border-emerald-400/30 bg-emerald-400/5'
                    : 'border-amber-400/30 bg-amber-400/5'
                }`}
              >
                <input
                  type="checkbox"
                  className="mt-0.5 h-4 w-4 accent-amber-400"
                  checked={acceptProfile}
                  onChange={(event) => setAcceptProfile(event.target.checked)}
                />
                <span>
                  <span className="block text-xs font-medium text-amber-200">
                    第三方模板需要确认
                  </span>
                  <span className="mt-1 block text-[11px] leading-5 text-slate-400">
                    我已知晓来源「{selectedProfile.origin.name || '未知来源'}」、上述创建操作与所需权限，并同意用它创建 Workspace。DevKit 只执行固定的 Workspace 创建流程，不会运行模板仓库中的代码。
                  </span>
                </span>
              </label>
            ) : null}
          </div>
        ) : null}

        <div className="rounded-xl border border-slate-800 bg-slate-950/40 p-4">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs font-medium text-slate-300">依赖预检</span>
              {profileCheck.isFetching ? <Spinner className="h-3 w-3" /> : null}
              {checkReport ? (
                <>
                  <Badge tone={checkTone}>
                    {checkReport.ready ? '可创建' : '存在错误'}
                  </Badge>
                  <span className="text-[11px] text-slate-500">
                    {checkReport.error_count} 错误 · {checkReport.warning_count} 提醒
                  </span>
                </>
              ) : null}
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => void profileCheck.refetch()}
              disabled={!selectedProfile || profileCheck.isFetching}
            >
              重新检查
            </Button>
          </div>
          {profileCheck.error ? (
            <div className="mt-2">
              <ErrorNotice error={profileCheck.error} />
            </div>
          ) : checkReport ? (
            <div className="mt-2 space-y-1.5">
              {checkReport.checks.map((check) => (
                <div key={check.id} className="flex items-start gap-2 text-xs">
                  <Badge
                    tone={profileCheckTone(check.status)}
                    className="shrink-0"
                  >
                    {profileCheckLabels[check.status]}
                  </Badge>
                  <div className="min-w-0">
                    <span className="text-slate-300">{check.title}</span>
                    <span className="text-slate-500"> · {check.message}</span>
                    {check.fix_action && check.status !== 'ok' && check.status !== 'info' ? (
                      <p className="text-[11px] leading-4 text-slate-500">
                        → {check.fix_action}
                      </p>
                    ) : null}
                  </div>
                </div>
              ))}
              {!checkReport.ready ? (
                <p className="text-[11px] leading-4 text-rose-300/90">
                  Runtime 错误通常会导致项目初始化失败；仍可先创建 Workspace，修复环境后再继续。
                </p>
              ) : null}
            </div>
          ) : (
            <p className="mt-2 text-xs text-slate-500">
              {selectedProfile ? '正在检查 Runtime、服务与源码配置…' : '选择 Profile 后开始检查。'}
            </p>
          )}
        </div>

        <Field
          label="名称"
          htmlFor="workspace-name"
          hint="1–64 个字符，不区分大小写保持唯一。允许中文与空格。"
          error={nameError || undefined}
        >
          <input
            id="workspace-name"
            className={inputClass}
            value={name}
            onChange={(event) => setName(event.target.value)}
            placeholder="例如：Discourse 主站"
            autoFocus
            maxLength={64}
          />
        </Field>

        <Field label="描述" htmlFor="workspace-description" hint="可选，最多 500 个字符。">
          <textarea
            id="workspace-description"
            className={`${inputClass} min-h-[80px] resize-y`}
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            placeholder="这个 Workspace 用于…"
            maxLength={500}
          />
        </Field>

        <Field
          label="源码仓库"
          htmlFor="workspace-repository"
          hint="仅记录地址，不会在创建时克隆。凭据请使用 SSH Agent 或 Git 凭据助手。"
          error={sourceError || undefined}
        >
          <input
            id="workspace-repository"
            className={inputClass}
            value={repositoryURL}
            onChange={(event) => setRepositoryURL(event.target.value)}
            placeholder="https://github.com/discourse/discourse.git"
            maxLength={2048}
          />
        </Field>

        <Field
          label="分支"
          htmlFor="workspace-branch"
          hint="留空时使用模板或 DevKit 的默认分支。"
          error={branchError || undefined}
        >
          <input
            id="workspace-branch"
            className={inputClass}
            value={branch}
            onChange={(event) => setBranch(event.target.value)}
            placeholder="main"
            maxLength={255}
          />
        </Field>

        {mutation.error ? <ErrorNotice error={mutation.error} /> : null}
      </form>
    </Modal>
  )
}

function InspectWorkspaceModal({
  workspaceId,
  onClose,
}: {
  workspaceId: string
  onClose: () => void
}) {
  const status = useWorkspaceStatus(workspaceId)
  const workspace = status.data?.workspace

  return (
    <Modal
      open
      onClose={onClose}
      title={workspace ? `Workspace「${workspace.name}」` : 'Workspace 详情'}
      description={workspaceId}
      width="max-w-3xl"
      footer={
        <>
          <Button variant="ghost" onClick={onClose}>
            关闭
          </Button>
          <Button variant="secondary" onClick={() => void status.refetch()} disabled={status.isFetching}>
            {status.isFetching ? <Spinner /> : <span className="text-xs">↻</span>}
            刷新
          </Button>
        </>
      }
    >
      {status.isLoading ? (
        <div className="space-y-3">
          <Skeleton className="h-8 w-56" />
          <Skeleton className="h-24 w-full" />
          <Skeleton className="h-32 w-full" />
        </div>
      ) : status.error ? (
        <ErrorNotice error={status.error} onRetry={() => void status.refetch()} />
      ) : workspace ? (
        <div className="space-y-6">
          <div className="flex flex-wrap items-center gap-3">
            <StateBadge state={workspace.state} />
            <Badge tone="slate">{workspace.project_type}</Badge>
            {workspace.profile_id ? (
              <Badge tone="sky">Profile {workspace.profile_id}</Badge>
            ) : null}
            <Badge tone="slate">Runtime {workspace.runtime_profile}</Badge>
          </div>
          <p className="text-sm leading-6 text-slate-400">
            {workspace.description || '未填写描述。'}
          </p>

          {workspace.last_error ? (
            <div className="rounded-xl border border-rose-400/30 bg-rose-400/10 px-4 py-3 text-sm text-rose-200">
              {workspace.last_error}
            </div>
          ) : null}

          <div className="grid gap-3 sm:grid-cols-2">
            <Detail label="Workspace ID" value={workspace.workspace_id} mono />
            <Detail label="创建时间" value={formatDate(workspace.created_at)} />
            <Detail label="HTTP 地址" value={`http://${workspace.network.host}:${workspace.network.http_port}`} mono />
            <Detail label="项目进程" value={workspace.project.process_state || 'STOPPED'} />
          </div>

          <div className="grid gap-3 sm:grid-cols-2">
            <div className="rounded-xl border border-slate-800 bg-slate-950/40 p-4">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-slate-500">
                PostgreSQL
              </h3>
              <dl className="mt-3 space-y-2 text-sm">
                <Row label="模式" value={workspace.database.mode} />
                <Row label="数据库" value={workspace.database.database_name} mono />
                <Row label="用户" value={workspace.database.username} mono />
                <Row label="凭据引用" value={workspace.database.password_ref} mono />
                <Row
                  label="创建状态"
                  value={workspace.database.provisioned ? '已就绪' : '待创建'}
                />
                {workspace.database.provisioned_at ? (
                  <Row label="创建时间" value={formatDate(workspace.database.provisioned_at)} />
                ) : null}
              </dl>
              {!workspace.database.provisioned && workspace.database.provision_error ? (
                <p className="mt-3 rounded-lg border border-amber-400/25 bg-amber-400/5 px-3 py-2 text-[11px] leading-5 text-amber-200">
                  {workspace.database.provision_error}
                </p>
              ) : null}
            </div>
            <div className="rounded-xl border border-slate-800 bg-slate-950/40 p-4">
              <h3 className="text-xs font-semibold uppercase tracking-wide text-slate-500">Redis</h3>
              <dl className="mt-3 space-y-2 text-sm">
                <Row label="模式" value={workspace.redis.mode} />
                <Row label="DB" value={String(workspace.redis.db_index)} />
                <Row label="Key 前缀" value={workspace.redis.key_prefix} mono />
              </dl>
            </div>
          </div>

          <div className="rounded-xl border border-slate-800 bg-slate-950/40 p-4">
            <h3 className="text-xs font-semibold uppercase tracking-wide text-slate-500">源码</h3>
            {workspace.source.installed ? (
              <dl className="mt-3 grid gap-2 text-sm sm:grid-cols-2">
                <Row label="仓库" value={workspace.source.repository_url} mono />
                <Row label="分支" value={workspace.source.branch || '-'} />
                <Row label="提交" value={workspace.source.commit || '-'} mono />
                <Row label="路径" value={workspace.source.path || 'source'} mono />
              </dl>
            ) : (
              <p className="mt-3 text-sm text-slate-400">
                尚未安装源码。Git 与 Discourse 初始化将在 M3 提供。
              </p>
            )}
          </div>

          <div>
            <h3 className="mb-3 text-xs font-semibold uppercase tracking-wide text-slate-500">
              检查
            </h3>
            <div className="space-y-2">
              {(status.data?.status.checks ?? []).map((check) => (
                <div
                  key={check.name}
                  className="flex items-start gap-3 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3"
                >
                  <span
                    className={`mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[10px] ${
                      check.ok
                        ? 'bg-emerald-400/15 text-emerald-300'
                        : 'bg-rose-400/15 text-rose-300'
                    }`}
                  >
                    {check.ok ? '✓' : '✕'}
                  </span>
                  <div className="min-w-0">
                    <p className="text-sm text-slate-200">{check.name}</p>
                    {check.message ? (
                      <p className="mt-0.5 break-all text-xs text-slate-500">{check.message}</p>
                    ) : null}
                  </div>
                </div>
              ))}
            </div>
          </div>
        </div>
      ) : null}
    </Modal>
  )
}

function DeleteWorkspaceModal({
  target,
  onClose,
  onDeleted,
}: {
  target: Workspace
  onClose: () => void
  onDeleted: () => void
}) {
  const [mode, setMode] = useState<'record' | 'purge'>('record')
  const [confirmed, setConfirmed] = useState(false)
  const [taskId, setTaskId] = useState<string | null>(null)
  const [localError, setLocalError] = useState<string | null>(null)
  const notified = useRef(false)

  const preview = useQuery({
    queryKey: ['delete-preview', target.workspace_id],
    queryFn: () => api.previewDelete(target.workspace_id),
    retry: false,
    gcTime: 0,
    staleTime: 0,
  })
  const taskQuery = useTask(taskId)
  const task = taskQuery.data
  const taskRunning = Boolean(task && !isTerminalTask(task.status))

  const mutation = useMutation({
    mutationFn: (plan: DeletePlan) =>
      api.deleteWorkspace(target.workspace_id, plan.confirm_token, mode),
    onSuccess: (accepted) => {
      setLocalError(null)
      setTaskId(accepted.task_id)
    },
    onError: (error) => {
      setLocalError(errorMessage(error))
    },
  })

  useEffect(() => {
    if (task?.status === 'succeeded' && !notified.current) {
      notified.current = true
      onDeleted()
    }
  }, [task?.status, onDeleted])

  const plan = preview.data
  const canConfirm = Boolean(plan) && !plan?.running && confirmed && !mutation.isPending && !taskRunning
  const previewExpired =
    mutation.error instanceof ApiError && mutation.error.code === 'PREVIEW_REQUIRED'

  const resetPreview = () => {
    notified.current = false
    setTaskId(null)
    setConfirmed(false)
    setLocalError(null)
    mutation.reset()
    void preview.refetch()
  }

  return (
    <Modal
      open
      onClose={onClose}
      title={`删除 Workspace「${target.name}」`}
      description="删除前将展示资源清单；确认令牌 5 分钟内有效且仅可使用一次。"
      width="max-w-3xl"
      footer={
        taskId ? (
          <Button onClick={onClose} disabled={taskRunning}>
            {task?.status === 'succeeded' ? '完成' : '关闭'}
          </Button>
        ) : (
          <>
            <Button variant="ghost" onClick={onClose} disabled={mutation.isPending}>
              取消
            </Button>
            <Button
              variant="danger"
              data-testid="workspace-delete-submit"
              onClick={() => plan && mutation.mutate(plan)}
              disabled={!canConfirm}
            >
              {mutation.isPending ? <Spinner /> : null}
              {mode === 'purge' ? '彻底删除' : '移除 Workspace'}
            </Button>
          </>
        )
      }
    >
      <div className="space-y-5">
        <div className="rounded-xl border border-rose-500/25 bg-rose-500/5 px-4 py-3 text-sm leading-6 text-rose-200">
          此操作不可撤销。删除只会影响当前 Workspace，不会删除其他 Workspace 或共享服务。
        </div>

        {preview.isLoading ? (
          <div className="flex items-center gap-3 rounded-xl border border-slate-800 px-4 py-6 text-sm text-slate-400">
            <Spinner />
            正在生成资源预览…
          </div>
        ) : preview.error ? (
          <ErrorNotice error={preview.error} onRetry={() => void preview.refetch()} />
        ) : plan ? (
          <>
            <div className="grid gap-3 sm:grid-cols-3">
              <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                <p className="text-[11px] uppercase tracking-wide text-slate-500">Workspace ID</p>
                <p className="mt-1 truncate font-mono text-xs text-slate-300">{plan.workspace_id}</p>
              </div>
              <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                <p className="text-[11px] uppercase tracking-wide text-slate-500">可释放空间</p>
                <p className="mt-1 text-sm text-slate-200">{formatBytes(plan.total_bytes)}</p>
              </div>
              <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                <p className="text-[11px] uppercase tracking-wide text-slate-500">预览有效期</p>
                <p className="mt-1 text-sm text-slate-200">{formatDate(plan.expires_at)}</p>
              </div>
            </div>

            {plan.running ? (
              <div className="rounded-xl border border-amber-400/30 bg-amber-400/10 px-4 py-3 text-sm text-amber-200">
                Workspace 当前处于运行状态，需先停止后才能删除。
              </div>
            ) : null}

            <div className="overflow-hidden rounded-xl border border-slate-800">
              <table className="w-full text-left text-sm">
                <thead className="bg-slate-950/60 text-[11px] uppercase tracking-wide text-slate-500">
                  <tr>
                    <th className="whitespace-nowrap px-4 py-2.5 font-medium">资源</th>
                    <th className="whitespace-nowrap px-4 py-2.5 font-medium">状态</th>
                    <th className="whitespace-nowrap px-4 py-2.5 font-medium">大小</th>
                    <th className="whitespace-nowrap px-4 py-2.5 font-medium">说明</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-slate-800/70">
                  {plan.resources.map((resource, index) => (
                    <tr key={`${resource.kind}-${index}`}>
                      <td className="whitespace-nowrap px-4 py-2.5 text-slate-200">
                        {resourceLabels[resource.kind] ?? resource.kind}
                      </td>
                      <td className="px-4 py-2.5">
                        <span className={resource.exists ? 'text-slate-300' : 'text-slate-600'}>
                          {resource.exists ? '存在' : '不存在'}
                        </span>
                      </td>
                      <td className="whitespace-nowrap px-4 py-2.5 font-mono text-xs text-slate-400">
                        {resource.size_bytes > 0 ? formatBytes(resource.size_bytes) : '-'}
                      </td>
                      <td className="max-w-[260px] px-4 py-2.5">
                        <p className="text-xs text-slate-400">
                          {resource.note || '—'}
                          {resource.shared ? '（共享资源）' : ''}
                        </p>
                        {resource.path ? (
                          <p
                            className="mt-0.5 truncate font-mono text-[10px] text-slate-600"
                            title={resource.path}
                          >
                            {resource.path}
                          </p>
                        ) : null}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>

            {!taskId ? (
              <>
                <div className="grid gap-3 sm:grid-cols-2">
                  <label
                    className={`flex cursor-pointer gap-3 rounded-xl border px-4 py-3 transition ${
                      mode === 'record'
                        ? 'border-cyan-400/40 bg-cyan-400/5'
                        : 'border-slate-800 bg-slate-950/40 hover:border-slate-700'
                    }`}
                  >
                    <input
                      type="radio"
                      name="delete-mode"
                      className="mt-1 accent-cyan-400"
                      checked={mode === 'record'}
                      onChange={() => setMode('record')}
                    />
                    <span>
                      <span className="block text-sm font-medium text-slate-200">
                        仅移除管理记录
                      </span>
                      <span className="mt-1 block text-xs leading-5 text-slate-400">
                        保留源码、配置、数据与备份，并保留数据库与 Redis 数据，之后仍可从磁盘恢复。
                      </span>
                    </span>
                  </label>
                  <label
                    className={`flex cursor-pointer gap-3 rounded-xl border px-4 py-3 transition ${
                      mode === 'purge'
                        ? 'border-rose-400/40 bg-rose-400/5'
                        : 'border-slate-800 bg-slate-950/40 hover:border-slate-700'
                    }`}
                  >
                    <input
                      type="radio"
                      name="delete-mode"
                      className="mt-1 accent-rose-500"
                      checked={mode === 'purge'}
                      onChange={() => setMode('purge')}
                    />
                    <span>
                      <span className="block text-sm font-medium text-slate-200">彻底删除</span>
                      <span className="mt-1 block text-xs leading-5 text-slate-400">
                        删除源码、配置、日志、数据与备份，并删除该 Workspace 的数据库与 Redis 逻辑 DB。
                      </span>
                    </span>
                  </label>
                </div>

                <label className="flex items-start gap-3 rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
                  <input
                    type="checkbox"
                    data-testid="workspace-delete-confirm"
                    className="mt-0.5 h-4 w-4 accent-rose-500"
                    checked={confirmed}
                    onChange={(event) => setConfirmed(event.target.checked)}
                    disabled={plan.running}
                  />
                  <span className="text-sm leading-5 text-slate-300">
                    我确认删除 Workspace「{plan.name}」，并理解此操作不可撤销。
                  </span>
                </label>
              </>
            ) : null}

            {localError ? (
              <div className="space-y-2">
                <ErrorNotice error={localError} />
                {previewExpired ? (
                  <Button variant="secondary" size="sm" onClick={resetPreview}>
                    重新获取预览
                  </Button>
                ) : null}
              </div>
            ) : null}

            {taskId ? (
              <div className="rounded-xl border border-slate-800 bg-slate-950/50 p-4">
                <div className="flex items-center justify-between gap-3">
                  <span className="text-sm font-medium text-slate-200">删除任务</span>
                  {task ? (
                    <Badge tone={taskTone(task.status)}>{taskLabels[task.status]}</Badge>
                  ) : (
                    <Badge tone="slate">载入中</Badge>
                  )}
                </div>
                <div className="mt-3">
                  <ProgressBar value={task?.progress ?? 0} />
                </div>
                <p className="mt-2 text-xs text-slate-400">{task?.message ?? '正在提交任务…'}</p>
                {task?.status === 'failed' ? (
                  <div className="mt-3 space-y-2">
                    <p className="text-xs text-rose-300">{task.error_message || '删除失败'}</p>
                    <Button variant="secondary" size="sm" onClick={resetPreview}>
                      重新获取预览并重试
                    </Button>
                  </div>
                ) : null}
                {task?.status === 'canceled' ? (
                  <div className="mt-3">
                    <Button variant="secondary" size="sm" onClick={resetPreview}>
                      重新获取预览
                    </Button>
                  </div>
                ) : null}
                {task?.status === 'succeeded' ? (
                  <p className="mt-3 text-xs text-emerald-300">{task.message}</p>
                ) : null}
              </div>
            ) : null}
          </>
        ) : null}
      </div>
    </Modal>
  )
}

function Detail({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className="rounded-xl border border-slate-800 bg-slate-950/40 px-4 py-3">
      <p className="text-[11px] uppercase tracking-wide text-slate-500">{label}</p>
      <p className={`mt-1 break-all text-sm text-slate-200 ${mono ? 'font-mono text-xs' : ''}`}>
        {value}
      </p>
    </div>
  )
}

function Row({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <dt className="shrink-0 text-xs text-slate-500">{label}</dt>
      <dd
        className={`break-all text-right text-xs text-slate-300 ${mono ? 'font-mono' : ''}`}
        title={value}
      >
        {value || '-'}
      </dd>
    </div>
  )
}
