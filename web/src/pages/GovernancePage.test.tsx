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
      if (path.includes('/horizontal-pod-autoscalers')) {
        requested.push(path)
        return Promise.resolve(dataResponse([autoscaler]))
      }
      if (path.includes('/pod-disruption-budgets')) {
        requested.push(path)
        return Promise.resolve(dataResponse([disruptionBudget]))
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

    await user.click(screen.getByRole('button', { name: 'HPA' }))
    expect(await screen.findByText('gateway-autoscaler')).toBeInTheDocument()
    expect(screen.getByText('Deployment / gateway')).toBeInTheDocument()
    expect(screen.getByText('3 -> 5')).toBeInTheDocument()
    expect(screen.getByText('2 - 10')).toBeInTheDocument()
    expect(screen.getByText('1 / 2')).toBeInTheDocument()
    expect(screen.getByText('ScalingActive=True')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/horizontal-pod-autoscalers?namespace=payments')

    await user.click(screen.getByRole('button', { name: 'PDB' }))
    expect(await screen.findByText('gateway-budget')).toBeInTheDocument()
    expect(screen.getByText('带筛选条件')).toBeInTheDocument()
    expect(screen.getByText('75%')).toBeInTheDocument()
    expect(screen.getByText('3 / 3')).toBeInTheDocument()
    expect(screen.getByText('IfHealthyBudget（默认）')).toBeInTheDocument()
    expect(screen.getByText('DisruptionAllowed=True')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/pod-disruption-budgets?namespace=payments')
    expect(requested).toHaveLength(4)
  })

  it('requires an explicit namespace before reading policy objects', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      requested.push(path)
      return Promise.resolve(dataResponse(path.includes('/horizontal-pod-autoscalers') ? [autoscaler] : [quota]))
    }))
    const user = userEvent.setup()

    renderPage({ selectedNamespace: '' })

    expect(await screen.findByText('请选择命名空间')).toBeInTheDocument()
    expect(requested).toEqual([])
    await user.click(screen.getByRole('button', { name: 'HPA' }))
    expect(requested).toEqual([])
    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    expect(await screen.findByText('gateway-autoscaler')).toBeInTheDocument()
    expect(requested).toEqual(['/api/v1/clusters/clu_1/horizontal-pod-autoscalers?namespace=payments'])
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

  it('renders defaulted, stale, empty, and alternate availability states', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/horizontal-pod-autoscalers')) {
        return Promise.resolve(dataResponse([{
          ...autoscaler,
          target_api_version: undefined,
          min_replicas: 1,
          min_replicas_defaulted: true,
          observed: false,
          conditions: [],
          condition_count: 0,
          last_scale_time: undefined,
        }]))
      }
      if (path.includes('/pod-disruption-budgets')) {
        return Promise.resolve(dataResponse([
          {
            ...disruptionBudget,
            name: 'all-budget',
            selector_mode: 'all',
            selector_label_count: 0,
            selector_expression_count: 0,
            min_available: undefined,
            max_unavailable: '1',
            observed: false,
            unhealthy_pod_eviction_policy: 'AlwaysAllow',
            unhealthy_pod_eviction_policy_defaulted: false,
            conditions: [],
            condition_count: 0,
          },
          {
            ...disruptionBudget,
            name: 'none-budget',
            selector_mode: 'none',
            min_available: undefined,
            conditions: disruptionBudget.conditions,
            condition_count: 3,
            conditions_truncated: true,
          },
        ]))
      }
      return Promise.resolve(dataResponse([quota]))
    }))
    const user = userEvent.setup()

    renderPage()
    await screen.findAllByText('compute-quota')
    await user.click(screen.getByRole('button', { name: 'HPA' }))
    expect(await screen.findByText('1（默认） - 10')).toBeInTheDocument()
    expect(screen.getByText('待同步')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'PDB' }))
    expect(await screen.findByText('全部 Pod')).toBeInTheDocument()
    expect(screen.getByText('不匹配 Pod')).toBeInTheDocument()
    expect(screen.getByText('最多不可用')).toBeInTheDocument()
    expect(screen.getByText('AlwaysAllow')).toBeInTheDocument()
    expect(screen.getByText('+2')).toBeInTheDocument()
    expect(screen.getByRole('status')).toHaveTextContent('结果已按安全上限截断')
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

const autoscaler = {
  namespace: 'payments', name: 'gateway-autoscaler', target_api_version: 'apps/v1',
  target_kind: 'Deployment', target_name: 'gateway', min_replicas: 2, min_replicas_defaulted: false,
  max_replicas: 10, current_replicas: 3, desired_replicas: 5, metric_count: 2, current_metric_count: 1,
  observed: true, conditions: [{ type: 'ScalingActive', status: 'True', reason: 'ValidMetricFound' }],
  condition_count: 1, conditions_truncated: false, last_scale_time: '2026-07-28T03:05:00Z',
  created_at: '2026-07-28T03:00:00Z',
}

const disruptionBudget = {
  namespace: 'payments', name: 'gateway-budget', selector_mode: 'filtered', selector_label_count: 1,
  selector_expression_count: 1, min_available: '75%', current_healthy: 3, desired_healthy: 3,
  disruptions_allowed: 1, expected_pods: 4, observed: true,
  unhealthy_pod_eviction_policy: 'IfHealthyBudget', unhealthy_pod_eviction_policy_defaulted: true,
  conditions: [{ type: 'DisruptionAllowed', status: 'True', reason: 'SufficientPods' }],
  condition_count: 1, conditions_truncated: false, created_at: '2026-07-28T03:12:00Z',
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
