import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { api } from './client'
import type { AuditFilter, ExtensionKind, ServiceName, Task } from './types'

export function isTerminalTask(status: Task['status']): boolean {
  return status === 'succeeded' || status === 'failed' || status === 'canceled'
}

export function useInfo() {
  return useQuery({
    queryKey: ['info'],
    queryFn: api.info,
    refetchInterval: 15_000,
  })
}

export function useWorkspaces() {
  return useQuery({
    queryKey: ['workspaces'],
    queryFn: api.workspaces,
    refetchInterval: 10_000,
  })
}

export function useProfiles() {
  return useQuery({
    queryKey: ['profiles'],
    queryFn: api.profiles,
    staleTime: 5 * 60_000,
  })
}

export function useProfileSources() {
  return useQuery({
    queryKey: ['profile-sources'],
    queryFn: api.profileSources,
    staleTime: 30_000,
  })
}

export function useProfileCheck(profileId: string | null, enabled: boolean) {
  return useQuery({
    queryKey: ['profile-check', profileId],
    queryFn: () => api.profileCheck(profileId as string),
    enabled: enabled && Boolean(profileId),
    staleTime: 30_000,
    retry: false,
  })
}

export function useDiskUsage(enabled = true) {
  return useQuery({
    queryKey: ['disk-usage'],
    queryFn: api.diskUsage,
    enabled,
    refetchInterval: 60_000,
    retry: false,
  })
}

export function useDiagnostics() {
  return useQuery({
    queryKey: ['diagnostics'],
    queryFn: api.diagnostics,
    staleTime: 15_000,
  })
}

export function useSettings() {
  return useQuery({
    queryKey: ['settings'],
    queryFn: api.settings,
    staleTime: 5_000,
  })
}

export function useLogSources() {
  return useQuery({
    queryKey: ['log-sources'],
    queryFn: api.logSources,
    refetchInterval: 15_000,
  })
}

export function useLog(source: string | null, tail: number, live: boolean) {
  return useQuery({
    queryKey: ['log', source, tail],
    queryFn: () => api.log(source as string, tail),
    enabled: Boolean(source),
    refetchInterval: live ? 3_000 : false,
    staleTime: 0,
    gcTime: 0,
  })
}

export function useAudit(filter: AuditFilter, live: boolean) {
  return useQuery({
    queryKey: [
      'audit',
      filter.limit,
      filter.action,
      filter.workspaceId,
      filter.result,
      filter.since,
    ],
    queryFn: () => api.audit(filter),
    refetchInterval: live ? 10_000 : false,
    staleTime: 0,
    placeholderData: (previous) => previous,
  })
}

export type LiveEventState = 'connecting' | 'live' | 'offline'

// useLiveEvents subscribes to the daemon's SSE stream and refreshes cached
// queries when background tasks update. EventSource reconnects on its own.
export function useLiveEvents(): LiveEventState {
  const queryClient = useQueryClient()
  const [state, setState] = useState<LiveEventState>('connecting')

  useEffect(() => {
    const source = new EventSource('/api/v1/events')
    let timer: number | undefined
    let workspaceID = ''

    const refresh = () => {
      window.clearTimeout(timer)
      timer = window.setTimeout(() => {
        void queryClient.invalidateQueries({ queryKey: ['workspaces'] })
        void queryClient.invalidateQueries({ queryKey: ['services'] })
        void queryClient.invalidateQueries({ queryKey: ['diagnostics'] })
        void queryClient.invalidateQueries({ queryKey: ['info'] })
        if (workspaceID) {
          void queryClient.invalidateQueries({ queryKey: ['workspace-status', workspaceID] })
          void queryClient.invalidateQueries({ queryKey: ['project', workspaceID] })
          void queryClient.invalidateQueries({ queryKey: ['backups', workspaceID] })
          void queryClient.invalidateQueries({ queryKey: ['project-logs', workspaceID] })
        }
      }, 400)
    }

    const onReady = () => setState('live')
    const onTask = (event: MessageEvent<string>) => {
      try {
        const task = JSON.parse(event.data) as Task
        if (task.workspace_id) {
          workspaceID = task.workspace_id
        }
      } catch {
        // Ignore malformed frames; the polling queries still converge.
      }
      refresh()
    }

    source.addEventListener('ready', onReady)
    source.addEventListener('task', onTask as EventListener)
    source.onopen = () => setState('live')
    source.onerror = () => setState((current) => (current === 'live' ? 'offline' : 'connecting'))

    return () => {
      window.clearTimeout(timer)
      source.removeEventListener('ready', onReady)
      source.removeEventListener('task', onTask as EventListener)
      source.close()
    }
  }, [queryClient])

  return state
}

