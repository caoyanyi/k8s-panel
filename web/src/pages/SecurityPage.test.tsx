import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { SecurityPage } from './SecurityPage'

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

describe('SecurityPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads only the bounded Pod Security Admission posture and renders explicit states', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      requests.push(String(input))
      return Promise.resolve(dataResponse(postures))
    }))

    renderPage()

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(screen.getByText('显式配置')).toBeInTheDocument()
    expect(screen.getByText('restricted')).toBeInTheDocument()
    expect(screen.getByText('v1.30（固定）')).toBeInTheDocument()
    expect(screen.getByText('baseline')).toBeInTheDocument()
    expect(screen.getByText('latest（默认）')).toBeInTheDocument()
    expect(screen.getAllByText('继承集群默认值')).toHaveLength(6)
    expect(screen.getByText('存在无效标签')).toBeInTheDocument()
    expect(screen.getByText('配置无效')).toBeInTheDocument()
    expect(screen.getByText('继承默认值')).toBeInTheDocument()
    expect(requests).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])
    expect(requests.some((path) => path.endsWith('/namespaces') && !path.includes('pod-security-admission'))).toBe(false)
  })

  it('searches and paginates the projected namespace rows locally', async () => {
    const items = Array.from({ length: 101 }, (_, index) => ({
      ...postures[0],
      name: `namespace-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse(items)))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('namespace-000')).toBeInTheDocument()
    expect(screen.queryByText('namespace-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('namespace-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索 Pod 安全态势'), 'namespace-042')
    expect(await screen.findByText('namespace-042')).toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '资源清单分页' })).not.toBeInTheDocument()
  })

  it('does not request Kubernetes resources without a selected cluster', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    renderPage({ clusters: [], selectedClusterId: '' })

    expect(screen.getByText('尚未选择集群')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('aborts the active posture request when the page unmounts', async () => {
    let signal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      signal = init?.signal ?? null
      return new Promise<Response>((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
      })
    }))

    const rendered = renderPage()
    await waitFor(() => expect(signal).not.toBeNull())
    rendered.unmount()

    expect((signal as AbortSignal | null)?.aborted).toBe(true)
  })
})

const inheritedMode = { status: 'inherited' as const, version_defaulted: false }

const postures = [
  {
    name: 'payments',
    enforce: { status: 'configured' as const, level: 'restricted', version: 'v1.30', version_defaulted: false },
    audit: { status: 'configured' as const, level: 'baseline', version: 'latest', version_defaulted: true },
    warn: inheritedMode,
    invalid_mode_count: 0,
    created_at: '2026-07-20T08:00:00Z',
  },
  {
    name: 'legacy',
    enforce: { status: 'invalid' as const, version_defaulted: false },
    audit: inheritedMode,
    warn: inheritedMode,
    invalid_mode_count: 1,
    created_at: '2026-07-19T08:00:00Z',
  },
  {
    name: 'platform',
    enforce: inheritedMode,
    audit: inheritedMode,
    warn: inheritedMode,
    invalid_mode_count: 0,
    created_at: '2026-07-18T08:00:00Z',
  },
]

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(
    <PanelContext.Provider value={{ ...baseContext, ...overrides }}>
      <SecurityPage />
    </PanelContext.Provider>,
  )
}

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
