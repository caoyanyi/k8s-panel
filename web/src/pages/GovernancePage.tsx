import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  KubernetesHorizontalPodAutoscaler,
  KubernetesLimitRange,
  KubernetesLimitRangeConstraint,
  KubernetesPodDisruptionBudget,
  KubernetesPolicyCondition,
  KubernetesQuotaResource,
  KubernetesResourceQuota,
  KubernetesSelectorMode,
  Namespace,
} from '../types'
import { formatDateTime } from '../utils'

type GovernanceView = 'quotas' | 'limits' | 'autoscalers' | 'budgets'
type GovernanceInventory =
  | { kind: 'quotas'; items: KubernetesResourceQuota[] }
  | { kind: 'limits'; items: KubernetesLimitRange[] }
  | { kind: 'autoscalers'; items: KubernetesHorizontalPodAutoscaler[] }
  | { kind: 'budgets'; items: KubernetesPodDisruptionBudget[] }

interface QuotaRow {
  quota: KubernetesResourceQuota
  resource: KubernetesQuotaResource
}

interface LimitRow {
  limitRange: KubernetesLimitRange
  constraint: KubernetesLimitRangeConstraint
}

const resourceLabels: Record<GovernanceView, string> = {
  quotas: 'ResourceQuota',
  limits: 'LimitRange',
  autoscalers: 'HPA',
  budgets: 'PDB',
}

