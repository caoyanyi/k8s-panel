import { Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { CSIDriverDetailModal } from '../components/CSIDriverDetailModal'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  KubernetesCSIDriver,
  KubernetesPersistentVolume,
  KubernetesPersistentVolumeClaim,
  KubernetesStorageClass,
  Namespace,
} from '../types'
import { formatDateTime } from '../utils'

type StorageView = 'claims' | 'volumes' | 'classes' | 'drivers'
type StorageInventory =
  | { kind: 'claims'; items: KubernetesPersistentVolumeClaim[] }
  | { kind: 'volumes'; items: KubernetesPersistentVolume[] }
  | { kind: 'classes'; items: KubernetesStorageClass[] }
  | { kind: 'drivers'; items: KubernetesCSIDriver[] }
type StorageItem = KubernetesPersistentVolumeClaim | KubernetesPersistentVolume | KubernetesStorageClass | KubernetesCSIDriver

const storageLabels: Record<StorageView, string> = {
  claims: 'PVC',
  volumes: 'PV',
  classes: 'StorageClass',
  drivers: 'CSIDriver',
}

export function StoragePage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [view, setView] = useState<StorageView>('claims')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedCSIDriver, setSelectedCSIDriver] = useState<KubernetesCSIDriver | null>(null)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaceScoped = view === 'claims'
  const namespaceScope = namespaceScoped ? selectedNamespace : ''
  const namespaces = useResource(
    (signal) => selectedClusterId && namespaceScoped
      ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal)
      : Promise.resolve([]),
    [namespaceScoped, selectedClusterId],
  )
  const inventory = useResource<StorageInventory>(async (signal) => {
    if (!selectedClusterId) return emptyInventory(view)
    if (view === 'claims') {
      const suffix = namespaceScope ? `?${new URLSearchParams({ namespace: namespaceScope })}` : ''
      const items = await api.get<KubernetesPersistentVolumeClaim[]>(
        `/api/v1/clusters/${selectedClusterId}/persistent-volume-claims${suffix}`,
        signal,
      )
      return { kind: 'claims', items }
    }
    if (view === 'volumes') {
      const items = await api.get<KubernetesPersistentVolume[]>(
        `/api/v1/clusters/${selectedClusterId}/persistent-volumes`,
        signal,
      )
      return { kind: 'volumes', items }
    }
    if (view === 'classes') {
      const items = await api.get<KubernetesStorageClass[]>(
        `/api/v1/clusters/${selectedClusterId}/storage-classes`,
        signal,
      )
      return { kind: 'classes', items }
    }
    const items = await api.get<KubernetesCSIDriver[]>(
      `/api/v1/clusters/${selectedClusterId}/csi-drivers`,
      signal,
    )
    return { kind: 'drivers', items }
  }, [selectedClusterId, namespaceScope, view])

  useEffect(() => {
    if (!namespaceScoped) return
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaceScoped, namespaces.data, selectedNamespace, setSelectedNamespace])
  useEffect(() => setSelectedCSIDriver(null), [selectedClusterId, view])

  const normalizedSearch = search.trim().toLowerCase()
  const activeItems = inventory.data?.kind === view ? inventory.data.items : []
  const visibleItems = useMemo(() => activeItems.filter((item) => (
    !normalizedSearch || storageSearchText(view, item).toLowerCase().includes(normalizedSearch)
  )), [activeItems, normalizedSearch, view])
  useEffect(() => setPage(0), [normalizedSearch, namespaceScope, selectedClusterId, view])

  const totalPages = Math.max(1, Math.ceil(visibleItems.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageItems = visibleItems.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const resourceLabel = storageLabels[view]

  return (
    <div className="page">
      <PageHeader
        title="存储资源"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeItems.length} 个 ${resourceLabel}` : '选择一个集群'}
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
          <div className="segmented-control" role="group" aria-label="存储资源类型">
            <button type="button" className={view === 'claims' ? 'active' : ''} aria-pressed={view === 'claims'} onClick={() => setView('claims')}>PVC</button>
            <button type="button" className={view === 'volumes' ? 'active' : ''} aria-pressed={view === 'volumes'} onClick={() => setView('volumes')}>PV</button>
            <button type="button" className={view === 'classes' ? 'active' : ''} aria-pressed={view === 'classes'} onClick={() => setView('classes')}>StorageClass</button>
            <button type="button" className={view === 'drivers' ? 'active' : ''} aria-pressed={view === 'drivers'} onClick={() => setView('drivers')}>CSIDriver</button>
          </div>
          <section className="toolbar" aria-label="存储资源筛选">
            <div className="toolbar-field">
              <label htmlFor="storage-namespace">命名空间</label>
              <select
                id="storage-namespace"
                value={view === 'claims' ? selectedNamespace : ''}
                onChange={(event) => setSelectedNamespace(event.target.value)}
                disabled={view !== 'claims' || namespaces.loading}
              >
                <option value="">全部命名空间</option>
                {namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
              </select>
            </div>
            <div className="search-field">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="storage-search">搜索存储资源</label>
              <input
                id="storage-search"
                type="search"
                placeholder={`搜索 ${resourceLabel}`}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {view === 'claims' && namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} />
              : inventory.loading ? <LoadingState label={`正在读取 ${resourceLabel}`} />
                : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} />
                  : visibleItems.length === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前范围没有 ${resourceLabel}`} />
                    : (
                      <>
                        <StorageTable view={view} items={pageItems} onSelectCSIDriver={setSelectedCSIDriver} />
                        <TablePagination page={currentPage} totalItems={visibleItems.length} onPage={setPage} />
                      </>
                    )}
          </section>
        </>
      )}
      {selectedCSIDriver && (
        <CSIDriverDetailModal
          clusterId={selectedClusterId}
          resource={selectedCSIDriver}
          onClose={() => setSelectedCSIDriver(null)}
        />
      )}
    </div>
  )
}

