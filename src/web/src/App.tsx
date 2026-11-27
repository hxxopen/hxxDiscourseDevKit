import { useEffect, useState } from 'react'
import { Layout, type Page } from './components/Layout'
import { Audit } from './pages/Audit'
import { Backup } from './pages/Backup'
import { Dashboard } from './pages/Dashboard'
import { Diagnostics } from './pages/Diagnostics'
import { Logs } from './pages/Logs'
import { Plugins } from './pages/Plugins'
import { Project } from './pages/Project'
import { Runtime } from './pages/Runtime'
import { Themes } from './pages/Themes'
import { Services } from './pages/Services'
import { Settings } from './pages/Settings'
import { Workspaces } from './pages/Workspaces'

function parseHash(): Page {
  const hash = window.location.hash.replace(/^#\/?/, '')
  if (hash.startsWith('workspaces')) {
    return 'workspaces'
  }
  if (hash.startsWith('project')) {
    return 'project'
  }
  if (hash.startsWith('plugins')) {
    return 'plugins'
  }
  if (hash.startsWith('themes')) {
    return 'themes'
  }
  if (hash.startsWith('backup')) {
    return 'backup'
  }
  if (hash.startsWith('runtime')) {
    return 'runtime'
  }
  if (hash.startsWith('services')) {
    return 'services'
  }
  if (hash.startsWith('logs')) {
    return 'logs'
  }
  if (hash.startsWith('audit')) {
    return 'audit'
  }
  if (hash.startsWith('settings')) {
    return 'settings'
  }
  if (hash.startsWith('diagnostics')) {
    return 'diagnostics'
  }
  return 'dashboard'
}

export default function App() {
  const [page, setPage] = useState<Page>(parseHash)

  useEffect(() => {
    const onHashChange = () => setPage(parseHash())
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const navigate = (next: Page) => {
    window.location.hash = `/${next}`
    setPage(next)
    window.scrollTo({ top: 0, behavior: 'smooth' })
  }

  return (
    <Layout page={page} onNavigate={navigate}>
      {page === 'dashboard' ? <Dashboard onNavigate={navigate} /> : null}
      {page === 'workspaces' ? <Workspaces /> : null}
      {page === 'project' ? <Project /> : null}
      {page === 'plugins' ? <Plugins /> : null}
      {page === 'themes' ? <Themes /> : null}
      {page === 'backup' ? <Backup /> : null}
      {page === 'runtime' ? <Runtime /> : null}
      {page === 'services' ? <Services /> : null}
      {page === 'logs' ? <Logs /> : null}
      {page === 'audit' ? <Audit /> : null}
      {page === 'diagnostics' ? <Diagnostics /> : null}
      {page === 'settings' ? <Settings /> : null}
    </Layout>
  )
}