export function GovernancePage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [view, setView] = useState<GovernanceView>('quotas')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaces = useResource(
    (signal) => selectedClusterId
      ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal)
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const inventory = useResource<GovernanceInventory>(async (signal) => {
    if (!selectedClusterId || !selectedNamespace) return emptyInventory(view)
    const query = new URLSearchParams({ namespace: selectedNamespace })
    switch (view) {
      case 'quotas':
        return {
          kind: 'quotas',
          items: await api.get<KubernetesResourceQuota[]>(
            `/api/v1/clusters/${selectedClusterId}/resource-quotas?${query}`,
            signal,
          ),
        }
      case 'limits':
        return {
          kind: 'limits',
          items: await api.get<KubernetesLimitRange[]>(
            `/api/v1/clusters/${selectedClusterId}/limit-ranges?${query}`,
            signal,
          ),
        }
      case 'autoscalers':
        return {
          kind: 'autoscalers',
          items: await api.get<KubernetesHorizontalPodAutoscaler[]>(
            `/api/v1/clusters/${selectedClusterId}/horizontal-pod-autoscalers?${query}`,
            signal,
          ),
        }
      case 'budgets':
        return {
          kind: 'budgets',
          items: await api.get<KubernetesPodDisruptionBudget[]>(
            `/api/v1/clusters/${selectedClusterId}/pod-disruption-budgets?${query}`,
            signal,
          ),
        }
    }
  }, [selectedClusterId, selectedNamespace, view])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const quotaItems = inventory.data?.kind === 'quotas' ? inventory.data.items : []
  const limitItems = inventory.data?.kind === 'limits' ? inventory.data.items : []
  const autoscalerItems = inventory.data?.kind === 'autoscalers' ? inventory.data.items : []
  const budgetItems = inventory.data?.kind === 'budgets' ? inventory.data.items : []
  const quotaRows = useMemo(() => flattenQuotas(quotaItems), [quotaItems])
  const limitRows = useMemo(() => flattenLimitRanges(limitItems), [limitItems])
  const normalizedSearch = search.trim().toLowerCase()
  const visibleQuotaRows = useMemo(() => quotaRows.filter(({ quota, resource }) => (
    !normalizedSearch || `${quota.name} ${resource.name} ${quota.scopes.join(' ')}`.toLowerCase().includes(normalizedSearch)
  )), [normalizedSearch, quotaRows])
  const visibleLimitRows = useMemo(() => limitRows.filter(({ limitRange, constraint }) => (
    !normalizedSearch || `${limitRange.name} ${constraint.type} ${constraint.resource}`.toLowerCase().includes(normalizedSearch)
  )), [limitRows, normalizedSearch])
  const visibleAutoscalers = useMemo(() => autoscalerItems.filter((item) => (
    !normalizedSearch || `${item.name} ${item.target_api_version ?? ''} ${item.target_kind} ${item.target_name} ${conditionSearchText(item.conditions)}`
      .toLowerCase().includes(normalizedSearch)
  )), [autoscalerItems, normalizedSearch])
  const visibleBudgets = useMemo(() => budgetItems.filter((item) => (
    !normalizedSearch || `${item.name} ${item.selector_mode} ${item.unhealthy_pod_eviction_policy} ${conditionSearchText(item.conditions)}`
      .toLowerCase().includes(normalizedSearch)
  )), [budgetItems, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, selectedNamespace, view])

  const activeItemCount = activeInventoryCount(inventory.data, view)
  const visibleRowCount = visibleInventoryCount(view, visibleQuotaRows, visibleLimitRows, visibleAutoscalers, visibleBudgets)
  const totalPages = Math.max(1, Math.ceil(visibleRowCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const namespaceMissing = !selectedNamespace
  const resourceLabel = resourceLabels[view]
  const truncated = inventoryIsTruncated(inventory.data)

  return (
    <div className="page">
      <PageHeader
        title="资源治理"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeItemCount} 个 ${resourceLabel}` : '选择一个集群'}
        actions={(
          <button
            type="button"
            className="button button-secondary"
            disabled={!selectedClusterId || namespaceMissing || inventory.loading}
            onClick={() => void inventory.refresh()}
          >
            <RefreshCw size={16} className={inventory.loading ? 'spin' : ''} /> 刷新
          </button>
        )}
      />
      {selectedCluster?.environment === 'production' && (
        <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>
      )}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control governance-kind-control" role="group" aria-label="资源治理类型">
            <button type="button" className={view === 'quotas' ? 'active' : ''} aria-pressed={view === 'quotas'} onClick={() => setView('quotas')}>ResourceQuota</button>
            <button type="button" className={view === 'limits' ? 'active' : ''} aria-pressed={view === 'limits'} onClick={() => setView('limits')}>LimitRange</button>
            <button type="button" className={view === 'autoscalers' ? 'active' : ''} aria-pressed={view === 'autoscalers'} onClick={() => setView('autoscalers')}>HPA</button>
            <button type="button" className={view === 'budgets' ? 'active' : ''} aria-pressed={view === 'budgets'} onClick={() => setView('budgets')}>PDB</button>
          </div>
          <section className="toolbar" aria-label="资源治理筛选">
            <div className="toolbar-field">
              <label htmlFor="governance-namespace">命名空间</label>
              <select
                id="governance-namespace"
                value={selectedNamespace}
                onChange={(event) => setSelectedNamespace(event.target.value)}
                disabled={namespaces.loading}
              >
                <option value="">请选择</option>
                {namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
              </select>
            </div>
            <div className="search-field">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="governance-search">搜索资源治理策略</label>
              <input
                id="governance-search"
                type="search"
                placeholder={`搜索 ${resourceLabel} 或资源`}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          {truncated && <div className="inventory-alert" role="status">结果已按安全上限截断</div>}
          <section className="section-block table-section">
            {namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} />
              : namespaceMissing ? <EmptyState title="请选择命名空间" />
                : inventory.loading ? <LoadingState label={`正在读取 ${resourceLabel}`} />
                  : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} />
                    : visibleRowCount === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前命名空间没有 ${resourceLabel}`} />
                      : view === 'quotas' ? (
                        <>
                          <QuotaTable rows={visibleQuotaRows.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleQuotaRows.length} onPage={setPage} />
                        </>
                      ) : view === 'limits' ? (
                        <>
                          <LimitRangeTable rows={visibleLimitRows.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleLimitRows.length} onPage={setPage} />
                        </>
                      ) : view === 'autoscalers' ? (
                        <>
                          <AutoscalerTable items={visibleAutoscalers.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleAutoscalers.length} onPage={setPage} />
                        </>
                      ) : (
                        <>
                          <DisruptionBudgetTable items={visibleBudgets.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleBudgets.length} onPage={setPage} />
                        </>
                      )}
          </section>
        </>
      )}
    </div>
  )
}

function emptyInventory(view: GovernanceView): GovernanceInventory {
  switch (view) {
    case 'quotas': return { kind: 'quotas', items: [] }
    case 'limits': return { kind: 'limits', items: [] }
    case 'autoscalers': return { kind: 'autoscalers', items: [] }
    case 'budgets': return { kind: 'budgets', items: [] }
  }
}

