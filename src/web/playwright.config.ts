import { defineConfig } from '@playwright/test'

const port = Number(process.env.E2E_PORT ?? 17870)
const baseURL = `http://127.0.0.1:${port}`
// Reuse the locally installed Edge on Windows; CI can set PLAYWRIGHT_CHANNEL
// or install the bundled Chromium with `npx playwright install chromium`.
const channel = process.env.PLAYWRIGHT_CHANNEL ?? (process.platform === 'win32' ? 'msedge' : undefined)

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  workers: 1,
  timeout: 180_000,
  expect: { timeout: 20_000 },
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : [['list']],
  use: {
    baseURL,
    channel,
    headless: true,
    viewport: { width: 1440, height: 900 },
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command: 'node e2e/start-devkit.mjs',
    url: `${baseURL}/api/v1/system/health`,
    reuseExistingServer: false,
    timeout: 120_000,
    stdout: 'pipe',
    stderr: 'pipe',
  },
})
