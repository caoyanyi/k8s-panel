import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type {
  KubernetesCertificateSigningRequest,
  KubernetesCertificateSigningRequestDetail,
  KubernetesCertificateSigningRequestState,
} from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

interface CertificateSigningRequestDetailModalProps {
  clusterId: string
  resource: KubernetesCertificateSigningRequest
  onClose: () => void
}

export function CertificateSigningRequestDetailModal({
  clusterId,
  resource,
  onClose,
}: CertificateSigningRequestDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesCertificateSigningRequestDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/certificate-signing-requests/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesCertificateSigningRequestDetail>(resourcePath, controller.signal)
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
    <Modal title={`证书请求 · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取证书请求详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <CertificateSigningRequestDetailView detail={detail} /> : null}
    </Modal>
  )
}

function CertificateSigningRequestDetailView({ detail }: { detail: KubernetesCertificateSigningRequestDetail }) {
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>状态</dt><dd><CertificateSigningRequestStatus state={detail.state} /></dd></div>
        <div><dt>请求者</dt><dd className="mono" title={detail.requester}>{detail.requester}</dd></div>
        <div><dt>签名器</dt><dd className="mono" title={detail.signer_name}>{detail.signer_name}</dd></div>
        <div><dt>请求有效期</dt><dd>{formatRequestedDuration(detail.requested_expiration_seconds)}</dd></div>
        <div><dt>证书状态</dt><dd>{detail.certificate_issued ? '已写入' : '未写入'}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
      <section className="detail-section">
        <h3>请求用途</h3>
        <div className="inline-labels">
          {detail.usages.map((usage) => <span className="kind-label" key={usage}>{usage}</span>)}
        </div>
      </section>
      <section className="detail-section">
        <div className="access-detail-heading">
          <h3>状态条件</h3>
          <span>{detail.condition_count} 个条件</span>
        </div>
        {detail.conditions.length === 0 ? <span className="detail-muted">暂无条件</span> : (
          <div className="table-wrap" role="region" aria-label="证书请求状态条件" tabIndex={0}>
            <table className="detail-table csr-condition-table">
              <thead><tr><th>类型</th><th>状态</th><th>原因</th><th>更新时间</th><th>变化时间</th></tr></thead>
              <tbody>{detail.conditions.map((condition) => (
                <tr key={condition.type}>
                  <td><strong>{condition.type}</strong></td>
                  <td><StatusBadge status={condition.status} /></td>
                  <td className="mono">{condition.reason || '-'}</td>
                  <td>{condition.last_update_time ? formatDateTime(condition.last_update_time) : '-'}</td>
                  <td>{condition.last_transition_time ? formatDateTime(condition.last_transition_time) : '-'}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}

const certificateSigningRequestStateLabels: Record<KubernetesCertificateSigningRequestState, string> = {
  pending: '等待审批',
  approved: '已批准，等待签发',
  denied: '已拒绝',
  failed: '签发失败',
  issued: '已签发',
}

const certificateSigningRequestStateClasses: Record<KubernetesCertificateSigningRequestState, string> = {
  pending: 'status-pending',
  approved: 'status-progressing',
  denied: 'status-disabled',
  failed: 'status-failed',
  issued: 'status-ready',
}

function CertificateSigningRequestStatus({ state }: { state: KubernetesCertificateSigningRequestState }) {
  return (
    <span className={`status-badge ${certificateSigningRequestStateClasses[state]}`}>
      <span className="status-dot" aria-hidden="true" />
      {certificateSigningRequestStateLabels[state]}
    </span>
  )
}

function formatRequestedDuration(seconds?: number): string {
  if (seconds === undefined) return '由签名器决定'
  const parts: string[] = []
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainingSeconds = seconds % 60
  if (days > 0) parts.push(`${days} 天`)
  if (hours > 0) parts.push(`${hours} 小时`)
  if (minutes > 0) parts.push(`${minutes} 分钟`)
  if (remainingSeconds > 0 || parts.length === 0) parts.push(`${remainingSeconds} 秒`)
  return `${parts.join(' ')}（请求值）`
}
