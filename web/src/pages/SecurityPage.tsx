import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge, statusLabel } from '../components/StatusBadge'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  KubernetesNodeVersionSkew,
  KubernetesNodeVersionSkewReport,
  KubernetesNodeVersionSkewStatus,
  KubernetesPodSecurityAdmissionMode,
  KubernetesPodSecurityAdmissionNamespace,
} from '../types'
import { formatDateTime } from '../utils'

type SecurityView = 'pod-security' | 'version-skew'
type SecurityInventory =
  | { kind: 'pod-security'; items: KubernetesPodSecurityAdmissionNamespace[] }
  | { kind: 'version-skew'; report: KubernetesNodeVersionSkewReport }

const emptyNodeVersionReport: KubernetesNodeVersionSkewReport = { api_server_version: '', nodes: [] }
const attentionStatuses = new Set<KubernetesNodeVersionSkewStatus>([
  'upgrade-blocking',
  'outside-policy',
  'newer-than-server',
  'major-mismatch',
])

export function SecurityPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [view, setView] = useState<SecurityView>('pod-security')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const inventory = useResource<SecurityInventory>(async (signal) => {
    if (!selectedClusterId) {
      return view === 'pod-security'
        ? { kind: 'pod-security', items: [] }
        : { kind: 'version-skew', report: emptyNodeVersionReport }
    }
    if (view === 'pod-security') {
      const items = await api.get<KubernetesPodSecurityAdmissionNamespace[]>(
        `/api/v1/clusters/${selectedClusterId}/pod-security-admission/namespaces`,
        signal,
      )
      return { kind: 'pod-security', items }
    }
    const report = await api.get<KubernetesNodeVersionSkewReport>(
      `/api/v1/clusters/${selectedClusterId}/upgrade-readiness/node-versions`,
      signal,
    )
    return { kind: 'version-skew', report }
  }, [selectedClusterId, view])
  const postureItems = inventory.data?.kind === 'pod-security' ? inventory.data.items : []
  const nodeVersionReport = inventory.data?.kind === 'version-skew' ? inventory.data.report : emptyNodeVersionReport
  const normalizedSearch = search.trim().toLowerCase()
  const visiblePostureItems = useMemo(() => postureItems.filter((item) => (
    !normalizedSearch || podSecurityAdmissionSearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, postureItems])
  const visibleNodeVersions = useMemo(() => nodeVersionReport.nodes.filter((item) => (
    !normalizedSearch || nodeVersionSearchText(item).includes(normalizedSearch)
  )), [nodeVersionReport, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, view])

  const visibleCount = view === 'pod-security' ? visiblePostureItems.length : visibleNodeVersions.length
  const activeCount = view === 'pod-security' ? postureItems.length : nodeVersionReport.nodes.length
  const resourceLabel = view === 'pod-security' ? '个命名空间' : '个节点'

  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pagePostureItems = visiblePostureItems.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageNodeVersions = visibleNodeVersions.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const attentionCount = nodeVersionReport.nodes.filter((node) => attentionStatuses.has(node.status)).length
  const selectView = (nextView: SecurityView) => {
    if (nextView === view) return
    setSearch('')
    setView(nextView)
  }

  return (
    <div className="page">
      <PageHeader
        title="安全态势"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} ${resourceLabel}` : '选择一个集群'}
        actions={(
          <button
            type="button"
            className="button button-secondary"
            disabled={!selectedClusterId || inventory.loading}
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
          <div className="segmented-control security-view-control" role="group" aria-label="安全态势视图">
            <button
              type="button"
              className={view === 'pod-security' ? 'active' : ''}
              aria-pressed={view === 'pod-security'}
              onClick={() => selectView('pod-security')}
            >Pod 安全</button>
            <button
              type="button"
              className={view === 'version-skew' ? 'active' : ''}
              aria-pressed={view === 'version-skew'}
              onClick={() => selectView('version-skew')}
            >版本偏差</button>
          </div>
          <section className="toolbar" aria-label={view === 'pod-security' ? 'Pod 安全态势筛选' : '节点版本偏差筛选'}>
            <div className="search-field search-field-wide">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="security-posture-search">
                {view === 'pod-security' ? '搜索 Pod 安全态势' : '搜索节点版本偏差'}
              </label>
              <input
                id="security-posture-search"
                type="search"
                placeholder={view === 'pod-security' ? '搜索命名空间、级别或版本' : '搜索节点、Kubelet 或判定'}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {inventory.loading ? <LoadingState label={view === 'pod-security' ? '正在读取 Pod 安全态势' : '正在读取节点版本偏差'} />
              : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} />
                : view === 'pod-security'
                  ? visiblePostureItems.length === 0
                    ? <EmptyState title={normalizedSearch ? '没有匹配的命名空间' : '当前集群没有命名空间'} />
                    : (
                      <>
                        <PodSecurityAdmissionTable items={pagePostureItems} />
                        <TablePagination page={currentPage} totalItems={visiblePostureItems.length} onPage={setPage} />
                      </>
                    )
                  : (
                    <NodeVersionSkewView
                      report={nodeVersionReport}
                      items={pageNodeVersions}
                      totalItems={visibleNodeVersions.length}
                      attentionCount={attentionCount}
                      normalizedSearch={normalizedSearch}
                      page={currentPage}
                      onPage={setPage}
                    />
                  )}
          </section>
        </>
      )}
    </div>
  )
}

interface NodeVersionSkewViewProps {
  report: KubernetesNodeVersionSkewReport
  items: KubernetesNodeVersionSkew[]
  totalItems: number
  attentionCount: number
  normalizedSearch: string
  page: number
  onPage: (page: number) => void
}

function NodeVersionSkewView({
  report,
  items,
  totalItems,
  attentionCount,
  normalizedSearch,
  page,
  onPage,
}: NodeVersionSkewViewProps) {
  return (
    <>
      <div className="security-version-summary">
        <div><span>观测到的 API Server</span><strong className="mono">{report.api_server_version}</strong></div>
        {attentionCount > 0 && <div className="inventory-alert" role="status">{attentionCount} 个节点需处理</div>}
      </div>
      {totalItems === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的节点' : '当前集群没有节点'} /> : (
        <>
          <div className="table-wrap" role="region" aria-label="节点版本偏差态势" tabIndex={0}>
            <table className="security-version-table">
              <thead><tr><th>节点</th><th>Kubelet</th><th>与 API Server 偏差</th><th>判定</th></tr></thead>
              <tbody>{items.map((item) => (
                <tr key={item.name}>
                  <td className="mono"><strong>{item.name}</strong></td>
                  <td className="mono">{item.kubelet_version}</td>
                  <td><NodeVersionSkewValue item={item} /></td>
                  <td><StatusBadge status={item.status} /></td>
                </tr>
              ))}</tbody>
            </table>
          </div>
          <TablePagination page={page} totalItems={totalItems} onPage={onPage} />
        </>
      )}
    </>
  )
}

function NodeVersionSkewValue({ item }: { item: KubernetesNodeVersionSkew }) {
  if (!item.minor_skew_comparable) return <span className="detail-muted">不可比较</span>
  if (item.minor_skew === 0) return <span>偏差 0 个次版本</span>
  if (item.minor_skew < 0) return <span className="security-risk">领先 {Math.abs(item.minor_skew)} 个次版本</span>
  return (
    <div className="primary-cell security-skew-value">
      <span>落后 {item.minor_skew} 个次版本</span>
      <small>政策上限 {item.maximum_minor_skew} 个次版本</small>
    </div>
  )
}

function PodSecurityAdmissionTable({ items }: { items: KubernetesPodSecurityAdmissionNamespace[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="Pod Security Admission 命名空间态势" tabIndex={0}>
      <table className="security-posture-table">
        <thead><tr><th>命名空间</th><th>配置状态</th><th>Enforce</th><th>Audit</th><th>Warn</th><th>创建时间</th></tr></thead>
        <tbody>{items.map((item) => (
          <tr key={item.name}>
            <td className="mono"><strong>{item.name}</strong></td>
            <td><NamespacePostureStatus item={item} /></td>
            <td><PodSecurityAdmissionModeValue mode={item.enforce} /></td>
            <td><PodSecurityAdmissionModeValue mode={item.audit} /></td>
            <td><PodSecurityAdmissionModeValue mode={item.warn} /></td>
            <td>{formatDateTime(item.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function NamespacePostureStatus({ item }: { item: KubernetesPodSecurityAdmissionNamespace }) {
  if (item.invalid_mode_count > 0) return <span className="replica-warning">存在无效标签</span>
  if ([item.enforce, item.audit, item.warn].every((mode) => mode.status === 'inherited')) {
    return <span className="detail-muted">继承默认值</span>
  }
  return <span className="replica-ready">显式配置</span>
}

function PodSecurityAdmissionModeValue({ mode }: { mode: KubernetesPodSecurityAdmissionMode }) {
  if (mode.status === 'invalid') return <span className="security-risk">配置无效</span>
  if (mode.status === 'inherited') return <span className="detail-muted">继承集群默认值</span>
  return (
    <div className="primary-cell security-mode-value">
      <strong className="mono">{mode.level}</strong>
      <small className="mono">{mode.version}{mode.version_defaulted ? '（默认）' : '（固定）'}</small>
    </div>
  )
}

function podSecurityAdmissionSearchText(item: KubernetesPodSecurityAdmissionNamespace): string {
  return [item.name, item.enforce, item.audit, item.warn]
    .flatMap((value) => typeof value === 'string' ? [value] : [value.status, value.level ?? '', value.version ?? ''])
    .join(' ')
    .toLowerCase()
}

function nodeVersionSearchText(item: KubernetesNodeVersionSkew): string {
  return [item.name, item.kubelet_version, item.status, statusLabel(item.status)].join(' ').toLowerCase()
}
