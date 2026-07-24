import { RefreshCw } from 'lucide-react'
import { useEffect } from 'react'
import { api } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { useResource } from '../hooks'
import type { Operation } from '../types'
import { formatDateTime } from '../utils'

export function OperationsPage() {
  const operations = useResource((signal) => api.get<Operation[]>('/api/v1/operations?limit=100', signal), [])
  useEffect(() => {
    const timer = window.setInterval(() => void operations.refresh(), 5000)
    return () => window.clearInterval(timer)
  }, [operations.refresh])

  return (
    <div className="page">
      <PageHeader
        title="操作中心"
        meta="Helm 异步任务状态"
        actions={<button className="button button-secondary" onClick={() => void operations.refresh()} disabled={operations.loading}><RefreshCw size={16} className={operations.loading ? 'spin' : ''} /> 刷新</button>}
      />
      <section className="section-block table-section">
        {operations.loading && !operations.data ? <LoadingState label="正在读取操作记录" /> : operations.error ? <ErrorState error={operations.error} onRetry={() => void operations.refresh()} /> : !operations.data?.length ? <EmptyState title="暂无操作记录" /> : (
          <div className="table-wrap"><table>
            <thead><tr><th>目标</th><th>动作</th><th>状态</th><th>作用域</th><th>提交人</th><th>提交时间</th><th>结果</th><th>请求 ID</th></tr></thead>
            <tbody>{operations.data.map((operation) => <tr key={operation.id}>
              <td><div className="primary-cell"><strong>{operation.target}</strong><span className="mono subtle-id">{operation.id}</span></div></td>
              <td>{operationLabel(operation.kind)}</td>
              <td><StatusBadge status={operation.state} /></td>
              <td><div className="primary-cell"><span>{operation.namespace}</span><small className="mono">{operation.cluster_id}</small></div></td>
              <td>{operation.submitted_by}</td>
              <td>{formatDateTime(operation.created_at)}</td>
              <td>{operation.error_code ? <span className="error-code">{operation.error_code}</span> : operation.summary || '-'}</td>
              <td className="mono subtle-id">{operation.request_id}</td>
            </tr>)}</tbody>
          </table></div>
        )}
      </section>
    </div>
  )
}

function operationLabel(kind: string) {
  return ({
    'helm.install': 'Helm 安装',
    'helm.upgrade': 'Helm 升级',
    'helm.rollback': 'Helm 回滚',
    'helm.uninstall': 'Helm 卸载',
  } as Record<string, string>)[kind] ?? kind
}
