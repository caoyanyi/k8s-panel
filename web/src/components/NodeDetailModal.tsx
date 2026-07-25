import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { KubernetesEvent, NodeDetail } from '../types'
import { formatDateTime } from '../utils'
import { ErrorState, LoadingState } from './DataState'
import { KubernetesEvents } from './KubernetesEvents'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

type NodeDetailTab = 'overview' | 'events'

interface NodeDetailModalProps {
  clusterId: string
  nodeName: string
  open: boolean
  onClose: () => void
}

export function NodeDetailModal({ clusterId, nodeName, open, onClose }: NodeDetailModalProps) {
  const [tab, setTab] = useState<NodeDetailTab>('overview')
  const [detail, setDetail] = useState<NodeDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<unknown>(null)
  const [events, setEvents] = useState<KubernetesEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<unknown>(null)
  const [eventsLoaded, setEventsLoaded] = useState(false)
  const resourcePath = useMemo(() => {
    const segments = [clusterId, nodeName].map(encodeURIComponent)
    return `/api/v1/clusters/${segments[0]}/nodes/${segments[1]}`
  }, [clusterId, nodeName])

  useEffect(() => {
    if (!open) return
    const controller = new AbortController()
    let active = true
    setTab('overview')
    setDetail(null)
    setDetailLoading(true)
    setDetailError(null)
    setEvents([])
    setEventsLoading(false)
    setEventsError(null)
    setEventsLoaded(false)

    api.get<NodeDetail>(resourcePath, controller.signal)
      .then((value) => { if (active) setDetail(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setDetailError(error)
      })
      .finally(() => { if (active) setDetailLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [open, resourcePath])

  useEffect(() => {
    if (!open || tab !== 'events' || eventsLoaded) return
    const controller = new AbortController()
    let active = true
    setEventsLoading(true)
    setEventsError(null)
    api.get<KubernetesEvent[]>(`${resourcePath}/events?limit=50`, controller.signal)
      .then((value) => { if (active) setEvents(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setEventsError(error)
      })
      .finally(() => {
        if (active) {
          setEventsLoading(false)
          setEventsLoaded(true)
        }
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [eventsLoaded, open, resourcePath, tab])

  return (
    <Modal title={`节点 · ${nodeName}`} open={open} onClose={onClose} width="large">
      {detailLoading ? <LoadingState label="正在读取节点详情" /> : detailError ? <ErrorState error={detailError} /> : detail ? (
        <div className="resource-detail">
          <div className="detail-tabs" role="tablist" aria-label="节点详情视图">
            <button type="button" role="tab" aria-selected={tab === 'overview'} className={tab === 'overview' ? 'active' : ''} onClick={() => setTab('overview')}>概览</button>
            <button type="button" role="tab" aria-selected={tab === 'events'} className={tab === 'events' ? 'active' : ''} onClick={() => setTab('events')}>事件</button>
          </div>
          {tab === 'overview' && <NodeOverview detail={detail} />}
          {tab === 'events' && (
            !eventsLoaded || eventsLoading ? <LoadingState label="正在读取节点事件" /> : eventsError ? <ErrorState error={eventsError} onRetry={() => setEventsLoaded(false)} /> : <KubernetesEvents events={events} />
          )}
        </div>
      ) : null}
    </Modal>
  )
}

function NodeOverview({ detail }: { detail: NodeDetail }) {
  const labels = Object.entries(detail.labels).sort(([left], [right]) => left.localeCompare(right))
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>名称</dt><dd title={detail.name}>{detail.name}</dd></div>
        <div><dt>状态</dt><dd><StatusBadge status={detail.status} /></dd></div>
        <div><dt>调度</dt><dd>{detail.unschedulable ? '已停止调度' : '可调度'}</dd></div>
        <div><dt>UID</dt><dd className="mono" title={detail.uid}>{detail.uid}</dd></div>
        <div><dt>Resource Version</dt><dd className="mono">{detail.resource_version}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
      </dl>

      <section className="detail-section">
        <h3>资源容量</h3>
        <div className="table-wrap" role="region" aria-label="节点资源容量" tabIndex={0}><table className="detail-table node-resource-table">
          <thead><tr><th>资源</th><th>可分配</th><th>容量</th></tr></thead>
          <tbody>
            <ResourceRow label="CPU" allocatable={detail.allocatable.cpu} capacity={detail.capacity.cpu} />
            <ResourceRow label="内存" allocatable={detail.allocatable.memory} capacity={detail.capacity.memory} />
            <ResourceRow label="Pods" allocatable={detail.allocatable.pods} capacity={detail.capacity.pods} />
            <ResourceRow label="临时存储" allocatable={detail.allocatable.ephemeral_storage} capacity={detail.capacity.ephemeral_storage} />
          </tbody>
        </table></div>
      </section>

      <section className="detail-section">
        <h3>系统信息</h3>
        <dl className="detail-grid node-system-grid">
          <div><dt>操作系统</dt><dd title={detail.system_info.os_image}>{detail.system_info.os_image || '-'}</dd></div>
          <div><dt>内核</dt><dd className="mono">{detail.system_info.kernel_version || '-'}</dd></div>
          <div><dt>容器运行时</dt><dd className="mono" title={detail.system_info.container_runtime_version}>{detail.system_info.container_runtime_version || '-'}</dd></div>
          <div><dt>Kubelet</dt><dd className="mono">{detail.system_info.kubelet_version || '-'}</dd></div>
          <div><dt>系统</dt><dd>{detail.system_info.operating_system || '-'}</dd></div>
          <div><dt>架构</dt><dd>{detail.system_info.architecture || '-'}</dd></div>
        </dl>
      </section>

      <section className="detail-section">
        <h3>地址</h3>
        {detail.addresses.length ? <div className="address-list">{detail.addresses.map((address) => (
          <div key={`${address.type}:${address.address}`}><span>{address.type}</span><strong className="mono">{address.address}</strong></div>
        ))}</div> : <span className="detail-muted">无地址</span>}
      </section>

      <section className="detail-section">
        <h3>污点</h3>
        {detail.taints.length ? <div className="table-wrap" role="region" aria-label="节点污点" tabIndex={0}><table className="detail-table">
          <thead><tr><th>键</th><th>值</th><th>效果</th><th>添加时间</th></tr></thead>
          <tbody>{detail.taints.map((taint) => <tr key={`${taint.key}:${taint.effect}`}>
            <td className="mono">{taint.key}</td><td className="mono">{taint.value || '-'}</td><td><span className="kind-label">{taint.effect}</span></td><td>{formatDateTime(taint.time_added)}</td>
          </tr>)}</tbody>
        </table></div> : <span className="detail-muted">无污点</span>}
      </section>

      <section className="detail-section">
        <h3>标签</h3>
        {labels.length ? <div className="label-list">{labels.map(([key, value]) => <span key={key}><strong>{key}</strong>{value ? `=${value}` : ''}</span>)}</div> : <span className="detail-muted">无标签</span>}
      </section>

      <section className="detail-section">
        <h3>条件</h3>
        {detail.conditions.length ? <div className="condition-list">{detail.conditions.map((condition) => (
          <div key={`${condition.type}:${condition.last_transition_time ?? ''}`}>
            <StatusBadge status={nodeConditionBadge(condition)} /><strong>{condition.type}</strong><span>{condition.reason || '-'}</span><time>{formatDateTime(condition.last_transition_time)}</time>
            {condition.message && <p>{condition.message}</p>}
          </div>
        ))}</div> : <span className="detail-muted">无条件记录</span>}
      </section>
    </div>
  )
}

function ResourceRow({ label, allocatable, capacity }: { label: string; allocatable?: string; capacity?: string }) {
  return <tr><td><strong>{label}</strong></td><td className="mono">{allocatable || '-'}</td><td className="mono">{capacity || '-'}</td></tr>
}

function nodeConditionBadge(condition: NodeDetail['conditions'][number]) {
  if (condition.type === 'Ready') {
    if (condition.status === 'True') return 'Ready'
    if (condition.status === 'False') return 'NotReady'
    return 'Unknown'
  }
  if (['MemoryPressure', 'DiskPressure', 'PIDPressure', 'NetworkUnavailable'].includes(condition.type)) {
    if (condition.status === 'False') return 'Normal'
    if (condition.status === 'True') return 'Warning'
    return 'Unknown'
  }
  return condition.status
}
