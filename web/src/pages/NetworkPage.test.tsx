import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { NetworkPage } from './NetworkPage'

const baseContext: PanelContextValue = {
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

describe('NetworkPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads only the active network kind and scopes it by namespace', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/services')) return Promise.resolve(dataResponse([service]))
      if (path.includes('/ingresses')) return Promise.resolve(dataResponse([ingress]))
      if (path.includes('/endpoint-slices')) return Promise.resolve(dataResponse([endpointSlice]))
      if (path.includes('/network-policies')) return Promise.resolve(dataResponse([networkPolicy]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('gateway-service')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/ingresses'))).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/endpoint-slices'))).toBe(false)
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/network-policies'))).toBe(false)
    expect(screen.getByText('10.96.0.20')).toBeInTheDocument()
    expect(screen.getAllByText('+1')).toHaveLength(2)

    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    expect(await screen.findByText('gateway-service')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/services?namespace=payments'))).toBe(true)
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/ingresses'))).toBe(false)

    await user.click(screen.getByRole('button', { name: 'Ingress' }))
    expect(await screen.findByText('gateway-ingress')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/ingresses?namespace=payments'))).toBe(true)
    expect(screen.getByText('gateway.example.com')).toBeInTheDocument()
    expect(screen.getByText('已启用')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'EndpointSlice' }))
    const endpointRow = await screen.findByRole('row', { name: /gateway-ipv4/ })
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/endpoint-slices?namespace=payments'))).toBe(true)
    expect(within(endpointRow).getByText('gateway-service')).toBeInTheDocument()
    expect(within(endpointRow).getByText('IPv4')).toBeInTheDocument()
    expect(within(endpointRow).getAllByText('2 / 3')).toHaveLength(2)
    expect(within(endpointRow).getByText('1 / 3')).toBeInTheDocument()
    expect(within(endpointRow).getAllByText('1 个按 API 默认')).toHaveLength(3)

    await user.click(screen.getByRole('button', { name: 'NetworkPolicy' }))
    expect(await screen.findByText('gateway-policy')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/network-policies?namespace=payments'))).toBe(true)
    expect(screen.getByText('带筛选条件')).toBeInTheDocument()
    expect(screen.getByText('本策略无出站规则')).toBeInTheDocument()
    expect(screen.getByText('按 API 默认')).toBeInTheDocument()
  })

  it('aborts a manual refresh when the active resource kind changes', async () => {
    let serviceRequests = 0
    let refreshSignal: AbortSignal | null = null
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/ingresses')) return Promise.resolve(dataResponse([ingress]))
      if (path.includes('/services')) {
        serviceRequests++
        if (serviceRequests === 1) return Promise.resolve(dataResponse([service]))
        refreshSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          refreshSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('gateway-service')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(serviceRequests).toBe(2))
    await user.click(screen.getByRole('button', { name: 'Ingress' }))

    await waitFor(() => expect(refreshSignal?.aborted).toBe(true))
    expect(await screen.findByText('gateway-ingress')).toBeInTheDocument()
  })

  it('searches and paginates the active inventory locally', async () => {
    const services = Array.from({ length: 101 }, (_, index) => ({
      ...service,
      name: `service-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : services))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('service-000')).toBeInTheDocument()
    expect(screen.queryByText('service-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('service-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索网络资源'), 'missing')
    expect(screen.getByText('没有匹配的 Service')).toBeInTheDocument()
  })

  it('does not request Kubernetes resources without a selected cluster', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    renderPage({ clusters: [], selectedClusterId: '' })

    expect(screen.getByText('尚未选择集群')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

const namespace = {
  name: 'payments', status: 'Active', labels: { team: 'payments' }, finalizers: ['kubernetes'],
  created_at: '2026-07-20T08:00:00Z',
}

const service = {
  namespace: 'payments', name: 'gateway-service', type: 'LoadBalancer', cluster_ip: '10.96.0.20',
  external_addresses: ['203.0.113.20', 'gateway-lb.example.com'], address_count: 3,
  ports: [
    { name: 'http', protocol: 'TCP', port: 80, target_port: '8080', node_port: 30080 },
    { name: 'metrics', protocol: 'TCP', port: 9090, target_port: 'metrics' },
  ],
  port_count: 3,
  created_at: '2026-07-24T08:00:00Z',
}

const ingress = {
  namespace: 'payments', name: 'gateway-ingress', class_name: 'nginx',
  hosts: ['gateway.example.com'], host_count: 1,
  addresses: ['203.0.113.30'], address_count: 1,
  tls: true, rule_count: 1, path_count: 2,
  created_at: '2026-07-24T08:00:00Z',
}

const endpointSlice = {
  namespace: 'payments', name: 'gateway-ipv4', service_name: 'gateway-service', address_type: 'IPv4',
  endpoint_count: 3,
  ready_endpoint_count: 2, ready_defaulted_count: 1,
  serving_endpoint_count: 2, serving_defaulted_count: 1,
  terminating_endpoint_count: 1, terminating_defaulted_count: 1,
  port_count: 1,
  created_at: '2026-07-28T05:00:00Z',
}

const networkPolicy = {
  namespace: 'payments', name: 'gateway-policy', pod_selector_mode: 'filtered',
  pod_selector_label_count: 1, pod_selector_expression_count: 1,
  policy_types: ['Ingress', 'Egress'], policy_types_defaulted: true,
  ingress_rule_count: 1, ingress_peer_count: 2, ingress_port_count: 1,
  egress_rule_count: 0, egress_peer_count: 0, egress_port_count: 0,
  created_at: '2026-07-28T04:00:00Z',
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><NetworkPage /></ContextHarness>)
}

function ContextHarness({ overrides, children }: { overrides: Partial<PanelContextValue>; children: ReactNode }) {
  const [selectedNamespace, setSelectedNamespace] = useState(overrides.selectedNamespace ?? baseContext.selectedNamespace)
  const value = {
    ...baseContext,
    ...overrides,
    selectedNamespace,
    setSelectedNamespace,
  }
  return <PanelContext.Provider value={value}>{children}</PanelContext.Provider>
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
