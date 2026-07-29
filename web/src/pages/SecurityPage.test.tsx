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

  it('loads bounded node version evidence only after the version view is selected', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      requests.push(path)
      if (path.endsWith('/upgrade-readiness/node-versions')) {
        return Promise.resolve(dataResponse(nodeVersionReport))
      }
      return Promise.resolve(dataResponse(postures))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(requests).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])

    await user.click(screen.getByRole('button', { name: '版本偏差' }))

    expect(await screen.findByText('v1.36.2')).toBeInTheDocument()
    expect(screen.getByText('worker-00')).toBeInTheDocument()
    expect(screen.getByText('同一次版本')).toBeInTheDocument()
    expect(screen.getByText('政策范围内')).toBeInTheDocument()
    expect(screen.getByText('升级前需处理')).toBeInTheDocument()
    expect(screen.getByText('超出偏差范围')).toBeInTheDocument()
    expect(screen.getByText('新于 API Server')).toBeInTheDocument()
    expect(screen.getByText('主版本不一致')).toBeInTheDocument()
    expect(screen.getByText('落后 3 个次版本')).toBeInTheDocument()
    expect(screen.getByText('4 个节点需处理')).toBeInTheDocument()
    expect(requests).toEqual([
      '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
      '/api/v1/clusters/clu_1/upgrade-readiness/node-versions',
    ])
    expect(requests.some((path) => path.endsWith('/nodes'))).toBe(false)

    await user.type(screen.getByLabelText('搜索节点版本偏差'), '主版本不一致')
    expect(screen.getByText('worker-major')).toBeInTheDocument()
    expect(screen.queryByText('worker-00')).not.toBeInTheDocument()
  })

  it('loads deprecated API evidence only after its view is selected and searches locally', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      requests.push(path)
      if (path.endsWith('/upgrade-readiness/deprecated-apis')) {
        return Promise.resolve(dataResponse(deprecatedAPIRequests))
      }
      return Promise.resolve(dataResponse(postures))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(requests).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])

    await user.click(screen.getByRole('button', { name: '废弃 API' }))

    expect(await screen.findByText('extensions/v1beta1')).toBeInTheDocument()
    expect(screen.getByText('ingresses')).toBeInTheDocument()
    expect(screen.getByText('deployments')).toBeInTheDocument()
    expect(screen.getByText('scale')).toBeInTheDocument()
    expect(screen.getByText('v1.22')).toBeInTheDocument()
    expect(screen.getByText('检测到 2 项废弃 API 请求证据')).toBeInTheDocument()
    expect(requests).toEqual([
      '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
      '/api/v1/clusters/clu_1/upgrade-readiness/deprecated-apis',
    ])
    expect(requests.some((path) => path.endsWith('/metrics') || path.endsWith('/nodes'))).toBe(false)

    await user.type(screen.getByLabelText('搜索废弃 API 请求证据'), 'apps/v1beta1')
    expect(screen.getByText('apps/v1beta1')).toBeInTheDocument()
    expect(screen.queryByText('extensions/v1beta1')).not.toBeInTheDocument()
  })

  it('scopes empty deprecated API evidence to the observed API server instance', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/upgrade-readiness/deprecated-apis') ? [] : postures))
    )))
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('payments')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '废弃 API' }))

    expect(await screen.findByText('当前 API Server 实例未报告废弃 API 请求证据')).toBeInTheDocument()
    expect(screen.queryByText('整个集群未使用废弃 API')).not.toBeInTheDocument()
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

const nodeVersionReport = {
  api_server_version: 'v1.36.2',
  nodes: [
    { name: 'worker-00', kubelet_version: 'v1.36.1', status: 'same-minor', minor_skew: 0, maximum_minor_skew: 3, minor_skew_comparable: true },
    { name: 'worker-01', kubelet_version: 'v1.35.4', status: 'within-policy', minor_skew: 1, maximum_minor_skew: 3, minor_skew_comparable: true },
    { name: 'worker-02', kubelet_version: 'v1.33.9', status: 'upgrade-blocking', minor_skew: 3, maximum_minor_skew: 3, minor_skew_comparable: true },
    { name: 'worker-03', kubelet_version: 'v1.32.8', status: 'outside-policy', minor_skew: 4, maximum_minor_skew: 3, minor_skew_comparable: true },
    { name: 'worker-04', kubelet_version: 'v1.37.0', status: 'newer-than-server', minor_skew: -1, maximum_minor_skew: 3, minor_skew_comparable: true },
    { name: 'worker-major', kubelet_version: 'v2.33.0', status: 'major-mismatch', minor_skew: 0, maximum_minor_skew: 0, minor_skew_comparable: false },
  ],
}

const deprecatedAPIRequests = [
  { group: 'extensions', version: 'v1beta1', resource: 'ingresses', subresource: '', removed_release: '1.22' },
  { group: 'apps', version: 'v1beta1', resource: 'deployments', subresource: 'scale', removed_release: '1.16' },
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
