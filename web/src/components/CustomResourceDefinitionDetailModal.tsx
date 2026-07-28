import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesCustomResourceDefinition, KubernetesCustomResourceDefinitionDetail } from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

interface CustomResourceDefinitionDetailModalProps {
  clusterId: string
  resource: KubernetesCustomResourceDefinition
  onClose: () => void
}

export function CustomResourceDefinitionDetailModal({
  clusterId,
  resource,
  onClose,
}: CustomResourceDefinitionDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesCustomResourceDefinitionDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/custom-resource-definitions/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesCustomResourceDefinitionDetail>(resourcePath, controller.signal)
      .then((value) => { if (active) setDetail(value) })
      .catch((caught: unknown) => {
        if (active && !(caught instanceof DOMException && caught.name === 'AbortError')) setError(caught)
      })
      .finally(() => { if (active) setLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [attempt, resourcePath])

  return (
    <Modal title={`CRD · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取 CRD 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <CustomResourceDefinitionDetailView detail={detail} /> : null}
    </Modal>
  )
}

function CustomResourceDefinitionDetailView({ detail }: { detail: KubernetesCustomResourceDefinitionDetail }) {
  const hasTruncation = detail.short_names_truncated || detail.categories_truncated || detail.versions_truncated ||
    detail.stored_versions_truncated || detail.conditions_truncated
  return (
    <div className="crd-detail detail-overview">
      <dl className="detail-grid">
        <div><dt>API 组</dt><dd className="mono" title={detail.group}>{detail.group}</dd></div>
        <div><dt>资源</dt><dd className="mono">{detail.resource}</dd></div>
        <div><dt>作用域</dt><dd>{detail.scope}</dd></div>
        <div><dt>Kind</dt><dd className="mono">{detail.kind}</dd></div>
        <div><dt>List Kind</dt><dd className="mono">{detail.list_kind}</dd></div>
        <div><dt>单数名称</dt><dd className="mono">{detail.singular}</dd></div>
        <div><dt>转换策略</dt><dd>{detail.conversion_strategy}{detail.conversion_strategy_defaulted ? '（默认）' : ''}</dd></div>
        <div><dt>控制器观测</dt><dd className="mono">{detail.observed_generation} / {detail.generation}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
      <NameSummary detail={detail} />
      {hasTruncation && <div className="access-truncated" role="status">详情已按安全上限截断</div>}
      <VersionSummary detail={detail} />
      <ConditionSummary detail={detail} />
    </div>
  )
}

function NameSummary({ detail }: { detail: KubernetesCustomResourceDefinitionDetail }) {
  return (
    <section className="detail-section">
      <h3>发现名称</h3>
      <dl className="detail-grid">
        <div>
          <dt>短名称</dt>
          <dd>{detail.short_names.length ? <span className="inline-labels">{detail.short_names.map((name) => <span className="kind-label" key={name}>{name}</span>)}</span> : '-'}</dd>
        </div>
        <div>
          <dt>类别</dt>
          <dd>{detail.categories.length ? <span className="inline-labels">{detail.categories.map((name) => <span className="kind-label" key={name}>{name}</span>)}</span> : '-'}</dd>
        </div>
        <div><dt>持久化版本</dt><dd className="mono">{detail.stored_versions.join(', ') || '-'}</dd></div>
      </dl>
    </section>
  )
}

function VersionSummary({ detail }: { detail: KubernetesCustomResourceDefinitionDetail }) {
  return (
    <section className="detail-section">
      <div className="access-detail-heading">
        <h3>版本</h3>
        <span>显示 {detail.versions.length} / {detail.version_count} 个版本</span>
      </div>
      <div className="table-wrap" role="region" aria-label="CRD 版本" tabIndex={0}>
        <table className="detail-table crd-version-table">
          <thead><tr><th>版本</th><th>提供服务</th><th>存储版本</th><th>持久化状态</th><th>生命周期</th></tr></thead>
          <tbody>{detail.versions.map((version) => (
            <tr key={version.name}>
              <td className="mono"><strong>{version.name}</strong></td>
              <td>{version.served ? '已提供' : <span className="detail-muted">未提供</span>}</td>
              <td>{version.storage ? <span className="kind-label">当前存储</span> : '-'}</td>
              <td>{detail.stored_versions.includes(version.name) ? '已有持久化对象' : '-'}</td>
              <td>{version.deprecated ? <span className="scheduling-disabled">已弃用</span> : '有效'}</td>
            </tr>
          ))}</tbody>
        </table>
      </div>
    </section>
  )
}

function ConditionSummary({ detail }: { detail: KubernetesCustomResourceDefinitionDetail }) {
  return (
    <section className="detail-section">
      <div className="access-detail-heading">
        <h3>控制器条件</h3>
        <span>显示 {detail.conditions.length} / {detail.condition_count} 个条件</span>
      </div>
      {detail.conditions.length === 0 ? <span className="detail-muted">暂无条件</span> : (
        <div className="table-wrap" role="region" aria-label="CRD 控制器条件" tabIndex={0}>
          <table className="detail-table crd-condition-table">
            <thead><tr><th>类型</th><th>状态</th><th>原因</th><th>观测代次</th><th>变化时间</th></tr></thead>
            <tbody>{detail.conditions.map((condition) => (
              <tr key={condition.type}>
                <td><strong>{condition.type}</strong></td>
                <td><StatusBadge status={condition.status} /></td>
                <td className="mono">{condition.reason || '-'}</td>
                <td className="mono">{condition.observed_generation || '-'}</td>
                <td>{formatDateTime(condition.last_transition_time)}</td>
              </tr>
            ))}</tbody>
          </table>
        </div>
      )}
    </section>
  )
}
