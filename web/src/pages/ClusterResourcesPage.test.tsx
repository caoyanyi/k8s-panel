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
