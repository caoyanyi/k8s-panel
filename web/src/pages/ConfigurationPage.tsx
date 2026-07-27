import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { KubernetesConfigMap, KubernetesSecret, Namespace } from '../types'
import { formatDateTime } from '../utils'

type ConfigurationView = 'configmaps' | 'secrets'
type ConfigurationInventory =
  | { kind: 'configmaps'; items: KubernetesConfigMap[] }
  | { kind: 'secrets'; items: KubernetesSecret[] }

export function ConfigurationPage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [view, setView] = useState<ConfigurationView>('configmaps')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaces = useResource(
    (signal) => selectedClusterId
      ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal)
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const inventory = useResource<ConfigurationInventory>(async (signal) => {
    if (!selectedClusterId) {
      return view === 'configmaps' ? { kind: 'configmaps', items: [] } : { kind: 'secrets', items: [] }
    }
    if (view === 'secrets' && !selectedNamespace) {
      return { kind: 'secrets', items: [] }
    }
    const suffix = selectedNamespace ? `?${new URLSearchParams({ namespace: selectedNamespace })}` : ''
    if (view === 'configmaps') {
      const items = await api.get<KubernetesConfigMap[]>(`/api/v1/clusters/${selectedClusterId}/configmaps${suffix}`, signal)
      return { kind: 'configmaps', items }
    }
    const items = await api.get<KubernetesSecret[]>(`/api/v1/clusters/${selectedClusterId}/secrets${suffix}`, signal)
    return { kind: 'secrets', items }
  }, [selectedClusterId, selectedNamespace, view])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const normalizedSearch = search.trim().toLowerCase()
  const configMaps = inventory.data?.kind === 'configmaps' ? inventory.data.items : []
  const secrets = inventory.data?.kind === 'secrets' ? inventory.data.items : []
  const visibleConfigMaps = useMemo(() => configMaps.filter((item) => (
    !normalizedSearch || `${item.name} ${item.namespace}`.toLowerCase().includes(normalizedSearch)
  )), [configMaps, normalizedSearch])
  const visibleSecrets = useMemo(() => secrets.filter((item) => (
    !normalizedSearch || `${item.name} ${item.namespace} ${item.type}`.toLowerCase().includes(normalizedSearch)
  )), [normalizedSearch, secrets])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, selectedNamespace, view])

  const visibleCount = view === 'configmaps' ? visibleConfigMaps.length : visibleSecrets.length
  const activeCount = view === 'configmaps' ? configMaps.length : secrets.length
  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageConfigMaps = visibleConfigMaps.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageSecrets = visibleSecrets.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const resourceLabel = view === 'configmaps' ? 'ConfigMap' : 'Secret'
  const secretScopeMissing = view === 'secrets' && !selectedNamespace

  return (
    <div className="page">
      <PageHeader
        title="配置资源"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} 个 ${resourceLabel}` : '选择一个集群'}
        actions={<button type="button" className="button button-secondary" disabled={!selectedClusterId || inventory.loading || secretScopeMissing} onClick={() => void inventory.refresh()}><RefreshCw size={16} className={inventory.loading ? 'spin' : ''} /> 刷新</button>}
      />
      {selectedCluster?.environment === 'production' && <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control" role="group" aria-label="配置资源类型">
            <button type="button" className={view === 'configmaps' ? 'active' : ''} onClick={() => setView('configmaps')}>ConfigMap</button>
            <button type="button" className={view === 'secrets' ? 'active' : ''} onClick={() => setView('secrets')}>Secret</button>
          </div>
          <section className="toolbar" aria-label="配置资源筛选">
            <div className="toolbar-field"><label htmlFor="configuration-namespace">命名空间</label><select id="configuration-namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)} disabled={namespaces.loading}><option value="">全部命名空间</option>{namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></div>
            <div className="search-field"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="configuration-search">搜索配置资源</label><input id="configuration-search" type="search" placeholder={`搜索 ${resourceLabel} 名称或命名空间`} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : secretScopeMissing ? <EmptyState title="请选择命名空间" /> : inventory.loading ? <LoadingState label={`正在读取 ${resourceLabel}`} /> : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} /> : visibleCount === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前范围没有 ${resourceLabel}`} /> : view === 'configmaps' ? (
              <><ConfigMapTable configMaps={pageConfigMaps} /><TablePagination page={currentPage} totalItems={visibleConfigMaps.length} onPage={setPage} /></>
            ) : (
              <><SecretTable secrets={pageSecrets} /><TablePagination page={currentPage} totalItems={visibleSecrets.length} onPage={setPage} /></>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function ConfigMapTable({ configMaps }: { configMaps: KubernetesConfigMap[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="ConfigMap 清单" tabIndex={0}><table className="configuration-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>数据项</th><th>创建时间</th></tr></thead>
      <tbody>{configMaps.map((configMap) => <tr key={`${configMap.namespace}:${configMap.name}`}>
        <td><strong>{configMap.name}</strong></td>
        <td className="mono">{configMap.namespace}</td>
        <td>{configMap.data_count} 项</td>
        <td>{formatDateTime(configMap.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}

function SecretTable({ secrets }: { secrets: KubernetesSecret[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="Secret 清单" tabIndex={0}><table className="configuration-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>类型</th><th>数据项</th><th>创建时间</th></tr></thead>
      <tbody>{secrets.map((secret) => <tr key={`${secret.namespace}:${secret.name}`}>
        <td><strong>{secret.name}</strong></td>
        <td className="mono">{secret.namespace}</td>
        <td className="mono clipped-cell" title={secret.type}>{secret.type}</td>
        <td>{secret.data_count} 项</td>
        <td>{formatDateTime(secret.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}
