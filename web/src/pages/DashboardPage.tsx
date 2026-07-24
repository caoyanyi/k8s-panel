import { AlertTriangle, CheckCircle2, CircleX, Clock3, Server } from 'lucide-react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { AuditEvent, Operation } from '../types'
import { formatDateTime } from '../utils'

export function DashboardPage({ onOpenClusters }: { onOpenClusters: () => void }) {
  const { clusters, clustersLoading, clustersError, refreshClusters } = usePanel()
  const operations = useResource((signal) => api.get<Operation[]>('/api/v1/operations?limit=20', signal), [])
  const audits = useResource((signal) => api.get<AuditEvent[]>('/api/v1/audit-events?limit=8', signal), [])
  const connected = clusters.filter((cluster) => cluster.status === 'connected').length
  const attention = clusters.filter((cluster) => cluster.status === 'degraded' || cluster.status === 'unreachable').length
  const activeOperations = (operations.data ?? []).filter((operation) => operation.state === 'queued' || operation.state === 'running').length
  const failedOperations = (operations.data ?? []).filter((operation) => operation.state === 'failed' || operation.state === 'unknown').length

  return (
    <div className="page">
      <PageHeader title="运行总览" meta="多集群运行状态与最近变更" />
      <section className="summary-grid" aria-label="运行摘要">
        <Summary icon={Server} label="已接入集群" value={clusters.length} detail={`${connected} 个连接正常`} tone="neutral" />
        <Summary icon={CheckCircle2} label="连接正常" value={connected} detail={attention ? `${attention} 个需要处理` : '没有连接告警'} tone="success" />
        <Summary icon={Clock3} label="执行中操作" value={activeOperations} detail="排队与运行任务" tone="info" />
        <Summary icon={failedOperations ? CircleX : CheckCircle2} label="异常操作" value={failedOperations} detail="失败或结果待确认" tone={failedOperations ? 'danger' : 'success'} />
      </section>

      <section className="section-block">
        <div className="section-heading"><h2>集群状态</h2><button className="text-button" onClick={onOpenClusters}>查看全部</button></div>
        {clustersLoading ? <LoadingState label="正在读取集群" /> : clustersError ? (
          <ErrorState error={clustersError} onRetry={() => void refreshClusters()} />
        ) : clusters.length === 0 ? (
          <EmptyState
            title="尚未接入集群"
            action={<button type="button" className="button button-primary" onClick={onOpenClusters}>接入集群</button>}
          />
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>集群</th><th>环境</th><th>状态</th><th>版本</th><th>最近检测</th></tr></thead>
              <tbody>{clusters.slice(0, 6).map((cluster) => (
                <tr key={cluster.id}>
                  <td><div className="primary-cell"><strong>{cluster.name}</strong><span>{cluster.server}</span></div></td>
                  <td>{environmentLabel(cluster.environment)}</td>
                  <td><StatusBadge status={cluster.status} /></td>
                  <td className="mono">{cluster.version || '-'}</td>
                  <td>{formatDateTime(cluster.last_checked_at)}</td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      <div className="two-column-bands">
        <section className="section-block">
          <div className="section-heading"><h2>最近操作</h2></div>
          {operations.loading ? <LoadingState /> : operations.error ? <ErrorState error={operations.error} /> : !operations.data?.length ? (
            <EmptyState title="暂无操作记录" />
          ) : (
            <div className="compact-list">{operations.data.slice(0, 6).map((operation) => (
              <div className="compact-row" key={operation.id}>
                <div><strong>{operation.target}</strong><span>{operation.kind} · {operation.namespace}</span></div>
                <StatusBadge status={operation.state} />
              </div>
            ))}</div>
          )}
        </section>
        <section className="section-block">
          <div className="section-heading"><h2>审计动态</h2></div>
          {audits.loading ? <LoadingState /> : audits.error ? <ErrorState error={audits.error} /> : !audits.data?.length ? (
            <EmptyState title="暂无审计事件" />
          ) : (
            <div className="compact-list">{audits.data.map((event) => (
              <div className="compact-row" key={event.id}>
                <div><strong>{event.action}</strong><span>{event.actor} · {event.target || '-'}</span></div>
                <time>{formatDateTime(event.created_at)}</time>
              </div>
            ))}</div>
          )}
        </section>
      </div>
    </div>
  )
}

function Summary({ icon: Icon, label, value, detail, tone }: {
  icon: typeof AlertTriangle
  label: string
  value: number
  detail: string
  tone: 'neutral' | 'success' | 'info' | 'danger'
}) {
  return (
    <div className={`summary-item summary-${tone}`}>
      <div><span>{label}</span><strong>{value}</strong><small>{detail}</small></div>
      <Icon size={21} aria-hidden="true" />
    </div>
  )
}

function environmentLabel(environment: string) {
  return ({ development: '开发', staging: '预发', production: '生产' } as Record<string, string>)[environment] ?? environment
}
