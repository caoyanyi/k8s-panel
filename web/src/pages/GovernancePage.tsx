import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  KubernetesLimitRange,
  KubernetesLimitRangeConstraint,
  KubernetesQuotaResource,
  KubernetesResourceQuota,
  Namespace,
} from '../types'
import { formatDateTime } from '../utils'

type GovernanceView = 'quotas' | 'limits'
type GovernanceInventory =
  | { kind: 'quotas'; items: KubernetesResourceQuota[] }
  | { kind: 'limits'; items: KubernetesLimitRange[] }

interface QuotaRow {
  quota: KubernetesResourceQuota
  resource: KubernetesQuotaResource
}

interface LimitRow {
  limitRange: KubernetesLimitRange
  constraint: KubernetesLimitRangeConstraint
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
    if (view === 'quotas') {
      const items = await api.get<KubernetesResourceQuota[]>(
        `/api/v1/clusters/${selectedClusterId}/resource-quotas?${query}`,
        signal,
      )
      return { kind: 'quotas', items }
    }
    const items = await api.get<KubernetesLimitRange[]>(
      `/api/v1/clusters/${selectedClusterId}/limit-ranges?${query}`,
      signal,
    )
    return { kind: 'limits', items }
  }, [selectedClusterId, selectedNamespace, view])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const quotaItems = inventory.data?.kind === 'quotas' ? inventory.data.items : []
  const limitItems = inventory.data?.kind === 'limits' ? inventory.data.items : []
  const quotaRows = useMemo(() => flattenQuotas(quotaItems), [quotaItems])
  const limitRows = useMemo(() => flattenLimitRanges(limitItems), [limitItems])
  const normalizedSearch = search.trim().toLowerCase()
  const visibleQuotaRows = useMemo(() => quotaRows.filter(({ quota, resource }) => (
    !normalizedSearch || `${quota.name} ${resource.name} ${quota.scopes.join(' ')}`.toLowerCase().includes(normalizedSearch)
  )), [normalizedSearch, quotaRows])
  const visibleLimitRows = useMemo(() => limitRows.filter(({ limitRange, constraint }) => (
    !normalizedSearch || `${limitRange.name} ${constraint.type} ${constraint.resource}`.toLowerCase().includes(normalizedSearch)
  )), [limitRows, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, selectedNamespace, view])

  const activeItems = view === 'quotas' ? quotaItems : limitItems
  const visibleRows = view === 'quotas' ? visibleQuotaRows : visibleLimitRows
  const totalPages = Math.max(1, Math.ceil(visibleRows.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const namespaceMissing = !selectedNamespace
  const resourceLabel = view === 'quotas' ? 'ResourceQuota' : 'LimitRange'
  const truncated = activeItems.some((item) => (
    'resources_truncated' in item
      ? item.resources_truncated || item.scopes_truncated
      : item.constraints_truncated
  ))

  return (
    <div className="page">
      <PageHeader
        title="资源治理"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeItems.length} 个 ${resourceLabel}` : '选择一个集群'}
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
          <div className="segmented-control" role="group" aria-label="资源治理类型">
            <button type="button" className={view === 'quotas' ? 'active' : ''} aria-pressed={view === 'quotas'} onClick={() => setView('quotas')}>ResourceQuota</button>
            <button type="button" className={view === 'limits' ? 'active' : ''} aria-pressed={view === 'limits'} onClick={() => setView('limits')}>LimitRange</button>
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
                    : visibleRows.length === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前命名空间没有 ${resourceLabel}`} />
                      : view === 'quotas' ? (
                        <>
                          <QuotaTable rows={visibleQuotaRows.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleQuotaRows.length} onPage={setPage} />
                        </>
                      ) : (
                        <>
                          <LimitRangeTable rows={visibleLimitRows.slice(pageStart, pageStart + TABLE_PAGE_SIZE)} />
                          <TablePagination page={currentPage} totalItems={visibleLimitRows.length} onPage={setPage} />
                        </>
                      )}
          </section>
        </>
      )}
    </div>
  )
}

function emptyInventory(view: GovernanceView): GovernanceInventory {
  return view === 'quotas' ? { kind: 'quotas', items: [] } : { kind: 'limits', items: [] }
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

function displayValue(value?: string) {
  return value || '-'
}
