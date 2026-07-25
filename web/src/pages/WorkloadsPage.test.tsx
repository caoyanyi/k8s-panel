import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { WorkloadsPage } from './WorkloadsPage'

const context: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'development', environment: 'development', server: 'https://api.example.com',
    status: 'connected', version: 'v1.36.2', credentials_configured: true,
    created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-24T08:00:00Z',
  }],
  clustersLoading: false,
  clustersError: null,
  selectedClusterId: 'clu_1',
  selectedNamespace: 'payments',
  setSelectedClusterId: vi.fn(),
  setSelectedNamespace: vi.fn(),
  refreshClusters: vi.fn().mockResolvedValue(undefined),
}

describe('WorkloadsPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('paginates large results and avoids a redundant summary request', async () => {
    const workloads = Array.from({ length: 101 }, (_, index) => ({
      kind: 'Deployment', namespace: 'payments', name: `gateway-${String(index).padStart(3, '0')}`,
      ready: 1, desired: 1, status: 'Ready', images: ['gateway:1.0.0'], created_at: '2026-07-24T08:00:00Z',
    }))
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([{
        name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
      }]))
      if (path.includes('/workloads?')) return Promise.resolve(dataResponse(workloads))
      return Promise.resolve(new Response('', { status: 404 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<PanelContext.Provider value={context}>
      <WorkloadsPage notify={vi.fn()} openOperations={vi.fn()} />
    </PanelContext.Provider>)

    expect(await screen.findByText('gateway-000')).toBeInTheDocument()
    expect(screen.queryByText('gateway-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/summary'))).toBe(false)
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('gateway-100')).toBeInTheDocument()
    expect(screen.queryByText('gateway-000')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '上一页' }))
    expect(await screen.findByText('gateway-000')).toBeInTheDocument()
  })

  it('filters, refreshes and opens a deployment detail without loading events', async () => {
    const workload = {
      kind: 'Deployment', namespace: 'payments', name: 'gateway-api', ready: 2, desired: 3,
      status: 'Progressing', images: ['registry.example.com/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([{
        name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
      }]))
      if (path.endsWith('/workloads/deployment/payments/gateway-api')) return Promise.resolve(dataResponse({
        ...workload, uid: 'uid-gateway-api', resource_version: '73', labels: {}, containers: [], conditions: [], yaml: 'kind: Deployment\n',
      }))
      if (path.includes('/workloads?')) return Promise.resolve(dataResponse([workload]))
      return Promise.resolve(new Response('', { status: 404 }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<PanelContext.Provider value={context}>
      <WorkloadsPage notify={vi.fn()} openOperations={vi.fn()} />
    </PanelContext.Provider>)

    expect(await screen.findByText('gateway-api')).toBeInTheDocument()
    await user.type(screen.getByLabelText('搜索工作负载'), 'missing')
    expect(await screen.findByText('当前范围没有工作负载')).toBeInTheDocument()
    await user.clear(screen.getByLabelText('搜索工作负载'))
    await user.selectOptions(screen.getByLabelText('类型'), 'deployment')
    expect(await screen.findByText('gateway-api')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('kind=deployment'))).toBe(true)
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await user.click(screen.getByRole('button', { name: '查看 gateway-api' }))
    expect(await screen.findByRole('dialog', { name: 'Deployment · gateway-api' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '扩缩容' })).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/events'))).toBe(false)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
