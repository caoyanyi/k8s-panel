import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type {
  KubernetesAdmissionMatchSummary,
  KubernetesAdmissionPolicyResource,
  KubernetesValidatingAdmissionPolicyBindingDetail,
  KubernetesValidatingAdmissionPolicyDetail,
} from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface AdmissionPolicyResourceDetailModalProps {
  clusterId: string
  resource: KubernetesAdmissionPolicyResource
  onClose: () => void
}

type AdmissionPolicyDetail = KubernetesValidatingAdmissionPolicyDetail | KubernetesValidatingAdmissionPolicyBindingDetail

export function AdmissionPolicyResourceDetailModal({
  clusterId,
  resource,
  onClose,
}: AdmissionPolicyResourceDetailModalProps) {
  const [detail, setDetail] = useState<AdmissionPolicyDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => {
    const collection = resource.kind === 'policy'
      ? 'validating-admission-policies'
      : 'validating-admission-policy-bindings'
    return `/api/v1/clusters/${encodeURIComponent(clusterId)}/${collection}/${encodeURIComponent(resource.name)}`
  }, [clusterId, resource.kind, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<AdmissionPolicyDetail>(resourcePath, controller.signal)
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

  const title = resource.kind === 'policy'
    ? `校验准入策略 · ${resource.name}`
    : `准入策略绑定 · ${resource.name}`
  return (
    <Modal title={title} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取准入策略详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail?.kind === 'policy' ? <AdmissionPolicyDetailView detail={detail} />
            : detail?.kind === 'binding' ? <AdmissionPolicyBindingDetailView detail={detail} /> : null}
    </Modal>
  )
}

function AdmissionPolicyDetailView({ detail }: { detail: KubernetesValidatingAdmissionPolicyDetail }) {
  return (
    <div className="admission-policy-detail detail-overview">
      <dl className="detail-grid">
        <div><dt>失败策略</dt><dd>{defaultedValue(detail.failure_policy, detail.failure_policy_defaulted)}</dd></div>
        <div><dt>参数类型</dt><dd className="mono">{detail.param_kind_configured ? `${detail.param_api_version} · ${detail.param_kind}` : '未配置'}</dd></div>
        <div><dt>匹配策略</dt><dd>{matchPolicy(detail.match)}</dd></div>
        <div><dt>匹配规则</dt><dd>{matchRuleSummary(detail.match)}</dd></div>
        <div><dt>选择器</dt><dd>{selectorSummary(detail.match)}</dd></div>
        <div><dt>策略内容</dt><dd>{`${detail.validation_count} 个校验 · ${detail.audit_annotation_count} 个审计注解`}</dd></div>
        <div><dt>组合条件</dt><dd>{`${detail.match_condition_count} 个匹配条件 · ${detail.variable_count} 个变量`}</dd></div>
        <div><dt>控制器观测</dt><dd className="mono">{detail.observed_generation} / {detail.generation}</dd></div>
        <div><dt>类型检查</dt><dd>{detail.type_checking_observed ? `已完成 · ${detail.expression_warning_count} 个警告` : '尚未报告'}</dd></div>
        <div><dt>控制器条件</dt><dd>{detail.condition_count}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
    </div>
  )
}

function AdmissionPolicyBindingDetailView({ detail }: { detail: KubernetesValidatingAdmissionPolicyBindingDetail }) {
  return (
    <div className="admission-policy-detail detail-overview">
      <dl className="detail-grid">
        <div><dt>引用策略</dt><dd className="mono" title={detail.policy_name}>{detail.policy_name}</dd></div>
        <div>
          <dt>执行动作</dt>
          <dd>{detail.validation_actions.length
            ? <span className="inline-labels">{detail.validation_actions.map((action) => <span className="kind-label" key={action}>{action}</span>)}</span>
            : '-'}</dd>
        </div>
        <div><dt>参数引用</dt><dd>{paramRefSummary(detail)}</dd></div>
        <div><dt>参数缺失</dt><dd>{detail.param_ref_configured ? detail.parameter_not_found_action : '-'}</dd></div>
        <div><dt>匹配策略</dt><dd>{matchPolicy(detail.match)}</dd></div>
        <div><dt>匹配规则</dt><dd>{matchRuleSummary(detail.match)}</dd></div>
        <div><dt>选择器</dt><dd>{selectorSummary(detail.match)}</dd></div>
        <div><dt>代次</dt><dd className="mono">{detail.generation}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
    </div>
  )
}

function defaultedValue(value: string, defaulted: boolean) {
  return defaulted ? `${value}（默认）` : value
}

function matchPolicy(match: KubernetesAdmissionMatchSummary) {
  if (!match.configured) return '继承策略范围'
  return defaultedValue(match.match_policy ?? 'Equivalent', match.match_policy_defaulted)
}

function matchRuleSummary(match: KubernetesAdmissionMatchSummary) {
  if (!match.configured) return '未限制'
  return `${match.resource_rule_count} 条包含 · ${match.exclude_resource_rule_count} 条排除 · ${match.resource_count} 个资源`
}

function selectorSummary(match: KubernetesAdmissionMatchSummary) {
  if (!match.configured) return '继承策略范围'
  const namespaces = match.namespace_selector_label_count + match.namespace_selector_expression_count
  const objects = match.object_selector_label_count + match.object_selector_expression_count
  return `命名空间 ${namespaces} · 对象 ${objects}`
}

function paramRefSummary(detail: KubernetesValidatingAdmissionPolicyBindingDetail) {
  if (!detail.param_ref_configured) return '未配置'
  const mode = detail.param_ref_mode === 'name'
    ? '按名称'
    : `按选择器（${detail.param_selector_label_count + detail.param_selector_expression_count} 项）`
  return detail.param_namespace ? `${mode} · ${detail.param_namespace}` : `${mode} · 请求命名空间`
}
