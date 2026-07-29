import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  KubernetesPodSecurityAdmissionMode,
  KubernetesPodSecurityAdmissionNamespace,
} from '../types'
import { formatDateTime } from '../utils'

export function SecurityPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const posture = useResource(
    (signal) => selectedClusterId
      ? api.get<KubernetesPodSecurityAdmissionNamespace[]>(
        `/api/v1/clusters/${selectedClusterId}/pod-security-admission/namespaces`,
        signal,
      )
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const normalizedSearch = search.trim().toLowerCase()
  const visibleItems = useMemo(() => (posture.data ?? []).filter((item) => (
    !normalizedSearch || podSecurityAdmissionSearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, posture.data])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId])

  const totalPages = Math.max(1, Math.ceil(visibleItems.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageItems = visibleItems.slice(pageStart, pageStart + TABLE_PAGE_SIZE)

  return (
    <div className="page">
      <PageHeader
        title="安全态势"
        meta={selectedCluster ? `${selectedCluster.name} · ${posture.data?.length ?? 0} 个命名空间` : '选择一个集群'}
        actions={(
          <button
            type="button"
            className="button button-secondary"
            disabled={!selectedClusterId || posture.loading}
            onClick={() => void posture.refresh()}
          >
            <RefreshCw size={16} className={posture.loading ? 'spin' : ''} /> 刷新
          </button>
        )}
      />
      {selectedCluster?.environment === 'production' && (
        <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>
      )}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <section className="toolbar" aria-label="Pod 安全态势筛选">
            <div className="search-field search-field-wide">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="pod-security-admission-search">搜索 Pod 安全态势</label>
              <input
                id="pod-security-admission-search"
                type="search"
                placeholder="搜索命名空间、级别或版本"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {posture.loading ? <LoadingState label="正在读取 Pod 安全态势" />
              : posture.error ? <ErrorState error={posture.error} onRetry={() => void posture.refresh()} />
                : visibleItems.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的命名空间' : '当前集群没有命名空间'} />
                  : (
                    <>
                      <PodSecurityAdmissionTable items={pageItems} />
                      <TablePagination page={currentPage} totalItems={visibleItems.length} onPage={setPage} />
                    </>
                  )}
          </section>
        </>
      )}
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
