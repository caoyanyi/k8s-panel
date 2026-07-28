import { RefreshCw, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { TablePagination, TABLE_PAGE_SIZE } from '../components/TablePagination'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { KubernetesEvent, Namespace } from '../types'
import { formatDateTime } from '../utils'

type EventTypeFilter = 'Warning' | ''

const EVENT_RESULT_LIMIT = 200

export function EventsPage() {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [eventType, setEventType] = useState<EventTypeFilter>('Warning')
  const [search, setSearch] = useState('')
  const [page, setPage] = useState(0)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const namespaces = useResource(
    (signal) => selectedClusterId
      ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal)
      : Promise.resolve([]),
    [selectedClusterId],
  )
  const events = useResource(
    (signal) => {
      if (!selectedClusterId) return Promise.resolve([])
      const query = new URLSearchParams()
      if (selectedNamespace) query.set('namespace', selectedNamespace)
      if (eventType) query.set('type', eventType)
      query.set('limit', String(EVENT_RESULT_LIMIT))
      return api.get<KubernetesEvent[]>(`/api/v1/clusters/${selectedClusterId}/events?${query}`, signal)
    },
    [selectedClusterId, selectedNamespace, eventType],
  )

  useEffect(() => {
    if (selectedNamespace && namespaces.data && !namespaces.data.some((item) => item.name === selectedNamespace)) {
      setSelectedNamespace('')
    }
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const normalizedSearch = search.trim().toLowerCase()
  const visibleEvents = useMemo(() => (events.data ?? []).filter((event) => (
    !normalizedSearch || eventSearchText(event).toLowerCase().includes(normalizedSearch)
  )), [events.data, normalizedSearch])
  useEffect(() => setPage(0), [eventType, normalizedSearch, selectedClusterId, selectedNamespace])

  const totalPages = Math.max(1, Math.ceil(visibleEvents.length / TABLE_PAGE_SIZE))
  const currentPage = Math.min(page, totalPages - 1)
  const pageStart = currentPage * TABLE_PAGE_SIZE
  const pageItems = visibleEvents.slice(pageStart, pageStart + TABLE_PAGE_SIZE)
  const eventLabel = eventType === 'Warning' ? '警告事件' : '事件'

  return (
    <div className="page">
      <PageHeader
        title="集群事件"
        meta={selectedCluster ? `${selectedCluster.name} · ${events.data?.length ?? 0} 条${eventLabel}` : '选择一个集群'}
        actions={(
          <button
            type="button"
            className="button button-secondary"
            disabled={!selectedClusterId || events.loading}
            onClick={() => void events.refresh()}
          >
            <RefreshCw size={16} className={events.loading ? 'spin' : ''} /> 刷新
          </button>
        )}
      />
      {selectedCluster?.environment === 'production' && (
        <div className="production-banner"><strong>生产环境</strong><span>{selectedCluster.name}</span></div>
      )}
      {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : (
        <>
          <div className="segmented-control" role="group" aria-label="事件类型">
            <button
              type="button"
              className={eventType === 'Warning' ? 'active' : ''}
              aria-pressed={eventType === 'Warning'}
              onClick={() => setEventType('Warning')}
            >警告事件</button>
            <button
              type="button"
              className={eventType === '' ? 'active' : ''}
              aria-pressed={eventType === ''}
              onClick={() => setEventType('')}
            >全部事件</button>
          </div>
          <section className="toolbar" aria-label="事件筛选">
            <div className="toolbar-field">
              <label htmlFor="event-namespace">命名空间</label>
              <select
                id="event-namespace"
                value={selectedNamespace}
                onChange={(event) => setSelectedNamespace(event.target.value)}
                disabled={namespaces.loading || Boolean(namespaces.error)}
              >
                <option value="">全部命名空间</option>
                {namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
              </select>
            </div>
            {Boolean(namespaces.error) && <span className="toolbar-status-error" role="status">命名空间列表不可用</span>}
            <div className="search-field">
              <Search size={16} aria-hidden="true" />
              <label className="sr-only" htmlFor="event-search">搜索事件</label>
              <input
                id="event-search"
                type="search"
                placeholder="搜索事件"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
              />
            </div>
          </section>
          <section className="section-block table-section">
            {events.loading ? <LoadingState label={`正在读取${eventLabel}`} />
              : events.error ? <ErrorState error={events.error} onRetry={() => void events.refresh()} />
                : visibleEvents.length === 0 ? <EmptyState title={normalizedSearch ? '没有匹配的事件' : `当前范围没有${eventLabel}`} />
                  : (
                    <>
                      <EventTable events={pageItems} />
                      <TablePagination page={currentPage} totalItems={visibleEvents.length} onPage={setPage} />
                    </>
                  )}
          </section>
        </>
      )}
    </div>
  )
}

function EventTable({ events }: { events: KubernetesEvent[] }) {
  return (
    <div className="table-wrap" role="region" aria-label="Kubernetes 事件清单" tabIndex={0}>
      <table className="event-table">
        <thead><tr><th>最近时间</th><th>类型</th><th>原因</th><th>命名空间</th><th>涉及对象</th><th>次数</th><th>来源</th><th>消息</th></tr></thead>
        <tbody>{events.map((event) => (
          <tr key={`${event.namespace ?? ''}:${event.name}`}>
            <td><time>{formatDateTime(event.last_seen ?? event.created_at)}</time></td>
            <td><StatusBadge status={event.type || 'Unknown'} /></td>
            <td><strong>{displayValue(event.reason)}</strong></td>
            <td className="mono">{displayValue(event.namespace)}</td>
            <td className="mono clipped-cell" title={eventObject(event)}>{eventObject(event)}</td>
            <td>{event.count}</td>
            <td className="mono clipped-cell" title={event.source}>{displayValue(event.source)}</td>
            <td className="event-message-cell" title={event.message}>
              <span>{displayValue(event.message)}</span>
              {event.message_truncated && <span className="kind-label">已截断</span>}
            </td>
          </tr>
        ))}</tbody>
      </table>
    </div>
  )
}

function eventSearchText(event: KubernetesEvent) {
  return [
    event.namespace, event.name, event.type, event.reason, event.message,
    event.source, event.object_kind, event.object_name,
  ].filter(Boolean).join(' ')
}

function eventObject(event: KubernetesEvent) {
  if (event.object_kind && event.object_name) return `${event.object_kind}/${event.object_name}`
  return event.object_name || event.object_kind || '-'
}

function displayValue(value?: string) {
  return value || '-'
}
