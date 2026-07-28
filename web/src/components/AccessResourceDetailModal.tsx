import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type {
  KubernetesAccessResource,
  KubernetesAccessResourceDetail,
  KubernetesAccessResourceKind,
} from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface AccessResourceDetailModalProps {
  clusterId: string
  resourceKind: KubernetesAccessResourceKind
  resource: KubernetesAccessResource
  onClose: () => void
}

export function AccessResourceDetailModal({
  clusterId,
  resourceKind,
  resource,
  onClose,
}: AccessResourceDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesAccessResourceDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => {
    const segments = [clusterId, resourceKind, resource.name].map(encodeURIComponent)
    const params = resource.namespace ? `?${new URLSearchParams({ namespace: resource.namespace })}` : ''
    return `/api/v1/clusters/${segments[0]}/access-resources/${segments[1]}/${segments[2]}${params}`
  }, [clusterId, resource, resourceKind])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesAccessResourceDetail>(resourcePath, controller.signal)
      .then((value) => { if (active) setDetail(value) })
      .catch((caught: unknown) => {
        if (active && !(caught instanceof DOMException && caught.name === 'AbortError')) setError(caught)
      })
      .finally(() => { if (active) setLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [attempt, resource, resourcePath])

  return (
    <Modal
      title={`${resource.kind} · ${resource.name}`}
      open
      onClose={onClose}
      width="large"
    >
      {loading ? <LoadingState label="正在读取访问控制详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <AccessResourceDetailView detail={detail} /> : null}
    </Modal>
  )
}

function AccessResourceDetailView({ detail }: { detail: KubernetesAccessResourceDetail }) {
  return (
    <div className="access-detail detail-overview">
      <dl className="detail-grid">
        <div><dt>类型</dt><dd><span className="kind-label">{detail.kind}</span></dd></div>
        <div><dt>名称</dt><dd title={detail.name}>{detail.name}</dd></div>
        <div><dt>命名空间</dt><dd className="mono">{detail.namespace || '集群级'}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
        {detail.role_ref && <div><dt>引用角色</dt><dd className="mono">{detail.role_ref.kind}/{detail.role_ref.name}</dd></div>}
      </dl>
      {detail.kind === 'ServiceAccount' && <ServiceAccountSummary detail={detail} />}
      {(detail.kind === 'Role' || detail.kind === 'ClusterRole') && <RulesSummary detail={detail} />}
      {(detail.kind === 'RoleBinding' || detail.kind === 'ClusterRoleBinding') && <SubjectsSummary detail={detail} />}
    </div>
  )
}

function ServiceAccountSummary({ detail }: { detail: KubernetesAccessResourceDetail }) {
  const automount = detail.automount_service_account_token === undefined
    ? '继承默认值'
    : detail.automount_service_account_token ? '已启用' : '已禁用'
  return (
    <section className="detail-section">
      <h3>安全摘要</h3>
      <dl className="detail-grid">
        <div><dt>自动挂载 Token</dt><dd>{automount}</dd></div>
        <div><dt>Secret 引用</dt><dd>{detail.secret_count}</dd></div>
        <div><dt>镜像拉取 Secret 引用</dt><dd>{detail.image_pull_secret_count}</dd></div>
      </dl>
    </section>
  )
}

function RulesSummary({ detail }: { detail: KubernetesAccessResourceDetail }) {
  return (
    <section className="detail-section">
      <div className="access-detail-heading">
        <h3>规则</h3>
        <span>显示 {detail.rules.length} / {detail.rule_count} 条规则</span>
      </div>
      {detail.rules_truncated && <div className="access-truncated" role="status">规则已按安全上限截断</div>}
      {detail.rules.length === 0 ? <span className="detail-muted">无规则</span> : (
        <div className="table-wrap" role="region" aria-label="RBAC 规则" tabIndex={0}>
          <table className="detail-table access-rule-table">
            <thead><tr><th>API 组</th><th>资源 / 非资源 URL</th><th>动词</th><th>资源名称</th></tr></thead>
            <tbody>{detail.rules.map((rule, index) => (
              <tr key={`${index}:${rule.verbs.join(',')}:${rule.resources.join(',')}`}>
                <td className="mono access-list-cell">{formatAPIGroupList(rule.api_groups)}</td>
                <td className="mono access-list-cell">{formatRuleTargets(rule.resources, rule.non_resource_urls)}</td>
                <td className="mono access-list-cell">{formatValues(rule.verbs)}</td>
                <td className="mono access-list-cell">{formatValues(rule.resource_names)}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function SubjectsSummary({ detail }: { detail: KubernetesAccessResourceDetail }) {
  return (
    <section className="detail-section">
      <div className="access-detail-heading">
        <h3>主体</h3>
        <span>显示 {detail.subjects.length} / {detail.subject_count} 个主体</span>
      </div>
      {detail.subjects_truncated && <div className="access-truncated" role="status">主体已按安全上限截断</div>}
      {detail.subjects.length === 0 ? <span className="detail-muted">无主体</span> : (
        <div className="table-wrap" role="region" aria-label="RBAC 主体" tabIndex={0}>
          <table className="detail-table access-subject-table">
            <thead><tr><th>类型</th><th>名称</th><th>命名空间</th></tr></thead>
            <tbody>{detail.subjects.map((subject, index) => (
              <tr key={`${subject.kind}:${subject.namespace ?? ''}:${subject.name}:${index}`}>
                <td><span className="kind-label">{subject.kind}</span></td>
                <td className="mono access-list-cell">{subject.name}</td>
                <td className="mono">{subject.namespace || '-'}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
    </section>
  )
}

function formatAPIGroupList(values: string[]) {
  return values.length ? values.map((value) => value || 'core').join(', ') : '-'
}

function formatRuleTargets(resources: string[], nonResourceURLs: string[]) {
  const values = [...resources, ...nonResourceURLs]
  return formatValues(values)
}

function formatValues(values: string[]) {
  return values.length ? values.join(', ') : '-'
}
