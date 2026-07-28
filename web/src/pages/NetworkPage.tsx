import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { KubernetesEndpointSlice, KubernetesIngress, KubernetesNetworkPolicy, KubernetesService, Namespace, ServicePort } from '../types'
import { formatDateTime, truncate } from '../utils'

type NetworkView = 'services' | 'ingresses' | 'endpoints' | 'policies'
type NetworkInventory =
  | { kind: 'services'; items: KubernetesService[] }
  | { kind: 'ingresses'; items: KubernetesIngress[] }
  | { kind: 'endpoints'; items: KubernetesEndpointSlice[] }
  | { kind: 'policies'; items: KubernetesNetworkPolicy[] }

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
      if (view === 'services') return { kind: 'services', items: [] }
      if (view === 'ingresses') return { kind: 'ingresses', items: [] }
      if (view === 'endpoints') return { kind: 'endpoints', items: [] }
      return { kind: 'policies', items: [] }
    }
    const query = new URLSearchParams()
    if (selectedNamespace) query.set('namespace', selectedNamespace)
    const queryString = query.toString()
    const suffix = queryString ? `?${queryString}` : ''
    if (view === 'services') {
      const items = await api.get<KubernetesService[]>(`/api/v1/clusters/${selectedClusterId}/services${suffix}`, signal)
      return { kind: 'services', items }
    }
    if (view === 'ingresses') {
      const items = await api.get<KubernetesIngress[]>(`/api/v1/clusters/${selectedClusterId}/ingresses${suffix}`, signal)
      return { kind: 'ingresses', items }
    }
    if (view === 'endpoints') {
      const items = await api.get<KubernetesEndpointSlice[]>(`/api/v1/clusters/${selectedClusterId}/endpoint-slices${suffix}`, signal)
      return { kind: 'endpoints', items }
    }
    const items = await api.get<KubernetesNetworkPolicy[]>(`/api/v1/clusters/${selectedClusterId}/network-policies${suffix}`, signal)
    return { kind: 'policies', items }
  }, [selectedClusterId, selectedNamespace, view])

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const normalizedSearch = search.trim().toLowerCase()
  const services = inventory.data?.kind === 'services' ? inventory.data.items : []
  const ingresses = inventory.data?.kind === 'ingresses' ? inventory.data.items : []
  const endpointSlices = inventory.data?.kind === 'endpoints' ? inventory.data.items : []
  const policies = inventory.data?.kind === 'policies' ? inventory.data.items : []
  const visibleServices = useMemo(() => services.filter((item) => (
    !normalizedSearch || serviceSearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, services])
  const visibleIngresses = useMemo(() => ingresses.filter((item) => (
    !normalizedSearch || ingressSearchText(item).includes(normalizedSearch)
  )), [ingresses, normalizedSearch])
  const visibleEndpointSlices = useMemo(() => endpointSlices.filter((item) => (
    !normalizedSearch || endpointSliceSearchText(item).includes(normalizedSearch)
  )), [endpointSlices, normalizedSearch])
  const visiblePolicies = useMemo(() => policies.filter((item) => (
    !normalizedSearch || networkPolicySearchText(item).includes(normalizedSearch)
  )), [normalizedSearch, policies])
  useEffect(() => setPage(0), [normalizedSearch, selectedClusterId, selectedNamespace, view])

  const visibleCount = view === 'services'
    ? visibleServices.length
    : view === 'ingresses' ? visibleIngresses.length : view === 'endpoints' ? visibleEndpointSlices.length : visiblePolicies.length
  const activeCount = view === 'services'
    ? services.length
    : view === 'ingresses' ? ingresses.length : view === 'endpoints' ? endpointSlices.length : policies.length
  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageServices = visibleServices.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageIngresses = visibleIngresses.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageEndpointSlices = visibleEndpointSlices.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pagePolicies = visiblePolicies.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const resourceLabel = view === 'services'
    ? 'Service'
    : view === 'ingresses' ? 'Ingress' : view === 'endpoints' ? 'EndpointSlice' : 'NetworkPolicy'
  const searchPlaceholder = view === 'services'
    ? '搜索 Service、地址或端口'
    : view === 'ingresses'
      ? '搜索 Ingress、域名或地址'
      : view === 'endpoints' ? '搜索名称、Service、地址族' : '搜索 NetworkPolicy、命名空间或方向'

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
          <div className="segmented-control network-kind-control" role="group" aria-label="网络资源类型">
            <button type="button" aria-pressed={view === 'services'} className={view === 'services' ? 'active' : ''} onClick={() => setView('services')}>Service</button>
            <button type="button" aria-pressed={view === 'ingresses'} className={view === 'ingresses' ? 'active' : ''} onClick={() => setView('ingresses')}>Ingress</button>
            <button type="button" aria-pressed={view === 'endpoints'} className={view === 'endpoints' ? 'active' : ''} onClick={() => setView('endpoints')}>EndpointSlice</button>
            <button type="button" aria-pressed={view === 'policies'} className={view === 'policies' ? 'active' : ''} onClick={() => setView('policies')}>NetworkPolicy</button>
          </div>
          <section className="toolbar" aria-label="网络资源筛选">
            <div className="toolbar-field"><label htmlFor="network-namespace">命名空间</label><select id="network-namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)} disabled={namespaces.loading}><option value="">全部命名空间</option>{namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></div>
            <div className="search-field"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="network-search">搜索网络资源</label><input id="network-search" type="search" placeholder={searchPlaceholder} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : inventory.loading ? <LoadingState label={`正在读取 ${resourceLabel}`} /> : inventory.error ? <ErrorState error={inventory.error} onRetry={() => void inventory.refresh()} /> : visibleCount === 0 ? <EmptyState title={normalizedSearch ? `没有匹配的 ${resourceLabel}` : `当前范围没有 ${resourceLabel}`} /> : view === 'services' ? (
              <><ServiceTable services={pageServices} /><TablePagination page={currentPage} totalItems={visibleServices.length} onPage={setPage} /></>
            ) : view === 'ingresses' ? (
              <><IngressTable ingresses={pageIngresses} /><TablePagination page={currentPage} totalItems={visibleIngresses.length} onPage={setPage} /></>
            ) : view === 'endpoints' ? (
              <><EndpointSliceTable endpointSlices={pageEndpointSlices} /><TablePagination page={currentPage} totalItems={visibleEndpointSlices.length} onPage={setPage} /></>
            ) : (
              <><NetworkPolicyTable policies={pagePolicies} /><TablePagination page={currentPage} totalItems={visiblePolicies.length} onPage={setPage} /></>
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

function EndpointSliceTable({ endpointSlices }: { endpointSlices: KubernetesEndpointSlice[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="EndpointSlice 清单" tabIndex={0}><table className="endpoint-slice-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>Service</th><th>地址族</th><th>端点</th><th>Ready</th><th>Serving</th><th>Terminating</th><th>端口</th><th>创建时间</th></tr></thead>
      <tbody>{endpointSlices.map((endpointSlice) => <tr key={`${endpointSlice.namespace}:${endpointSlice.name}`}>
        <td><strong>{endpointSlice.name}</strong></td>
        <td className="mono">{endpointSlice.namespace}</td>
        <td className="mono">{endpointSlice.service_name}</td>
        <td><div className="inline-labels"><span className="kind-label">{endpointSlice.address_type}</span>{endpointSlice.address_type === 'FQDN' && <span className="detail-muted">已弃用</span>}</div></td>
        <td className="network-count">{endpointSlice.endpoint_count}</td>
        <td><EndpointConditionCount count={endpointSlice.ready_endpoint_count} total={endpointSlice.endpoint_count} defaulted={endpointSlice.ready_defaulted_count} /></td>
        <td><EndpointConditionCount count={endpointSlice.serving_endpoint_count} total={endpointSlice.endpoint_count} defaulted={endpointSlice.serving_defaulted_count} /></td>
        <td><EndpointConditionCount count={endpointSlice.terminating_endpoint_count} total={endpointSlice.endpoint_count} defaulted={endpointSlice.terminating_defaulted_count} inverse /></td>
        <td className="network-count">{endpointSlice.port_count}</td>
        <td>{formatDateTime(endpointSlice.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}

function EndpointConditionCount({ count, total, defaulted, inverse = false }: { count: number; total: number; defaulted: number; inverse?: boolean }) {
  const attention = inverse ? count > 0 : total === 0 || count < total
  return <div className="network-value-list"><span className={attention ? 'replica-warning' : 'replica-ready'}>{count} / {total}</span>{defaulted > 0 && <span className="detail-muted">{defaulted} 个按 API 默认</span>}</div>
}

function NetworkPolicyTable({ policies }: { policies: KubernetesNetworkPolicy[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="NetworkPolicy 清单" tabIndex={0}><table className="network-policy-table">
      <thead><tr><th>名称</th><th>命名空间</th><th>Pod 选择范围</th><th>隔离方向</th><th>允许规则</th><th>对端 / 端口</th><th>无允许规则</th><th>创建时间</th></tr></thead>
      <tbody>{policies.map((policy) => <tr key={`${policy.namespace}:${policy.name}`}>
        <td><strong>{policy.name}</strong></td>
        <td className="mono">{policy.namespace}</td>
        <td><div className="network-value-list"><span>{policy.pod_selector_mode === 'all' ? '全部 Pod' : '带筛选条件'}</span><span className="detail-muted">{policy.pod_selector_label_count} 个标签 / {policy.pod_selector_expression_count} 个表达式</span></div></td>
        <td><div className="inline-labels">{policy.policy_types.map((policyType) => <span className="kind-label" key={policyType}>{policyType}</span>)}{policy.policy_types_defaulted && <span className="detail-muted">按 API 默认</span>}</div></td>
        <td className="network-count">入站 {policy.ingress_rule_count} / 出站 {policy.egress_rule_count}</td>
        <td><div className="network-value-list"><span>入站：{policy.ingress_peer_count} 个对端 / {policy.ingress_port_count} 个端口</span><span>出站：{policy.egress_peer_count} 个对端 / {policy.egress_port_count} 个端口</span></div></td>
        <td><PolicyEmptyDirections policy={policy} /></td>
        <td>{formatDateTime(policy.created_at)}</td>
      </tr>)}</tbody>
    </table></div>
  )
}

function PolicyEmptyDirections({ policy }: { policy: KubernetesNetworkPolicy }) {
  const ingressEmpty = policy.policy_types.includes('Ingress') && policy.ingress_rule_count === 0
  const egressEmpty = policy.policy_types.includes('Egress') && policy.egress_rule_count === 0
  if (!ingressEmpty && !egressEmpty) return <>-</>
  return <div className="network-value-list">{ingressEmpty && <span className="replica-warning">本策略无入站规则</span>}{egressEmpty && <span className="replica-warning">本策略无出站规则</span>}</div>
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

function endpointSliceSearchText(endpointSlice: KubernetesEndpointSlice) {
  return [endpointSlice.name, endpointSlice.namespace, endpointSlice.service_name, endpointSlice.address_type].join(' ').toLowerCase()
}

function networkPolicySearchText(policy: KubernetesNetworkPolicy) {
  return [policy.name, policy.namespace, policy.pod_selector_mode, ...policy.policy_types].join(' ').toLowerCase()
}
