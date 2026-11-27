import type { ReactNode } from 'react'
import { useInfo, useLiveEvents } from '../api/hooks'

export type Page =
  | 'dashboard'
  | 'workspaces'
  | 'project'
  | 'plugins'
  | 'themes'
  | 'backup'
  | 'runtime'
  | 'services'
  | 'logs'
  | 'audit'
  | 'diagnostics'
  | 'settings'

const navItems: Array<{ id: Page; label: string; hint: string }> = [
  { id: 'dashboard', label: '概览', hint: '运行状态与快捷入口' },
  { id: 'workspaces', label: 'Workspaces', hint: '隔离的开发容器' },
  { id: 'project', label: '项目', hint: '源码、依赖与进程' },
  { id: 'plugins', label: '插件', hint: 'Discourse 插件脚手架' },
  { id: 'themes', label: '主题', hint: '主题脚手架与资源' },
  { id: 'backup', label: '备份与恢复', hint: '快照、校验与恢复' },
  { id: 'runtime', label: '运行时', hint: '组件矩阵与验证' },
  { id: 'services', label: '服务', hint: 'PostgreSQL 与 Redis' },
  { id: 'logs', label: '日志', hint: 'DevKit、项目与服务日志' },
  { id: 'audit', label: '审计', hint: '管理操作与拒绝记录' },
  { id: 'diagnostics', label: '诊断', hint: '环境健康检查' },
  { id: 'settings', label: '设置', hint: '路径、网络与默认值' },
]

function NavIcon({ page }: { page: Page }) {
  const common = {
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.8,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
  }
  switch (page) {
    case 'dashboard':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
      )
    case 'workspaces':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="m12 3 8 4.5v9L12 21l-8-4.5v-9L12 3Z" />
          <path d="m4 7.5 8 4.5 8-4.5" />
          <path d="M12 12v9" />
        </svg>
      )
    case 'project':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M4 7h16" />
          <path d="M4 12h16" />
          <path d="M4 17h10" />
          <circle cx="18" cy="17" r="2" />
        </svg>
      )
    case 'plugins':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M9 4h3.5a1.5 1.5 0 0 1 0 3H12v2.5a1.5 1.5 0 0 1 3 0V12h2.5a1.5 1.5 0 0 1 0 3H15v2.5a1.5 1.5 0 0 1-3 0V15H9.5A1.5 1.5 0 0 1 8 13.5V12H6.5a1.5 1.5 0 0 1 0-3H8V6.5A1.5 1.5 0 0 1 9.5 5Z" />
          <path d="M4 4h2M18 20h2M20 4h-2M4 20h2" />
        </svg>
      )
    case 'themes':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M12 3a9 9 0 1 0 0 18c1.4 0 2-1 2-2 0-1.5-1-1.7-1-3 0-1 .8-2 2-2h1.5A4.5 4.5 0 0 0 21 9.5C21 5.9 17 3 12 3Z" />
          <circle cx="7.5" cy="10" r="1" />
          <circle cx="10" cy="6.8" r="1" />
          <circle cx="14.5" cy="6.8" r="1" />
          <circle cx="17.2" cy="10" r="1" />
        </svg>
      )
    case 'backup':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M4 8.5 12 4l8 4.5v7L12 20l-8-4.5v-7Z" />
          <path d="m4 8.5 8 4.5 8-4.5M12 13v7" />
          <path d="M8 6.2v4.3M16 6.2v4.3" />
        </svg>
      )
    case 'runtime':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <rect x="6" y="6" width="12" height="12" rx="2" />
          <path d="M9 1.5v3M15 1.5v3M9 19.5v3M15 19.5v3M1.5 9h3M1.5 15h3M19.5 9h3M19.5 15h3" />
          <rect x="9.5" y="9.5" width="5" height="5" rx="1" />
        </svg>
      )
    case 'services':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <ellipse cx="12" cy="5.5" rx="7.5" ry="2.5" />
          <path d="M4.5 5.5v6c0 1.4 3.4 2.5 7.5 2.5s7.5-1.1 7.5-2.5v-6" />
          <path d="M4.5 11.5v6c0 1.4 3.4 2.5 7.5 2.5s7.5-1.1 7.5-2.5v-6" />
        </svg>
      )
    case 'logs':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M5 3h11l3 3v15H5z" />
          <path d="M8 9h8M8 13h8M8 17h5" />
        </svg>
      )
    case 'audit':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M12 3 20 6v5c0 4.6-3.2 8.2-8 10-4.8-1.8-8-5.4-8-10V6l8-3Z" />
          <path d="m8.5 12 2.3 2.3 4.7-4.7" />
        </svg>
      )
    case 'settings':
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <circle cx="12" cy="12" r="3" />
          <path d="M19 12a7 7 0 0 0-.1-1.2l2-1.5-2-3.4-2.3.9a7 7 0 0 0-2-1.2L14.2 3h-4.4l-.4 2.6a7 7 0 0 0-2 1.2l-2.3-.9-2 3.4 2 1.5a7 7 0 0 0 0 2.4l-2 1.5 2 3.4 2.3-.9a7 7 0 0 0 2 1.2l.4 2.6h4.4l.4-2.6a7 7 0 0 0 2-1.2l2.3.9 2-3.4-2-1.5c.1-.4.1-.8.1-1.2Z" />
        </svg>
      )
    default:
      return (
        <svg viewBox="0 0 24 24" className="h-4 w-4" aria-hidden="true" {...common}>
          <path d="M3 12h4l2.5-6 4 12 2.5-6h5" />
        </svg>
      )
  }
}

