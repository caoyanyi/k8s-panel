import { Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { AccessResourceDetailModal } from '../components/AccessResourceDetailModal'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { KubernetesAccessResource, KubernetesAccessResourceKind, Namespace } from '../types'
import { formatDateTime } from '../utils'

interface AccessViewDefinition {
  kind: KubernetesAccessResourceKind
  label: string
  objectKind: KubernetesAccessResource['kind']
  namespaced: boolean
}

interface AccessSelection {
  clusterId: string
  resourceKind: KubernetesAccessResourceKind
  resource: KubernetesAccessResource
}

const accessViews: AccessViewDefinition[] = [
  { kind: 'clusterroles', label: 'ClusterRole', objectKind: 'ClusterRole', namespaced: false },
  { kind: 'clusterrolebindings', label: 'ClusterRoleBinding', objectKind: 'ClusterRoleBinding', namespaced: false },
  { kind: 'roles', label: 'Role', objectKind: 'Role', namespaced: true },
  { kind: 'rolebindings', label: 'RoleBinding', objectKind: 'RoleBinding', namespaced: true },
  { kind: 'serviceaccounts', label: 'ServiceAccount', objectKind: 'ServiceAccount', namespaced: true },
]

export function AccessPage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [view, setView] = useState<KubernetesAccessResourceKind>('clusterroles')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selection, setSelection] = useState<AccessSelection | null>(null)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const viewDefinition = accessViews.find((candidate) => candidate.kind === view) ?? accessViews[0]
  const namespaceScope = viewDefinition.namespaced ? selectedNamespace : ''

  const namespaces = useResource(
    (signal) => selectedClusterId
      ? api.get<Namespace[]>(`/api/v1/clusters/${encodeURIComponent(selectedClusterId)}/namespaces`, signal)
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const inventory = useResource<KubernetesAccessResource[]>(async (signal) => {
    if (!selectedClusterId || (viewDefinition.namespaced && !namespaceScope)) return []
    const params = new URLSearchParams({ kind: viewDefinition.kind })
    if (namespaceScope) params.set('namespace', namespaceScope)
    return api.get<KubernetesAccessResource[]>(
      `/api/v1/clusters/${encodeURIComponent(selectedClusterId)}/access-resources?${params}`,
      signal,
    )
  }, [namespaceScope, selectedClusterId, viewDefinition.kind, viewDefinition.namespaced])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])
  useEffect(() => {
    setPage(0)
    setSelection(null)
  }, [namespaceScope, selectedClusterId, view])

  const normalizedSearch = search.trim().toLowerCase()
  const visibleItems = useMemo(() => (inventory.data ?? []).filter((item) => (
    !normalizedSearch || `${item.kind} ${item.namespace ?? ''} ${item.name}`.toLowerCase().includes(normalizedSearch)
  )), [inventory.data, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch])

  const totalPages = Math.max(1, Math.ceil(visibleItems.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageItems = visibleItems.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const scopeMissing = viewDefinition.namespaced && !namespaceScope

  return (
    <div className="page">
      <PageHeader
        title="访问控制"
        meta={selectedCluster ? `${selectedCluster.name} · ${(inventory.data ?? []).length} 个 ${viewDefinition.label}` : '选择一个集群'}
        actions={(
          <button
            type="button"
            className="button button-secondary"
            disabled={!selectedClusterId || scopeMissing || inventory.loading}
            onClick={() => {
              setSelection(null)
              void inventory.refresh()
            }}
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
          <div className="segmented-control access-kind-control" role="group" aria-label="访问控制资源类型">
            {accessViews.map((item) => (
              <button
                type="button"
                key={item.kind}
                className={view === item.kind ? 'active' : ''}
                aria-pressed={view === item.kind}
                onClick={() => {
                  setSelection(null)
                  setView(item.kind)
                }}
              >
                {item.label}
              </button>
            ))}
          </div>
          <section className="toolbar" aria-label="访问控制资源筛选">
            <div className="toolbar-field">
              <label htmlFor="access-namespace">命名空间</label>
              <select
                id="access-namespace"
                value={viewDefinition.namespaced ? selectedNamespace : ''}
                onChange={(event) => {
                  setSelection(null)
                  setSelectedNamespace(event.target.value)
                }}
                disabled={!viewDefinition.namespaced || namespaces.loading}
              >
                <option value="">{viewDefinition.namespaced ? '请选择命名空间' : '集群级资源'}</option>
                {namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
              </select>
            </div>
            <div className="search-field">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="access-search">搜索访问控制资源</label>
              <input
                id="access-search"
                type="search"
                placeholder={`搜索 ${viewDefinition.label}`}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {viewDefinition.namespaced && namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} />
              : scopeMissing ? <EmptyState title="请选择命名空间" />
                : inventory.loading ? <LoadingState label={`正在读取 ${viewDefinition.label}`} />
                  : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} />
                    : visibleItems.length === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${viewDefinition.label}` : `当前范围没有 ${viewDefinition.label}`} />
                      : (
                        <>
                          <AccessResourceTable
                            items={pageItems}
                            onInspect={(resource) => setSelection({
                              clusterId: selectedClusterId,
                              resourceKind: viewDefinition.kind,
                              resource,
                            })}
                          />
                          <TablePagination page={currentPage} totalItems={visibleItems.length} onPage={setPage} />
                        </>
                      )}
          </section>
        </>
      )}
      {selection && (
        <AccessResourceDetailModal
          clusterId={selection.clusterId}
          resourceKind={selection.resourceKind}
          resource={selection.resource}
          namespaces={namespaces.data ?? []}
          onClose={() => setSelection(null)}
        />
      )}
    </div>
  )
}

function AccessResourceTable({
  items,
  onInspect,
}: {
  items: KubernetesAccessResource[]
  onInspect: (item: KubernetesAccessResource) => void
}) {
  return (
    <div className="table-wrap" role="region" aria-label="访问控制资源清单" tabIndex={0}>
      <table className="access-table">
        <thead><tr><th>名称</th><th>类型</th><th>命名空间</th><th>创建时间</th><th className="operation-action-column">操作</th></tr></thead>
        <tbody>{items.map((item) => (
          <tr key={`${item.kind}:${item.namespace ?? ''}:${item.name}`}>
            <td><strong>{item.name}</strong></td>
            <td><span className="kind-label">{item.kind}</span></td>
            <td className="mono">{item.namespace || '集群级'}</td>
            <td>{formatDateTime(item.created_at)}</td>
            <td className="operation-action-column">
              <button
                type="button"
                className="icon-button"
                aria-label={`查看 ${item.name}`}
                title="查看详情"
                onClick={() => onInspect(item)}
              >
                <Eye size={17} />
              </button>
            </td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}