function activeInventoryCount(inventory: GovernanceInventory | null, view: GovernanceView): number {
  return inventory?.kind === view ? inventory.items.length : 0
}

function inventoryIsTruncated(inventory: GovernanceInventory | null): boolean {
  if (!inventory) return false
  switch (inventory.kind) {
    case 'quotas': return inventory.items.some((item) => item.resources_truncated || item.scopes_truncated)
    case 'limits': return inventory.items.some((item) => item.constraints_truncated)
    case 'autoscalers': return inventory.items.some((item) => item.conditions_truncated)
    case 'budgets': return inventory.items.some((item) => item.conditions_truncated)
  }
}

function visibleInventoryCount(
  view: GovernanceView,
  quotas: QuotaRow[],
  limits: LimitRow[],
  autoscalers: KubernetesHorizontalPodAutoscaler[],
  budgets: KubernetesPodDisruptionBudget[],
): number {
  switch (view) {
    case 'quotas': return quotas.length
    case 'limits': return limits.length
    case 'autoscalers': return autoscalers.length
    case 'budgets': return budgets.length
  }
}

function flattenQuotas(items: KubernetesResourceQuota[]): QuotaRow[] {
  return items.flatMap((quota) => quota.resources.map((resource) => ({ quota, resource })))
}

function flattenLimitRanges(items: KubernetesLimitRange[]): LimitRow[] {
  return items.flatMap((limitRange) => limitRange.constraints.map((constraint) => ({ limitRange, constraint })))
}