function emptyInventory(view: StorageView): StorageInventory {
  if (view === 'claims') return { kind: 'claims', items: [] }
  if (view === 'volumes') return { kind: 'volumes', items: [] }
  if (view === 'classes') return { kind: 'classes', items: [] }
  return { kind: 'drivers', items: [] }
}

function storageSearchText(
  view: StorageView,
  item: StorageItem,
) {
  if (view === 'claims') {
    const claim = item as KubernetesPersistentVolumeClaim
    return `${claim.namespace} ${claim.name} ${claim.status} ${claim.volume ?? ''} ${claim.storage_class ?? ''}`
  }
  if (view === 'volumes') {
    const volume = item as KubernetesPersistentVolume
    return `${volume.name} ${volume.status} ${volume.claim ?? ''} ${volume.storage_class ?? ''}`
  }
  if (view === 'drivers') return (item as KubernetesCSIDriver).name
  const storageClass = item as KubernetesStorageClass
  return `${storageClass.name} ${storageClass.provisioner} ${storageClass.reclaim_policy ?? ''} ${storageClass.volume_binding_mode ?? ''}`
}

function StorageTable({
  view,
  items,
  onSelectCSIDriver,
}: {
  view: StorageView
  items: StorageItem[]
  onSelectCSIDriver: (item: KubernetesCSIDriver) => void
}) {
  if (view === 'claims') return <ClaimTable claims={items as KubernetesPersistentVolumeClaim[]} />
  if (view === 'volumes') return <VolumeTable volumes={items as KubernetesPersistentVolume[]} />
  if (view === 'classes') return <StorageClassTable storageClasses={items as KubernetesStorageClass[]} />
  return <CSIDriverTable drivers={items as KubernetesCSIDriver[]} onSelect={onSelectCSIDriver} />
}

