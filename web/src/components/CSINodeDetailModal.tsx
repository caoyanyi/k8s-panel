import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesCSINode, KubernetesCSINodeDetail } from '../types'
import { formatDateTime } from '../utils'
import { EmptyState, ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'

interface CSINodeDetailModalProps {
  clusterId: string
  resource: KubernetesCSINode
  onClose: () => void
}

export function CSINodeDetailModal({ clusterId, resource, onClose }: CSINodeDetailModalProps) {
  const [detail, setDetail] = useState<KubernetesCSINodeDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const resourcePath = useMemo(() => (
    `/api/v1/clusters/${encodeURIComponent(clusterId)}/csi-nodes/${encodeURIComponent(resource.name)}`
  ), [clusterId, resource.name])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setDetail(null)
    setLoading(true)
    setError(null)
    api.get<KubernetesCSINodeDetail>(resourcePath, controller.signal)
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
    <Modal title={`CSINode · ${resource.name}`} open onClose={onClose} width="large">
      {loading ? <LoadingState label="正在读取 CSINode 详情" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : detail ? <CSINodeDetailView detail={detail} /> : null}
    </Modal>
  )
}

function CSINodeDetailView({ detail }: { detail: KubernetesCSINodeDetail }) {
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>节点</dt><dd className="mono detail-value-wrap">{detail.name}</dd></div>
        <div><dt>已注册驱动</dt><dd>{detail.driver_count}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>
      <section className="detail-section" aria-label="CSI 驱动摘要">
        <h3>CSI 驱动</h3>
        {detail.drivers.length === 0 ? <EmptyState title="未记录 CSI 驱动" /> : (
          <div className="table-wrap" role="region" aria-label="CSINode 驱动清单" tabIndex={0}>
            <table className="csi-node-driver-table">
              <thead><tr><th>驱动名称</th><th>可调度卷上限</th><th>拓扑键数量</th></tr></thead>
              <tbody>{detail.drivers.map((driver) => (
                <tr key={driver.name}>
                  <td className="mono"><strong>{driver.name}</strong></td>
                  <td className="mono">
                    {driver.allocatable_count === undefined
                      ? <span className="detail-muted">未声明上限</span>
                      : driver.allocatable_count}
                  </td>
                  <td className="mono">{driver.topology_key_count}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>
    </div>
  )
}
