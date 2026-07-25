import type { KubernetesEvent } from '../types'
import { formatDateTime } from '../utils'
import { EmptyState } from './DataState'
import { StatusBadge } from './StatusBadge'

export function KubernetesEvents({ events }: { events: KubernetesEvent[] }) {
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