function QuotaTable({ rows }: { rows: QuotaRow[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="ResourceQuota 清单" tabIndex={0}>
      <table className="governance-table governance-quota-table">
        <thead><tr><th>策略</th><th>资源</th><th>已用 / 上限</th><th>状态</th><th>Scope</th><th>选择条件</th><th>创建时间</th></tr></thead>
        <tbody>{rows.map(({ quota, resource }) => (
          <tr key={`${quota.namespace}:${quota.name}:${resource.name}`}>
            <td><strong>{quota.name}</strong></td>
            <td className="mono clipped-cell" title={resource.name}>{resource.name}</td>
            <td className="mono">{displayValue(resource.used)} / {displayValue(resource.hard)}</td>
            <td>{resource.observed ? <span className="replica-ready">已同步</span> : <span className="replica-warning">待同步</span>}</td>
            <td>{quota.scopes.length ? <div className="inline-labels">{quota.scopes.map((scope, index) => <span className="kind-label" key={`${scope}:${index}`}>{scope}</span>)}</div> : '-'}</td>
            <td>{quota.scope_selector_count ? `${quota.scope_selector_count} 条` : '-'}</td>
            <td>{formatDateTime(quota.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function LimitRangeTable({ rows }: { rows: LimitRow[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="LimitRange 清单" tabIndex={0}>
      <table className="governance-table governance-limit-table">
        <thead><tr><th>策略</th><th>对象类型</th><th>资源</th><th>默认请求</th><th>默认限制</th><th>最小值</th><th>最大值</th><th>最大比例</th><th>创建时间</th></tr></thead>
        <tbody>{rows.map(({ limitRange, constraint }, index) => (
          <tr key={`${limitRange.namespace}:${limitRange.name}:${constraint.type}:${constraint.resource}:${index}`}>
            <td><strong>{limitRange.name}</strong></td>
            <td><span className="kind-label">{constraint.type}</span></td>
            <td className="mono clipped-cell" title={constraint.resource}>{constraint.resource}</td>
            <td className="mono">{displayValue(constraint.default_request)}</td>
            <td className="mono">{displayValue(constraint.default)}</td>
            <td className="mono">{displayValue(constraint.min)}</td>
            <td className="mono">{displayValue(constraint.max)}</td>
            <td className="mono">{displayValue(constraint.max_limit_request_ratio)}</td>
            <td>{formatDateTime(limitRange.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function AutoscalerTable({ items }: { items: KubernetesHorizontalPodAutoscaler[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="HPA 清单" tabIndex={0}>
      <table className="governance-table governance-hpa-table">
        <thead><tr><th>策略</th><th>目标</th><th>当前 -&gt; 期望</th><th>副本范围</th><th>当前 / 配置指标</th><th>状态</th><th>条件</th><th>最近伸缩</th><th>创建时间</th></tr></thead>
        <tbody>{items.map((item) => (
          <tr key={`${item.namespace}:${item.name}`}>
            <td><strong>{item.name}</strong></td>
            <td>
              <div className="primary-cell">
                <span>{item.target_kind} / {item.target_name}</span>
                {item.target_api_version && <small className="mono">{item.target_api_version}</small>}
              </div>
            </td>
            <td className="mono">{item.current_replicas} -&gt; {item.desired_replicas}</td>
            <td className="mono">{item.min_replicas}{item.min_replicas_defaulted ? '（默认）' : ''} - {item.max_replicas}</td>
            <td className="mono">{item.current_metric_count} / {item.metric_count}</td>
            <td>{item.observed ? <span className="replica-ready">已同步</span> : <span className="replica-warning">待同步</span>}</td>
            <td><ConditionLabels conditions={item.conditions} conditionCount={item.condition_count} /></td>
            <td>{formatDateTime(item.last_scale_time)}</td>
            <td>{formatDateTime(item.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function DisruptionBudgetTable({ items }: { items: KubernetesPodDisruptionBudget[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="PDB 清单" tabIndex={0}>
      <table className="governance-table governance-pdb-table">
        <thead><tr><th>策略</th><th>选择器</th><th>可用性约束</th><th>当前 / 期望健康</th><th>允许中断</th><th>预期 Pod</th><th>状态</th><th>异常 Pod 策略</th><th>条件</th><th>创建时间</th></tr></thead>
        <tbody>{items.map((item) => (
          <tr key={`${item.namespace}:${item.name}`}>
            <td><strong>{item.name}</strong></td>
            <td>
              <div className="primary-cell">
                <span>{selectorModeLabel(item.selector_mode)}</span>
                {item.selector_mode === 'filtered' && <small>{item.selector_label_count} 标签 · {item.selector_expression_count} 表达式</small>}
              </div>
            </td>
            <td><AvailabilityValue item={item} /></td>
            <td className="mono">{item.current_healthy} / {item.desired_healthy}</td>
            <td className="mono">{item.disruptions_allowed}</td>
            <td className="mono">{item.expected_pods}</td>
            <td>{item.observed ? <span className="replica-ready">已同步</span> : <span className="replica-warning">待同步</span>}</td>
            <td>{item.unhealthy_pod_eviction_policy}{item.unhealthy_pod_eviction_policy_defaulted ? '（默认）' : ''}</td>
            <td><ConditionLabels conditions={item.conditions} conditionCount={item.condition_count} /></td>
            <td>{formatDateTime(item.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function AvailabilityValue({ item }: { item: KubernetesPodDisruptionBudget }) {
  if (item.min_available) return <div className="primary-cell"><strong className="mono">{item.min_available}</strong><small>最少可用</small></div>
  if (item.max_unavailable) return <div className="primary-cell"><strong className="mono">{item.max_unavailable}</strong><small>最多不可用</small></div>
  return <>-</>
}

function ConditionLabels({ conditions, conditionCount }: { conditions: KubernetesPolicyCondition[]; conditionCount: number }) {
  if (!conditionCount) return <>-</>
  return (
    <div className="inline-labels governance-conditions">
      {conditions.map((condition, index) => (
        <span className="kind-label" title={condition.reason} key={`${condition.type}:${condition.status}:${index}`}>
          {condition.type}={condition.status}
        </span>
      ))}
      {conditionCount > conditions.length && <span className="detail-muted">+{conditionCount - conditions.length}</span>}
    </div>
  )
}

function selectorModeLabel(mode: KubernetesSelectorMode): string {
  switch (mode) {
    case 'none': return '不匹配 Pod'
    case 'all': return '全部 Pod'
    case 'filtered': return '带筛选条件'
  }
}

function conditionSearchText(conditions: KubernetesPolicyCondition[]): string {
  return conditions.map((condition) => `${condition.type} ${condition.status} ${condition.reason ?? ''}`).join(' ')
}

function displayValue(value?: string) {
  return value || '-'
}
