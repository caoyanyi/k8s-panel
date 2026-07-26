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

test('production deployment scale enters the adaptive operation queue', async ({ page }, testInfo) => {
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
  await expect(page.getByText('队列 1 / 64')).toBeVisible()

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  await page.screenshot({ path: `test-results/${testInfo.project.name}-controlled-scale.png`, fullPage: true })
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

  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(1)
  const result = await new AxeBuilder({ page }).analyze()
  expect(result.violations.filter((violation) => ['serious', 'critical'].includes(violation.impact ?? ''))).toEqual([])
  await page.screenshot({ path: `test-results/${testInfo.project.name}-image-update.png`, fullPage: true })
  expect(consoleErrors).toEqual([])
})

test('cluster resources show node diagnostics and namespaces', async ({ page }, testInfo) => {
  const consoleErrors: string[] = []
  page.on('console', (message) => {
    if (message.type() === 'error') consoleErrors.push(message.text())
  })
  page.on('pageerror', (error) => consoleErrors.push(error.message))
  await mockClusterResources(page)

  await page.goto('/#resources')
  await expect(page.getByRole('heading', { name: '集群资源' })).toBeVisible()
  await expect(page.getByText('control-01.example.internal', { exact: true }).first()).toBeVisible()
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

async function mockWorkloadDiagnostics(page: Page) {
  let operationKind = 'workload.scale'
  let operationID = 'op_scale'
  let operationSummary = 'replicas=5, resource_version=73'
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
      data = [
        {
          kind: 'Pod', namespace: 'payments', name: 'gateway-0', ready: 1, desired: 1, status: 'Ready',
          images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        },
        {
          kind: 'Deployment', namespace: 'payments', name: 'gateway-api', ready: 3, desired: 3, status: 'Ready',
          images: ['registry.example.com/payments/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
        },
      ]
    } else if (path.endsWith('/workloads/deployment/payments/gateway-api/scales') && route.request().method() === 'POST') {
      operationKind = 'workload.scale'
      operationID = 'op_scale'
      operationSummary = 'replicas=5, resource_version=73'
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
      data = {
        id: operationID, request_id: 'req_image', kind: operationKind, state: 'queued',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: operationSummary, created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:05:00Z',
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
    } else if (path === '/api/v1/operations') {
      data = [{
        id: operationID, request_id: operationKind === 'workload.image_update' ? 'req_image' : 'req_scale', kind: operationKind, state: 'queued',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway-api', submitted_by: 'diagnostics-admin',
        summary: operationSummary, created_at: '2026-07-25T08:05:00Z', updated_at: '2026-07-25T08:05:00Z',
      }]
    } else if (path === '/api/v1/system/resources') {
      data = {
        adaptive: true, pressure: 'constrained', memory_ratio: 0.84, load_ratio: 0.62,
        active_operations: 1, operation_limit: 1, maximum_operations: 2,
        queue_depth: 1, queue_capacity: 64, sampled_at: '2026-07-25T08:05:00Z',
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

async function mockClusterResources(page: Page) {
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
      data = [{
        name: 'payments', status: 'Active', labels: { team: 'payments' }, finalizers: ['kubernetes'],
        created_at: '2026-07-20T08:00:00Z',
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
