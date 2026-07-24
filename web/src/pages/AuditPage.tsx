import { RefreshCw, Search } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { useResource } from '../hooks'
import type { AuditEvent } from '../types'
import { formatDateTime } from '../utils'

export function AuditPage() {
  const [search, setSearch] = useState('')
  const events = useResource((signal) => api.get<AuditEvent[]>('/api/v1/audit-events?limit=100', signal), [])
  const visible = useMemo(() => {
    const query = search.trim().toLowerCase()
    if (!query) return events.data ?? []
    return (events.data ?? []).filter((event) => `${event.actor} ${event.action} ${event.target} ${event.request_id}`.toLowerCase().includes(query))
  }, [events.data, search])

  return (
    <div className="page">
      <PageHeader title="审计日志" meta="连接配置与集群写操作" actions={<button className="button button-secondary" onClick={() => void events.refresh()}><RefreshCw size={16} className={events.loading ? 'spin' : ''} /> 刷新</button>} />
      <section className="toolbar"><div className="search-field search-field-wide"><Search size={16} /><label className="sr-only" htmlFor="audit-search">搜索审计日志</label><input id="audit-search" type="search" placeholder="搜索用户、动作、目标或请求 ID" value={search} onChange={(event) => setSearch(event.target.value)} /></div></section>
      <section className="section-block table-section">
        {events.loading ? <LoadingState label="正在读取审计日志" /> : events.error ? <ErrorState error={events.error} onRetry={() => void events.refresh()} /> : visible.length === 0 ? <EmptyState title="没有匹配的审计事件" /> : (
          <div className="table-wrap"><table>
            <thead><tr><th>时间</th><th>操作者</th><th>动作</th><th>结果</th><th>目标</th><th>作用域</th><th>摘要</th><th>请求 ID</th></tr></thead>
            <tbody>{visible.map((event) => <tr key={event.id}>
              <td>{formatDateTime(event.created_at)}</td><td><strong>{event.actor}</strong></td><td className="mono">{event.action}</td><td><StatusBadge status={event.result} /></td><td>{event.target || '-'}</td><td><div className="primary-cell"><span>{event.namespace || '-'}</span><small className="mono subtle-id">{event.cluster_id || '-'}</small></div></td><td>{event.summary || '-'}</td><td className="mono subtle-id">{event.request_id}</td>
            </tr>)}</tbody>
          </table></div>
        )}
      </section>
    </div>
  )
}
