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
  await navigate(page, testInfo.project.name, '集群资源')
  await expect(page.getByRole('heading', { name: '集群资源' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '网络')
  await expect(page.getByRole('heading', { name: '网络资源' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '配置')
  await expect(page.getByRole('heading', { name: '配置资源' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '存储')
  await expect(page.getByRole('heading', { name: '存储资源' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '资源治理')
  await expect(page.getByRole('heading', { name: '资源治理' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '安全态势')
  await expect(page.getByRole('heading', { name: '安全态势' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '访问控制')
  await expect(page.getByRole('heading', { name: '访问控制' })).toBeVisible()
  await expect(page.getByText('尚未选择集群')).toBeVisible()
  await navigate(page, testInfo.project.name, '事件')
  await expect(page.getByRole('heading', { name: '集群事件' })).toBeVisible()
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

test('cluster credentials rotate through a confirmed bounded form', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockClusterManagement(page)

  await page.goto('/#clusters')
  await expect(page.getByRole('heading', { name: '集群管理' })).toBeVisible()
  await page.getByRole('button', { name: '轮换 production-east 凭据' }).click()
  const dialog = page.getByRole('dialog', { name: '轮换集群凭据' })
  await dialog.getByLabel('Bearer Token').fill('new-service-account-token')
  await dialog.getByLabel('CA 证书（PEM，可选）').fill('new-test-ca')
  await expect(dialog.getByRole('button', { name: '验证并轮换' })).toBeDisabled()
  await dialog.getByLabel('输入集群名称确认').fill('production-east')
  const requestPromise = page.waitForRequest((request) => (
    request.method() === 'POST' && request.url().endsWith('/clusters/clu_1/credential-rotations')
  ))
  await dialog.getByRole('button', { name: '验证并轮换' }).click()

  const request = await requestPromise
  expect(request.postDataJSON()).toEqual({
    bearer_token: 'new-service-account-token', ca_cert: 'new-test-ca', confirmation: 'production-east',
  })
  await expect(page.getByText('production-east 凭据已轮换')).toBeVisible()
  await expect(dialog).not.toBeVisible()
  await expect(page.getByText('v1.36.3')).toBeVisible()
  expect(await page.locator('body').textContent()).not.toContain('new-service-account-token')
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: `test-results/${testInfo.project.name}-credential-rotation.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('cluster capability scan is explicit, bounded and namespace scoped', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockClusterManagement(page)

  await page.goto('/#clusters')
  await page.getByRole('button', { name: '检测 production-east 权限' }).click()
  const dialog = page.getByRole('dialog', { name: '权限能力检测' })
  await expect(dialog.getByLabel('命名空间')).toHaveValue('default')
  await dialog.getByLabel('命名空间').fill('payments')
  const requestPromise = page.waitForRequest((request) => (
    request.method() === 'GET' && new URL(request.url()).pathname.endsWith('/clusters/clu_1/capabilities')
  ))
  await dialog.getByRole('button', { name: '检测权限' }).click()

  const request = await requestPromise
  expect(new URL(request.url()).searchParams.get('namespace')).toBe('payments')
  await expect(dialog.getByRole('row', { name: /命名空间列表.*允许/ })).toBeVisible()
  await expect(dialog.getByRole('row', { name: /Pod 日志.*拒绝/ })).toBeVisible()
  await expect(dialog.getByRole('row', { name: /Deployment 变更.*无法判定/ })).toBeVisible()
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: `test-results/${testInfo.project.name}-cluster-capabilities.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('workload diagnostics show details, events, YAML and bounded logs', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockWorkloadDiagnostics(page)

  await page.goto('/#workloads')
  await expect(page.getByRole('heading', { name: '工作负载' })).toBeVisible()
  await expect(page.getByText('gateway-0', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '查看 gateway-0' }).click()

  const dialog = page.getByRole('dialog', { name: 'Pod · gateway-0' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('uid-gateway-0')).toBeVisible()

  await dialog.getByRole('tab', { name: '事件' }).click()
  await expect(dialog.getByText('BackOff')).toBeVisible()
  await dialog.getByRole('tab', { name: 'YAML' }).click()
  await expect(dialog.getByText(/value: <redacted>/)).toBeVisible()
  await dialog.getByRole('tab', { name: '日志' }).click()
  await expect(dialog.getByText(/gateway ready/)).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-workload-diagnostics.png` })
  expect(consoleErrors).toEqual([])
})

test('Deployment revision history loads on demand within a bounded detail view', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  let revisionRequests = 0
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.endsWith('/workloads/deployment/payments/gateway-api/revisions')) {
      revisionRequests++
    }
  })
  await mockWorkloadDiagnostics(page)

  await page.goto('/#workloads')
  await expect(page.getByText('gateway-api', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '查看 gateway-api' }).click()
  const dialog = page.getByRole('dialog', { name: 'Deployment · gateway-api' })
  await expect(dialog.getByText('uid-gateway-api')).toBeVisible()
  expect(revisionRequests).toBe(0)

  await dialog.getByRole('tab', { name: '发布历史' }).click()
  await expect(dialog.getByText('gateway-api-7f9d8')).toBeVisible()
  await expect(dialog.getByText('gateway-api-6c8b7')).toBeVisible()
  await expect(dialog.getByText('当前')).toBeVisible()
  await expect(dialog.getByText('1 个 ReplicaSet 尚未记录修订号')).toBeVisible()
  expect(revisionRequests).toBe(1)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-deployment-revisions.png` })
  expect(consoleErrors).toEqual([])
})

test('batch workloads stay scoped and expose read-only diagnostics', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockWorkloadDiagnostics(page)

  await page.goto('/#workloads')
  const requestPromise = page.waitForRequest((request) => (
    request.method() === 'GET' && new URL(request.url()).pathname.endsWith('/clusters/clu_1/workloads') &&
    new URL(request.url()).searchParams.get('kind') === 'cronjob'
  ))
  await page.getByLabel('类型').selectOption('cronjob')
  await requestPromise
  await expect(page.getByText('nightly-report', { exact: true })).toBeVisible()
  await expect(page.getByText('等待调度')).toBeVisible()
  await page.getByRole('button', { name: '查看 nightly-report' }).click()

  const dialog = page.getByRole('dialog', { name: 'CronJob · nightly-report' })
  await expect(dialog.getByText('0 个活动任务')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '扩缩容' })).toHaveCount(0)
  await expect(dialog.getByRole('tab', { name: '日志' })).toHaveCount(0)
  await dialog.getByRole('tab', { name: 'YAML' }).click()
  await expect(dialog.getByText(/value: <redacted>/)).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-batch-workloads.png` })
  expect(consoleErrors).toEqual([])
})

test('queued deployment scale can be canceled while resources are constrained', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockWorkloadDiagnostics(page)

  await page.goto('/#workloads')
  await expect(page.getByText('gateway-api', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '查看 gateway-api' }).click()

  const dialog = page.getByRole('dialog', { name: 'Deployment · gateway-api' })
  await expect(dialog.getByText('uid-gateway-api')).toBeVisible()
  await dialog.getByRole('button', { name: '扩缩容' }).click()
  await dialog.getByLabel('副本数').fill('5')
  await dialog.getByLabel('输入集群名称确认').fill('production-cn')
  const requestPromise = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/workloads/deployment/payments/gateway-api/scales'))
  await dialog.getByRole('button', { name: '提交扩缩容' }).click()

  const request = await requestPromise
  expect(request.postDataJSON()).toEqual({ resource_version: '73', confirmation: 'production-cn', replicas: 5 })
  await expect(page.getByRole('heading', { name: '操作中心' })).toBeVisible()
  await expect(page.getByText('工作负载扩缩容')).toBeVisible()
  await expect(page.getByText('资源承压')).toBeVisible()
  await expect(page.getByText('读取槽 2 / 2')).toBeVisible()
  await expect(page.getByText('连接缓存 3 / 4')).toBeVisible()
  await expect(page.getByText('队列 1 / 64')).toBeVisible()

  const cancelRequestPromise = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/operations/op_scale/cancellations'))
  await page.getByRole('button', { name: '取消任务 op_scale' }).click()
  const cancelRequest = await cancelRequestPromise
  expect(cancelRequest.postDataJSON()).toEqual({})
  await expect(page.getByText('已取消', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '取消任务 op_scale' })).toHaveCount(0)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-canceled-scale.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('production deployment image update requires a fresh dry-run preview', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockWorkloadDiagnostics(page)

  await page.goto('/#workloads')
  await expect(page.getByText('gateway-api', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '查看 gateway-api' }).click()
  const dialog = page.getByRole('dialog', { name: 'Deployment · gateway-api' })
  await expect(dialog.getByText('uid-gateway-api')).toBeVisible()
  await dialog.getByRole('button', { name: '更新镜像' }).click()
  await dialog.getByLabel('新镜像').fill('registry.example.com/payments/gateway:1.5.0')

  const previewRequestPromise = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/workloads/deployment/payments/gateway-api/image-previews'))
  await dialog.getByRole('button', { name: '预览变更' }).click()
  const previewRequest = await previewRequestPromise
  expect(previewRequest.postDataJSON()).toEqual({
    container: 'app',
    current_image: 'registry.example.com/payments/gateway:1.4.0',
    image: 'registry.example.com/payments/gateway:1.5.0',
    resource_version: '73',
  })
  await expect(dialog.getByText('服务端 dry-run 通过')).toBeVisible()
  expect(await dialog.evaluate((element) => element.scrollWidth - element.clientWidth)).toBeLessThanOrEqual(1)
  const previewAccessibility = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(previewAccessibility.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-image-preview.png` })
  await dialog.getByLabel('输入集群名称确认').fill('production-cn')

  const updateRequestPromise = page.waitForRequest((request) => request.method() === 'POST' && request.url().endsWith('/workloads/deployment/payments/gateway-api/image-updates'))
  await dialog.getByRole('button', { name: '提交镜像更新' }).click()
  const updateRequest = await updateRequestPromise
  expect(updateRequest.postDataJSON()).toEqual({
    container: 'app',
    current_image: 'registry.example.com/payments/gateway:1.4.0',
    image: 'registry.example.com/payments/gateway:1.5.0',
    resource_version: '73',
    confirmation: 'production-cn',
  })
  await expect(page.getByRole('heading', { name: '操作中心' })).toBeVisible()
  await expect(page.getByText('工作负载镜像更新')).toBeVisible()
  await expect(page.getByText('资源承压')).toBeVisible()
  await expect(page.getByText('读取槽 2 / 2')).toBeVisible()
  await expect(page.getByText('连接缓存 3 / 4')).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-image-update.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('cluster resources show node diagnostics, bounded extensions and admission resources', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedResources: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockClusterResources(page, requestedResources)

  await page.goto('/#resources')
  await expect(page.getByRole('heading', { name: '集群资源' })).toBeVisible()
  await expect(page.getByText('control-01.example.internal', { exact: true }).first()).toBeVisible()
  expect(requestedResources).toEqual(['nodes'])
  await page.getByRole('button', { name: '查看 control-01.example.internal' }).click()

  const dialog = page.getByRole('dialog', { name: '节点 · control-01.example.internal' })
  await expect(dialog).toBeVisible()
  await expect(dialog.getByText('containerd://2.1.4')).toBeVisible()
  await dialog.getByRole('tab', { name: '事件' }).click()
  await expect(dialog.getByText('NodeNotReady')).toBeVisible()

  let overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  let result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-node-diagnostics.png` })

  await dialog.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: '命名空间' }).click()
  await expect(page.getByText('payments', { exact: true })).toBeVisible()
  await expect(page.getByText('team=payments')).toBeVisible()
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])

  await page.getByRole('button', { name: 'CRD' }).click()
  await expect(page.getByText('widgets', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('crds')
  expect(requestedResources).not.toContain('crd-detail')
  await page.getByRole('button', { name: '查看 widgets.platform.example.com' }).click()
  const crdDialog = page.getByRole('dialog', { name: 'CRD · widgets.platform.example.com' })
  await expect(crdDialog.getByText('Widget', { exact: true })).toBeVisible()
  await expect(crdDialog.getByText('v1beta1')).toBeVisible()
  await expect(crdDialog.getByText('已弃用')).toBeVisible()
  await expect(crdDialog.getByText('Established')).toBeVisible()
  expect(requestedResources).toContain('crd-detail')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-crd-detail.png` })

  await crdDialog.getByRole('button', { name: '关闭' }).click()
  expect(requestedResources).not.toContain('csrs')
  await page.getByRole('button', { name: '证书请求' }).click()
  await expect(page.getByText('worker-01', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('csrs')
  expect(requestedResources).not.toContain('csr-detail')
  await page.getByRole('button', { name: '查看 worker-01' }).click()
  const csrDialog = page.getByRole('dialog', { name: '证书请求 · worker-01' })
  await expect(csrDialog.getByText('已批准，等待签发')).toBeVisible()
  await expect(csrDialog.getByText('system:node:worker-01')).toBeVisible()
  await expect(csrDialog.getByText('example.com/node-client')).toBeVisible()
  await expect(csrDialog.getByText('1 天（请求值）')).toBeVisible()
  await expect(csrDialog.getByText('private-pkcs10')).toHaveCount(0)
  await expect(csrDialog.getByText('private-certificate')).toHaveCount(0)
  await expect(csrDialog.getByText('private-group')).toHaveCount(0)
  expect(requestedResources).toContain('csr-detail')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-csr-detail.png` })

  await csrDialog.getByRole('button', { name: '关闭' }).click()
  expect(requestedResources).not.toContain('api-services')
  await page.getByRole('button', { name: '聚合 API' }).click()
  await expect(page.getByText('metrics.k8s.io/v1beta1')).toBeVisible()
  await expect(page.getByText('kube-system/metrics-server:443')).toBeVisible()
  await expect(page.getByText('不可用')).toBeVisible()
  await expect(page.getByText('FailedDiscoveryCheck')).toBeVisible()
  await expect(page.getByText('跳过 TLS 校验')).toBeVisible()
  expect(requestedResources).toContain('api-services')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-api-services.png`, fullPage: true })

  expect(requestedResources).not.toContain('admission-validating')
  await page.getByRole('button', { name: '准入' }).click()
  await expect(page.getByText('policy.platform.example.com', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('admission-validating')
  expect(requestedResources).not.toContain('admission-detail')
  await page.getByRole('button', { name: '查看 policy.platform.example.com' }).click()
  const admissionDialog = page.getByRole('dialog', { name: '准入 Webhook · policy.platform.example.com' })
  await expect(admissionDialog.getByText('policy-system/policy-webhook:443（默认）')).toBeVisible()
  await expect(admissionDialog.getByText('Fail（默认）')).toBeVisible()
  await expect(admissionDialog.getByText('1 条规则 · 2 个操作 · 2 个资源')).toBeVisible()
  await expect(admissionDialog.getByText('private-webhook-path')).toHaveCount(0)
  await expect(admissionDialog.getByText('private-ca-bundle')).toHaveCount(0)
  expect(requestedResources).toContain('admission-detail')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-admission-webhook-detail.png` })

  await admissionDialog.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: 'Mutating' }).click()
  await expect(page.getByText('mutate.platform.example.com', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('admission-mutating')

  expect(requestedResources).not.toContain('admission-policies')
  await page.getByRole('button', { name: '校验策略' }).click()
  await expect(page.getByText('replica-policy.example.com', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('admission-policies')
  expect(requestedResources).not.toContain('admission-policy-detail')
  expect(requestedResources).not.toContain('admission-policy-bindings')
  await page.getByRole('button', { name: '查看 replica-policy.example.com' }).click()
  const admissionPolicyDialog = page.getByRole('dialog', { name: '校验准入策略 · replica-policy.example.com' })
  await expect(admissionPolicyDialog.getByText('rules.example.com/v1 · ReplicaLimit')).toBeVisible()
  await expect(admissionPolicyDialog.getByText('2 个校验 · 1 个审计注解')).toBeVisible()
  await expect(admissionPolicyDialog.getByText('已完成 · 1 个警告')).toBeVisible()
  await expect(admissionPolicyDialog.getByText('private CEL expression')).toHaveCount(0)
  await expect(admissionPolicyDialog.getByText('private warning')).toHaveCount(0)
  expect(requestedResources).toContain('admission-policy-detail')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-admission-policy-detail.png` })

  await admissionPolicyDialog.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: '策略绑定' }).click()
  await expect(page.getByText('replica-binding.example.com', { exact: true })).toBeVisible()
  expect(requestedResources).toContain('admission-policy-bindings')
  expect(requestedResources).not.toContain('admission-policy-binding-detail')
  await page.getByRole('button', { name: '查看 replica-binding.example.com' }).click()
  const admissionPolicyBindingDialog = page.getByRole('dialog', { name: '准入策略绑定 · replica-binding.example.com' })
  await expect(admissionPolicyBindingDialog.getByText('replica-policy.example.com', { exact: true })).toBeVisible()
  await expect(admissionPolicyBindingDialog.getByText('按名称 · policy-system')).toBeVisible()
  await expect(admissionPolicyBindingDialog.getByText('private-param-name')).toHaveCount(0)
  await expect(admissionPolicyBindingDialog.getByText('private-selector')).toHaveCount(0)
  expect(requestedResources).toContain('admission-policy-binding-detail')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-admission-policy-binding-detail.png` })
  expect(consoleErrors).toEqual([])
})

test('network inventory loads one bounded resource kind at a time', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedKinds: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockNetworkResources(page, requestedKinds)

  await page.goto('/#network')
  await expect(page.getByRole('heading', { name: '网络资源' })).toBeVisible()
  await expect(page.getByText('gateway-service', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('services:all')
  expect(requestedKinds.some((request) => request.startsWith('ingresses:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('endpoint-slices:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('network-policies:'))).toBe(false)
  await expect(page.getByText('10.96.0.20')).toBeVisible()
  await expect(page.getByText('203.0.113.20')).toBeVisible()

  await page.getByLabel('命名空间').selectOption('payments')
  await expect.poll(() => requestedKinds).toContain('services:payments')
  expect(requestedKinds.some((request) => request.startsWith('ingresses:'))).toBe(false)
  await page.getByRole('button', { name: 'Ingress' }).click()
  await expect(page.getByText('gateway-ingress', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('ingresses:payments')
  await expect(page.getByText('gateway.example.com')).toBeVisible()
  await expect(page.getByText('已启用')).toBeVisible()

  await page.getByRole('button', { name: 'NetworkPolicy' }).click()
  await expect(page.getByText('gateway-policy', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('network-policies:payments')
  await expect(page.getByText('带筛选条件')).toBeVisible()
  await expect(page.getByText('本策略无出站规则')).toBeVisible()

  await page.getByRole('button', { name: 'EndpointSlice' }).click()
  await expect(page.getByText('gateway-ipv4', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('endpoint-slices:payments')
  await expect(page.getByText('gateway-service', { exact: true })).toBeVisible()
  await expect(page.getByText('1 个按 API 默认').first()).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-network-inventory.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('configuration inventory keeps Secret reads namespace scoped', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedKinds: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockConfigurationResources(page, requestedKinds)

  await page.goto('/#configuration')
  await expect(page.getByRole('heading', { name: '配置资源' })).toBeVisible()
  await expect(page.getByText('app-settings', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('configmaps:all')
  expect(requestedKinds.some((request) => request.startsWith('secrets:'))).toBe(false)

  await page.getByRole('button', { name: 'Secret' }).click()
  await expect(page.getByText('请选择命名空间')).toBeVisible()
  expect(requestedKinds.some((request) => request.startsWith('secrets:'))).toBe(false)
  await page.getByLabel('命名空间').selectOption('payments')
  await expect(page.getByText('registry-secret', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('secrets:payments')
  await expect(page.getByText('kubernetes.io/dockerconfigjson')).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-configuration-inventory.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('storage inventory keeps cluster resources namespace independent', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedKinds: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockStorageResources(page, requestedKinds)

  await page.goto('/#storage')
  await expect(page.getByRole('heading', { name: '存储资源' })).toBeVisible()
  await expect(page.getByText('payments-data', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('claims:all')
  expect(requestedKinds.some((request) => request.startsWith('volumes:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('classes:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('csidrivers:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('attachments:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('csinodes:'))).toBe(false)
  expect(requestedKinds.some((request) => request.startsWith('capacities:'))).toBe(false)

  await page.getByLabel('命名空间').selectOption('payments')
  await expect.poll(() => requestedKinds).toContain('claims:payments')
  await page.getByRole('button', { name: 'PV', exact: true }).click()
  await expect(page.getByText('pv-payments-data', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('volumes:all')
  await expect(page.getByLabel('命名空间')).toBeDisabled()

  await page.getByRole('button', { name: 'StorageClass' }).click()
  await expect(page.getByText('csi.example.com')).toBeVisible()
  expect(requestedKinds).toContain('classes:all')
  await expect(page.getByText('默认')).toBeVisible()
  await expect(page.getByText('支持')).toBeVisible()

  await page.getByRole('button', { name: 'CSIDriver' }).click()
  await expect(page.getByText('ebs.csi.example.com', { exact: true })).toBeVisible()
  await expect(page.getByText('local.csi.example.com', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('csidrivers:cluster')
  expect(requestedKinds.filter((request) => request === 'namespaces')).toHaveLength(1)
  await page.getByRole('button', { name: '查看 ebs.csi.example.com' }).click()
  const dialog = page.getByRole('dialog', { name: 'CSIDriver · ebs.csi.example.com' })
  await expect(dialog.getByText('File')).toBeVisible()
  await expect(dialog.getByText('Persistent · Ephemeral')).toBeVisible()
  await expect(dialog.getByText('2 项')).toBeVisible()
  await expect(dialog.getByText('private-storage-api')).toHaveCount(0)
  expect(requestedKinds.at(-1)).toBe('csidriver-detail:ebs.csi.example.com')

  await dialog.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: '卷挂接' }).click()
  await expect(page.getByText('attach-payments-data', { exact: true })).toBeVisible()
  await expect(page.getByText('已挂接', { exact: true })).toBeVisible()
  await expect(page.getByText('正在分离', { exact: true })).toBeVisible()
  await expect(page.getByText('内联迁移卷', { exact: true })).toBeVisible()
  await expect(page.getByText('private-attach-error')).toHaveCount(0)
  expect(requestedKinds).toContain('attachments:cluster')
  expect(requestedKinds.filter((request) => request === 'namespaces')).toHaveLength(1)

  await page.getByRole('button', { name: 'CSI节点' }).click()
  await expect(page.getByText('worker-01', { exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'CSINode 清单' }).getByText('2', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('csinodes:cluster')
  expect(requestedKinds.filter((request) => request === 'namespaces')).toHaveLength(1)
  await page.getByRole('button', { name: '查看 worker-01' }).click()
  const nodeDialog = page.getByRole('dialog', { name: 'CSINode · worker-01' })
  await expect(nodeDialog.getByText('ebs.csi.example.com', { exact: true })).toBeVisible()
  await expect(nodeDialog.getByText('local.csi.example.com', { exact: true })).toBeVisible()
  await expect(nodeDialog.getByText('未声明上限')).toBeVisible()
  await expect(nodeDialog.getByText('private-storage-node-01')).toHaveCount(0)
  await expect(nodeDialog.getByText('topology.example.com/zone')).toHaveCount(0)
  expect(requestedKinds.at(-1)).toBe('csinode-detail:worker-01')

  await nodeDialog.getByRole('button', { name: '关闭' }).click()
  await page.getByRole('button', { name: 'CSI容量' }).click()
  const capacityTable = page.getByRole('region', { name: 'CSIStorageCapacity 清单' })
  await expect(capacityTable.getByText('capacity-payments', { exact: true })).toBeVisible()
  await expect(capacityTable.getByText('80Gi', { exact: true })).toBeVisible()
  await expect(capacityTable.getByText('未报告', { exact: true })).toBeVisible()
  await expect(capacityTable.getByText('private-topology-zone')).toHaveCount(0)
  await expect(page.getByLabel('命名空间')).toBeEnabled()
  expect(requestedKinds).toContain('capacities:payments')
  expect(requestedKinds.filter((request) => request === 'namespaces')).toHaveLength(2)

  const storageKindLabelOverflow = await page.getByRole('group', { name: '存储资源类型' })
    .getByRole('button')
    .evaluateAll((buttons) => Math.max(...buttons.map((button) => button.scrollWidth - button.clientWidth)))
  expect(storageKindLabelOverflow).toBeLessThanOrEqual(1)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-csi-storage-capacity.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('governance reads one bounded policy kind at a time', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedKinds: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockGovernanceResources(page, requestedKinds)

  await page.goto('/#governance')
  await expect(page.getByRole('heading', { name: '资源治理' })).toBeVisible()
  await expect(page.getByText('请选择命名空间')).toBeVisible()
  expect(requestedKinds).toEqual([])

  await page.getByLabel('命名空间').selectOption('payments')
  await expect(page.getByText('compute-quota', { exact: true }).first()).toBeVisible()
  expect(requestedKinds).toEqual(['resourcequotas:payments'])
  await expect(page.getByText('2 / 4')).toBeVisible()
  await expect(page.getByText('已同步').first()).toBeVisible()

  await page.getByRole('button', { name: 'LimitRange' }).click()
  await expect(page.getByText('namespace-defaults', { exact: true })).toBeVisible()
  expect(requestedKinds).toEqual(['resourcequotas:payments', 'limitranges:payments'])
  await expect(page.getByText('250m')).toBeVisible()
  await expect(page.getByText('500m')).toBeVisible()

  await page.getByRole('button', { name: 'HPA' }).click()
  await expect(page.getByText('gateway-autoscaler', { exact: true })).toBeVisible()
  expect(requestedKinds).toEqual(['resourcequotas:payments', 'limitranges:payments', 'hpa:payments'])
  await expect(page.getByText('3 -> 5')).toBeVisible()
  await expect(page.getByText('ScalingActive=True')).toBeVisible()

  await page.getByRole('button', { name: 'PDB' }).click()
  await expect(page.getByText('gateway-budget', { exact: true })).toBeVisible()
  expect(requestedKinds).toEqual(['resourcequotas:payments', 'limitranges:payments', 'hpa:payments', 'pdb:payments'])
  await expect(page.getByText('75%')).toBeVisible()
  await expect(page.getByText('DisruptionAllowed=True')).toBeVisible()

  await page.getByRole('button', { name: 'PriorityClass' }).click()
  await expect(page.getByText('workload-high', { exact: true })).toBeVisible()
  await expect(page.getByText('system-cluster-critical', { exact: true })).toBeVisible()
  await expect(page.getByLabel('命名空间')).toHaveCount(0)
  expect(requestedKinds).toEqual([
    'resourcequotas:payments', 'limitranges:payments', 'hpa:payments', 'pdb:payments', 'priorityclasses:cluster',
  ])
  await page.getByRole('button', { name: '查看 workload-high' }).click()
  const dialog = page.getByRole('dialog', { name: 'PriorityClass · workload-high' })
  await expect(dialog.getByText('1,000,000')).toBeVisible()
  await expect(dialog.getByText('PreemptLowerPriority（默认）')).toBeVisible()
  await expect(dialog.getByText('是')).toBeVisible()
  await expect(dialog.getByText('private scheduling guidance')).toHaveCount(0)
  expect(requestedKinds.at(-1)).toBe('priorityclass-detail:workload-high')
  await dialog.getByRole('button', { name: '关闭' }).click()

  await page.getByRole('button', { name: 'RuntimeClass' }).click()
  await expect(page.getByText('kata-containers', { exact: true })).toBeVisible()
  await expect(page.getByText('runc', { exact: true })).toBeVisible()
  await expect(page.getByLabel('命名空间')).toHaveCount(0)
  expect(requestedKinds.at(-1)).toBe('runtimeclasses:cluster')
  await page.getByRole('button', { name: '查看 kata-containers' }).click()
  const runtimeDialog = page.getByRole('dialog', { name: 'RuntimeClass · kata-containers' })
  await expect(runtimeDialog.getByText('kata-fc')).toBeVisible()
  await expect(runtimeDialog.getByText('250m')).toBeVisible()
  await expect(runtimeDialog.getByText('120Mi')).toBeVisible()
  await expect(runtimeDialog.getByText('private.example.com/runtime')).toHaveCount(0)
  await expect(runtimeDialog.getByText('private-taint')).toHaveCount(0)
  expect(requestedKinds.at(-1)).toBe('runtimeclass-detail:kata-containers')

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-runtime-class-governance.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('security posture loads each bounded projection only when selected', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedResources: string[] = []
  const browserPaths: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  page.on('request', (request) => browserPaths.push(new URL(request.url()).pathname))
  await mockSecurityPosture(page, requestedResources)

  await page.goto('/#security')
  await expect(page.getByRole('heading', { name: '安全态势' })).toBeVisible()
  await expect(page.getByText('payments', { exact: true })).toBeVisible()
  await expect(page.getByText('restricted', { exact: true })).toBeVisible()
  await expect(page.getByText('v1.30（固定）')).toBeVisible()
  await expect(page.getByText('存在无效标签')).toBeVisible()
  expect(requestedResources).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])
  expect(requestedResources).not.toContain('/api/v1/clusters/clu_1/namespaces')

  await page.getByLabel('搜索 Pod 安全态势').fill('legacy')
  await expect(page.getByText('legacy', { exact: true })).toBeVisible()
  await expect(page.getByText('payments', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: '版本偏差' }).click()
  await expect(page.getByText('v1.36.2', { exact: true })).toBeVisible()
  await expect(page.getByText('worker-current', { exact: true })).toBeVisible()
  await expect(page.getByText('同一次版本', { exact: true })).toBeVisible()
  await expect(page.getByText('升级前需处理', { exact: true })).toBeVisible()
  await expect(page.getByText('2 个节点需处理', { exact: true })).toBeVisible()
  expect(requestedResources).toEqual([
    '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
    '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
  ])
  expect(requestedResources).not.toContain('/api/v1/clusters/clu_1/nodes')

  await page.getByLabel('搜索节点版本偏差').fill('主版本不一致')
  await expect(page.getByText('worker-major', { exact: true })).toBeVisible()
  await expect(page.getByText('worker-current', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: '废弃 API' }).click()
  await expect(page.getByText('extensions/v1beta1', { exact: true })).toBeVisible()
  await expect(page.getByText('ingresses', { exact: true })).toBeVisible()
  await expect(page.getByText('v1.22', { exact: true })).toBeVisible()
  await expect(page.getByText('检测到 2 项废弃 API 请求证据', { exact: true })).toBeVisible()
  expect(requestedResources).toEqual([
    '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
    '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
    '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis',
  ])
  expect(browserPaths).not.toContain('/api/v1/clusters/clu_1/nodes')
  expect(browserPaths).not.toContain('/metrics')
  expect(browserPaths).not.toContain('/api')
  expect(browserPaths).not.toContain('/apis')

  await page.getByLabel('搜索废弃 API 请求证据').fill('apps/v1beta1')
  await expect(page.getByText('apps/v1beta1', { exact: true })).toBeVisible()
  await expect(page.getByText('extensions/v1beta1', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: '中断预算' }).click()
  await expect(page.getByText('gateway-budget', { exact: true })).toBeVisible()
  await expect(page.getByText('当前受阻', { exact: true })).toBeVisible()
  await expect(page.getByText('允许中断', { exact: true })).toBeVisible()
  await expect(page.getByText('1 项当前受阻证据', { exact: true })).toBeVisible()
  await expect(page.getByText('不代表节点一定无法排空', { exact: true })).toBeVisible()
  expect(requestedResources).toEqual([
    '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
    '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
    '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis',
    '/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets',
  ])
  expect(browserPaths).not.toContain('/api/v1/clusters/clu_1/pods')
  expect(browserPaths).not.toContain('/api/v1/clusters/clu_1/nodes')
  await page.screenshot({ path: `test-results/${testInfo.project.name}-security-disruption-budgets.png`, fullPage: true })

  await page.getByLabel('搜索中断预算证据').fill('platform available')
  await expect(page.getByText('platform-budget', { exact: true })).toBeVisible()
  await expect(page.getByText('gateway-budget', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: 'TLS 证书' }).click()
  await expect(page.getByText('当前连接端点', { exact: true })).toBeVisible()
  await expect(page.getByText('TLS 握手叶证书', { exact: true })).toBeVisible()
  await expect(page.getByText('30 天内到期', { exact: true })).toBeVisible()
  await expect(page.getByText('2026-08-28 08:00 UTC', { exact: true })).toBeVisible()
  await expect(page.getByRole('searchbox')).toHaveCount(0)
  expect(requestedResources).toEqual([
    '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
    '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
    '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis',
    '/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets',
    '/api/v1/clusters/clu_1/upgrade-readiness/endpoint-certificate',
  ])
  expect(browserPaths).not.toContain('/metrics')
  expect(browserPaths).not.toContain('/api')
  expect(browserPaths).not.toContain('/apis')

  await page.getByRole('button', { name: 'API 就绪' }).click()
  await expect(page.getByText('当前连接端点未就绪', { exact: true })).toBeVisible()
  await expect(page.getByText('当前 API Server 连接端点', { exact: true })).toBeVisible()
  await expect(page.getByText('单次 /readyz 观测', { exact: true })).toBeVisible()
  await expect(page.getByText('1 项检查失败', { exact: true })).toBeVisible()
  await expect(page.getByText('ping', { exact: true })).toBeVisible()
  await expect(page.getByText('etcd', { exact: true })).toBeVisible()
  await expect(page.getByText('private-etcd.example.com:2379', { exact: true })).toHaveCount(0)
  expect(requestedResources).toEqual([
    '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
    '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
    '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis',
    '/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets',
    '/api/v1/clusters/clu_1/upgrade-readiness/endpoint-certificate',
    '/api/v1/clusters/clu_1/control-plane/readiness',
  ])
  expect(browserPaths).not.toContain('/readyz')
  expect(browserPaths).not.toContain('/livez')
  expect(browserPaths).not.toContain('/healthz')

  await page.getByLabel('搜索 API Server 就绪检查').fill('etcd 失败')
  await expect(page.getByText('etcd', { exact: true })).toBeVisible()
  await expect(page.getByText('ping', { exact: true })).toHaveCount(0)

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-security-posture.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('access inventory loads metadata by kind and fetches details on demand', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedKinds: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockAccessResources(page, requestedKinds)

  await page.goto('/#access')
  await expect(page.getByRole('heading', { name: '访问控制' })).toBeVisible()
  await expect(page.getByText('view', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('clusterroles:all')
  expect(requestedKinds.some((request) => request.startsWith('roles:'))).toBe(false)

  await page.getByRole('button', { name: '查看 view' }).click()
  const dialog = page.getByRole('dialog', { name: 'ClusterRole · view' })
  await expect(dialog.getByText('pods, deployments')).toBeVisible()
  await expect(dialog.getByText('get, list')).toBeVisible()
  expect(requestedKinds).toContain('detail:clusterroles:view:all')
  let overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const detailAccessibility = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(detailAccessibility.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-access-detail.png` })
  await dialog.getByRole('button', { name: '关闭' }).click()

  await page.getByRole('button', { name: 'Role', exact: true }).click()
  await expect(page.getByText('请选择命名空间', { exact: true }).last()).toBeVisible()
  expect(requestedKinds.some((request) => request.startsWith('roles:'))).toBe(false)
  await page.getByLabel('命名空间').selectOption('payments')
  await expect(page.getByText('gateway-reader', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('roles:payments')

  await page.getByRole('button', { name: 'ServiceAccount' }).click()
  await expect(page.getByText('gateway', { exact: true })).toBeVisible()
  expect(requestedKinds).toContain('serviceaccounts:payments')
  await page.getByRole('button', { name: '查看 gateway' }).click()
  const serviceAccountDialog = page.getByRole('dialog', { name: 'ServiceAccount · gateway' })
  await expect(serviceAccountDialog.getByRole('heading', { name: '权限模拟' })).toBeVisible()
  expect(requestedKinds.some((request) => request.startsWith('review:'))).toBe(false)
  await serviceAccountDialog.getByLabel('目标资源').selectOption('pod-logs')
  await serviceAccountDialog.getByLabel('对象名称（可选）').fill('gateway-0')
  const reviewRequestPromise = page.waitForRequest((request) => (
    request.method() === 'POST' && request.url().endsWith('/service-account-access-reviews')
  ))
  await serviceAccountDialog.getByRole('button', { name: '检查权限' }).click()
  const reviewRequest = await reviewRequestPromise
  expect(reviewRequest.postDataJSON()).toEqual({
    service_account: { namespace: 'payments', name: 'gateway' },
    resource_attributes: { resource: 'pods', subresource: 'log', verb: 'get', namespace: 'payments', name: 'gateway-0' },
  })
  await expect(serviceAccountDialog.getByText('允许', { exact: true })).toBeVisible()
  const reviewAccessibility = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(reviewAccessibility.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-service-account-access-review.png` })

  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-access-inventory.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('Helm release history is metadata-only, on demand and rollback-confirmed', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedHistory: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockHelmResources(page, requestedHistory)

  await page.goto('/#helm')
  await expect(page.getByRole('heading', { name: 'Helm' })).toBeVisible()
  await expect(page.getByText('gateway-1.4.0', { exact: true })).toBeVisible()
  await expect(page.getByLabel('命名空间')).toHaveValue('payments')
  expect(requestedHistory).toEqual([])

  await page.getByRole('button', { name: '查看 gateway 修订历史' }).click()
  const dialog = page.getByRole('dialog', { name: '修订历史 · gateway' })
  await expect(dialog.getByText('仅显示最近 10 个修订')).toBeVisible()
  await expect(dialog.getByText('当前')).toBeVisible()
  await expect(dialog.getByText('失败')).toBeVisible()
  await expect(dialog.getByText('已替代')).toBeVisible()
  await expect(dialog.getByText('private-release-values')).toHaveCount(0)
  await expect(dialog.getByText('private-release-manifest')).toHaveCount(0)
  await expect(dialog.getByText('sh.helm.release.v1.gateway.v4')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '选择 revision 2 回滚' })).toBeInViewport()
  expect(requestedHistory).toEqual(['clu_1:payments:gateway'])

  let overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const modalAccessibility = await new AxeBuilder({ page }).include('.modal').analyze()
  expect(modalAccessibility.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-helm-release-history.png`, fullPage: true })

  await dialog.getByRole('button', { name: '选择 revision 2 回滚' }).click()
  const rollbackDialog = page.getByRole('dialog', { name: '回滚 gateway' })
  await expect(rollbackDialog.getByLabel('目标 Revision')).toHaveValue('2')
  overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  expect(consoleErrors).toEqual([])
})

test('event center defaults to bounded Warning events and supports scoped inspection', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  const requestedEvents: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockEventResources(page, requestedEvents)

  await page.goto('/#events')
  await expect(page.getByRole('heading', { name: '集群事件' })).toBeVisible()
  await expect(page.getByText('BackOff', { exact: true })).toBeVisible()
  expect(requestedEvents).toContain('all:Warning:200')
  await expect(page.getByText('Scheduled', { exact: true })).toHaveCount(0)

  await page.getByRole('button', { name: '全部事件' }).click()
  await expect(page.getByText('Scheduled', { exact: true })).toBeVisible()
  expect(requestedEvents).toContain('all:all:200')
  await page.getByLabel('命名空间').selectOption('payments')
  await expect.poll(() => requestedEvents).toContain('payments:all:200')

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-event-center.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
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

async function mockClusterManagement(page: Page) {
  let rotated = false
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'cluster-admin', role: 'admin', expires_at: '2026-07-27T16:00:00Z' }
    } else if (path === '/api/v1/clusters' && route.request().method() === 'GET') {
      data = [{
        id: 'clu_1', name: 'production-east', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: rotated ? 'v1.36.3' : 'v1.36.2', credentials_configured: true,
        last_checked_at: '2026-07-27T08:00:00Z', created_at: '2026-07-24T08:00:00Z', updated_at: '2026-07-27T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/credential-rotations' && route.request().method() === 'POST') {
      rotated = true
      data = {
        id: 'clu_1', name: 'production-east', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.3', credentials_configured: true,
        last_checked_at: '2026-07-27T08:05:00Z', created_at: '2026-07-24T08:00:00Z', updated_at: '2026-07-27T08:05:00Z',
      }
    } else if (path === '/api/v1/clusters/clu_1/capabilities' && route.request().method() === 'GET') {
      data = {
        namespace: new URL(route.request().url()).searchParams.get('namespace'), checked_at: '2026-07-27T08:10:00Z',
        checks: [
          { key: 'namespaces.list', state: 'allowed' },
          { key: 'pods.logs.get', state: 'denied' },
          { key: 'deployments.patch', state: 'indeterminate' },
        ],
      }
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockNetworkResources(page: Page, requestedKinds: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'network-admin', role: 'admin', expires_at: '2026-07-28T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-27T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/services') {
      requestedKinds.push(`services:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'gateway-service', type: 'LoadBalancer', cluster_ip: '10.96.0.20',
        external_addresses: ['203.0.113.20', 'gateway-lb.example.com'], address_count: 2,
        ports: [{ name: 'http', protocol: 'TCP', port: 80, target_port: '8080', node_port: 30080 }], port_count: 1,
        created_at: '2026-07-24T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/ingresses') {
      requestedKinds.push(`ingresses:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'gateway-ingress', class_name: 'nginx',
        hosts: ['gateway.example.com'], host_count: 1, addresses: ['203.0.113.30'], address_count: 1,
        tls: true, rule_count: 1, path_count: 2, created_at: '2026-07-24T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/endpoint-slices') {
      requestedKinds.push(`endpoint-slices:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'gateway-ipv4', service_name: 'gateway-service', address_type: 'IPv4',
        endpoint_count: 3,
        ready_endpoint_count: 2, ready_defaulted_count: 1,
        serving_endpoint_count: 2, serving_defaulted_count: 1,
        terminating_endpoint_count: 1, terminating_defaulted_count: 1,
        port_count: 1, created_at: '2026-07-28T05:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/network-policies') {
      requestedKinds.push(`network-policies:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'gateway-policy', pod_selector_mode: 'filtered',
        pod_selector_label_count: 1, pod_selector_expression_count: 1,
        policy_types: ['Ingress', 'Egress'], policy_types_defaulted: true,
        ingress_rule_count: 1, ingress_peer_count: 2, ingress_port_count: 1,
        egress_rule_count: 0, egress_peer_count: 0, egress_port_count: 0,
        created_at: '2026-07-28T04:00:00Z',
      }]
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockConfigurationResources(page: Page, requestedKinds: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'configuration-admin', role: 'admin', expires_at: '2026-07-28T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-27T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/configmaps') {
      requestedKinds.push(`configmaps:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{ namespace: 'payments', name: 'app-settings', data_count: 3, created_at: '2026-07-24T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/secrets') {
      requestedKinds.push(`secrets:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'registry-secret', type: 'kubernetes.io/dockerconfigjson', data_count: 1,
        created_at: '2026-07-25T08:00:00Z',
      }]
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockStorageResources(page: Page, requestedKinds: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'storage-admin', role: 'admin', expires_at: '2026-07-28T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-27T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      requestedKinds.push('namespaces')
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/persistent-volume-claims') {
      requestedKinds.push(`claims:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        namespace: 'payments', name: 'payments-data', status: 'Bound', volume: 'pv-payments-data', capacity: '20Gi',
        access_modes: 'RWO', storage_class: 'standard', volume_mode: 'Filesystem', created_at: '2026-07-24T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/persistent-volumes') {
      requestedKinds.push(`volumes:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        name: 'pv-payments-data', status: 'Bound', claim: 'payments/payments-data', capacity: '20Gi', access_modes: 'RWO',
        storage_class: 'standard', reclaim_policy: 'Delete', volume_mode: 'Filesystem', created_at: '2026-07-23T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/storage-classes') {
      requestedKinds.push(`classes:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [{
        name: 'standard', provisioner: 'csi.example.com', reclaim_policy: 'Delete',
        volume_binding_mode: 'WaitForFirstConsumer', allow_volume_expansion: true, default: true,
        created_at: '2026-07-22T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/csi-drivers/ebs.csi.example.com') {
      requestedKinds.push('csidriver-detail:ebs.csi.example.com')
      data = {
        name: 'ebs.csi.example.com', created_at: '2026-07-22T08:05:00Z',
        attach_required: true, pod_info_on_mount: true, storage_capacity: true, requires_republish: false,
        se_linux_mount: true, fs_group_policy: 'File', volume_lifecycle_modes: ['Persistent', 'Ephemeral'],
        token_request_count: 2, tokenRequests: [{ audience: 'private-storage-api', expirationSeconds: 3600 }],
      }
    } else if (path === '/api/v1/clusters/clu_1/csi-drivers') {
      requestedKinds.push('csidrivers:cluster')
      data = [
        { name: 'ebs.csi.example.com', created_at: '2026-07-22T08:05:00Z' },
        { name: 'local.csi.example.com', created_at: '2026-07-22T08:10:00Z' },
      ]
    } else if (path === '/api/v1/clusters/clu_1/volume-attachments') {
      requestedKinds.push('attachments:cluster')
      data = [
        {
          name: 'attach-payments-data', attacher: 'ebs.csi.example.com', persistent_volume: 'pv-payments-data',
          node: 'worker-01', status: 'attached', created_at: '2026-07-31T08:00:00Z',
        },
        {
          name: 'attach-inline', attacher: 'kubernetes.io/csi-migrated', node: 'worker-02', status: 'detaching',
          created_at: '2026-07-31T08:02:00Z', attach_error: 'private-attach-error',
        },
      ]
    } else if (path === '/api/v1/clusters/clu_1/csi-storage-capacities') {
      requestedKinds.push(`capacities:${url.searchParams.get('namespace') ?? 'all'}`)
      data = [
        {
          namespace: 'payments', name: 'capacity-payments', storage_class: 'standard', capacity: '80Gi',
          created_at: '2026-07-31T08:03:00Z', node_topology: 'private-topology-zone',
        },
        {
          namespace: 'payments', name: 'capacity-unset', storage_class: 'archive',
          created_at: '2026-07-31T08:04:00Z',
        },
      ]
    } else if (path === '/api/v1/clusters/clu_1/csi-nodes/worker-01') {
      requestedKinds.push('csinode-detail:worker-01')
      data = {
        name: 'worker-01', driver_count: 2, created_at: '2026-07-31T08:00:00Z',
        drivers: [
          {
            name: 'ebs.csi.example.com', allocatable_count: 12, topology_key_count: 2,
            node_id: 'private-storage-node-01', topology_keys: ['topology.example.com/zone'],
          },
          { name: 'local.csi.example.com', topology_key_count: 0 },
        ],
      }
    } else if (path === '/api/v1/clusters/clu_1/csi-nodes') {
      requestedKinds.push('csinodes:cluster')
      data = [
        { name: 'worker-01', driver_count: 2, created_at: '2026-07-31T08:00:00Z' },
        { name: 'worker-02', driver_count: 0, created_at: '2026-07-31T08:02:00Z' },
      ]
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockGovernanceResources(page: Page, requestedKinds: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'governance-admin', role: 'admin', expires_at: '2026-07-29T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-28T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/resource-quotas') {
      requestedKinds.push(`resourcequotas:${url.searchParams.get('namespace') ?? 'missing'}`)
      data = [{
        namespace: 'payments', name: 'compute-quota', scopes: ['NotTerminating'], scope_count: 1,
        scopes_truncated: false, scope_selector_count: 1,
        resources: [
          { name: 'requests.cpu', hard: '4', used: '2', observed: true },
          { name: 'requests.memory', hard: '8Gi', used: '6Gi', observed: true },
        ],
        resource_count: 2, resources_truncated: false, created_at: '2026-07-28T02:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/limit-ranges') {
      requestedKinds.push(`limitranges:${url.searchParams.get('namespace') ?? 'missing'}`)
      data = [{
        namespace: 'payments', name: 'namespace-defaults',
        constraints: [{
          type: 'Container', resource: 'cpu', default_request: '250m', default: '500m',
          min: '100m', max: '2', max_limit_request_ratio: '4',
        }],
        constraint_count: 1, constraints_truncated: false, created_at: '2026-07-28T02:05:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/horizontal-pod-autoscalers') {
      requestedKinds.push(`hpa:${url.searchParams.get('namespace') ?? 'missing'}`)
      data = [{
        namespace: 'payments', name: 'gateway-autoscaler', target_api_version: 'apps/v1',
        target_kind: 'Deployment', target_name: 'gateway', min_replicas: 2, min_replicas_defaulted: false,
        max_replicas: 10, current_replicas: 3, desired_replicas: 5, metric_count: 2, current_metric_count: 1,
        observed: true, conditions: [{ type: 'ScalingActive', status: 'True', reason: 'ValidMetricFound' }],
        condition_count: 1, conditions_truncated: false, last_scale_time: '2026-07-28T03:05:00Z',
        created_at: '2026-07-28T03:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/pod-disruption-budgets') {
      requestedKinds.push(`pdb:${url.searchParams.get('namespace') ?? 'missing'}`)
      data = [{
        namespace: 'payments', name: 'gateway-budget', selector_mode: 'filtered', selector_label_count: 1,
        selector_expression_count: 1, min_available: '75%', current_healthy: 3, desired_healthy: 3,
        disruptions_allowed: 1, expected_pods: 4, observed: true,
        unhealthy_pod_eviction_policy: 'IfHealthyBudget', unhealthy_pod_eviction_policy_defaulted: true,
        conditions: [{ type: 'DisruptionAllowed', status: 'True', reason: 'SufficientPods' }],
        condition_count: 1, conditions_truncated: false, created_at: '2026-07-28T03:12:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/priority-classes/workload-high') {
      requestedKinds.push('priorityclass-detail:workload-high')
      data = {
        name: 'workload-high', created_at: '2026-07-28T03:15:00Z', value: 1000000,
        global_default: true, preemption_policy: 'PreemptLowerPriority', preemption_policy_defaulted: true,
        description: 'private scheduling guidance', annotations: { private: 'value' },
      }
    } else if (path === '/api/v1/clusters/clu_1/priority-classes') {
      requestedKinds.push('priorityclasses:cluster')
      data = [
        { name: 'system-cluster-critical', created_at: '2026-07-20T08:00:00Z' },
        { name: 'workload-high', created_at: '2026-07-28T03:15:00Z' },
      ]
    } else if (path === '/api/v1/clusters/clu_1/runtime-classes/kata-containers') {
      requestedKinds.push('runtimeclass-detail:kata-containers')
      data = {
        name: 'kata-containers', created_at: '2026-07-28T03:20:00Z', handler: 'kata-fc',
        overhead_configured: true, pod_overhead_cpu: '250m', pod_overhead_memory: '120Mi',
        overhead_resource_count: 3, scheduling_configured: true, node_selector_count: 2, toleration_count: 2,
        nodeSelector: { 'private.example.com/runtime': 'kata' }, tolerations: [{ key: 'private-taint' }],
      }
    } else if (path === '/api/v1/clusters/clu_1/runtime-classes') {
      requestedKinds.push('runtimeclasses:cluster')
      data = [
        { name: 'kata-containers', created_at: '2026-07-28T03:20:00Z' },
        { name: 'runc', created_at: '2026-07-20T08:00:00Z' },
      ]
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockSecurityPosture(page: Page, requestedResources: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'security-admin', role: 'admin', expires_at: '2026-07-29T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-28T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/pod-security-admission/namespaces') {
      requestedResources.push(path)
      data = [
        {
          name: 'payments',
          enforce: { status: 'configured', level: 'restricted', version: 'v1.30', version_defaulted: false },
          audit: { status: 'configured', level: 'baseline', version: 'latest', version_defaulted: true },
          warn: { status: 'inherited', version_defaulted: false },
          invalid_mode_count: 0, created_at: '2026-07-20T08:00:00Z',
        },
        {
          name: 'legacy',
          enforce: { status: 'invalid', version_defaulted: false },
          audit: { status: 'inherited', version_defaulted: false },
          warn: { status: 'inherited', version_defaulted: false },
          invalid_mode_count: 1, created_at: '2026-07-19T08:00:00Z',
        },
      ]
    } else if (path === '/api/v1/clusters/clu_1/upgrade-readiness/node-versions') {
      requestedResources.push(path)
      data = {
        api_server_version: 'v1.36.2',
        nodes: [
          {
            name: 'worker-current', kubelet_version: 'v1.36.1', status: 'same-minor',
            minor_skew: 0, maximum_minor_skew: 3, minor_skew_comparable: true,
          },
          {
            name: 'worker-old', kubelet_version: 'v1.33.9', status: 'upgrade-blocking',
            minor_skew: 3, maximum_minor_skew: 3, minor_skew_comparable: true,
          },
          {
            name: 'worker-major', kubelet_version: 'v2.33.0', status: 'major-mismatch',
            minor_skew: 0, maximum_minor_skew: 0, minor_skew_comparable: false,
          },
        ],
      }
    } else if (path === '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis') {
      requestedResources.push(path)
      data = [
        {
          group: 'apps', version: 'v1beta1', resource: 'deployments', subresource: 'scale',
          removed_release: '1.16',
        },
        {
          group: 'extensions', version: 'v1beta1', resource: 'ingresses', subresource: '',
          removed_release: '1.22',
        },
      ]
    } else if (path === '/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets') {
      requestedResources.push(path)
      const baseBudget = {
        selector_mode: 'filtered', selector_label_count: 1, selector_expression_count: 0,
        min_available: '75%', current_healthy: 3, desired_healthy: 3,
        expected_pods: 4, observed: true,
        unhealthy_pod_eviction_policy: 'IfHealthyBudget', unhealthy_pod_eviction_policy_defaulted: true,
        conditions: [], condition_count: 0, conditions_truncated: false,
        created_at: '2026-07-30T08:00:00Z',
      }
      data = [
        {
          ...baseBudget, namespace: 'payments', name: 'gateway-budget',
          disruptions_allowed: 0, disruption_status: 'blocked',
        },
        {
          ...baseBudget, namespace: 'platform', name: 'platform-budget',
          disruptions_allowed: 1, disruption_status: 'available',
        },
      ]
    } else if (path === '/api/v1/clusters/clu_1/upgrade-readiness/endpoint-certificate') {
      requestedResources.push(path)
      data = {
        observed_at: '2026-07-29T08:00:00Z',
        not_before: '2026-06-29T08:00:00Z',
        not_after: '2026-08-28T08:00:00Z',
        remaining_seconds: 2592000,
        status: 'expiring',
      }
    } else if (path === '/api/v1/clusters/clu_1/control-plane/readiness') {
      requestedResources.push(path)
      data = {
        observed_at: '2026-07-31T08:00:00Z',
        ready: false,
        passed_checks: 1,
        failed_checks: 1,
        checks: [
          { name: 'ping', status: 'passed' },
          { name: 'etcd', status: 'failed' },
        ],
      }
    } else {
      requestedResources.push(path)
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'not_found', message: `No mock for ${path}` } }),
      })
      return
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockHelmResources(page: Page, requestedHistory: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'helm-admin', role: 'admin', expires_at: '2026-07-30T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'development', environment: 'development', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-30T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/chart-repositories') {
      data = []
    } else if (path === '/api/v1/helm-releases/gateway/history') {
      requestedHistory.push(`clu_1:${url.searchParams.get('namespace')}:gateway`)
      data = {
        name: 'gateway', namespace: 'payments', truncated: true,
        revisions: [
          { revision: 4, status: 'deployed', created_at: '2026-07-30T09:04:00Z' },
          { revision: 3, status: 'failed', created_at: '2026-07-30T09:03:00Z' },
          { revision: 2, status: 'superseded', created_at: '2026-07-30T09:02:00Z' },
        ],
        storage_secret: 'sh.helm.release.v1.gateway.v4',
        values: 'private-release-values',
        manifest: 'private-release-manifest',
      }
    } else if (path === '/api/v1/helm-releases') {
      data = [{
        name: 'gateway', namespace: 'payments', revision: 4, status: 'deployed', chart: 'gateway-1.4.0',
        app_version: '1.4.0', updated_at: '2026-07-30T09:04:00Z',
      }]
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockEventResources(page: Page, requestedEvents: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'event-admin', role: 'admin', expires_at: '2026-07-29T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-28T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/events') {
      const namespace = url.searchParams.get('namespace') ?? 'all'
      const eventType = url.searchParams.get('type') ?? 'all'
      const limit = url.searchParams.get('limit') ?? 'missing'
      requestedEvents.push(`${namespace}:${eventType}:${limit}`)
      const events = [{
        namespace: 'payments', name: 'gateway-warning', type: 'Warning', reason: 'BackOff',
        message: 'Back-off restarting container', message_truncated: false,
        source: 'kubelet', object_kind: 'Pod', object_name: 'gateway-0', count: 3,
        first_seen: '2026-07-28T07:50:00Z', last_seen: '2026-07-28T08:03:00Z', created_at: '2026-07-28T07:50:00Z',
      }]
      if (eventType !== 'Warning') {
        events.push({
          namespace: 'payments', name: 'gateway-scheduled', type: 'Normal', reason: 'Scheduled',
          message: 'Successfully assigned pod', message_truncated: false,
          source: 'default-scheduler', object_kind: 'Pod', object_name: 'gateway-0', count: 1,
          first_seen: '2026-07-28T08:04:00Z', last_seen: '2026-07-28T08:04:00Z', created_at: '2026-07-28T08:04:00Z',
        })
      }
      data = events
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockAccessResources(page: Page, requestedKinds: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown
    if (path === '/api/v1/session') {
      data = { username: 'access-admin', role: 'admin', expires_at: '2026-07-29T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-28T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/access-resources') {
      const kind = url.searchParams.get('kind') ?? 'missing'
      const namespace = url.searchParams.get('namespace') ?? 'all'
      requestedKinds.push(`${kind}:${namespace}`)
      if (kind === 'clusterroles') {
        data = [{ kind: 'ClusterRole', name: 'view', created_at: '2026-07-24T08:00:00Z' }]
      } else if (kind === 'roles') {
        data = [{ kind: 'Role', namespace: 'payments', name: 'gateway-reader', created_at: '2026-07-24T08:00:00Z' }]
      } else if (kind === 'serviceaccounts') {
        data = [{ kind: 'ServiceAccount', namespace: 'payments', name: 'gateway', created_at: '2026-07-24T08:00:00Z' }]
      } else {
        data = []
      }
    } else if (path === '/api/v1/clusters/clu_1/access-resources/clusterroles/view') {
      requestedKinds.push('detail:clusterroles:view:all')
      data = {
        kind: 'ClusterRole', name: 'view', created_at: '2026-07-24T08:00:00Z',
        rules: [{
          api_groups: ['', 'apps'], resources: ['pods', 'deployments'], resource_names: [],
          verbs: ['get', 'list'], non_resource_urls: [],
        }],
        rule_count: 1, rules_truncated: false, subjects: [], subject_count: 0, subjects_truncated: false,
        secret_count: 0, image_pull_secret_count: 0,
      }
    } else if (path === '/api/v1/clusters/clu_1/access-resources/serviceaccounts/gateway') {
      requestedKinds.push('detail:serviceaccounts:gateway:payments')
      data = {
        kind: 'ServiceAccount', namespace: 'payments', name: 'gateway', created_at: '2026-07-24T08:00:00Z',
        rules: [], rule_count: 0, rules_truncated: false,
        subjects: [], subject_count: 0, subjects_truncated: false,
        automount_service_account_token: true, secret_count: 0, image_pull_secret_count: 1,
      }
    } else if (path === '/api/v1/clusters/clu_1/service-account-access-reviews' && route.request().method() === 'POST') {
      const input = route.request().postDataJSON() as {
        service_account: { namespace: string; name: string }
        resource_attributes: Record<string, string>
      }
      requestedKinds.push(`review:${input.service_account.namespace}:${input.service_account.name}`)
      data = { ...input, state: 'allowed', checked_at: '2026-07-28T08:30:00Z' }
    } else {
      data = []
    }
    await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data }) })
  })
}

async function mockWorkloadDiagnostics(page: Page) {
  let operationKind = 'workload.scale'
  let operationID = 'op_scale'
  let operationSummary = 'replicas=5, resource_version=73'
  let operationState = 'queued'
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown

    if (path === '/api/v1/session') {
      data = { username: 'diagnostics-admin', role: 'admin', expires_at: '2026-07-25T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-24T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      data = [{ name: 'payments', status: 'Active', created_at: '2026-07-24T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/workloads') {
      const workloads = [
        {
          kind: 'Pod', namespace: 'payments', name: 'gateway-0', ready: 1, desired: 1, status: 'Ready',
          images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        },
        {
          kind: 'Deployment', namespace: 'payments', name: 'gateway-api', ready: 3, desired: 3, status: 'Ready',
          images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        },
        {
          kind: 'Job', namespace: 'payments', name: 'daily-settlement', ready: 2, desired: 4, status: 'Running',
          images: ['registry.example.com/payments/settlement:1.8.0'], created_at: '2026-07-28T01:00:00Z',
        },
        {
          kind: 'CronJob', namespace: 'payments', name: 'nightly-report', ready: 0, desired: 0, status: 'Scheduled',
          images: ['registry.example.com/payments/report:2.3.0'], created_at: '2026-07-27T01:00:00Z',
        },
      ]
      const kind = url.searchParams.get('kind')
      data = kind ? workloads.filter((item) => item.kind.toLowerCase() === kind) : workloads
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api/scales') && route.request().method() === 'POST') {
      operationKind = 'workload.scale'
      operationID = 'op_scale'
      operationSummary = 'replicas=5, resource_version=73'
      operationState = 'queued'
      data = {
        id: 'op_scale', request_id: 'req_scale', kind: 'workload.scale', state: 'queued',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: 'replicas=5, resource_version=73', created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:05:00Z',
      }
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api/image-previews') && route.request().method() === 'POST') {
      data = {
        kind: 'Deployment', namespace: 'payments', name: 'gateway-api', container: 'app', resource_version: '73',
        changes: [{
          field: 'spec.template.spec.containers[name=app].image',
          before: 'registry.example.com/payments/gateway:1.4.0',
          after: 'registry.example.com/payments/gateway:1.5.0',
        }],
      }
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api/image-updates') && route.request().method() === 'POST') {
      operationKind = 'workload.image_update'
      operationID = 'op_image'
      operationSummary = 'container=app, fields=1, resource_version=73'
      operationState = 'queued'
      data = {
        id: operationID, request_id: 'req_image', kind: operationKind, state: 'queued',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: operationSummary, created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:05:00Z',
      }
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api/revisions')) {
      data = {
        namespace: 'payments', name: 'gateway-api', current_revision: 7,
        unassigned_replicaset_count: 1, truncated: false,
        revisions: [
          { revision: 7, replica_set: 'gateway-api-7f9d8', created_at: '2026-07-30T09:07:00Z', current: true },
          { revision: 6, replica_set: 'gateway-api-6c8b7', created_at: '2026-07-29T09:06:00Z', current: false },
        ],
      }
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api')) {
      data = {
        kind: 'Deployment', namespace: 'payments', name: 'gateway-api', ready: 3, desired: 3, status: 'Ready',
        images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        uid: 'uid-gateway-api', resource_version: '73', labels: { app: 'gateway' }, conditions: [],
        containers: [{
          name: 'app', image: 'registry.example.com/payments/gateway:1.4.0', type: 'container',
          ready: true, restart_count: 0, state: 'Running',
        }],
        yaml: 'apiVersion: apps/v1\nkind: Deployment\n',
      }
    } else if (path.endsWith('/workloads/cronjob/payments/nightly-report')) {
      data = {
        kind: 'CronJob', namespace: 'payments', name: 'nightly-report', ready: 0, desired: 0, status: 'Scheduled',
        images: ['registry.example.com/payments/report:2.3.0'], created_at: '2026-07-27T01:00:00Z',
        uid: 'uid-nightly-report', resource_version: '72', labels: { app: 'report' },
        containers: [{
          name: 'report', image: 'registry.example.com/payments/report:2.3.0', type: 'container',
          ready: false, restart_count: 0,
        }],
        conditions: [],
        yaml: 'apiVersion: batch/v1\nkind: CronJob\nspec:\n  schedule: 0 2 * * *\n  jobTemplate:\n    spec:\n      template:\n        spec:\n          containers:\n            - env:\n                - name: REPORT_TOKEN\n                  value: <redacted>\n',
      }
    } else if (path === `/api/v1/operations/${operationID}/cancellations` && route.request().method() === 'POST') {
      operationState = 'canceled'
      data = {
        id: operationID, request_id: operationKind === 'workload.image_update' ? 'req_image' : 'req_scale', kind: operationKind, state: operationState,
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: operationSummary, created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:06:00Z',
        finished_at: '2026-07-25T08:06:00Z',
      }
    } else if (path === '/api/v1/operations') {
      data = [{
        id: operationID, request_id: operationKind === 'workload.image_update' ? 'req_image' : 'req_scale', kind: operationKind, state: operationState,
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: operationSummary, created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:05:00Z',
      }]
    } else if (path === '/api/v1/system/resources') {
      data = {
        adaptive: true, pressure: 'constrained', memory_ratio: 0.84, load_ratio: 0.62,
        active_operations: 1, operation_limit: 1, maximum_operations: 2,
        queue_depth: operationState === 'queued' ? 1 : 0, queue_capacity: 64, sampled_at: '2026-07-25T08:05:00Z',
        kubernetes_reads: { adaptive: true, pressure: 'constrained', active: 2, limit: 2, maximum: 4 },
        kubernetes_clients: { entries: 3, capacity: 4, maximum: 8, building: 0 },
      }
    } else if (path.endsWith('/workloads/pod/payments/gateway-0/events')) {
      data = [{
        name: 'gateway-warning', type: 'Warning', reason: 'BackOff', message: 'Back-off restarting container',
        source: 'kubelet', count: 3, first_seen: '2026-07-25T07:50:00Z', last_seen: '2026-07-25T08:03:00Z',
      }]
    } else if (path.endsWith('/workloads/pod/payments/gateway-0')) {
      data = {
        kind: 'Pod', namespace: 'payments', name: 'gateway-0', ready: 1, desired: 1, status: 'Ready',
        images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        uid: 'uid-gateway-0', resource_version: '42', labels: { app: 'gateway', tier: 'api' },
        containers: [{
          name: 'app', image: 'registry.example.com/payments/gateway:1.4.0', type: 'container',
          ready: true, restart_count: 2, state: 'Running',
        }],
        conditions: [{
          type: 'Ready', status: 'True', reason: 'ContainersReady', message: 'All containers are ready',
          last_transition_time: '2026-07-25T08:01:00Z',
        }],
        yaml: 'apiVersion: v1\nkind: Pod\nmetadata:\n  name: gateway-0\nspec:\n  containers:\n    - env:\n        - name: API_TOKEN\n          value: <redacted>\n',
      }
    } else if (path.endsWith('/pods/payments/gateway-0/logs')) {
      data = {
        namespace: 'payments', pod: 'gateway-0', container: 'app', tail_lines: 200,
        previous: false, timestamps: true, truncated: false,
        content: '2026-07-25T08:04:00Z gateway ready\n',
      }
    } else {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'not_found', message: `No mock for ${path}` } }),
      })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data }),
    })
  })
}

async function mockClusterResources(page: Page, requestedResources: string[]) {
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    let data: unknown

    if (path === '/api/v1/session') {
      data = { username: 'resources-admin', role: 'admin', expires_at: '2026-07-25T16:00:00Z' }
    } else if (path === '/api/v1/clusters') {
      data = [{
        id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
        status: 'connected', version: 'v1.36.2', credentials_configured: true,
        created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/nodes') {
      requestedResources.push('nodes')
      data = [{
        name: 'control-01.example.internal', status: 'Ready', roles: ['control-plane'], version: 'v1.36.2',
        internal_ip: '10.0.0.11', os_image: 'Ubuntu 24.04.2 LTS', architecture: 'amd64',
        capacity: { cpu: '4', memory: '16Gi', pods: '110', ephemeral_storage: '100Gi' },
        allocatable: { cpu: '3500m', memory: '15Gi', pods: '100', ephemeral_storage: '90Gi' },
        unschedulable: true, taint_count: 1, created_at: '2026-07-20T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/nodes/control-01.example.internal/events') {
      data = [{
        name: 'node-warning', type: 'Warning', reason: 'NodeNotReady', message: 'Node is not ready',
        source: 'node-controller', count: 2, last_seen: '2026-07-25T08:03:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/nodes/control-01.example.internal') {
      data = {
        name: 'control-01.example.internal', status: 'Ready', roles: ['control-plane'], version: 'v1.36.2',
        internal_ip: '10.0.0.11', os_image: 'Ubuntu 24.04.2 LTS', architecture: 'amd64',
        capacity: { cpu: '4', memory: '16Gi', pods: '110', ephemeral_storage: '100Gi' },
        allocatable: { cpu: '3500m', memory: '15Gi', pods: '100', ephemeral_storage: '90Gi' },
        unschedulable: true, taint_count: 1, created_at: '2026-07-20T08:00:00Z',
        uid: 'uid-control-01', resource_version: '91',
        labels: { 'node-role.kubernetes.io/control-plane': '', 'topology.kubernetes.io/zone': 'cn-east-1a' },
        taints: [{ key: 'node-role.kubernetes.io/control-plane', effect: 'NoSchedule' }],
        addresses: [{ type: 'InternalIP', address: '10.0.0.11' }, { type: 'Hostname', address: 'control-01.example.internal' }],
        conditions: [{
          type: 'Ready', status: 'True', reason: 'KubeletReady', message: 'kubelet is ready',
          last_transition_time: '2026-07-25T08:00:00Z',
        }],
        system_info: {
          os_image: 'Ubuntu 24.04.2 LTS', kernel_version: '6.8.0', container_runtime_version: 'containerd://2.1.4',
          kubelet_version: 'v1.36.2', operating_system: 'linux', architecture: 'amd64',
        },
      }
    } else if (path === '/api/v1/clusters/clu_1/namespaces') {
      requestedResources.push('namespaces')
      data = [{
        name: 'payments', status: 'Active', labels: { team: 'payments' }, finalizers: ['kubernetes'],
        created_at: '2026-07-20T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/custom-resource-definitions') {
      requestedResources.push('crds')
      data = [{
        name: 'widgets.platform.example.com', resource: 'widgets', group: 'platform.example.com',
        created_at: '2026-07-26T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/custom-resource-definitions/widgets.platform.example.com') {
      requestedResources.push('crd-detail')
      data = {
        name: 'widgets.platform.example.com', resource: 'widgets', group: 'platform.example.com',
        created_at: '2026-07-26T08:00:00Z', scope: 'Namespaced', singular: 'widget', kind: 'Widget',
        list_kind: 'WidgetList', short_names: ['wdg'], short_name_count: 1, short_names_truncated: false,
        categories: ['all'], category_count: 1, categories_truncated: false,
        versions: [
          { name: 'v1', served: true, storage: true, deprecated: false },
          { name: 'v1beta1', served: false, storage: false, deprecated: true },
        ],
        version_count: 2, versions_truncated: false,
        stored_versions: ['v1'], stored_version_count: 1, stored_versions_truncated: false,
        conversion_strategy: 'None', conversion_strategy_defaulted: true,
        generation: 7, observed_generation: 7,
        conditions: [{
          type: 'Established', status: 'True', reason: 'InitialNamesAccepted', observed_generation: 7,
          last_transition_time: '2026-07-26T08:01:00Z',
        }],
        condition_count: 1, conditions_truncated: false,
      }
    } else if (path === '/api/v1/clusters/clu_1/certificate-signing-requests') {
      requestedResources.push('csrs')
      data = [{ name: 'worker-01', created_at: '2026-07-30T09:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/certificate-signing-requests/worker-01') {
      requestedResources.push('csr-detail')
      data = {
        name: 'worker-01', created_at: '2026-07-30T09:00:00Z',
        requester: 'system:node:worker-01', signer_name: 'example.com/node-client',
        requested_expiration_seconds: 86400, usages: ['client auth', 'digital signature'],
        state: 'approved', certificate_issued: false,
        conditions: [{
          type: 'Approved', status: 'True', reason: 'AutoApproved',
          last_update_time: '2026-07-30T09:01:00Z', last_transition_time: '2026-07-30T09:00:30Z',
          message: 'private approval message',
        }],
        condition_count: 1, request: 'private-pkcs10', certificate: 'private-certificate',
        uid: 'private-uid', groups: ['private-group'], extra: { private: ['private-value'] },
      }
    } else if (path === '/api/v1/clusters/clu_1/api-services') {
      requestedResources.push('api-services')
      data = [{
        name: 'v1beta1.metrics.k8s.io', group: 'metrics.k8s.io', version: 'v1beta1', local: false,
        service_namespace: 'kube-system', service_name: 'metrics-server', service_port: 443,
        service_port_defaulted: true, availability_observed: true, availability_status: 'False',
        availability_reason: 'FailedDiscoveryCheck', availability_transition_time: '2026-07-26T08:02:00Z',
        condition_count: 1, insecure_skip_tls_verify: true, group_priority_minimum: 100,
        version_priority: 100, created_at: '2026-07-26T08:00:00Z',
      }]
    } else if (path === '/api/v1/clusters/clu_1/validating-admission-policies/replica-policy.example.com') {
      requestedResources.push('admission-policy-detail')
      data = {
        kind: 'policy', name: 'replica-policy.example.com', generation: 4,
        failure_policy: 'Ignore', failure_policy_defaulted: false,
        param_kind_configured: true, param_api_version: 'rules.example.com/v1', param_kind: 'ReplicaLimit',
        match: {
          configured: true, match_policy: 'Exact', match_policy_defaulted: false,
          resource_rule_count: 1, exclude_resource_rule_count: 1,
          operation_count: 3, api_group_count: 2, api_version_count: 2, resource_count: 3,
          namespace_selector_label_count: 1, namespace_selector_expression_count: 1,
          object_selector_label_count: 1, object_selector_expression_count: 1,
        },
        validation_count: 2, audit_annotation_count: 1, match_condition_count: 1, variable_count: 1,
        observed_generation: 4, type_checking_observed: true, expression_warning_count: 1, condition_count: 1,
        created_at: '2026-07-29T08:00:00Z', expression: 'private CEL expression', warning: 'private warning',
      }
    } else if (path === '/api/v1/clusters/clu_1/validating-admission-policies') {
      requestedResources.push('admission-policies')
      data = [{ kind: 'policy', name: 'replica-policy.example.com', created_at: '2026-07-29T08:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/validating-admission-policy-bindings/replica-binding.example.com') {
      requestedResources.push('admission-policy-binding-detail')
      data = {
        kind: 'binding', name: 'replica-binding.example.com', generation: 3,
        policy_name: 'replica-policy.example.com', validation_actions: ['Deny', 'Audit'],
        param_ref_configured: true, param_ref_mode: 'name', param_namespace: 'policy-system',
        parameter_not_found_action: 'Deny', param_selector_label_count: 0, param_selector_expression_count: 0,
        match: {
          configured: true, match_policy: 'Equivalent', match_policy_defaulted: true,
          resource_rule_count: 1, exclude_resource_rule_count: 0,
          operation_count: 1, api_group_count: 1, api_version_count: 1, resource_count: 1,
          namespace_selector_label_count: 0, namespace_selector_expression_count: 0,
          object_selector_label_count: 0, object_selector_expression_count: 0,
        },
        created_at: '2026-07-29T09:00:00Z', param_name: 'private-param-name', selector: 'private-selector',
      }
    } else if (path === '/api/v1/clusters/clu_1/validating-admission-policy-bindings') {
      requestedResources.push('admission-policy-bindings')
      data = [{ kind: 'binding', name: 'replica-binding.example.com', created_at: '2026-07-29T09:00:00Z' }]
    } else if (path === '/api/v1/clusters/clu_1/admission-webhook-configurations/policy.platform.example.com') {
      requestedResources.push('admission-detail')
      data = {
        kind: 'validating', name: 'policy.platform.example.com', webhook_count: 1, generation: 3,
        created_at: '2026-07-28T08:00:00Z',
        webhooks: [{
          name: 'validate.policy.platform.example.com', target_type: 'service',
          service_namespace: 'policy-system', service_name: 'policy-webhook', service_port: 443,
          service_port_defaulted: true, ca_bundle_configured: true,
          failure_policy: 'Fail', failure_policy_defaulted: true,
          match_policy: 'Equivalent', match_policy_defaulted: true, side_effects: 'None',
          timeout_seconds: 10, timeout_seconds_defaulted: true, admission_review_versions: ['v1'],
          rule_count: 1, operation_count: 2, api_group_count: 1, api_version_count: 1, resource_count: 2,
          namespace_selector_label_count: 1, namespace_selector_expression_count: 0,
          object_selector_label_count: 0, object_selector_expression_count: 0, match_condition_count: 1,
        }],
      }
    } else if (path === '/api/v1/clusters/clu_1/admission-webhook-configurations') {
      const kind = url.searchParams.get('kind')
      requestedResources.push(kind === 'mutating' ? 'admission-mutating' : 'admission-validating')
      data = [{
        kind, name: kind === 'mutating' ? 'mutate.platform.example.com' : 'policy.platform.example.com',
        created_at: '2026-07-28T08:00:00Z',
      }]
    } else {
      await route.fulfill({
        status: 404,
        contentType: 'application/json',
        body: JSON.stringify({ error: { code: 'not_found', message: `No mock for ${path}` } }),
      })
      return
    }

    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data }),
    })
  })
}
