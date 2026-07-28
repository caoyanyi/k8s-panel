import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { AccessPage } from './AccessPage'

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

describe('AccessPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads one access kind at a time and requires namespace scope for namespaced kinds', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/access-resources')) {
        requested.push(path)
        const query = new URL(path, 'https://panel.example').searchParams
        if (query.get('kind') === 'clusterroles') return Promise.resolve(dataResponse([clusterRole]))
        if (query.get('kind') === 'roles') return Promise.resolve(dataResponse([role]))
        if (query.get('kind') === 'clusterrolebindings') return Promise.resolve(dataResponse([clusterRoleBinding]))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('view', { exact: true })).toBeInTheDocument()
    expect(requested).toEqual(['/api/v1/clusters/clu_1/access-resources?kind=clusterroles'])
    await user.click(screen.getByRole('button', { name: 'Role' }))
    expect(await screen.findByText('请选择命名空间', { selector: 'strong' })).toBeInTheDocument()
    expect(requested).toHaveLength(1)

    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    expect(await screen.findByText('gateway-reader')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/access-resources?kind=roles&namespace=payments')

    await user.click(screen.getByRole('button', { name: 'ClusterRoleBinding' }))
    expect(await screen.findByText('all-readers')).toBeInTheDocument()
    expect(screen.getByLabelText('命名空间')).toBeDisabled()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/access-resources?kind=clusterrolebindings')
  })

  it('fetches a single bounded detail only after explicit inspection', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.endsWith('/access-resources?kind=clusterroles')) {
        requested.push(path)
        return Promise.resolve(dataResponse([clusterRole]))
      }
      if (path.endsWith('/access-resources/clusterroles/view')) {
        requested.push(path)
        return Promise.resolve(dataResponse(clusterRoleDetail))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('view', { exact: true })).toBeInTheDocument()
    expect(requested).toHaveLength(1)
    await user.click(screen.getByRole('button', { name: '查看 view' }))

    const dialog = await screen.findByRole('dialog', { name: 'ClusterRole · view' })
    expect(dialog).toHaveTextContent('pods, deployments')
    expect(dialog).toHaveTextContent('get, list')
    expect(dialog).toHaveTextContent('显示 1 / 1 条规则')
    expect(requested).toEqual([
      '/api/v1/clusters/clu_1/access-resources?kind=clusterroles',
      '/api/v1/clusters/clu_1/access-resources/clusterroles/view',
    ])
  })

  it('aborts an active list refresh when the access kind changes', async () => {
    let clusterRoleRequests = 0
    let refreshSignal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('kind=clusterrolebindings')) return Promise.resolve(dataResponse([clusterRoleBinding]))
      if (path.includes('kind=clusterroles')) {
        clusterRoleRequests++
        if (clusterRoleRequests === 1) return Promise.resolve(dataResponse([clusterRole]))
        refreshSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          refreshSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('view', { exact: true })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(clusterRoleRequests).toBe(2))
    await user.click(screen.getByRole('button', { name: 'ClusterRoleBinding' }))

    await waitFor(() => expect(refreshSignal?.aborted).toBe(true))
    expect(await screen.findByText('all-readers')).toBeInTheDocument()
  })

  it('aborts an active detail request when the dialog closes', async () => {
    let detailSignal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.endsWith('/access-resources?kind=clusterroles')) return Promise.resolve(dataResponse([clusterRole]))
      if (path.endsWith('/access-resources/clusterroles/view')) {
        detailSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          detailSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('view', { exact: true })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '查看 view' }))
    const dialog = await screen.findByRole('dialog', { name: 'ClusterRole · view' })
    await waitFor(() => expect(detailSignal).not.toBeNull())
    await user.click(within(dialog).getByRole('button', { name: '关闭' }))

    await waitFor(() => expect(detailSignal?.aborted).toBe(true))
    expect(screen.queryByRole('dialog', { name: 'ClusterRole · view' })).not.toBeInTheDocument()
  })

  it('searches and paginates the active inventory locally', async () => {
    const roles = Array.from({ length: 101 }, (_, index) => ({
      ...clusterRole,
      name: `role-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : roles))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('role-000')).toBeInTheDocument()
    expect(screen.queryByText('role-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('role-100')).toBeInTheDocument()
    await user.type(screen.getByLabelText('搜索访问控制资源'), 'missing')
    expect(screen.getByText('没有匹配的 ClusterRole')).toBeInTheDocument()
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
  name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
}

const clusterRole = {
  kind: 'ClusterRole', name: 'view', created_at: '2026-07-24T08:00:00Z',
}

const role = {
  kind: 'Role', namespace: 'payments', name: 'gateway-reader', created_at: '2026-07-24T08:00:00Z',
}

const clusterRoleBinding = {
  kind: 'ClusterRoleBinding', name: 'all-readers', created_at: '2026-07-25T08:00:00Z',
}

const clusterRoleDetail = {
  ...clusterRole,
  rules: [{
    api_groups: ['', 'apps'], resources: ['pods', 'deployments'], resource_names: [], verbs: ['get', 'list'], non_resource_urls: [],
  }],
  rule_count: 1,
  rules_truncated: false,
  subjects: [],
  subject_count: 0,
  subjects_truncated: false,
  secret_count: 0,
  image_pull_secret_count: 0,
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><AccessPage /></ContextHarness>)
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
