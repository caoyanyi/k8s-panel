import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesPriorityClass, KubernetesPriorityClassDetail } from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface PriorityClassDetailModalProps {
  clusterId: string
  resource: KubernetesPriorityClass
  onClose: () => void
}

export function PriorityClassDetailModal({ clusterId, resource, onClose }: PriorityClassDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesPriorityClassDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/priority-classes/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesPriorityClassDetail>(resourcePath, controller.signal)
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
    <Modal title={`PriorityClass · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取 PriorityClass 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <PriorityClassDetailView detail={detail} /> : null}
    </Modal>
  )
}

function PriorityClassDetailView({ detail }: { detail: KubernetesPriorityClassDetail }) {
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>整数优先级</dt><dd className="mono">{formatPriorityValue(detail.value)}</dd></div>
        <div><dt>全局默认</dt><dd>{detail.global_default ? '是' : '否'}</dd></div>
        <div>
          <dt>抢占策略</dt>
          <dd className="mono detail-value-wrap">
            {detail.preemption_policy}{detail.preemption_policy_defaulted ? '（默认）' : ''}
          </dd>
        </div>
        <div><dt>配置来源</dt><dd>{detail.preemption_policy_defaulted ? '默认值' : '显式配置'}</dd></div>
        <div><dt>作用域</dt><dd>集群级</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
    </div>
  )
}

function formatPriorityValue(value: number): string {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 }).format(value)
}
