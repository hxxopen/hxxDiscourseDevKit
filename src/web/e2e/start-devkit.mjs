// Starts an isolated devkitd instance for browser E2E tests.
//
// The binary must already be built (bash scripts/build.sh). The instance root
// is disposable and is recreated on every run; it is never the user's default
// DevKit root.
import { spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const webDir = path.resolve(here, '..')
const srcDir = path.resolve(webDir, '..')
const binary = path.join(
  srcDir,
  'bin',
  process.platform === 'win32' ? 'devkitd.exe' : 'devkitd',
)
const root = process.env.E2E_ROOT ?? path.join(srcDir, '.devkit', 'e2e-browser')
const port = process.env.E2E_PORT ?? '17870'
const startupLog = path.join(root, 'logs', 'devkitd-startup.log')

if (!existsSync(binary)) {
  console.error(`[e2e] devkitd binary not found: ${binary}`)
  console.error('[e2e] build it first: bash scripts/build.sh')
  process.exit(1)
}

rmSync(root, { recursive: true, force: true })
mkdirSync(path.join(root, 'config'), { recursive: true })
// Pin data_root to the disposable root. Otherwise a fresh Windows instance
// defaults to the shared WSL2 user data directory and the tests would read and
// modify the developer's real workspaces.
writeFileSync(
  path.join(root, 'config', 'devkit.json'),
  `${JSON.stringify({ storage: { data_root: root } }, null, 2)}\n`,
  'utf8',
)

console.log(`[e2e] starting ${binary}`)
console.log(`[e2e] root=${root} port=${port}`)

const child = spawn(binary, ['--root', root, '--port', port, '--quiet'], {
  stdio: 'inherit',
  windowsHide: true,
})

let stopping = false

function stop(code) {
  if (stopping) return
  stopping = true
  try {
    if (child.exitCode === null) {
      child.kill()
    }
  } catch {
    // Best effort; Playwright also terminates the process tree on Windows.
  }
  if (child.exitCode !== null) {
    process.exit(code)
  }
  const timer = setTimeout(() => process.exit(code), 5_000)
  timer.unref()
  child.once('exit', () => process.exit(code))
}

for (const signal of ['SIGINT', 'SIGTERM', 'SIGHUP']) {
  process.on(signal, () => stop(0))
}
if (process.platform === 'win32') {
  process.on('SIGBREAK', () => stop(0))
}

child.on('error', (error) => {
  console.error(`[e2e] failed to start devkitd: ${error.message}`)
  process.exit(1)
})

child.on('exit', (code, signal) => {
  if (stopping) {
    return
  }
  console.error(`[e2e] devkitd exited unexpectedly (code=${code}, signal=${signal})`)
  try {
    const tail = readFileSync(startupLog, 'utf8').split(/\r?\n/).slice(-30).join('\n')
    if (tail.trim()) {
      console.error(`[e2e] ${startupLog}:\n${tail}`)
    }
  } catch {
    // The daemon may have failed before creating its log directory.
  }
  process.exit(code ?? 1)
})
