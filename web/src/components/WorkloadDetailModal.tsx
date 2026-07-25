import { Copy, Download, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api } from '../api'
import type { KubernetesEvent, PodLogs, Workload, WorkloadDetail } from '../types'
import { formatDateTime } from '../utils'
import { EmptyState, ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

type DetailTab = 'overview' | 'events' | 'yaml' | 'logs'

interface WorkloadDetailModalProps {
  clusterId: string
  workload: Workload
  open: boolean
  onClose: () => void
}

export function WorkloadDetailModal({ clusterId, workload, open, onClose }: WorkloadDetailModalProps) {
  const [tab, setTab] = useState<DetailTab>('overview')
  const [detail, setDetail] = useState<WorkloadDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<unknown>(null)
  const [events, setEvents] = useState<KubernetesEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<unknown>(null)
  const [selectedContainer, setSelectedContainer] = useState('')
  const [tailLines, setTailLines] = useState(200)
  const [previous, setPrevious] = useState(false)
  const [logs, setLogs] = useState<PodLogs | null>(null)
  const [logsLoading, setLogsLoading] = useState(false)
  const [logsError, setLogsError] = useState<unknown>(null)
  const [copied, setCopied] = useState(false)
  const logRequestRef = useRef<AbortController | null>(null)

  const resourcePath = useMemo(() => {
    const segments = [clusterId, workload.kind.toLowerCase(), workload.namespace, workload.name].map(encodeURIComponent)
    return `/api/v1/clusters/${segments[0]}/workloads/${segments[1]}/${segments[2]}/${segments[3]}`
  }, [clusterId, workload.kind, workload.name, workload.namespace])

  useEffect(() => {
    if (!open) {
      logRequestRef.current?.abort()
      logRequestRef.current = null
      return
    }
    const controller = new AbortController()
    let active = true
    logRequestRef.current?.abort()
    logRequestRef.current = null
    setTab('overview')
    setDetail(null)
    setDetailLoading(true)
    setDetailError(null)
    setEvents([])
    setEventsLoading(true)
    setEventsError(null)
    setLogs(null)
    setLogsError(null)
    setSelectedContainer('')
    setPrevious(false)
    setTailLines(200)
    setCopied(false)

    api.get<WorkloadDetail>(resourcePath, controller.signal)
      .then((value) => { if (active) setDetail(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setDetailError(error)
      })
      .finally(() => { if (active) setDetailLoading(false) })
    api.get<KubernetesEvent[]>(`${resourcePath}/events?limit=50`, controller.signal)
      .then((value) => { if (active) setEvents(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setEventsError(error)
      })
      .finally(() => { if (active) setEventsLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [open, resourcePath])

  useEffect(() => () => {
    logRequestRef.current?.abort()
    logRequestRef.current = null
  }, [])

  useEffect(() => {
    if (!detail?.containers.length) return
    if (!detail.containers.some((container) => container.name === selectedContainer)) {
      setSelectedContainer((detail.containers.find((container) => container.type === 'container') ?? detail.containers[0]).name)
    }
  }, [detail, selectedContainer])

  const loadLogs = useCallback(async () => {
    if (!open || workload.kind !== 'Pod' || !selectedContainer) return
    logRequestRef.current?.abort()
    const controller = new AbortController()
    logRequestRef.current = controller
    const query = new URLSearchParams({
      container: selectedContainer,
      tail_lines: String(tailLines),
      previous: String(previous),
      timestamps: 'true',
    })
    const segments = [clusterId, workload.namespace, workload.name].map(encodeURIComponent)
    setLogsLoading(true)
    setLogsError(null)
    try {
      const response = await api.get<PodLogs>(
        `/api/v1/clusters/${segments[0]}/pods/${segments[1]}/${segments[2]}/logs?${query}`,
        controller.signal,
      )
      if (logRequestRef.current === controller) setLogs(response)
    } catch (error) {
      if (logRequestRef.current === controller && !(error instanceof DOMException && error.name === 'AbortError')) {
        setLogsError(error)
      }
    } finally {
      if (logRequestRef.current === controller) {
        logRequestRef.current = null
        setLogsLoading(false)
      }
    }
  }, [clusterId, open, previous, selectedContainer, tailLines, workload.kind, workload.name, workload.namespace])

  useEffect(() => {
    if (tab === 'logs') {
      void loadLogs()
      return
    }
    logRequestRef.current?.abort()
    logRequestRef.current = null
  }, [loadLogs, tab])

  const tabs: Array<{ id: DetailTab; label: string }> = [
    { id: 'overview', label: '概览' },
    { id: 'events', label: '事件' },
    { id: 'yaml', label: 'YAML' },
  ]
  if (workload.kind === 'Pod') tabs.push({ id: 'logs', label: '日志' })

  const copyYAML = async () => {
    if (!detail || !navigator.clipboard?.writeText) return
    try {
      await navigator.clipboard.writeText(detail.yaml)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Modal title={`${workload.kind} · ${workload.name}`} open={open} onClose={onClose} width="large">
      {detailLoading ? <LoadingState label="正在读取资源详情" /> : detailError ? (
        <ErrorState error={detailError} />
      ) : detail ? (
        <div className="workload-detail">
          <div className="detail-tabs" role="tablist" aria-label="资源详情视图">
            {tabs.map((item) => (
              <button
                type="button"
                role="tab"
                key={item.id}
                aria-selected={tab === item.id}
                className={tab === item.id ? 'active' : ''}
                onClick={() => setTab(item.id)}
              >
                {item.label}
              </button>
            ))}
          </div>
          {tab === 'overview' && <Overview detail={detail} />}
          {tab === 'events' && (
            eventsLoading ? <LoadingState label="正在读取事件" /> : eventsError ? <ErrorState error={eventsError} /> : <Events events={events} />
          )}
          {tab === 'yaml' && (
            <div className="code-panel">
              <div className="code-toolbar">
                <span>脱敏清单</span>
                <div>
                  <button type="button" className="button button-secondary" onClick={() => void copyYAML()}><Copy size={15} /> {copied ? '已复制' : '复制'}</button>
                  <button type="button" className="button button-secondary" onClick={() => downloadText(`${detail.name}.yaml`, detail.yaml)}><Download size={15} /> 下载</button>
                </div>
              </div>
              <pre className="yaml-view">{detail.yaml}</pre>
            </div>
          )}
          {tab === 'logs' && (
            <div className="logs-panel">
              <div className="logs-toolbar">
                <div className="toolbar-field">
                  <label htmlFor="log-container">容器</label>
                  <select id="log-container" value={selectedContainer} onChange={(event) => setSelectedContainer(event.target.value)}>
                    {detail.containers.map((container) => <option key={`${container.type}:${container.name}`} value={container.name}>{container.name}{container.type === 'init' ? ' (init)' : ''}</option>)}
                  </select>
                </div>
                <div className="toolbar-field">
                  <label htmlFor="log-lines">行数</label>
                  <select id="log-lines" value={tailLines} onChange={(event) => setTailLines(Number(event.target.value))}>
                    {[100, 200, 500, 1000, 2000].map((value) => <option key={value} value={value}>{value}</option>)}
                  </select>
                </div>
                <label className="checkbox-field"><input type="checkbox" checked={previous} onChange={(event) => setPrevious(event.target.checked)} />上一实例</label>
                <div className="logs-actions">
                  <button type="button" className="icon-button" aria-label="刷新日志" title="刷新日志" disabled={logsLoading || !selectedContainer} onClick={() => void loadLogs()}><RefreshCw size={17} className={logsLoading ? 'spin' : ''} /></button>
                  <button type="button" className="icon-button" aria-label="下载日志" title="下载日志" disabled={!logs} onClick={() => logs && downloadText(`${logs.pod}-${logs.container}.log`, logs.content)}><Download size={17} /></button>
                </div>
              </div>
              {logsLoading ? <LoadingState label="正在读取日志" /> : logsError ? <ErrorState error={logsError} onRetry={() => void loadLogs()} /> : logs ? (
                <>
                  {logs.truncated && <div className="log-warning" role="status">日志达到 2 MiB 响应上限，已截断</div>}
                  <pre className="log-view">{logs.content || '当前范围没有日志'}</pre>
                </>
              ) : <EmptyState title="请选择容器" />}
            </div>
          )}
        </div>
      ) : null}
    </Modal>
  )
}

function Overview({ detail }: { detail: WorkloadDetail }) {
  const labels = Object.entries(detail.labels)
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>状态</dt><dd><StatusBadge status={detail.status} /></dd></div>
        <div><dt>命名空间</dt><dd className="mono">{detail.namespace}</dd></div>
        <div><dt>就绪</dt><dd>{detail.ready}/{detail.desired}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
        <div><dt>UID</dt><dd className="mono">{detail.uid}</dd></div>
        <div><dt>Resource Version</dt><dd className="mono">{detail.resource_version}</dd></div>
      </dl>
      <section className="detail-section">
        <h3>标签</h3>
        {labels.length ? <div className="label-list">{labels.map(([key, value]) => <span key={key}><strong>{key}</strong>={value}</span>)}</div> : <span className="detail-muted">无标签</span>}
      </section>
      <section className="detail-section">
        <h3>容器</h3>
        {detail.containers.length ? <div className="table-wrap"><table className="detail-table">
          <thead><tr><th>名称</th><th>类型</th><th>状态</th><th>重启</th><th>镜像</th></tr></thead>
          <tbody>{detail.containers.map((container) => <tr key={`${container.type}:${container.name}`}>
            <td><strong>{container.name}</strong></td><td>{container.type}</td><td>{container.state || '-'}</td><td>{container.restart_count}</td><td className="mono clipped-cell" title={container.image}>{container.image}</td>
          </tr>)}</tbody>
        </table></div> : <span className="detail-muted">无容器</span>}
      </section>
      <section className="detail-section">
        <h3>条件</h3>
        {detail.conditions.length ? <div className="condition-list">{detail.conditions.map((condition) => (
          <div key={`${condition.type}:${condition.last_transition_time ?? ''}`}>
            <StatusBadge status={condition.status} /><strong>{condition.type}</strong><span>{condition.reason || '-'}</span><time>{formatDateTime(condition.last_transition_time)}</time>
            {condition.message && <p>{condition.message}</p>}
          </div>
        ))}</div> : <span className="detail-muted">无条件记录</span>}
      </section>
    </div>
  )
}

function Events({ events }: { events: KubernetesEvent[] }) {
  if (!events.length) return <EmptyState title="当前资源没有事件" />
  return (
    <div className="event-list">
      {events.map((event) => (
        <div key={event.name || `${event.reason}:${event.last_seen}`} className={event.type === 'Warning' ? 'event-row event-warning' : 'event-row'}>
          <div><StatusBadge status={event.type} /><strong>{event.reason || '-'}</strong><span>{event.source || '-'}</span></div>
          <p>{event.message || '-'}</p>
          <div><span>次数 {event.count}</span><time>{formatDateTime(event.last_seen)}</time></div>
        </div>
      ))}
    </div>
  )
}

function downloadText(filename: string, content: string) {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}
