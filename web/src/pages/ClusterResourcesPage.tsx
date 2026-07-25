import { ChevronLeft, ChevronRight, Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { NodeDetailModal } from '../components/NodeDetailModal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { ClusterNode, Namespace } from '../types'
import { formatDateTime } from '../utils'

type ResourceView = 'nodes' | 'namespaces'

const resourcePageSize = 100

export function ClusterResourcesPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [view, setView] = useState<ResourceView>('nodes')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedNode, setSelectedNode] = useState('')
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const nodes = useResource(
    (signal) => selectedClusterId && view === 'nodes' ? api.get<ClusterNode[]>(`/api/v1/clusters/${selectedClusterId}/nodes`, signal) : Promise.resolve([]),
    [selectedClusterId, view],
  )
  const namespaces = useResource(
    (signal) => selectedClusterId && view === 'namespaces' ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal) : Promise.resolve([]),
    [selectedClusterId, view],
  )

  useEffect(() => setSelectedNode(''), [selectedClusterId])
  const normalizedSearch = search.trim().toLowerCase()
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, view])
  const visibleNodes = useMemo(() => (nodes.data ?? []).filter((node) => (
    !normalizedSearch || `${node.name} ${node.internal_ip ?? ''} ${node.roles.join(' ')} ${node.version}`.toLowerCase().includes(normalizedSearch)
  )), [nodes.data, normalizedSearch])
  const visibleNamespaces = useMemo(() => (namespaces.data ?? []).filter((namespace) => (
    !normalizedSearch || `${namespace.name} ${Object.entries(namespace.labels).flat().join(' ')}`.toLowerCase().includes(normalizedSearch)
  )), [namespaces.data, normalizedSearch])
  const active = view === 'nodes' ? nodes : namespaces
  const activeCount = active.data?.length ?? 0
  const visibleCount = view === 'nodes' ? visibleNodes.length : visibleNamespaces.length
  const totalPages = Math.max(1, Math.ceil(visibleCount / resourcePageSize))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * resourcePageSize
  const pageNodes = visibleNodes.slice(pageStart, pageStart + resourcePageSize)
  const pageNamespaces = visibleNamespaces.slice(pageStart, pageStart + resourcePageSize)

  return (
    <div className="page">
      <PageHeader
        title="集群资源"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} 个${view === 'nodes' ? '节点' : '命名空间'}` : '选择一个集群'}
        actions={<button type="button" className="button button-secondary" disabled={!selectedClusterId || active.loading} onClick={() => void active.refresh()}><RefreshCw size={16} className={active.loading ? 'spin' : ''} /> 刷新</button>}
      />
      {selectedCluster?.environment === 'production' && <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control" role="group" aria-label="集群资源类型">
            <button type="button" className={view === 'nodes' ? 'active' : ''} onClick={() => setView('nodes')}>节点</button>
            <button type="button" className={view === 'namespaces' ? 'active' : ''} onClick={() => setView('namespaces')}>命名空间</button>
          </div>
          <section className="toolbar" aria-label="集群资源筛选">
            <div className="search-field search-field-wide"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="cluster-resource-search">搜索集群资源</label><input id="cluster-resource-search" type="search" placeholder={view === 'nodes' ? '搜索节点、IP 或角色' : '搜索命名空间或标签'} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {view === 'nodes' ? (
              nodes.loading ? <LoadingState label="正在读取节点" /> : nodes.error ? <ErrorState error={nodes.error} onRetry={() => void nodes.refresh()} /> : visibleNodes.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的节点' : '当前集群没有节点'} /> : <><NodeTable nodes={pageNodes} onSelect={setSelectedNode} /><TablePagination page={currentPage} totalItems={visibleNodes.length} onPage={setPage} /></>
            ) : (
              namespaces.loading ? <LoadingState label="正在读取命名空间" /> : namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : visibleNamespaces.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的命名空间' : '当前集群没有命名空间'} /> : <><NamespaceTable namespaces={pageNamespaces} /><TablePagination page={currentPage} totalItems={visibleNamespaces.length} onPage={setPage} /></>
            )}
          </section>
        </>
      )}
      {selectedNode && <NodeDetailModal clusterId={selectedClusterId} nodeName={selectedNode} open onClose={() => setSelectedNode('')} />}
    </div>
  )
}

function NodeTable({ nodes, onSelect }: { nodes: ClusterNode[]; onSelect: (name: string) => void }) {
  return (
    <div className="table-wrap"><table className="node-table">
      <thead><tr><th>名称</th><th>状态</th><th>角色</th><th>调度</th><th>CPU</th><th>内存</th><th>Pods</th><th>Kubelet</th><th className="actions-column">操作</th></tr></thead>
      <tbody>{nodes.map((node) => <tr key={node.name}>
        <td><div className="primary-cell"><strong>{node.name}</strong><span className="mono">{node.internal_ip || '-'}</span></div></td>
        <td><StatusBadge status={node.status} /></td>
        <td>{node.roles.length ? <div className="inline-labels">{node.roles.map((role) => <span className="kind-label" key={role}>{role}</span>)}</div> : '-'}</td>
        <td>{node.unschedulable ? <span className="scheduling-disabled">已停止调度</span> : <span className="detail-muted">可调度</span>}</td>
        <td className="mono">{resourceRatio(node.allocatable.cpu, node.capacity.cpu)}</td>
        <td className="mono">{resourceRatio(node.allocatable.memory, node.capacity.memory)}</td>
        <td className="mono">{resourceRatio(node.allocatable.pods, node.capacity.pods)}</td>
        <td className="mono">{node.version || '-'}</td>
        <td className="actions-column"><button type="button" className="icon-button" aria-label={`查看 ${node.name}`} title="查看节点详情" onClick={() => onSelect(node.name)}><Eye size={16} /></button></td>
      </tr>)}</tbody>
    </table></div>
  )
}

function NamespaceTable({ namespaces }: { namespaces: Namespace[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="命名空间清单" tabIndex={0}><table>
      <thead><tr><th>名称</th><th>状态</th><th>标签</th><th>终结器</th><th>创建时间</th></tr></thead>
      <tbody>{namespaces.map((namespace) => {
        const labels = Object.entries(namespace.labels).sort(([left], [right]) => left.localeCompare(right))
        return <tr key={namespace.name}>
          <td><strong>{namespace.name}</strong></td>
          <td><StatusBadge status={namespace.status} /></td>
          <td>{labels.length ? <div className="inline-labels namespace-labels">{labels.slice(0, 3).map(([key, value]) => <span className="kind-label" key={key}>{key}{value ? `=${value}` : ''}</span>)}{labels.length > 3 && <span className="detail-muted">+{labels.length - 3}</span>}</div> : '-'}</td>
          <td>{namespace.finalizers.length ? `${namespace.finalizers.length} 个终结器` : '-'}</td>
          <td>{formatDateTime(namespace.created_at)}</td>
        </tr>
      })}</tbody>
    </table></div>
  )
}

function resourceRatio(allocatable?: string, capacity?: string) {
  return `${allocatable || '-'} / ${capacity || '-'}`
}

function TablePagination({ page, totalItems, onPage }: { page: number; totalItems: number; onPage: (page: number) => void }) {
  const totalPages = Math.ceil(totalItems / resourcePageSize)
  if (totalPages <= 1) return null
  return (
    <nav className="table-pagination" aria-label="资源清单分页">
      <span>第 {page + 1} / {totalPages} 页 · {totalItems} 条</span>
      <button type="button" className="icon-button" aria-label="上一页" title="上一页" disabled={page === 0} onClick={() => onPage(page - 1)}><ChevronLeft size={17} /></button>
      <button type="button" className="icon-button" aria-label="下一页" title="下一页" disabled={page + 1 >= totalPages} onClick={() => onPage(page + 1)}><ChevronRight size={17} /></button>
    </nav>
  )
}
