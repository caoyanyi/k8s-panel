import AxeBuilder from '@axe-core/playwright'
import { expect, test, type Page } from '@playwright/test'

test('login, navigation, validation and logout', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))

  await page.goto('/')
  await expect(page.getByRole('heading', { name: '登录 KubePanel' })).toBeVisible()
  await page.getByLabel('用户名').fill(requiredEnvironment('PANEL_E2E_USERNAME'))
  await page.getByLabel('密码').fill(requiredEnvironment('PANEL_E2E_PASSWORD'))
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '运行总览' })).toBeVisible()
  await expect(page.getByText('尚未接入集群')).toBeVisible()

  await navigate(page, testInfo.project.name, '集群')
  await expect(page.getByRole('heading', { name: '集群管理' })).toBeVisible()
  await page.getByRole('button', { name: '接入集群' }).first().click()
  await page.getByRole('button', { name: '保存并检测' }).click()
  await expect(page.getByText('名称、API Server 和 Bearer Token 为必填项')).toBeVisible()
  await page.getByRole('button', { name: '关闭', exact: true }).click()

  await navigate(page, testInfo.project.name, '工作负载')
  await expect(page.getByRole('heading', { name: '工作负载' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, 'Helm')
  await expect(page.getByRole('heading', { name: 'Helm' })).toBeVisible()
  await navigate(page, testInfo.project.name, '操作中心')
  await expect(page.getByRole('heading', { name: '操作中心' })).toBeVisible()
  await navigate(page, testInfo.project.name, '审计')
  await expect(page.getByRole('heading', { name: '审计日志' })).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: `test-results/${testInfo.project.name}-audit.png`, fullPage: true })

  await page.getByRole('button', { name: 'e2e-admin 账户菜单' }).click()
  await page.getByRole('button', { name: '退出登录' }).click()
  await expect(page.getByRole('heading', { name: '登录 KubePanel' })).toBeVisible()
  expect(consoleErrors.filter((message) => !message.includes('401 (Unauthorized)'))).toEqual([])
})

test('critical views have no serious accessibility violations', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name.includes('mobile'), 'desktop accessibility scan covers the same DOM semantics')
  await page.goto('/')
  let result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])

  await page.getByLabel('用户名').fill(requiredEnvironment('PANEL_E2E_USERNAME'))
  await page.getByLabel('密码').fill(requiredEnvironment('PANEL_E2E_PASSWORD'))
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByRole('heading', { name: '运行总览' })).toBeVisible()
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
})

async function navigate(page: Page, projectName: string, label: string) {
  if (projectName.includes('mobile')) {
    await page.getByRole('button', { name: '打开导航' }).click()
  }
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: label, exact: true }).click()
  if (projectName.includes('mobile')) {
    await expect.poll(async () => (await page.locator('.sidebar').boundingBox())?.x ?? 0).toBeLessThanOrEqual(-220)
  }
}

function requiredEnvironment(name: string) {
  const value = process.env[name]
  if (!value) throw new Error(`${name} is not set by global setup`)
  return value
}
