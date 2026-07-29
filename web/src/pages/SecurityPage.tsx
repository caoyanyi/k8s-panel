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
  KubernetesDeprecatedAPIRequest,
  KubernetesNodeVersionSkew,
  KubernetesNodeVersionSkewReport,
  KubernetesNodeVersionSkewStatus,
  KubernetesPodSecurityAdmissionMode,
  KubernetesPodSecurityAdmissionNamespace,
} from '../types'
import { formatDateTime } from '../utils'

type SecurityView = 'pod-security' | 'version-skew' | 'deprecated-api'
type SecurityInventory =
  | { kind: 'pod-security'; items: KubernetesPodSecurityAdmissionNamespace[] }
  | { kind: 'version-skew'; report: KubernetesNodeVersionSkewReport }
  | { kind: 'deprecated-api'; items: KubernetesDeprecatedAPIRequest[] }

const emptyNodeVersionReport: KubernetesNodeVersionSkewReport = { api_server_version: '', nodes: [] }
const attentionStatuses = new Set<KubernetesNodeVersionSkewStatus>([
  'upgrade-blocking',
  'outside-policy',
  'newer-than-server',
  'major-mismatch',
])
const securityViewCopy: Record<SecurityView, {
  filterLabel: string
  loadingLabel: string
  placeholder: string
  resourceLabel: string
  searchLabel: string
}> = {
  'pod-security': {
    filterLabel: 'Pod 安全态势筛选',
    loadingLabel: '正在读取 Pod 安全态势',
    placeholder: '搜索命名空间、级别或版本',
    resourceLabel: '个命名空间',
    searchLabel: '搜索 Pod 安全态势',
  },
  'version-skew': {
    filterLabel: '节点版本偏差筛选',
    loadingLabel: '正在读取节点版本偏差',
    placeholder: '搜索节点、Kubelet 或判定',
    resourceLabel: '个节点',
    searchLabel: '搜索节点版本偏差',
  },
  'deprecated-api': {
    filterLabel: '废弃 API 请求证据筛选',
    loadingLabel: '正在读取废弃 API 请求证据',
    placeholder: '搜索 API 版本、资源或移除版本',
    resourceLabel: '项证据',
    searchLabel: '搜索废弃 API 请求证据',
  },
}

