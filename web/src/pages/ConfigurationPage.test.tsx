import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { ConfigurationPage } from './ConfigurationPage'

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

describe('ConfigurationPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads ConfigMaps first and requires a namespace before requesting Secrets', async () => {
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/configmaps')) return Promise.resolve(dataResponse([configMap]))
      if (path.includes('/secrets')) return Promise.resolve(dataResponse([secret]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('app-settings')).toBeInTheDocument()
    expect(screen.getByText('3 项')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/secrets'))).toBe(false)

    await user.click(screen.getByRole('button', { name: 'Secret' }))
    expect(screen.getByText('请选择命名空间')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).includes('/secrets'))).toBe(false)

    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    expect(await screen.findByText('registry-secret')).toBeInTheDocument()
    expect(screen.getByText('kubernetes.io/dockerconfigjson')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/secrets?namespace=payments'))).toBe(true)
  })

  it('aborts a manual refresh when the active configuration kind changes', async () => {
    let configMapRequests = 0
    let refreshSignal: AbortSignal | null = null
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/secrets')) return Promise.resolve(dataResponse([secret]))
      if (path.includes('/configmaps')) {
        configMapRequests++
        if (configMapRequests === 1) return Promise.resolve(dataResponse([configMap]))
        refreshSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          refreshSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage({ selectedNamespace: 'payments' })
    expect(await screen.findByText('app-settings')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(configMapRequests).toBe(2))
    await user.click(screen.getByRole('button', { name: 'Secret' }))

    await waitFor(() => expect(refreshSignal?.aborted).toBe(true))
    expect(await screen.findByText('registry-secret')).toBeInTheDocument()
  })

  it('searches and paginates the active inventory locally', async () => {
    const configMaps = Array.from({ length: 101 }, (_, index) => ({
      ...configMap,
      name: `config-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : configMaps))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('config-000')).toBeInTheDocument()
    expect(screen.queryByText('config-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('config-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索配置资源'), 'missing')
    expect(screen.getByText('没有匹配的 ConfigMap')).toBeInTheDocument()
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

const configMap = {
  namespace: 'payments', name: 'app-settings', data_count: 3, created_at: '2026-07-24T08:00:00Z',
}

const secret = {
  namespace: 'payments', name: 'registry-secret', type: 'kubernetes.io/dockerconfigjson', data_count: 1,
  created_at: '2026-07-25T08:00:00Z',
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><ConfigurationPage /></ContextHarness>)
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
