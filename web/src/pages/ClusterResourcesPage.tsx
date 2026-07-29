import { Eye, RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { AdmissionPolicyResourceDetailModal } from '../components/AdmissionPolicyResourceDetailModal'
import { AdmissionWebhookConfigurationDetailModal } from '../components/AdmissionWebhookConfigurationDetailModal'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { CustomResourceDefinitionDetailModal } from '../components/CustomResourceDefinitionDetailModal'
import { NodeDetailModal } from '../components/NodeDetailModal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type {
  ClusterNode,
  KubernetesAdmissionPolicyResource,
  KubernetesAdmissionWebhookConfiguration,
  KubernetesAPIService,
  KubernetesCustomResourceDefinition,
  Namespace,
} from '../types'
import { formatDateTime } from '../utils'

type ResourceView = 'nodes' | 'namespaces' | 'crds' | 'api-services' | 'admission-webhooks'
type AdmissionResourceView = 'validating-webhooks' | 'mutating-webhooks' | 'policies' | 'bindings'

export function ClusterResourcesPage() {
  const { clusters, selectedClusterId } = usePanel()
  const [view, setView] = useState<ResourceView>('nodes')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const [selectedNode, setSelectedNode] = useState('')
  const [selectedCRD, setSelectedCRD] = useState<KubernetesCustomResourceDefinition | null>(null)
  const [admissionView, setAdmissionView] = useState<AdmissionResourceView>('validating-webhooks')
  const [selectedAdmissionWebhook, setSelectedAdmissionWebhook] = useState<KubernetesAdmissionWebhookConfiguration | null>(null)
  const [selectedAdmissionPolicyResource, setSelectedAdmissionPolicyResource] = useState<KubernetesAdmissionPolicyResource | null>(null)
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
  const apiServices = useResource(
    (signal) => selectedClusterId && view === 'api-services'
      ? api.get<KubernetesAPIService[]>(`/api/v1/clusters/${selectedClusterId}/api-services`, signal)
      : Promise.resolve([]),
    [selectedClusterId, view],
  )
  const admissionWebhooks = useResource(
    (signal) => selectedClusterId && view === 'admission-webhooks' &&
      (admissionView === 'validating-webhooks' || admissionView === 'mutating-webhooks')
      ? api.get<KubernetesAdmissionWebhookConfiguration[]>(
        `/api/v1/clusters/${selectedClusterId}/admission-webhook-configurations?${new URLSearchParams({ kind: admissionView === 'mutating-webhooks' ? 'mutating' : 'validating' })}`,
        signal,
      )
      : Promise.resolve([]),
    [selectedClusterId, view, admissionView],
  )
  const admissionPolicies = useResource(
    (signal) => selectedClusterId && view === 'admission-webhooks' && admissionView === 'policies'
      ? api.get<KubernetesAdmissionPolicyResource[]>(`/api/v1/clusters/${selectedClusterId}/validating-admission-policies`, signal)
      : Promise.resolve([]),
    [selectedClusterId, view, admissionView],
  )
  const admissionPolicyBindings = useResource(
    (signal) => selectedClusterId && view === 'admission-webhooks' && admissionView === 'bindings'
      ? api.get<KubernetesAdmissionPolicyResource[]>(`/api/v1/clusters/${selectedClusterId}/validating-admission-policy-bindings`, signal)
      : Promise.resolve([]),
    [selectedClusterId, view, admissionView],
  )

  useEffect(() => {
    setSelectedNode('')
    setSelectedCRD(null)
    setSelectedAdmissionWebhook(null)
    setSelectedAdmissionPolicyResource(null)
  }, [selectedClusterId, view])
  useEffect(() => {
    setSelectedAdmissionWebhook(null)
    setSelectedAdmissionPolicyResource(null)
  }, [admissionView])
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
  const visibleAPIServices = useMemo(() => (apiServices.data ?? []).filter((resource) => (
    !normalizedSearch || `${resource.name} ${resource.group} ${resource.version} ${resource.service_namespace ?? ''} ${resource.service_name ?? ''} ${resource.availability_reason ?? ''}`.toLowerCase().includes(normalizedSearch)
  )), [apiServices.data, normalizedSearch])
  const visibleAdmissionWebhooks = useMemo(() => (admissionWebhooks.data ?? []).filter((resource) => (
    !normalizedSearch || `${resource.name} ${resource.kind}`.toLowerCase().includes(normalizedSearch)
  )), [admissionWebhooks.data, normalizedSearch])
  const visibleAdmissionPolicies = useMemo(() => (admissionPolicies.data ?? []).filter((resource) => (
    !normalizedSearch || resource.name.toLowerCase().includes(normalizedSearch)
  )), [admissionPolicies.data, normalizedSearch])
  const visibleAdmissionPolicyBindings = useMemo(() => (admissionPolicyBindings.data ?? []).filter((resource) => (
    !normalizedSearch || resource.name.toLowerCase().includes(normalizedSearch)
  )), [admissionPolicyBindings.data, normalizedSearch])
  const activeAdmissionResource = admissionView === 'policies' ? admissionPolicies
    : admissionView === 'bindings' ? admissionPolicyBindings : admissionWebhooks
  const active = view === 'admission-webhooks' ? activeAdmissionResource
    : { nodes, namespaces, crds: customResourceDefinitions, 'api-services': apiServices }[view]
  const activeCount = active.data?.length ?? 0
  const visibleCount = {
    nodes: visibleNodes.length,
    namespaces: visibleNamespaces.length,
    crds: visibleCustomResourceDefinitions.length,
    'api-services': visibleAPIServices.length,
    'admission-webhooks': admissionView === 'policies' ? visibleAdmissionPolicies.length
      : admissionView === 'bindings' ? visibleAdmissionPolicyBindings.length : visibleAdmissionWebhooks.length,
  }[view]
  const totalPages = Math.max(1, Math.ceil(visibleCount / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageNodes = visibleNodes.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageNamespaces = visibleNamespaces.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageCustomResourceDefinitions = visibleCustomResourceDefinitions.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageAPIServices = visibleAPIServices.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageAdmissionWebhooks = visibleAdmissionWebhooks.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageAdmissionPolicies = visibleAdmissionPolicies.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const pageAdmissionPolicyBindings = visibleAdmissionPolicyBindings.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const admissionResourceLabel = admissionView === 'policies' ? '校验策略'
    : admissionView === 'bindings' ? '策略绑定' : 'Webhook 配置'
  const resourceLabel = view === 'admission-webhooks' ? admissionResourceLabel
    : { nodes: '节点', namespaces: '命名空间', crds: 'CRD', 'api-services': '聚合 API' }[view]
  const searchPlaceholder = {
    nodes: '搜索节点、IP 或角色',
    namespaces: '搜索命名空间或标签',
    crds: '搜索资源或 API 组',
    'api-services': '搜索 API、Service 或状态原因',
    'admission-webhooks': admissionView === 'policies' ? '搜索校验准入策略'
      : admissionView === 'bindings' ? '搜索准入策略绑定' : '搜索准入 Webhook 配置',
  }[view]

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
          <div className="segmented-control cluster-resource-control" role="group" aria-label="集群资源类型">
            <button type="button" className={view === 'nodes' ? 'active' : ''} onClick={() => setView('nodes')}>节点</button>
            <button type="button" className={view === 'namespaces' ? 'active' : ''} onClick={() => setView('namespaces')}>命名空间</button>
            <button type="button" className={view === 'crds' ? 'active' : ''} onClick={() => setView('crds')}>CRD</button>
            <button type="button" className={view === 'api-services' ? 'active' : ''} onClick={() => setView('api-services')}>聚合 API</button>
            <button type="button" className={view === 'admission-webhooks' ? 'active' : ''} onClick={() => setView('admission-webhooks')}>准入</button>
          </div>
          <section className="toolbar" aria-label="集群资源筛选">
            {view === 'admission-webhooks' && (
              <div className="segmented-control admission-kind-control" role="group" aria-label="准入资源类型">
                <button type="button" className={admissionView === 'validating-webhooks' ? 'active' : ''} onClick={() => setAdmissionView('validating-webhooks')}>Validating</button>
                <button type="button" className={admissionView === 'mutating-webhooks' ? 'active' : ''} onClick={() => setAdmissionView('mutating-webhooks')}>Mutating</button>
                <button type="button" className={admissionView === 'policies' ? 'active' : ''} onClick={() => setAdmissionView('policies')}>校验策略</button>
                <button type="button" className={admissionView === 'bindings' ? 'active' : ''} onClick={() => setAdmissionView('bindings')}>策略绑定</button>
              </div>
            )}
            <div className="search-field search-field-wide"><Search size={16} aria-hidden="true" /><label className="sr-only" htmlFor="cluster-resource-search">搜索集群资源</label><input id="cluster-resource-search" type="search" placeholder={searchPlaceholder} value={search} onChange={(event) => setSearch(event.target.value)} /></div>
          </section>
          <section className="section-block table-section">
            {view === 'nodes' && (
              nodes.loading ? <LoadingState label="正在读取节点" /> : nodes.error ? <ErrorState error={nodes.error} onRetry={() => void nodes.refresh()} /> : visibleNodes.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的节点' : '当前集群没有节点'} /> : <><NodeTable nodes={pageNodes} onSelect={setSelectedNode} /><TablePagination page={currentPage} totalItems={visibleNodes.length} onPage={setPage} /></>
            )}
            {view === 'namespaces' && (
              namespaces.loading ? <LoadingState label="正在读取命名空间" /> : namespaces.error ? <ErrorState error={namespaces.error} onRetry={() => void namespaces.refresh()} /> : visibleNamespaces.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的命名空间' : '当前集群没有命名空间'} /> : <><NamespaceTable namespaces={pageNamespaces} /><TablePagination page={currentPage} totalItems={visibleNamespaces.length} onPage={setPage} /></>
            )}
            {view === 'crds' && (
              customResourceDefinitions.loading ? <LoadingState label="正在读取 CRD 元数据" />
                : customResourceDefinitions.error ? <ErrorState error={customResourceDefinitions.error} onRetry={() => void customResourceDefinitions.refresh()} />
                  : visibleCustomResourceDefinitions.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的 CRD' : '当前集群没有 CRD'} />
                    : <><CustomResourceDefinitionTable resources={pageCustomResourceDefinitions} onSelect={setSelectedCRD} /><TablePagination page={currentPage} totalItems={visibleCustomResourceDefinitions.length} onPage={setPage} /></>
            )}
            {view === 'api-services' && (
              apiServices.loading ? <LoadingState label="正在读取聚合 API 健康状态" />
                : apiServices.error ? <ErrorState error={apiServices.error} onRetry={() => void apiServices.refresh()} />
                  : visibleAPIServices.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的聚合 API' : '当前集群没有聚合 API'} />
                    : <><APIServiceTable resources={pageAPIServices} /><TablePagination page={currentPage} totalItems={visibleAPIServices.length} onPage={setPage} /></>
            )}
            {view === 'admission-webhooks' && (
              admissionView === 'policies' ? (
                admissionPolicies.loading ? <LoadingState label="正在读取校验准入策略元数据" />
                  : admissionPolicies.error ? <ErrorState error={admissionPolicies.error} onRetry={() => void admissionPolicies.refresh()} />
                    : visibleAdmissionPolicies.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的校验准入策略' : '当前集群没有校验准入策略'} />
                      : <><AdmissionPolicyResourceTable resources={pageAdmissionPolicies} onSelect={setSelectedAdmissionPolicyResource} /><TablePagination page={currentPage} totalItems={visibleAdmissionPolicies.length} onPage={setPage} /></>
              ) : admissionView === 'bindings' ? (
                admissionPolicyBindings.loading ? <LoadingState label="正在读取准入策略绑定元数据" />
                  : admissionPolicyBindings.error ? <ErrorState error={admissionPolicyBindings.error} onRetry={() => void admissionPolicyBindings.refresh()} />
                    : visibleAdmissionPolicyBindings.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的准入策略绑定' : '当前集群没有准入策略绑定'} />
                      : <><AdmissionPolicyResourceTable resources={pageAdmissionPolicyBindings} onSelect={setSelectedAdmissionPolicyResource} /><TablePagination page={currentPage} totalItems={visibleAdmissionPolicyBindings.length} onPage={setPage} /></>
              ) : (
                admissionWebhooks.loading ? <LoadingState label="正在读取准入 Webhook 元数据" />
                  : admissionWebhooks.error ? <ErrorState error={admissionWebhooks.error} onRetry={() => void admissionWebhooks.refresh()} />
                    : visibleAdmissionWebhooks.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的准入 Webhook 配置' : '当前集群没有准入 Webhook 配置'} />
                      : <><AdmissionWebhookConfigurationTable resources={pageAdmissionWebhooks} onSelect={setSelectedAdmissionWebhook} /><TablePagination page={currentPage} totalItems={visibleAdmissionWebhooks.length} onPage={setPage} /></>
              )
            )}
          </section>
        </>
      )}
      {selectedNode && <NodeDetailModal clusterId={selectedClusterId} nodeName={selectedNode} open onClose={() => setSelectedNode('')} />}
      {selectedCRD && <CustomResourceDefinitionDetailModal clusterId={selectedClusterId} resource={selectedCRD} onClose={() => setSelectedCRD(null)} />}
      {selectedAdmissionWebhook && <AdmissionWebhookConfigurationDetailModal clusterId={selectedClusterId} resource={selectedAdmissionWebhook} onClose={() => setSelectedAdmissionWebhook(null)} />}
      {selectedAdmissionPolicyResource && <AdmissionPolicyResourceDetailModal clusterId={selectedClusterId} resource={selectedAdmissionPolicyResource} onClose={() => setSelectedAdmissionPolicyResource(null)} />}
    </div>
  )
}

