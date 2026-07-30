import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesRuntimeClass, KubernetesRuntimeClassDetail } from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface RuntimeClassDetailModalProps {
  clusterId: string
  resource: KubernetesRuntimeClass
  onClose: () => void
}

export function RuntimeClassDetailModal({ clusterId, resource, onClose }: RuntimeClassDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesRuntimeClassDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/runtime-classes/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesRuntimeClassDetail>(resourcePath, controller.signal)
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
    <Modal title={`RuntimeClass · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取 RuntimeClass 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <RuntimeClassDetailView detail={detail} /> : null}
    </Modal>
  )
}

function RuntimeClassDetailView({ detail }: { detail: KubernetesRuntimeClassDetail }) {
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>运行时 Handler</dt><dd className="mono detail-value-wrap">{detail.handler}</dd></div>
        <div><dt>Pod Overhead</dt><dd>{detail.overhead_configured ? '已配置' : '未配置'}</dd></div>
        <div><dt>CPU Overhead</dt><dd className="mono detail-value-wrap">{detail.pod_overhead_cpu ?? '未设置'}</dd></div>
        <div><dt>内存 Overhead</dt><dd className="mono detail-value-wrap">{detail.pod_overhead_memory ?? '未设置'}</dd></div>
        <div><dt>Overhead 资源项</dt><dd>{detail.overhead_resource_count} 项</dd></div>
        <div><dt>调度约束</dt><dd>{detail.scheduling_configured ? '已配置' : '未配置'}</dd></div>
        <div><dt>NodeSelector</dt><dd>{detail.node_selector_count} 项</dd></div>
        <div><dt>Toleration</dt><dd>{detail.toleration_count} 项</dd></div>
        <div><dt>作用域</dt><dd>集群级</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
    </div>
  )
}
