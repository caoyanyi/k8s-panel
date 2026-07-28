import { Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { CustomResourceDefinitionDetailModal } from '../components/CustomResourceDefinitionDetailModal'
import { NodeDetailModal } from '../components/NodeDetailModal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { ClusterNode, KubernetesCustomResourceDefinition, Namespace } from '../types'
import { formatDateTime } from '../utils'

type ResourceView = 'nodes' | 'namespaces' | 'crds'

export function ClusterResourcesPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [view, setView] = useState<ResourceView>('nodes')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedNode, setSelectedNode] = useState('')
  const [selectedCRD, setSelectedCRD] = useState<KubernetesCustomResourceDefinition | null>(null)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const nodes = useResource(
    (signal) => selectedClusterId && view === 'nodes' ? api.get<ClusterNode[]>(`/api/v1/clusters/${selectedClusterId}/nodes`, signal) : Promise.resolve([]),
    [selectedClusterId, view],
  )
  const namespaces = useResource(
    (signal) => selectedClusterId && view === 'namespaces' ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal) : Promise.resolve([]),
    [selectedClusterId, view],
  )
  const customResourceDefinitions = useResource(
    (signal) => selectedClusterId && view === 'crds'
      ? api.get<KubernetesCustomResourceDefinition[]>(`/api/v1/clusters/${selectedClusterId}/custom-resource-definitions`, signal)
      : Promise.resolve([]),
    [selectedClusterId, view],
  )

  useEffect(() => {
    setSelectedNode('')
    setSelectedCRD(null)
  }, [selectedClusterId, view])
  const normalizedSearch = search.trim().toLowerCase()
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, view])
  const visibleNodes = useMemo(() => (nodes.data ?? []).filter((node) => (
    !normalizedSearch || `${node.name} ${node.internal_ip ?? ''} ${node.roles.join(' ')} ${node.version}`.toLowerCase().includes(normalizedSearch)
  )), [nodes.data, normalizedSearch])
  const visibleNamespaces = useMemo(() => (namespaces.data ?? []).filter((namespace) => (
    !normalizedSearch || `${namespace.name} ${Object.entries(namespace.labels).flat().join(' ')}`.toLowerCase().includes(normalizedSearch)
  )), [namespaces.data, normalizedSearch])
  const visibleCustomResourceDefinitions = useMemo(() => (customResourceDefinitions.data ?? []).filter((resource) => (
    !normalizedSearch || `${resource.name} ${resource.resource} ${resource.group}`.toLowerCase().includes(normalizedSearch)
  )), [customResourceDefinitions.data, normalizedSearch])
  const active = view === 'nodes' ? nodes : view === 'namespaces' ? namespaces : customResourceDefinitions
  const activeCount = active.data?.length ?? 0
  const visibleCount = view === 'nodes'
    ? visibleNodes.length
    : view === 'namespaces' ? visibleNamespaces.length : visibleCustomResourceDefinitions.length
  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageNodes = visibleNodes.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageNamespaces = visibleNamespaces.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageCustomResourceDefinitions = visibleCustomResourceDefinitions.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const resourceLabel = view === 'nodes' ? '节点' : view === 'namespaces' ? '命名空间' : 'CRD'
  const searchPlaceholder = view === 'nodes'
    ? '搜索节点、IP 或角色'
    : view === 'namespaces' ? '搜索命名空间或标签' : '搜索资源或 API 组'

  return (
    <div className="page">
      <PageHeader
        title="集群资源"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} 个${resourceLabel}` : '选择一个集群'}
        actions={<button type="button" className="button button-secondary" disabled={!selectedClusterId || active.loading} onClick={() => void active.refresh()}><RefreshCw size={16} className={active.loading ? 'spin' : ''} /> 刷新</button>}
      />
      {selectedCluster?.environment === 'production' && <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control" role="group" aria-label="集群资源类型">
            <button type="button" className={view === 'nodes' ? 'active' : ''} onClick={() => setView('nodes')}>节点</button>
            <button type="button" className={view === 'namespaces' ? 'active' : ''} onClick={() => setView('namespaces')}>命名空间</button>
            <button type="button" className={view === 'crds' ? 'active' : ''} onClick={() => setView('crds')}>CRD</button>
          </div>
          <section className="toolbar" aria-label="集群资源筛选">
            <div className="search-field search-field-wide"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="cluster-resource-search">搜索集群资源</label><input id="cluster-resource-search" type="search" placeholder={searchPlaceholder} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {view === 'nodes' ? (
              nodes.loading ? <LoadingState label="正在读取节点" /> : nodes.error ? <ErrorState error={nodes.error} onRetry={() => void nodes.refresh()} /> : visibleNodes.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的节点' : '当前集群没有节点'} /> : <><NodeTable nodes={pageNodes} onSelect={setSelectedNode} /><TablePagination page={currentPage} totalItems={visibleNodes.length} onPage={setPage} /></>
            ) : view === 'namespaces' ? (
              namespaces.loading ? <LoadingState label="正在读取命名空间" /> : namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : visibleNamespaces.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的命名空间' : '当前集群没有命名空间'} /> : <><NamespaceTable namespaces={pageNamespaces} /><TablePagination page={currentPage} totalItems={visibleNamespaces.length} onPage={setPage} /></>
            ) : (
              customResourceDefinitions.loading ? <LoadingState label="正在读取 CRD 元数据" />
                : customResourceDefinitions.error ? <ErrorState error={customResourceDefinitions.error} onRetry={() => void customResourceDefinitions.refresh()} />
                  : visibleCustomResourceDefinitions.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的 CRD' : '当前集群没有 CRD'} />
                    : <><CustomResourceDefinitionTable resources={pageCustomResourceDefinitions} onSelect={setSelectedCRD} /><TablePagination page={currentPage} totalItems={visibleCustomResourceDefinitions.length} onPage={setPage} /></>
            )}
          </section>
        </>
      )}
      {selectedNode && <NodeDetailModal clusterId={selectedClusterId} nodeName={selectedNode} open onClose={() => setSelectedNode('')} />}
      {selectedCRD && <CustomResourceDefinitionDetailModal clusterId={selectedClusterId} resource={selectedCRD} onClose={() => setSelectedCRD(null)} />}
    </div>
  )
}

function CustomResourceDefinitionTable({
  resources,
  onSelect,
}: {
  resources: KubernetesCustomResourceDefinition[]
  onSelect: (resource: KubernetesCustomResourceDefinition) => void
}) {
  return (
    <div className="table-wrap" role="region" aria-label="CRD 清单" tabIndex={0}>
      <table className="crd-table">
        <thead><tr><th>资源</th><th>API 组</th><th>完整名称</th><th>创建时间</th><th className="actions-column">操作</th></tr></thead>
        <tbody>{resources.map((resource) => (
          <tr key={resource.name}>
            <td><strong>{resource.resource}</strong></td>
            <td className="mono">{resource.group}</td>
            <td className="mono">{resource.name}</td>
            <td>{formatDateTime(resource.created_at)}</td>
            <td className="actions-column"><button type="button" className="icon-button" aria-label={`查看 ${resource.name}`} title="查看 CRD 详情" onClick={() => onSelect(resource)}><Eye size={16} /></button></td>
          </tr>
        ))}</tbody>
      </table>
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