export function Layout({
  page,
  onNavigate,
  children,
}: {
  page: Page
  onNavigate: (page: Page) => void
  children: ReactNode
}) {
  const info = useInfo()
  const live = useLiveEvents()
  const daemonState = info.isError
    ? { dot: 'bg-rose-400', label: 'devkitd 未响应', tone: 'text-rose-300' }
    : info.isLoading
      ? { dot: 'bg-amber-400 animate-pulse', label: '连接 devkitd', tone: 'text-amber-300' }
      : { dot: 'bg-emerald-400', label: 'devkitd 运行中', tone: 'text-emerald-300' }
  const liveState =
    live === 'live'
      ? { dot: 'bg-emerald-400', label: '实时' }
      : live === 'connecting'
        ? { dot: 'bg-amber-400 animate-pulse', label: '事件流连接中' }
        : { dot: 'bg-slate-500', label: '事件流离线' }

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100">
      <div className="mx-auto flex w-full max-w-[1500px]">
        <aside className="sticky top-0 hidden h-screen w-64 shrink-0 flex-col border-r border-slate-800/70 bg-slate-950/70 px-4 py-6 lg:flex">
          <div className="flex items-center gap-3 px-1">
            <div className="flex h-10 w-10 items-center justify-center rounded-xl border border-cyan-400/30 bg-gradient-to-br from-cyan-400/20 to-emerald-400/10 text-sm font-bold tracking-tight text-cyan-300">
              HX
            </div>
            <div>
              <p className="text-sm font-semibold leading-tight text-slate-100">hxxDiscourse</p>
              <p className="text-xs text-slate-500">DevKit</p>
            </div>
          </div>

          <nav className="mt-8 min-h-0 flex-1 space-y-1 overflow-y-auto pr-1">
            {navItems.map((item) => {
              const active = page === item.id
              return (
                <button
                  key={item.id}
                  type="button"
                  data-testid={`nav-${item.id}`}
                  onClick={() => onNavigate(item.id)}
                  className={`group flex w-full items-center gap-3 rounded-xl px-3 py-2.5 text-left transition ${
                    active
                      ? 'bg-cyan-400/10 text-cyan-200 ring-1 ring-inset ring-cyan-400/20'
                      : 'text-slate-400 hover:bg-slate-900 hover:text-slate-200'
                  }`}
                >
                  <span
                    className={`flex h-8 w-8 items-center justify-center rounded-lg border ${
                      active
                        ? 'border-cyan-400/30 bg-cyan-400/10'
                        : 'border-slate-800 bg-slate-900 text-slate-500 group-hover:text-slate-300'
                    }`}
                  >
                    <NavIcon page={item.id} />
                  </span>
                  <span className="min-w-0">
                    <span className="block text-sm font-medium">{item.label}</span>
                    <span className="block truncate text-[11px] text-slate-500">{item.hint}</span>
                  </span>
                </button>
              )
            })}
          </nav>

          <div className="mt-auto space-y-3">
            <div className="rounded-xl border border-slate-800 bg-slate-900/50 p-3">
              <div className="flex items-center gap-2">
                <span className={`h-2 w-2 rounded-full ${daemonState.dot}`} />
                <span className={`text-xs font-medium ${daemonState.tone}`}>{daemonState.label}</span>
              </div>
              <p className="mt-2 truncate font-mono text-[10px] text-slate-500" title={info.data?.root}>
                {info.data?.root ?? '正在读取实例根目录…'}
              </p>
            </div>
            <p className="px-1 text-[10px] text-slate-600">
              v{info.data?.version.version ?? '0.1.0-dev'} · 本地开发环境管理器
            </p>
          </div>
        </aside>

        <div className="min-w-0 flex-1">
          <header className="sticky top-0 z-30 border-b border-slate-800/70 bg-slate-950/80 backdrop-blur-md">
            <div className="flex items-center justify-between gap-4 px-4 py-3 sm:px-6 lg:px-8">
              <div className="flex items-center gap-3">
                <div className="flex h-9 w-9 items-center justify-center rounded-lg border border-cyan-400/30 bg-gradient-to-br from-cyan-400/20 to-emerald-400/10 text-xs font-bold text-cyan-300 lg:hidden">
                  HX
                </div>
                <div>
                  <p className="text-sm font-semibold text-slate-100">DevKit 控制台</p>
                  <p className="hidden text-[11px] text-slate-500 sm:block">
                    {info.data?.platform.distro
                      ? `${info.data.platform.distro} · ${info.data.platform.arch}`
                      : `${info.data?.platform.os ?? '本地'} · ${info.data?.platform.arch ?? ''}`}
                  </p>
                </div>
              </div>
              <div className="flex items-center gap-3">
                <div
                  className="hidden items-center gap-2 rounded-full border border-slate-800 bg-slate-900/60 px-3 py-1.5 text-[11px] text-slate-400 md:flex"
                  title={liveState.label}
                >
                  <span className={`h-1.5 w-1.5 rounded-full ${liveState.dot}`} />
                  {liveState.label}
                </div>
                <div className="hidden items-center gap-2 rounded-full border border-slate-800 bg-slate-900/60 px-3 py-1.5 text-[11px] text-slate-400 sm:flex">
                  <span className={`h-1.5 w-1.5 rounded-full ${daemonState.dot}`} />
                  {info.data?.api_addr ?? '127.0.0.1'}
                </div>
                <button
                  type="button"
                  onClick={() => onNavigate('workspaces')}
                  className="rounded-lg bg-cyan-400 px-3 py-2 text-xs font-semibold text-slate-950 transition hover:bg-cyan-300"
                >
                  管理 Workspace
                </button>
              </div>
            </div>

            <div className="flex gap-1 overflow-x-auto border-t border-slate-900 px-3 py-2 lg:hidden">
              {navItems.map((item) => (
                <button
                  key={item.id}
                  type="button"
                  onClick={() => onNavigate(item.id)}
                  className={`shrink-0 rounded-lg px-3 py-1.5 text-xs font-medium transition ${
                    page === item.id
                      ? 'bg-cyan-400/10 text-cyan-200 ring-1 ring-inset ring-cyan-400/20'
                      : 'text-slate-400 hover:bg-slate-900 hover:text-slate-200'
                  }`}
                >
                  {item.label}
                </button>
              ))}
            </div>
          </header>

          <main data-testid="page-content" className="px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
            {children}
          </main>
        </div>
      </div>
    </div>
  )
}
