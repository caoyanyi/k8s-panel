import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { GovernancePage } from './GovernancePage'

const baseContext: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
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

describe('GovernancePage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads one namespace governance kind at a time', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/resource-quotas')) {
        requested.push(path)
        return Promise.resolve(dataResponse([quota]))
      }
      if (path.includes('/limit-ranges')) {
        requested.push(path)
        return Promise.resolve(dataResponse([limitRange]))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findAllByText('compute-quota')).toHaveLength(2)
    expect(screen.getByText('2 / 4')).toBeInTheDocument()
    expect(screen.getAllByText('已同步')).toHaveLength(2)
    expect(screen.getAllByText('NotTerminating')).toHaveLength(2)
    expect(requested).toEqual(['/api/v1/clusters/clu_1/resource-quotas?namespace=payments'])

    await user.click(screen.getByRole('button', { name: 'LimitRange' }))
    expect(await screen.findByText('namespace-defaults')).toBeInTheDocument()
    expect(screen.getByText('250m')).toBeInTheDocument()
    expect(screen.getByText('500m')).toBeInTheDocument()
    expect(screen.getByText('100m')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
    expect(screen.getByText('4')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/limit-ranges?namespace=payments')
    expect(requested).toHaveLength(2)
  })

  it('requires an explicit namespace before reading policy objects', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      requested.push(path)
      return Promise.resolve(dataResponse([quota]))
    }))
    const user = userEvent.setup()

    renderPage({ selectedNamespace: '' })

    expect(await screen.findByText('请选择命名空间')).toBeInTheDocument()
    expect(requested).toEqual([])
    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    expect(await screen.findAllByText('compute-quota')).toHaveLength(2)
    expect(requested).toEqual(['/api/v1/clusters/clu_1/resource-quotas?namespace=payments'])
  })

  it('searches and paginates projected quota rows locally', async () => {
    const quotas = Array.from({ length: 101 }, (_, index) => ({
      ...quota,
      name: `quota-${String(index).padStart(3, '0')}`,
      resources: [{ ...quota.resources[0], name: `requests.example.com/team-${String(index).padStart(3, '0')}` }],
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : quotas))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('quota-000')).toBeInTheDocument()
    expect(screen.queryByText('quota-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('quota-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索资源治理策略'), 'team-042')
    expect(await screen.findByText('quota-042')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '资源清单分页' })).not.toBeInTheDocument()
  })

  it('aborts the active policy request when the namespace changes', async () => {
    let firstSignal: AbortSignal | null = null
    let policyCalls = 0
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace, { ...namespace, name: 'billing' }]))
      policyCalls++
      if (policyCalls === 1) {
        firstSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          firstSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(dataResponse([{ ...quota, namespace: 'billing' }]))
    }))
    const user = userEvent.setup()

    renderPage()
    await waitFor(() => expect(policyCalls).toBe(1))
    await user.selectOptions(screen.getByLabelText('命名空间'), 'billing')

    await waitFor(() => expect(firstSignal?.aborted).toBe(true))
    expect(await screen.findAllByText('compute-quota')).toHaveLength(2)
    expect(policyCalls).toBe(2)
  })

  it('does not request Kubernetes resources without a selected cluster', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderPage({ clusters: [], selectedClusterId: '', selectedNamespace: '' })

    expect(screen.getByText('尚未选择集群')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

const namespace = {
  name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
}

const quota = {
  namespace: 'payments', name: 'compute-quota',
  scopes: ['NotTerminating'], scope_count: 1, scopes_truncated: false, scope_selector_count: 1,
  resources: [
    { name: 'requests.cpu', hard: '4', used: '2', observed: true },
    { name: 'requests.memory', hard: '8Gi', used: '6Gi', observed: true },
  ],
  resource_count: 2, resources_truncated: false, created_at: '2026-07-28T02:00:00Z',
}

const limitRange = {
  namespace: 'payments', name: 'namespace-defaults',
  constraints: [{
    type: 'Container', resource: 'cpu', default_request: '250m', default: '500m',
    min: '100m', max: '2', max_limit_request_ratio: '4',
  }],
  constraint_count: 1, constraints_truncated: false, created_at: '2026-07-28T02:05:00Z',
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><GovernancePage /></ContextHarness>)
}

function ContextHarness({ overrides, children }: { overrides: Partial<PanelContextValue>; children: ReactNode }) {
  const [selectedNamespace, setSelectedNamespace] = useState(overrides.selectedNamespace ?? baseContext.selectedNamespace)
  return (
    <PanelContext.Provider value={{ ...baseContext, ...overrides, selectedNamespace, setSelectedNamespace }}>
      {children}
    </PanelContext.Provider>
  )
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