export function SecurityPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [view, setView] = useState<SecurityView>('pod-security')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const inventory = useResource<SecurityInventory>(async (signal) => {
    if (!selectedClusterId) {
      if (view === 'pod-security') return { kind: 'pod-security', items: [] }
      if (view === 'version-skew') return { kind: 'version-skew', report: emptyNodeVersionReport }
      return { kind: 'deprecated-api', items: [] }
    }
    if (view === 'pod-security') {
      const items = await api.get<KubernetesPodSecurityAdmissionNamespace[]>(
        `/api/v1/clusters/${selectedClusterId}/pod-security-admission/namespaces`,
        signal,
      )
      return { kind: 'pod-security', items }
    }
    if (view === 'version-skew') {
      const report = await api.get<KubernetesNodeVersionSkewReport>(
        `/api/v1/clusters/${selectedClusterId}/upgrade-readiness/node-versions`,
        signal,
      )
      return { kind: 'version-skew', report }
    }
    const items = await api.get<KubernetesDeprecatedAPIRequest[]>(
      `/api/v1/clusters/${selectedClusterId}/upgrade-readiness/deprecated-apis`,
      signal,
    )
    return { kind: 'deprecated-api', items }
  }, [selectedClusterId, view])
  const postureItems = inventory.data?.kind === 'pod-security' ? inventory.data.items : []
  const nodeVersionReport = inventory.data?.kind === 'version-skew' ? inventory.data.report : emptyNodeVersionReport
  const deprecatedAPIRequests = inventory.data?.kind === 'deprecated-api' ? inventory.data.items : []
  const normalizedSearch = search.trim().toLowerCase()
  const visiblePostureItems = useMemo(() => postureItems.filter((item) => (
    !normalizedSearch || podSecurityAdmissionSearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, postureItems])
  const visibleNodeVersions = useMemo(() => nodeVersionReport.nodes.filter((item) => (
    !normalizedSearch || nodeVersionSearchText(item).includes(normalizedSearch)
  )), [nodeVersionReport, normalizedSearch])
  const visibleDeprecatedAPIRequests = useMemo(() => deprecatedAPIRequests.filter((item) => (
    !normalizedSearch || deprecatedAPISearchText(item).includes(normalizedSearch)
  )), [deprecatedAPIRequests, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, view])

  const visibleCount = view === 'pod-security'
    ? visiblePostureItems.length
    : view === 'version-skew' ? visibleNodeVersions.length : visibleDeprecatedAPIRequests.length
  const activeCount = view === 'pod-security'
    ? postureItems.length
    : view === 'version-skew' ? nodeVersionReport.nodes.length : deprecatedAPIRequests.length

  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pagePostureItems = visiblePostureItems.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageNodeVersions = visibleNodeVersions.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageDeprecatedAPIRequests = visibleDeprecatedAPIRequests.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
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
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} ${securityViewCopy[view].resourceLabel}` : '选择一个集群'}
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
            <button
              type="button"
              className={view === 'deprecated-api' ? 'active' : ''}
              aria-pressed={view === 'deprecated-api'}
              onClick={() => selectView('deprecated-api')}
            >废弃 API</button>
          </div>
          <section className="toolbar" aria-label={securityViewCopy[view].filterLabel}>
            <div className="search-field search-field-wide">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="security-posture-search">
                {securityViewCopy[view].searchLabel}
              </label>
              <input
                id="security-posture-search"
                type="search"
                placeholder={securityViewCopy[view].placeholder}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {inventory.loading ? <LoadingState label={securityViewCopy[view].loadingLabel} />
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
                  : view === 'version-skew' ? (
                    <NodeVersionSkewView
                      report={nodeVersionReport}
                      items={pageNodeVersions}
                      totalItems={visibleNodeVersions.length}
                      attentionCount={attentionCount}
                      normalizedSearch={normalizedSearch}
                      page={currentPage}
                      onPage={setPage}
                    />
                  ) : (
                    <DeprecatedAPIView
                      items={pageDeprecatedAPIRequests}
                      totalItems={visibleDeprecatedAPIRequests.length}
                      evidenceCount={deprecatedAPIRequests.length}
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
      <div className="security-evidence-summary">
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

interface DeprecatedAPIViewProps {
  items: KubernetesDeprecatedAPIRequest[]
  totalItems: number
  evidenceCount: number
  normalizedSearch: string
  page: number
  onPage: (page: number) => void
}

function DeprecatedAPIView({
  items,
  totalItems,
  evidenceCount,
  normalizedSearch,
  page,
  onPage,
}: DeprecatedAPIViewProps) {
  return (
    <>
      <div className="security-evidence-summary">
        <div><span>证据来源</span><strong>当前 API Server 实例</strong></div>
        {evidenceCount > 0 && (
          <div className="inventory-alert" role="status">检测到 {evidenceCount} 项废弃 API 请求证据</div>
        )}
      </div>
      {totalItems === 0 ? (
        <EmptyState title={normalizedSearch ? '没有匹配的废弃 API 请求证据' : '当前 API Server 实例未报告废弃 API 请求证据'} />
      ) : (
        <>
          <div className="table-wrap" role="region" aria-label="废弃 API 请求证据" tabIndex={0}>
            <table className="security-deprecated-api-table">
              <thead><tr><th>API 版本</th><th>资源</th><th>子资源</th><th>计划移除版本</th></tr></thead>
              <tbody>{items.map((item) => (
                <tr key={`${item.group}/${item.version}/${item.resource}/${item.subresource}/${item.removed_release}`}>
                  <td className="mono"><strong>{deprecatedAPIVersion(item)}</strong></td>
                  <td className="mono">{item.resource}</td>
                  <td className="mono">{item.subresource || <span className="detail-muted">无</span>}</td>
                  <td className="mono"><span className="security-risk">v{item.removed_release}</span></td>
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

function deprecatedAPIVersion(item: KubernetesDeprecatedAPIRequest): string {
  return `${item.group || 'core'}/${item.version}`
}

function deprecatedAPISearchText(item: KubernetesDeprecatedAPIRequest): string {
  return [
    deprecatedAPIVersion(item),
    item.group,
    item.version,
    item.resource,
    item.subresource,
    item.subresource ? `${item.resource}/${item.subresource}` : item.resource,
    item.removed_release,
    `v${item.removed_release}`,
  ].join(' ').toLowerCase()
}
