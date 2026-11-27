import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import { ensureSession } from './api/client'
import './index.css'
import { errorMessage } from './lib/format'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: 1,
      refetchOnWindowFocus: false,
      staleTime: 3_000,
    },
    mutations: {
      retry: 0,
    },
  },
})

function FatalError({ error }: { error: unknown }) {
  return (
    <div className="flex min-h-screen items-center justify-center px-6">
      <div className="w-full max-w-lg rounded-2xl border border-rose-500/30 bg-slate-900/80 p-8 shadow-2xl">
        <div className="flex h-11 w-11 items-center justify-center rounded-xl border border-rose-500/30 bg-rose-500/10 text-lg text-rose-300">
          !
        </div>
        <h1 className="mt-5 text-lg font-semibold text-slate-100">无法建立管理会话</h1>
        <p className="mt-2 text-sm leading-6 text-slate-400">
          管理界面未能连接 devkitd。请确认守护进程正在运行，然后刷新页面。
        </p>
        <pre className="mt-4 overflow-x-auto rounded-lg border border-slate-800 bg-slate-950/80 px-3 py-2 text-xs text-rose-300">
          {errorMessage(error)}
        </pre>
        <button
          type="button"
          onClick={() => window.location.reload()}
          className="mt-6 inline-flex h-10 items-center rounded-lg bg-cyan-400 px-4 text-sm font-medium text-slate-950 transition hover:bg-cyan-300"
        >
          重新加载
        </button>
      </div>
    </div>
  )
}

async function bootstrap() {
  const container = document.getElementById('root')
  if (!container) {
    return
  }
  const root = createRoot(container)
  try {
    await ensureSession()
  } catch (error) {
    root.render(<FatalError error={error} />)
    return
  }
  root.render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </StrictMode>,
  )
}

void bootstrap()
