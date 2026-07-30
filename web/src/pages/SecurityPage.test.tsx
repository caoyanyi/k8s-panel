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

  it('loads only the verified endpoint certificate after its view is selected', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      requests.push(path)
      if (path.endsWith('/upgrade-readiness/endpoint-certificate')) {
        return Promise.resolve(dataResponse(endpointCertificate))
      }
      return Promise.resolve(dataResponse(postures))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(requests).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])

    await user.click(screen.getByRole('button', { name: 'TLS 证书' }))

    expect(await screen.findByText('30 天内到期')).toBeInTheDocument()
    expect(screen.getByText('当前连接端点')).toBeInTheDocument()
    expect(screen.getByText('TLS 握手叶证书')).toBeInTheDocument()
    expect(screen.getByText('2026-08-28 08:00 UTC')).toBeInTheDocument()
    expect(screen.getByText('30 天')).toBeInTheDocument()
    expect(screen.getByText('可能由负载均衡器或代理终止')).toBeInTheDocument()
    expect(screen.queryByRole('searchbox')).not.toBeInTheDocument()
    expect(screen.queryByRole('navigation', { name: '资源清单分页' })).not.toBeInTheDocument()
    expect(requests).toEqual([
      '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
      '/api/v1/clusters/clu_1/upgrade-readiness/endpoint-certificate',
    ])
    expect(requests.some((path) => path.endsWith('/metrics') || path.endsWith('/nodes') || path.endsWith('/api') || path.endsWith('/apis'))).toBe(false)
  })

  it('loads cluster disruption budget evidence only after its view is selected and searches locally', async () => {
    const requests: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      requests.push(path)
      if (path.endsWith('/upgrade-readiness/disruption-budgets')) {
        return Promise.resolve(dataResponse(disruptionBudgets))
      }
      return Promise.resolve(dataResponse(postures))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('payments')).toBeInTheDocument()
    expect(requests).toEqual(['/api/v1/clusters/clu_1/pod-security-admission/namespaces'])

    await user.click(screen.getByRole('button', { name: '中断预算' }))

    expect(await screen.findByText('gateway-budget')).toBeInTheDocument()
    expect(screen.getByText('允许中断')).toBeInTheDocument()
    expect(screen.getByText('当前受阻')).toBeInTheDocument()
    expect(screen.getByText('未匹配 Pod')).toBeInTheDocument()
    expect(screen.getByText('待同步')).toBeInTheDocument()
    expect(screen.getByText('1 项当前受阻证据')).toBeInTheDocument()
    expect(screen.getByText('当前 PDB 控制器状态')).toBeInTheDocument()
    expect(screen.getByText('不代表节点一定无法排空')).toBeInTheDocument()
    expect(requests).toEqual([
      '/api/v1/clusters/clu_1/pod-security-admission/namespaces',
      '/api/v1/clusters/clu_1/upgrade-readiness/disruption-budgets',
    ])
    expect(requests.some((path) => path.endsWith('/pods') || path.endsWith('/nodes') || path.includes('/eviction'))).toBe(false)

    await user.type(screen.getByLabelText('搜索中断预算证据'), 'platform inactive')
    expect(screen.getByText('idle-budget')).toBeInTheDocument()
    expect(screen.queryByText('gateway-budget')).not.toBeInTheDocument()
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

const endpointCertificate = {
  observed_at: '2026-07-29T08:00:00Z',
  not_before: '2026-06-29T08:00:00Z',
  not_after: '2026-08-28T08:00:00Z',
  remaining_seconds: 30 * 24 * 60 * 60,
  status: 'expiring' as const,
}

const disruptionBudgetBase = {
  selector_mode: 'filtered' as const,
  selector_label_count: 1,
  selector_expression_count: 0,
  min_available: '75%',
  current_healthy: 3,
  desired_healthy: 3,
  disruptions_allowed: 0,
  expected_pods: 4,
  observed: true,
  unhealthy_pod_eviction_policy: 'IfHealthyBudget',
  unhealthy_pod_eviction_policy_defaulted: true,
  conditions: [],
  condition_count: 0,
  conditions_truncated: false,
  created_at: '2026-07-30T08:00:00Z',
}

const disruptionBudgets = [
  { ...disruptionBudgetBase, namespace: 'payments', name: 'gateway-budget', disruption_status: 'blocked' as const },
  {
    ...disruptionBudgetBase,
    namespace: 'payments',
    name: 'worker-budget',
    disruptions_allowed: 1,
    disruption_status: 'available' as const,
  },
  {
    ...disruptionBudgetBase,
    namespace: 'platform',
    name: 'idle-budget',
    expected_pods: 0,
    disruption_status: 'inactive' as const,
  },
  {
    ...disruptionBudgetBase,
    namespace: 'legacy',
    name: 'stale-budget',
    observed: false,
    disruption_status: 'unobserved' as const,
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
