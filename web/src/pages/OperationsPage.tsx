import { CircleX, Gauge, LoaderCircle, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { api, errorMessage } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { useResource } from '../hooks'
import type { Operation, OperationCapacity } from '../types'
import { formatDateTime } from '../utils'

export function OperationsPage({ notify }: {
  notify: (tone: 'success' | 'error', message: string) => void
}) {
  const operations = useResource((signal) => api.get<Operation[]>('/api/v1/operations?limit=100', signal), [])
  const capacity = useResource((signal) => api.get<OperationCapacity>('/api/v1/system/resources', signal), [])
  const refreshInFlight = useRef(false)
  const cancelRequestRef = useRef<AbortController | null>(null)
  const [cancelingId, setCancelingId] = useState('')

  const refreshAll = useCallback(async () => {
    if (refreshInFlight.current || operations.loading || capacity.loading || cancelingId !== '') return
    refreshInFlight.current = true
    try {
      await Promise.allSettled([operations.refresh(), capacity.refresh()])
    } finally {
      refreshInFlight.current = false
    }
  }, [cancelingId, capacity.loading, capacity.refresh, operations.loading, operations.refresh])

  useEffect(() => {
    const refresh = () => {
      if (!document.hidden) void refreshAll()
    }
    const onVisibilityChange = () => {
      refresh()
    }
    const timer = window.setInterval(refresh, 5000)
    document.addEventListener('visibilitychange', onVisibilityChange)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', onVisibilityChange)
    }
  }, [refreshAll])

  useEffect(() => () => {
    cancelRequestRef.current?.abort()
    cancelRequestRef.current = null
  }, [])

  const cancelOperation = async (operation: Operation) => {
    if (operation.state !== 'queued' || cancelingId !== '') return
    const controller = new AbortController()
    cancelRequestRef.current = controller
    setCancelingId(operation.id)
    try {
      await api.post<Operation>(
        `/api/v1/operations/${encodeURIComponent(operation.id)}/cancellations`, {}, controller.signal,
      )
      notify('success', `任务 ${operation.id} 已取消`)
      await Promise.allSettled([operations.refresh(), capacity.refresh()])
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) {
        notify('error', errorMessage(error))
      }
    } finally {
      if (cancelRequestRef.current === controller) {
        cancelRequestRef.current = null
        setCancelingId('')
      }
    }
  }

  return (
    <div className="page">
      <PageHeader
        title="操作中心"
        meta="异步任务与资源准入"
        actions={<button className="button button-secondary" onClick={() => void refreshAll()} disabled={operations.loading || capacity.loading}><RefreshCw size={16} className={operations.loading || capacity.loading ? 'spin' : ''} /> 刷新</button>}
      />
      <OperationCapacityBand capacity={capacity.data} loading={capacity.loading} error={capacity.error} onRetry={() => void capacity.refresh()} />
      <section className="section-block table-section">
        {operations.loading && !operations.data ? <LoadingState label="正在读取操作记录" /> : operations.error ? <ErrorState error={operations.error} onRetry={() => void operations.refresh()} /> : !operations.data?.length ? <EmptyState title="暂无操作记录" /> : (
          <div className="table-wrap" role="region" aria-label="操作记录" tabIndex={0}><table>
            <thead><tr><th>目标</th><th>动作</th><th>状态</th><th>作用域</th><th>提交人</th><th>提交时间</th><th>结果</th><th>请求 ID</th><th className="operation-action-column">操作</th></tr></thead>
            <tbody>{operations.data.map((operation) => <tr key={operation.id}>
              <td><div className="primary-cell"><strong>{operation.target}</strong><span className="mono subtle-id">{operation.id}</span></div></td>
              <td>{operationLabel(operation.kind)}</td>
              <td><StatusBadge status={operation.state} /></td>
              <td><div className="primary-cell"><span>{operation.namespace}</span><small className="mono">{operation.cluster_id}</small></div></td>
              <td>{operation.submitted_by}</td>
              <td>{formatDateTime(operation.created_at)}</td>
              <td>{operation.error_code ? <span className="error-code">{operation.error_code}</span> : operation.summary || '-'}</td>
              <td className="mono subtle-id">{operation.request_id}</td>
              <td className="operation-action-column">{operation.state === 'queued' && (
                <button
                  type="button"
                  className="icon-button icon-danger"
                  aria-label={`取消任务 ${operation.id}`}
                  title="取消排队任务"
                  disabled={cancelingId !== ''}
                  onClick={() => void cancelOperation(operation)}
                >
                  {cancelingId === operation.id ? <LoaderCircle className="spin" size={17} /> : <CircleX size={17} />}
                </button>
              )}</td>
            </tr>)}</tbody>
          </table></div>
        )}
      </section>
    </div>
  )
}

function OperationCapacityBand({ capacity, loading, error, onRetry }: {
  capacity: OperationCapacity | null
  loading: boolean
  error: unknown
  onRetry: () => void
}) {
  if (loading && !capacity) {
    return <section className="operation-capacity" aria-label="资源准入状态"><Gauge size={17} aria-hidden="true" /><span>正在读取资源状态</span></section>
  }
  if (error && !capacity) {
    return <section className="operation-capacity operation-capacity-error" aria-label="资源准入状态"><Gauge size={17} aria-hidden="true" /><span>资源状态暂不可用</span><button type="button" className="text-button" onClick={onRetry}>重试</button></section>
  }
  if (!capacity) return null

  return (
    <section className="operation-capacity" aria-label="资源准入状态">
      <div className="operation-capacity-state"><Gauge size={17} aria-hidden="true" /><StatusBadge status={capacity.pressure} /></div>
      <span>{`内存 ${formatRatio(capacity.memory_ratio)}`}</span>
      <span>{`负载 ${formatRatio(capacity.load_ratio)}`}</span>
      <span>{`执行槽 ${capacity.active_operations} / ${capacity.operation_limit}`}</span>
      <span>{`读取槽 ${capacity.kubernetes_reads.active} / ${capacity.kubernetes_reads.limit}`}</span>
      <span>{`连接缓存 ${capacity.kubernetes_clients.entries} / ${capacity.kubernetes_clients.capacity}`}</span>
      <span>{`队列 ${capacity.queue_depth} / ${capacity.queue_capacity}`}</span>
      {!capacity.adaptive && <span>自适应已关闭</span>}
    </section>
  )
}

function operationLabel(kind: string) {
  return ({
    'helm.install': 'Helm 安装',
    'helm.upgrade': 'Helm 升级',
    'helm.rollback': 'Helm 回滚',
    'helm.uninstall': 'Helm 卸载',
    'workload.scale': '工作负载扩缩容',
    'workload.restart': '工作负载滚动重启',
    'workload.image_update': '工作负载镜像更新',
  } as Record<string, string>)[kind] ?? kind
}

function formatRatio(value?: number) {
  if (value === undefined || !Number.isFinite(value)) return '-'
  return `${Math.round(Math.min(1, Math.max(0, value)) * 100)}%`
}
