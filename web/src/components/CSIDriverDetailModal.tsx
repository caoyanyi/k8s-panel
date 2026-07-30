import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesCSIDriver, KubernetesCSIDriverDetail } from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface CSIDriverDetailModalProps {
  clusterId: string
  resource: KubernetesCSIDriver
  onClose: () => void
}

export function CSIDriverDetailModal({ clusterId, resource, onClose }: CSIDriverDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesCSIDriverDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/csi-drivers/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesCSIDriverDetail>(resourcePath, controller.signal)
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
    <Modal title={`CSIDriver · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取 CSIDriver 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <CSIDriverDetailView detail={detail} /> : null}
    </Modal>
  )
}

function CSIDriverDetailView({ detail }: { detail: KubernetesCSIDriverDetail }) {
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>需要挂接</dt><dd>{booleanLabel(detail.attach_required)}</dd></div>
        <div><dt>挂载传递 Pod 信息</dt><dd>{booleanLabel(detail.pod_info_on_mount)}</dd></div>
        <div><dt>容量感知调度</dt><dd>{booleanLabel(detail.storage_capacity)}</dd></div>
        <div><dt>周期重新发布</dt><dd>{booleanLabel(detail.requires_republish)}</dd></div>
        <div><dt>SELinux 挂载</dt><dd>{booleanLabel(detail.se_linux_mount)}</dd></div>
        <div><dt>FSGroupPolicy</dt><dd className="mono detail-value-wrap">{detail.fs_group_policy}</dd></div>
        <div><dt>卷生命周期模式</dt><dd>{detail.volume_lifecycle_modes.join(' · ')}</dd></div>
        <div><dt>TokenRequest</dt><dd>{detail.token_request_count} 项</dd></div>
        <div><dt>作用域</dt><dd>集群级</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
    </div>
  )
}

function booleanLabel(value: boolean) {
  return value ? '是' : '否'
}