function AdmissionPolicyResourceTable({
  resources,
  onSelect,
}: {
  resources: KubernetesAdmissionPolicyResource[]
  onSelect: (resource: KubernetesAdmissionPolicyResource) => void
}) {
  return (
    <div className="table-wrap" role="region" aria-label="准入策略资源清单" tabIndex={0}>
      <table className="admission-policy-table">
        <thead><tr><th>名称</th><th>资源类型</th><th>创建时间</th><th className="actions-column">操作</th></tr></thead>
        <tbody>{resources.map((resource) => (
          <tr key={`${resource.kind}/${resource.name}`}>
            <td className="mono"><strong>{resource.name}</strong></td>
            <td><span className="kind-label">{resource.kind === 'policy' ? 'ValidatingAdmissionPolicy' : 'ValidatingAdmissionPolicyBinding'}</span></td>
            <td>{formatDateTime(resource.created_at)}</td>
            <td className="actions-column"><button type="button" className="icon-button" aria-label={`查看 ${resource.name}`} title="查看准入策略详情" onClick={() => onSelect(resource)}><Eye size={16} /></button></td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function AdmissionWebhookConfigurationTable({
  resources,
  onSelect,
}: {
  resources: KubernetesAdmissionWebhookConfiguration[]
  onSelect: (resource: KubernetesAdmissionWebhookConfiguration) => void
}) {
  return (
    <div className="table-wrap" role="region" aria-label="准入 Webhook 配置清单" tabIndex={0}>
      <table className="admission-webhook-table">
        <thead><tr><th>配置</th><th>类型</th><th>创建时间</th><th className="actions-column">操作</th></tr></thead>
        <tbody>{resources.map((resource) => (
          <tr key={`${resource.kind}/${resource.name}`}>
            <td className="mono"><strong>{resource.name}</strong></td>
            <td><span className="kind-label">{resource.kind === 'validating' ? 'Validating' : 'Mutating'}</span></td>
            <td>{formatDateTime(resource.created_at)}</td>
            <td className="actions-column"><button type="button" className="icon-button" aria-label={`查看 ${resource.name}`} title="查看准入 Webhook 详情" onClick={() => onSelect(resource)}><Eye size={16} /></button></td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function APIServiceTable({ resources }: { resources: KubernetesAPIService[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="聚合 API 清单" tabIndex={0}>
      <table className="api-service-table">
        <thead><tr><th>API</th><th>处理位置</th><th>后端</th><th>可用性</th><th>状态原因</th><th>TLS</th><th>优先级</th><th>创建时间</th></tr></thead>
        <tbody>{resources.map((resource) => (
          <tr key={resource.name}>
            <td><div className="primary-cell"><strong>{resource.group ? `${resource.group}/${resource.version}` : `core/${resource.version}`}</strong><span className="mono subtle-id">{resource.name}</span></div></td>
            <td>{resource.local ? '本地' : 'Service'}</td>
            <td className="mono">{resource.local ? 'kube-apiserver' : `${resource.service_namespace}/${resource.service_name}:${resource.service_port}`}</td>
            <td>{apiServiceAvailability(resource)}</td>
            <td><div className="primary-cell"><span className="mono">{resource.availability_reason || '-'}</span><small>{apiServiceConditionSummary(resource)}</small></div></td>
            <td>{resource.local ? '-' : resource.insecure_skip_tls_verify ? <span className="security-risk">跳过 TLS 校验</span> : <span className="detail-muted">启用校验</span>}</td>
            <td className="mono">{resource.group_priority_minimum} / {resource.version_priority}</td>
            <td>{formatDateTime(resource.created_at)}</td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function apiServiceAvailability(resource: KubernetesAPIService) {
  if (!resource.availability_observed) return <span className="detail-muted">未报告</span>
  if (resource.availability_status === 'True') return <StatusBadge status="True" />
  if (resource.availability_status === 'False') return <StatusBadge status="Unavailable" />
  return <StatusBadge status="Unknown" />
}

function apiServiceConditionSummary(resource: KubernetesAPIService) {
  const conditionCount = `${resource.condition_count} 个条件`
  return resource.availability_transition_time
    ? `${formatDateTime(resource.availability_transition_time)} · ${conditionCount}`
    : conditionCount
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
