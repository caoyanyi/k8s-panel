import { Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { WorkloadDetailModal } from '../components/WorkloadDetailModal'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { Namespace, Workload } from '../types'
import { formatDateTime, truncate } from '../utils'

interface WorkloadsPageProps {
  notify: (tone: 'success' | 'error', message: string) => void
  openOperations: () => void
}

export function WorkloadsPage({ notify, openOperations }: WorkloadsPageProps) {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [kind, setKind] = useState('')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedWorkload, setSelectedWorkload] = useState<Workload | null>(null)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaces = useResource(
    (signal) => selectedClusterId ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal) : Promise.resolve([]),
    [selectedClusterId],
  )
  const query = new URLSearchParams()
  if (selectedNamespace) query.set('namespace', selectedNamespace)
  if (kind) query.set('kind', kind)
  const workloads = useResource(
    (signal) => selectedClusterId ? api.get<Workload[]>(`/api/v1/clusters/${selectedClusterId}/workloads?${query}`, signal) : Promise.resolve([]),
    [selectedClusterId, selectedNamespace, kind],
  )

  useEffect(() => {
    if (!selectedNamespace && namespaces.data?.length) {
      setSelectedNamespace(namespaces.data[0].name)
    } else if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace(namespaces.data[0]?.name ?? '')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  useEffect(() => setSelectedWorkload(null), [selectedClusterId])

  const visible = useMemo(() => {
    const normalized = search.trim().toLowerCase()
    if (!normalized) return workloads.data ?? []
    return (workloads.data ?? []).filter((item) => `${item.name} ${item.namespace} ${item.images.join(' ')}`.toLowerCase().includes(normalized))
  }, [workloads.data, search])
  const normalizedSearch = search.trim().toLowerCase()
  useEffect(() => setPage(0), [kind, normalizedSearch, selectedClusterId, selectedNamespace])
  const totalPages = Math.max(1, Math.ceil(visible.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageWorkloads = visible.slice(pageStart, pageStart + TABLE_PAGE_SIZE)

  return (
    <div className="page">
      <PageHeader
        title="工作负载"
        meta={selectedCluster ? `${selectedCluster.name} · ${selectedCluster.version ?? '-'}` : '选择一个集群'}
        actions={<button type="button" className="button button-secondary" disabled={!selectedClusterId || workloads.loading} onClick={() => void workloads.refresh()}><RefreshCw size={16} className={workloads.loading ? 'spin' : ''} /> 刷新</button>}
      />
      {selectedCluster?.environment === 'production' && <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <section className="toolbar" aria-label="工作负载筛选">
            <div className="toolbar-field"><label htmlFor="workload-namespace">命名空间</label><select id="workload-namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)} disabled={namespaces.loading}><option value="">全部命名空间</option>{namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></div>
            <div className="toolbar-field"><label htmlFor="workload-kind">类型</label><select id="workload-kind" value={kind} onChange={(event) => setKind(event.target.value)}><option value="">全部类型</option><option value="deployment">Deployment</option><option value="statefulset">StatefulSet</option><option value="daemonset">DaemonSet</option><option value="pod">Pod</option></select></div>
            <div className="search-field"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="workload-search">搜索工作负载</label><input id="workload-search" type="search" placeholder="搜索名称或镜像" value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : workloads.loading ? <LoadingState label="正在读取工作负载" /> : workloads.error ? <ErrorState error={workloads.error} onRetry={() => void workloads.refresh()} /> : visible.length === 0 ? <EmptyState title="当前范围没有工作负载" /> : (
              <><div className="table-wrap"><table>
                <thead><tr><th>名称</th><th>类型</th><th>命名空间</th><th>状态</th><th>就绪</th><th>镜像</th><th>创建时间</th><th className="actions-column">操作</th></tr></thead>
                <tbody>{pageWorkloads.map((item) => <tr key={`${item.kind}:${item.namespace}:${item.name}`}>
                  <td><strong>{item.name}</strong></td>
                  <td><span className="kind-label">{item.kind}</span></td>
                  <td className="mono">{item.namespace}</td>
                  <td><StatusBadge status={item.status} /></td>
                  <td><span className={item.ready === item.desired ? 'replica-ready' : 'replica-warning'}>{item.ready}/{item.desired}</span></td>
                  <td><div className="image-list">{item.images.length ? item.images.map((image) => <span key={image} className="mono" title={image}>{truncate(image, 54)}</span>) : '-'}</div></td>
                  <td>{formatDateTime(item.created_at)}</td>
                  <td className="actions-column"><button type="button" className="icon-button" aria-label={`查看 ${item.name}`} title="查看详情" onClick={() => setSelectedWorkload(item)}><Eye size={16} /></button></td>
                </tr>)}</tbody>
              </table></div><TablePagination page={currentPage} totalItems={visible.length} onPage={setPage} /></>
            )}
          </section>
        </>
      )}
      {selectedWorkload && <WorkloadDetailModal
        clusterId={selectedClusterId}
        clusterName={selectedCluster?.name ?? ''}
        environment={selectedCluster?.environment ?? 'development'}
        workload={selectedWorkload}
        open
        onClose={() => setSelectedWorkload(null)}
        notify={notify}
        openOperations={openOperations}
      />}
    </div>
  )
}
