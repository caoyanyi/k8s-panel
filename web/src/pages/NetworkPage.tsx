import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { KubernetesIngress, KubernetesService, Namespace, ServicePort } from '../types'
import { formatDateTime, truncate } from '../utils'

type NetworkView = 'services' | 'ingresses'
type NetworkInventory =
  | { kind: 'services'; items: KubernetesService[] }
  | { kind: 'ingresses'; items: KubernetesIngress[] }

export function NetworkPage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [view, setView] = useState<NetworkView>('services')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaces = useResource(
    (signal) => selectedClusterId
      ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal)
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const inventory = useResource<NetworkInventory>(async (signal) => {
    if (!selectedClusterId) {
      return view === 'services' ? { kind: 'services', items: [] } : { kind: 'ingresses', items: [] }
    }
    const query = new URLSearchParams()
    if (selectedNamespace) query.set('namespace', selectedNamespace)
    const queryString = query.toString()
    const suffix = queryString ? `?${queryString}` : ''
    if (view === 'services') {
      const items = await api.get<KubernetesService[]>(`/api/v1/clusters/${selectedClusterId}/services${suffix}`, signal)
      return { kind: 'services', items }
    }
    const items = await api.get<KubernetesIngress[]>(`/api/v1/clusters/${selectedClusterId}/ingresses${suffix}`, signal)
    return { kind: 'ingresses', items }
  }, [selectedClusterId, selectedNamespace, view])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const normalizedSearch = search.trim().toLowerCase()
  const services = inventory.data?.kind === 'services' ? inventory.data.items : []
  const ingresses = inventory.data?.kind === 'ingresses' ? inventory.data.items : []
  const visibleServices = useMemo(() => services.filter((item) => (
    !normalizedSearch || serviceSearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, services])
  const visibleIngresses = useMemo(() => ingresses.filter((item) => (
    !normalizedSearch || ingressSearchText(item).includes(normalizedSearch)
  )), [ingresses, normalizedSearch])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, selectedNamespace, view])

  const visibleCount = view === 'services' ? visibleServices.length : visibleIngresses.length
  const activeCount = view === 'services' ? services.length : ingresses.length
  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageServices = visibleServices.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageIngresses = visibleIngresses.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const resourceLabel = view === 'services' ? 'Service' : 'Ingress'

  return (
    <div className="page">
      <PageHeader
        title="网络资源"
        meta={selectedCluster ? `${selectedCluster.name} · ${activeCount} 个 ${resourceLabel}` : '选择一个集群'}
        actions={<button type="button" className="button button-secondary" disabled={!selectedClusterId || inventory.loading} onClick={() => void inventory.refresh()}><RefreshCw size={16} className={inventory.loading ? 'spin' : ''} /> 刷新</button>}
      />
      {selectedCluster?.environment === 'production' && <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control" role="group" aria-label="网络资源类型">
            <button type="button" className={view === 'services' ? 'active' : ''} onClick={() => setView('services')}>Service</button>
            <button type="button" className={view === 'ingresses' ? 'active' : ''} onClick={() => setView('ingresses')}>Ingress</button>
          </div>
          <section className="toolbar" aria-label="网络资源筛选">
            <div className="toolbar-field"><label htmlFor="network-namespace">命名空间</label><select id="network-namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)} disabled={namespaces.loading}><option value="">全部命名空间</option>{namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></div>
            <div className="search-field"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="network-search">搜索网络资源</label><input id="network-search" type="search" placeholder={view === 'services' ? '搜索 Service、地址或端口' : '搜索 Ingress、域名或地址'} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : inventory.loading ? <LoadingState label={`正在读取 ${resourceLabel}`} /> : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} /> : visibleCount === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前范围没有 ${resourceLabel}`} /> : view === 'services' ? (
              <><ServiceTable services={pageServices} /><TablePagination page={currentPage} totalItems={visibleServices.length} onPage={setPage} /></>
            ) : (
              <><IngressTable ingresses={pageIngresses} /><TablePagination page={currentPage} totalItems={visibleIngresses.length} onPage={setPage} /></>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function ServiceTable({ services }: { services: KubernetesService[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="Service 清单" tabIndex={0}><table className="network-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>类型</th><th>Cluster IP</th><th>外部入口</th><th>端口</th><th>创建时间</th></tr></thead>
      <tbody>{services.map((service) => <tr key={`${service.namespace}:${service.name}`}>
        <td><strong>{service.name}</strong></td>
        <td className="mono">{service.namespace}</td>
        <td><span className="kind-label">{service.type}</span></td>
        <td className="mono">{service.cluster_ip || '-'}</td>
        <td><ServiceAddresses service={service} /></td>
        <td><BoundedValues values={service.ports.map(formatServicePort)} total={service.port_count} /></td>
        <td>{formatDateTime(service.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}

function IngressTable({ ingresses }: { ingresses: KubernetesIngress[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="Ingress 清单" tabIndex={0}><table className="network-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>IngressClass</th><th>域名</th><th>入口地址</th><th>TLS</th><th>路由</th><th>创建时间</th></tr></thead>
      <tbody>{ingresses.map((ingress) => <tr key={`${ingress.namespace}:${ingress.name}`}>
        <td><strong>{ingress.name}</strong></td>
        <td className="mono">{ingress.namespace}</td>
        <td>{ingress.class_name ? <span className="kind-label">{ingress.class_name}</span> : '-'}</td>
        <td><BoundedValues values={ingress.hosts} total={ingress.host_count} /></td>
        <td><BoundedValues values={ingress.addresses} total={ingress.address_count} /></td>
        <td>{ingress.tls ? <span className="replica-ready">已启用</span> : <span className="detail-muted">未配置</span>}</td>
        <td className="network-count">{ingress.rule_count} 条规则 / {ingress.path_count} 条路径</td>
        <td>{formatDateTime(ingress.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}

function ServiceAddresses({ service }: { service: KubernetesService }) {
  if (!service.external_name && service.external_addresses.length === 0) return <>-</>
  return <div className="network-value-list">{service.external_name && <span className="mono" title={service.external_name}>{truncate(service.external_name, 48)}</span>}{service.external_addresses.length > 0 && <BoundedValues values={service.external_addresses} total={service.address_count} nested />}</div>
}

function BoundedValues({ values, total, nested = false }: { values: string[]; total: number; nested?: boolean }) {
  if (values.length === 0) return <>-</>
  const content = <>{values.map((value, index) => <span className="mono" key={`${value}:${index}`} title={value}>{truncate(value, 48)}</span>)}{total > values.length && <span className="detail-muted">+{total - values.length}</span>}</>
  return nested ? content : <div className="network-value-list">{content}</div>
}

function formatServicePort(port: ServicePort) {
  const name = port.name ? `${port.name}: ` : ''
  const target = port.target_port ? ` / ${port.target_port}` : ''
  const nodePort = port.node_port ? ` / NodePort ${port.node_port}` : ''
  return `${name}${port.protocol} ${port.port}${target}${nodePort}`
}

function serviceSearchText(service: KubernetesService) {
  return [
    service.name, service.namespace, service.type, service.cluster_ip ?? '', service.external_name ?? '',
    ...service.external_addresses, ...service.ports.map(formatServicePort),
  ].join(' ').toLowerCase()
}

function ingressSearchText(ingress: KubernetesIngress) {
  return [ingress.name, ingress.namespace, ingress.class_name ?? '', ...ingress.hosts, ...ingress.addresses].join(' ').toLowerCase()
}
