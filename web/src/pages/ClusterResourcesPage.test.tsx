import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { ClusterResourcesPage } from './ClusterResourcesPage'

const context: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
    status: 'connected', version: 'v1.36.2', credentials_configured: true,
    created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-24T08:00:00Z',
  }],
  clustersLoading: false,
  clustersError: null,
  selectedClusterId: 'clu_1',
  selectedNamespace: '',
  setSelectedClusterId: vi.fn(),
  setSelectedNamespace: vi.fn(),
  refreshClusters: vi.fn().mockResolvedValue(undefined),
}

describe('ClusterResourcesPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows node inventory, detail and events', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/nodes/control-01.example.internal/events?limit=50')) {
        return Promise.resolve(dataResponse([{
          name: 'node-warning', type: 'Warning', reason: 'NodeNotReady', message: 'Node is not ready',
          source: 'node-controller', count: 2, last_seen: '2026-07-24T08:03:00Z',
        }]))
      }
      if (path.endsWith('/nodes/control-01.example.internal')) {
        return Promise.resolve(dataResponse({
          ...node,
          uid: 'uid-control-01', resource_version: '91',
          labels: { 'node-role.kubernetes.io/control-plane': '', 'topology.kubernetes.io/zone': 'cn-east-1a' },
          taints: [{ key: 'node-role.kubernetes.io/control-plane', effect: 'NoSchedule' }],
          addresses: [{ type: 'InternalIP', address: '10.0.0.11' }, { type: 'Hostname', address: node.name }],
          conditions: [
            {
              type: 'Ready', status: 'True', reason: 'KubeletReady', message: 'kubelet is ready',
              last_heartbeat_time: '2026-07-24T08:02:00Z', last_transition_time: '2026-07-24T08:00:00Z',
            },
            {
              type: 'MemoryPressure', status: 'False', reason: 'KubeletHasSufficientMemory',
              last_transition_time: '2026-07-24T08:00:00Z',
            },
          ],
          system_info: {
            os_image: 'Ubuntu 24.04.2 LTS', kernel_version: '6.8.0', container_runtime_version: 'containerd://2.1.4',
            kubelet_version: 'v1.36.2', operating_system: 'linux', architecture: 'amd64',
          },
        }))
      }
      if (path.endsWith('/nodes')) return Promise.resolve(dataResponse([node]))
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage(context)

    expect(await screen.findByText(node.name)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/namespaces'))).toBe(false)
    expect(screen.getByText('3500m / 4')).toBeInTheDocument()
    expect(screen.getByText('已停止调度')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: `查看 ${node.name}` }))

    const dialog = await screen.findByRole('dialog', { name: `节点 · ${node.name}` })
    expect(within(dialog).getByText('uid-control-01')).toBeInTheDocument()
    expect(within(dialog).getByText('containerd://2.1.4')).toBeInTheDocument()
    expect(within(dialog).getByText('NoSchedule')).toBeInTheDocument()
    expect(within(dialog).getByText('MemoryPressure')).toBeInTheDocument()
    expect(within(dialog).getAllByText('正常').length).toBeGreaterThan(0)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/events?limit=50'))).toBe(false)
    await user.click(within(dialog).getByRole('tab', { name: '事件' }))
    expect(await within(dialog).findByText('NodeNotReady')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/nodes/control-01.example.internal/events?limit=50',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('switches to searchable namespace inventory', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/nodes') ? [node] : [namespace]))
    ))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage(context)
    await screen.findByText(node.name)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/namespaces'))).toBe(false)
    await user.click(screen.getByRole('button', { name: '命名空间' }))

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/namespaces'))).toBe(true)
    expect(screen.getByText('team=payments')).toBeInTheDocument()
    expect(screen.getByText('1 个终结器')).toBeInTheDocument()
    await user.type(screen.getByLabelText('搜索集群资源'), 'missing')
    expect(screen.getByText('没有匹配的命名空间')).toBeInTheDocument()
  })

  it('loads CRD metadata only on demand and fetches one bounded detail', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/custom-resource-definitions/widgets.platform.example.com')) {
        return Promise.resolve(dataResponse({
          ...crd,
          scope: 'Namespaced', singular: 'widget', kind: 'Widget', list_kind: 'WidgetList',
          short_names: ['wdg'], categories: ['all'], generation: 7, observed_generation: 7,
          conversion_strategy: 'Webhook', conversion_strategy_defaulted: false,
          versions: [
            { name: 'v1', served: true, storage: true, deprecated: false },
            { name: 'v1beta1', served: false, storage: false, deprecated: true },
          ],
          version_count: 2, versions_truncated: false,
          stored_versions: ['v1'], stored_version_count: 1, stored_versions_truncated: false,
          conditions: [{
            type: 'Established', status: 'True', reason: 'InitialNamesAccepted', observed_generation: 7,
            last_transition_time: '2026-07-26T08:01:00Z',
          }],
          condition_count: 1, conditions_truncated: false,
        }))
      }
      if (path.endsWith('/custom-resource-definitions')) return Promise.resolve(dataResponse([crd]))
      if (path.endsWith('/nodes')) return Promise.resolve(dataResponse([node]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage(context)
    await screen.findByText(node.name)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/custom-resource-definitions'))).toBe(false)
    await user.click(screen.getByRole('button', { name: 'CRD' }))

    expect(await screen.findByText('widgets')).toBeInTheDocument()
    expect(screen.getByText('platform.example.com')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/namespaces'))).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/widgets.platform.example.com'))).toBe(false)
    await user.click(screen.getByRole('button', { name: `查看 ${crd.name}` }))

    const dialog = await screen.findByRole('dialog', { name: `CRD · ${crd.name}` })
    expect(within(dialog).getByText('Namespaced')).toBeInTheDocument()
    expect(within(dialog).getByText('Widget')).toBeInTheDocument()
    expect(within(dialog).getByText('v1beta1')).toBeInTheDocument()
    expect(within(dialog).getByText('已弃用')).toBeInTheDocument()
    expect(within(dialog).getByText('Established')).toBeInTheDocument()
    expect(within(dialog).queryByText('schema-secret')).not.toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/custom-resource-definitions/widgets.platform.example.com',
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('loads aggregated API health only when selected', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api-services')) return Promise.resolve(dataResponse([apiService]))
      if (path.endsWith('/nodes')) return Promise.resolve(dataResponse([node]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage(context)
    await screen.findByText(node.name)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/api-services'))).toBe(false)
    await user.click(screen.getByRole('button', { name: '聚合 API' }))

    expect(await screen.findByText('metrics.k8s.io/v1beta1')).toBeInTheDocument()
    expect(screen.getByText('kube-system/metrics-server:443')).toBeInTheDocument()
    expect(screen.getByText('不可用')).toBeInTheDocument()
    expect(screen.getByText('FailedDiscoveryCheck')).toBeInTheDocument()
    expect(screen.getByText('跳过 TLS 校验')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/namespaces'))).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/custom-resource-definitions'))).toBe(false)
  })

  it('loads one admission webhook kind and one redacted detail on demand', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/admission-webhook-configurations/policy.platform.example.com?kind=validating')) {
        return Promise.resolve(dataResponse(admissionWebhookDetail))
      }
      if (path.endsWith('/admission-webhook-configurations?kind=validating')) {
        return Promise.resolve(dataResponse([admissionWebhook]))
      }
      if (path.endsWith('/admission-webhook-configurations?kind=mutating')) {
        return Promise.resolve(dataResponse([{ ...admissionWebhook, kind: 'mutating', name: 'mutate.platform.example.com' }]))
      }
      if (path.endsWith('/nodes')) return Promise.resolve(dataResponse([node]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage(context)
    await screen.findByText(node.name)
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/admission-webhook-configurations'))).toBe(false)
    await user.click(screen.getByRole('button', { name: '准入' }))

    expect(await screen.findByText(admissionWebhook.name)).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('?kind=mutating'))).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes(`${admissionWebhook.name}?`))).toBe(false)
    await user.click(screen.getByRole('button', { name: `查看 ${admissionWebhook.name}` }))

    const dialog = await screen.findByRole('dialog', { name: `准入 Webhook · ${admissionWebhook.name}` })
    expect(within(dialog).getByText('policy-system/policy-webhook:443（默认）')).toBeInTheDocument()
    expect(within(dialog).getByText('Fail（默认）')).toBeInTheDocument()
    expect(within(dialog).getByText('Equivalent（默认）')).toBeInTheDocument()
    expect(within(dialog).getByText('None')).toBeInTheDocument()
    expect(within(dialog).getByText('10 秒（默认）')).toBeInTheDocument()
    expect(within(dialog).getByText('已配置')).toBeInTheDocument()
    expect(within(dialog).getByText('v1')).toBeInTheDocument()
    expect(within(dialog).getByText('1 条规则 · 2 个操作 · 2 个资源')).toBeInTheDocument()
    expect(within(dialog).queryByText('private-webhook-path')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('private-ca-bundle')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('private CEL expression')).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Mutating' }))
    expect(await screen.findByText('mutate.platform.example.com')).toBeInTheDocument()
    expect(screen.queryByRole('dialog', { name: `准入 Webhook · ${admissionWebhook.name}` })).not.toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('?kind=mutating'))).toBe(true)
  })

  it('shows an empty state without a selected cluster', () => {
    vi.stubGlobal('fetch', vi.fn())
    renderPage({ ...context, clusters: [], selectedClusterId: '' })

    expect(screen.getByText('尚未选择集群')).toBeInTheDocument()
    expect(fetch).not.toHaveBeenCalled()
  })

  it('paginates large namespace inventories', async () => {
    const manyNamespaces = Array.from({ length: 101 }, (_, index) => ({
      ...namespace,
      name: `team-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/nodes') ? [node] : manyNamespaces))
    )))
    const user = userEvent.setup()

    renderPage(context)
    await screen.findByText(node.name)
    await user.click(screen.getByRole('button', { name: '命名空间' }))

    expect(await screen.findByText('team-000')).toBeInTheDocument()
    expect(screen.queryByText('team-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('team-100')).toBeInTheDocument()
    expect(screen.queryByText('team-000')).not.toBeInTheDocument()
  })
})

const node = {
  name: 'control-01.example.internal', status: 'Ready', roles: ['control-plane'], version: 'v1.36.2',
  internal_ip: '10.0.0.11', os_image: 'Ubuntu 24.04.2 LTS', architecture: 'amd64',
  capacity: { cpu: '4', memory: '16Gi', pods: '110', ephemeral_storage: '100Gi' },
  allocatable: { cpu: '3500m', memory: '15Gi', pods: '100', ephemeral_storage: '90Gi' },
  unschedulable: true, taint_count: 1, created_at: '2026-07-20T08:00:00Z',
}

const namespace = {
  name: 'payments', status: 'Active', labels: { team: 'payments' }, finalizers: ['kubernetes'],
  created_at: '2026-07-20T08:00:00Z',
}

const crd = {
  name: 'widgets.platform.example.com', resource: 'widgets', group: 'platform.example.com',
  created_at: '2026-07-26T08:00:00Z',
}

const apiService = {
  name: 'v1beta1.metrics.k8s.io', group: 'metrics.k8s.io', version: 'v1beta1', local: false,
  service_namespace: 'kube-system', service_name: 'metrics-server', service_port: 443,
  service_port_defaulted: true, availability_observed: true, availability_status: 'False',
  availability_reason: 'FailedDiscoveryCheck', availability_transition_time: '2026-07-26T08:02:00Z',
  condition_count: 1, insecure_skip_tls_verify: true, group_priority_minimum: 100,
  version_priority: 100, created_at: '2026-07-26T08:00:00Z',
}

const admissionWebhook = {
  kind: 'validating', name: 'policy.platform.example.com',
  created_at: '2026-07-28T08:00:00Z',
}

const admissionWebhookDetail = {
  ...admissionWebhook,
  generation: 3,
  webhooks: [{
    name: 'validate.policy.platform.example.com', target_type: 'service',
    service_namespace: 'policy-system', service_name: 'policy-webhook', service_port: 443,
    service_port_defaulted: true, ca_bundle_configured: true,
    failure_policy: 'Fail', failure_policy_defaulted: true,
    match_policy: 'Equivalent', match_policy_defaulted: true,
    side_effects: 'None', timeout_seconds: 10, timeout_seconds_defaulted: true,
    admission_review_versions: ['v1'], rule_count: 1, operation_count: 2,
    api_group_count: 1, api_version_count: 1, resource_count: 2,
    namespace_selector_label_count: 1, namespace_selector_expression_count: 0,
    object_selector_label_count: 0, object_selector_expression_count: 0,
    match_condition_count: 1,
  }],
}

function renderPage(value: PanelContextValue) {
  return render(<PanelContext.Provider value={value}><ClusterResourcesPage /></PanelContext.Provider>)
}

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function errorResponse(status: number, code: string): Response {
  return new Response(JSON.stringify({ error: { code, message: code } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