function ClaimTable({ claims }: { claims: KubernetesPersistentVolumeClaim[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="PVC 清单" tabIndex={0}>
      <table className="storage-table">
        <thead><tr><th>名称</th><th>命名空间</th><th>状态</th><th>容量</th><th>访问模式</th><th>StorageClass</th><th>绑定卷</th><th>卷模式</th><th>创建时间</th></tr></thead>
        <tbody>{claims.map((claim) => (
          <tr key={`${claim.namespace}:${claim.name}`}>
            <td><strong>{claim.name}</strong></td>
            <td className="mono">{claim.namespace}</td>
            <td><StorageStatus status={claim.status} /></td>
            <td className="mono">{displayValue(claim.capacity)}</td>
            <td className="mono">{displayValue(claim.access_modes)}</td>
            <td className="mono clipped-cell" title={claim.storage_class}>{displayValue(claim.storage_class)}</td>
            <td className="mono clipped-cell" title={claim.volume}>{displayValue(claim.volume)}</td>
            <td>{displayValue(claim.volume_mode)}</td>
            <td>{formatDateTime(claim.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function VolumeTable({ volumes }: { volumes: KubernetesPersistentVolume[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="PV 清单" tabIndex={0}>
      <table className="storage-table">
        <thead><tr><th>名称</th><th>状态</th><th>容量</th><th>访问模式</th><th>StorageClass</th><th>回收策略</th><th>绑定声明</th><th>卷模式</th><th>创建时间</th></tr></thead>
        <tbody>{volumes.map((volume) => (
          <tr key={volume.name}>
            <td><strong>{volume.name}</strong></td>
            <td><StorageStatus status={volume.status} /></td>
            <td className="mono">{displayValue(volume.capacity)}</td>
            <td className="mono">{displayValue(volume.access_modes)}</td>
            <td className="mono clipped-cell" title={volume.storage_class}>{displayValue(volume.storage_class)}</td>
            <td>{displayValue(volume.reclaim_policy)}</td>
            <td className="mono clipped-cell" title={volume.claim}>{displayValue(volume.claim)}</td>
            <td>{displayValue(volume.volume_mode)}</td>
            <td>{formatDateTime(volume.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function StorageClassTable({ storageClasses }: { storageClasses: KubernetesStorageClass[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="StorageClass 清单" tabIndex={0}>
      <table className="storage-class-table">
        <thead><tr><th>名称</th><th>Provisioner</th><th>回收策略</th><th>绑定模式</th><th>卷扩容</th><th>创建时间</th></tr></thead>
        <tbody>{storageClasses.map((storageClass) => (
          <tr key={storageClass.name}>
            <td><div className="inline-labels"><strong>{storageClass.name}</strong>{storageClass.default && <span className="kind-label">默认</span>}</div></td>
            <td className="mono clipped-cell" title={storageClass.provisioner}>{storageClass.provisioner}</td>
            <td>{displayValue(storageClass.reclaim_policy)}</td>
            <td>{displayValue(storageClass.volume_binding_mode)}</td>
            <td>{storageClass.allow_volume_expansion ? <span className="replica-ready">支持</span> : <span className="detail-muted">不支持</span>}</td>
            <td>{formatDateTime(storageClass.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function CSIDriverTable({
  drivers,
  onSelect,
}: {
  drivers: KubernetesCSIDriver[]
  onSelect: (item: KubernetesCSIDriver) => void
}) {
  return (
    <div className="table-wrap" role="region" aria-label="CSIDriver 清单" tabIndex={0}>
      <table className="csi-driver-table">
        <thead><tr><th>名称</th><th>作用域</th><th>创建时间</th><th className="actions-column">操作</th></tr></thead>
        <tbody>{drivers.map((driver) => (
          <tr key={driver.name}>
            <td className="mono"><strong>{driver.name}</strong></td>
            <td><span className="kind-label">集群级</span></td>
            <td>{formatDateTime(driver.created_at)}</td>
            <td className="actions-column">
              <button
                type="button"
                className="icon-button"
                aria-label={`查看 ${driver.name}`}
                title="查看 CSIDriver 详情"
                onClick={() => onSelect(driver)}
              ><Eye size={16} /></button>
            </td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function StorageStatus({ status }: { status: string }) {
  const normalized = status.toLowerCase()
  const className = normalized === 'bound' || normalized === 'available'
    ? 'replica-ready'
    : normalized === 'pending' || normalized === 'released'
      ? 'replica-warning'
      : normalized === 'failed' || normalized === 'lost'
        ? 'scheduling-disabled'
        : 'detail-muted'
  return <span className={className}>{displayValue(status)}</span>
}

function displayValue(value?: string) {
  return value || '-'
}
