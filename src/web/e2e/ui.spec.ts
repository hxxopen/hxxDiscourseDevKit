import { expect, test, type Page } from '@playwright/test'

// The suite shares one devkitd instance and one disposable root, so run the
// tests serially in a single worker.
test.describe.configure({ mode: 'serial' })

const workspaceName = `E2E-${Date.now()}`

const routes: Array<{ page: string; heading: string }> = [
  { page: 'dashboard', heading: '概览' },
  { page: 'workspaces', heading: '工作区' },
  { page: 'project', heading: '项目' },
  { page: 'plugins', heading: '插件' },
  { page: 'themes', heading: '主题' },
  { page: 'backup', heading: '备份与恢复' },
  { page: 'runtime', heading: '运行时组件' },
  { page: 'services', heading: '服务与运行时' },
  { page: 'logs', heading: '日志中心' },
  { page: 'audit', heading: '操作审计' },
  { page: 'diagnostics', heading: '环境诊断' },
  { page: 'settings', heading: '设置' },
]

const pageErrors: string[] = []

test.beforeEach(async ({ page }) => {
  pageErrors.length = 0
  page.on('pageerror', (error) => pageErrors.push(error.message))
})

test.afterEach(() => {
  expect(pageErrors, 'uncaught page errors').toEqual([])
})

async function openConsole(page: Page) {
  await page.goto('/')
  await expect(page.getByText('DevKit 控制台')).toBeVisible()
  await expect(page.getByText('devkitd 运行中')).toBeVisible()
  await expect(page.getByTestId('page-content')).toBeVisible()
}

test('loads the console and navigates to every page', async ({ page }) => {
  await openConsole(page)

  for (const route of routes) {
    await page.getByTestId(`nav-${route.page}`).click()
    await expect(page).toHaveURL(new RegExp(`#/${route.page}$`))
    await expect(page.getByTestId('page-content').locator('h1')).toHaveText(route.heading)
  }
})

test('creates a workspace, records an audit event, and deletes it', async ({ page }) => {
  await openConsole(page)
  await page.getByTestId('nav-workspaces').click()
  await expect(page.getByTestId('page-content').locator('h1')).toHaveText('工作区')

  await page.getByTestId('workspace-create').click()
  const createDialog = page.getByRole('dialog')
  await expect(createDialog).toBeVisible()
  await expect(createDialog.locator('#workspace-profile')).not.toHaveValue('')
  await createDialog.locator('#workspace-name').fill(workspaceName)
  await createDialog.getByTestId('workspace-create-submit').click()

  await expect(createDialog).toBeHidden({ timeout: 60_000 })
  await expect(page.getByText(`已创建 Workspace「${workspaceName}」`)).toBeVisible()

  const workspaceRow = page.locator('tbody tr', { hasText: workspaceName })
  await expect(workspaceRow).toHaveCount(1)

  // The create call is recorded in the audit log without request bodies, so
  // the row identifies the action and result but not the new workspace name.
  await page.getByTestId('nav-audit').click()
  await expect(page.getByTestId('page-content').locator('h1')).toHaveText('操作审计')
  const auditRow = page.getByTestId('audit-row').filter({ hasText: '创建 Workspace' })
  await expect(auditRow.first()).toBeVisible({ timeout: 30_000 })
  await expect(auditRow.first()).toContainText('workspace.create')
  await expect(auditRow.first()).toContainText('HTTP 201')
  await expect(auditRow.first()).toContainText('成功')

  // Remove the management record only; the default mode keeps files and data.
  await page.getByTestId('nav-workspaces').click()
  await expect(workspaceRow).toContainText(workspaceName)
  await workspaceRow.getByTestId('workspace-delete').click()

  const deleteDialog = page.getByRole('dialog')
  await expect(deleteDialog).toBeVisible()
  const confirm = deleteDialog.getByTestId('workspace-delete-confirm')
  await expect(confirm).toBeEnabled({ timeout: 60_000 })
  await confirm.check()
  await deleteDialog.getByTestId('workspace-delete-submit').click()

  // Deletion is asynchronous: the footer becomes “完成” once the task succeeds.
  const done = deleteDialog.getByRole('button', { name: '完成' })
  await expect(done).toBeEnabled({ timeout: 120_000 })
  await done.click()
  await expect(deleteDialog).toBeHidden()
  await expect(page.getByText(`Workspace「${workspaceName}」已删除`)).toBeVisible()
  await expect(workspaceRow).toHaveCount(0)
})
