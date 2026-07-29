import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type {
  KubernetesAdmissionWebhook,
  KubernetesAdmissionWebhookConfiguration,
  KubernetesAdmissionWebhookConfigurationDetail,
} from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface AdmissionWebhookConfigurationDetailModalProps {
  clusterId: string
  resource: KubernetesAdmissionWebhookConfiguration
  onClose: () => void
}

export function AdmissionWebhookConfigurationDetailModal({
  clusterId,
  resource,
  onClose,
}: AdmissionWebhookConfigurationDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesAdmissionWebhookConfigurationDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => {
    const cluster = encodeURIComponent(clusterId)
    const name = encodeURIComponent(resource.name)
    const query = new URLSearchParams({ kind: resource.kind })
    return `/api/v1/clusters/${cluster}/admission-webhook-configurations/${name}?${query}`
  }, [clusterId, resource.kind, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesAdmissionWebhookConfigurationDetail>(resourcePath, controller.signal)
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
    <Modal title={`准入 Webhook · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取准入 Webhook 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <AdmissionWebhookConfigurationDetailView detail={detail} /> : null}
    </Modal>
  )
}

function AdmissionWebhookConfigurationDetailView({
  detail,
}: {
  detail: KubernetesAdmissionWebhookConfigurationDetail
}) {
  return (
    <div className="admission-webhook-detail detail-overview">
      <dl className="detail-grid">
        <div><dt>类型</dt><dd>{admissionKindLabel(detail.kind)}</dd></div>
        <div><dt>Webhook</dt><dd>{detail.webhook_count}</dd></div>
        <div><dt>代次</dt><dd className="mono">{detail.generation}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
      <section className="detail-section">
        <div className="access-detail-heading">
          <h3>Webhook 配置</h3>
          <span>{detail.webhooks.length} 个</span>
        </div>
        <div className="table-wrap" role="region" aria-label="准入 Webhook 配置" tabIndex={0}>
          <table className="detail-table admission-webhook-detail-table">
            <thead><tr><th>Webhook</th><th>目标</th><th>失败策略</th><th>匹配策略</th><th>副作用</th><th>超时</th>{detail.kind === 'mutating' && <th>重新调用</th>}<th>CA</th><th>AdmissionReview</th><th>规则</th><th>选择器</th><th>匹配条件</th></tr></thead>
            <tbody>{detail.webhooks.map((webhook) => (
              <AdmissionWebhookRow key={webhook.name} kind={detail.kind} webhook={webhook} />
            ))}</tbody>
          </table>
        </div>
      </section>
    </div>
  )
}

function AdmissionWebhookRow({
  kind,
  webhook,
}: {
  kind: KubernetesAdmissionWebhookConfigurationDetail['kind']
  webhook: KubernetesAdmissionWebhook
}) {
  return (
    <tr>
      <td className="mono"><strong>{webhook.name}</strong></td>
      <td className="mono">{admissionWebhookTarget(webhook)}</td>
      <td>{defaultedValue(webhook.failure_policy, webhook.failure_policy_defaulted)}</td>
      <td>{defaultedValue(webhook.match_policy, webhook.match_policy_defaulted)}</td>
      <td>{webhook.side_effects}</td>
      <td>{defaultedValue(`${webhook.timeout_seconds} 秒`, webhook.timeout_seconds_defaulted)}</td>
      {kind === 'mutating' && <td>{defaultedValue(webhook.reinvocation_policy ?? '-', webhook.reinvocation_policy_defaulted)}</td>}
      <td>{webhook.ca_bundle_configured ? '已配置' : <span className="detail-muted">未配置</span>}</td>
      <td>{webhook.admission_review_versions.length
        ? <span className="inline-labels">{webhook.admission_review_versions.map((version) => <span className="kind-label" key={version}>{version}</span>)}</span>
        : '-'}</td>
      <td>{`${webhook.rule_count} 条规则 · ${webhook.operation_count} 个操作 · ${webhook.resource_count} 个资源`}</td>
      <td>{admissionWebhookSelectorSummary(webhook)}</td>
      <td>{webhook.match_condition_count}</td>
    </tr>
  )
}

function admissionKindLabel(kind: KubernetesAdmissionWebhookConfigurationDetail['kind']) {
  return kind === 'validating' ? 'Validating' : 'Mutating'
}

function admissionWebhookTarget(webhook: KubernetesAdmissionWebhook) {
  if (webhook.target_type === 'url') return '外部 URL'
  const target = `${webhook.service_namespace}/${webhook.service_name}:${webhook.service_port}`
  return defaultedValue(target, webhook.service_port_defaulted)
}

function defaultedValue(value: string, defaulted: boolean) {
  return defaulted ? `${value}（默认）` : value
}

function admissionWebhookSelectorSummary(webhook: KubernetesAdmissionWebhook) {
  const namespaceCount = webhook.namespace_selector_label_count + webhook.namespace_selector_expression_count
  const objectCount = webhook.object_selector_label_count + webhook.object_selector_expression_count
  return `命名空间 ${namespaceCount} · 对象 ${objectCount}`
}