export function useWorkspaceStatus(workspaceId: string) {
  return useQuery({
    queryKey: ['workspace-status', workspaceId],
    queryFn: () => api.workspaceStatus(workspaceId),
    refetchInterval: 5_000,
  })
}

export function useTask(taskId: string | null) {
  return useQuery({
    queryKey: ['task', taskId],
    queryFn: () => api.task(taskId as string),
    enabled: Boolean(taskId),
    refetchInterval: (query) => {
      const status = query.state.data?.status
      return status && isTerminalTask(status) ? false : 400
    },
  })
}

export function useServices() {
  return useQuery({
    queryKey: ['services'],
    queryFn: api.services,
    refetchInterval: 10_000,
  })
}

export function useService(name: ServiceName) {
  return useQuery({
    queryKey: ['service', name],
    queryFn: () => api.service(name),
    refetchInterval: 10_000,
  })
}

export function useRuntimeComponents() {
  return useQuery({
    queryKey: ['runtime-components'],
    queryFn: api.runtimeComponents,
    staleTime: 30_000,
    // Component verification can change outside the page (daemon restart,
    // runtime install/upgrade). Keep it in sync while the page stays open
    // instead of showing a stale "未就绪" next to a running service.
    refetchInterval: 60_000,
    refetchOnMount: 'always',
    refetchOnWindowFocus: 'always',
  })
}

export function useRuntimeMatrix() {
  return useQuery({
    queryKey: ['runtime-matrix'],
    queryFn: api.runtimeMatrix,
    staleTime: 60_000,
  })
}

export function useWSLStatus(enabled = true) {
  return useQuery({
    queryKey: ['runtime-wsl'],
    queryFn: api.runtimeWSL,
    enabled,
    staleTime: 15_000,
    retry: false,
  })
}

export function useProject(workspaceId: string | null) {
  return useQuery({
    queryKey: ['project', workspaceId],
    queryFn: () => api.project(workspaceId as string),
    enabled: Boolean(workspaceId),
    refetchInterval: 5_000,
  })
}

export function useProjectLogs(workspaceId: string | null, source: string, tail = 200) {
  return useQuery({
    queryKey: ['project-logs', workspaceId, source, tail],
    queryFn: () => api.projectLogs(workspaceId as string, source, tail),
    enabled: Boolean(workspaceId),
    staleTime: 0,
    gcTime: 0,
  })
}

export function useBackups(workspaceId: string | null) {
  return useQuery({
    queryKey: ['backups', workspaceId],
    queryFn: () => api.backups(workspaceId as string),
    enabled: Boolean(workspaceId),
    refetchInterval: 15_000,
  })
}

export function useExtensions(kind: ExtensionKind, workspaceId: string | null) {
  return useQuery({
    queryKey: ['extensions', kind, workspaceId],
    queryFn: () => api.extensions(kind, workspaceId as string),
    enabled: Boolean(workspaceId),
    refetchInterval: 20_000,
  })
}

export function useServiceLogs(name: ServiceName | null, tail = 200) {
  return useQuery({
    queryKey: ['service-logs', name, tail],
    queryFn: () => api.serviceLogs(name as ServiceName, tail),
    enabled: Boolean(name),
    staleTime: 0,
    gcTime: 0,
  })
}
