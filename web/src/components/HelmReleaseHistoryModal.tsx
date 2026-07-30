import { RotateCcw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import type { HelmRelease, HelmReleaseHistory } from '../types'
import { formatDateTime } from '../utils'
import { EmptyState, ErrorState, LoadingState } from './DataState'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

interface HelmReleaseHistoryModalProps {
  clusterId: string
  release: HelmRelease
  onClose: () => void
  onRollback: (revision: number) => void
}

export function HelmReleaseHistoryModal({ clusterId, release, onClose, onRollback }: HelmReleaseHistoryModalProps) {
  const [history, setHistory] = useState<HelmReleaseHistory | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<unknown>(null)
  const [attempt, setAttempt] = useState(0)
  const historyPath = useMemo(() => {
    const query = new URLSearchParams({ cluster_id: clusterId, namespace: release.namespace })
    return `/api/v1/helm-releases/${encodeURIComponent(release.name)}/history?${query}`
  }, [clusterId, release.name, release.namespace])

  useEffect(() => {
    const controller = new AbortController()
    let active = true
    setHistory(null)
    setLoading(true)
    setError(null)
    api.get<HelmReleaseHistory>(historyPath, controller.signal)
      .then((value) => { if (active) setHistory(value) })
      .catch((caught: unknown) => {
        if (active && !(caught instanceof DOMException && caught.name === 'AbortError')) setError(caught)
      })
      .finally(() => { if (active) setLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [attempt, historyPath])

  return (
    <Modal title={`修订历史 · ${release.name}`} open onClose={onClose} width="wide">
      {loading ? <LoadingState label="正在读取 Release 修订历史" />
        : error ? <ErrorState error={error} onRetry={() => setAttempt((value) => value + 1)} />
          : history ? <HistoryView history={history} currentRevision={release.revision} onRollback={onRollback} /> : null}
    </Modal>
  )
}

function HistoryView({ history, currentRevision, onRollback }: {
  history: HelmReleaseHistory
  currentRevision: number
  onRollback: (revision: number) => void
}) {
  if (!history.revisions.length) return <EmptyState title="没有可用的修订历史" />
  return (
    <>
      {history.truncated && <div className="inventory-alert" role="status">仅显示最近 10 个修订</div>}
      <div className="table-wrap helm-history-table">
        <table>
          <thead><tr><th>Revision</th><th>状态</th><th>存储时间</th><th className="operation-action-column">操作</th></tr></thead>
          <tbody>{history.revisions.map((revision) => {
            const current = revision.revision === currentRevision
            return <tr key={revision.revision}>
              <td><strong>{revision.revision}</strong></td>
              <td><StatusBadge status={revision.status} /></td>
              <td>{formatDateTime(revision.created_at)}</td>
              <td className="operation-action-column">
                {current
                  ? <span className="kind-label">当前</span>
                  : <button
                    type="button"
                    className="icon-button"
                    title={`回滚到 revision ${revision.revision}`}
                    aria-label={`选择 revision ${revision.revision} 回滚`}
                    onClick={() => onRollback(revision.revision)}
                  ><RotateCcw size={16} /></button>}
              </td>
            </tr>
          })}</tbody>
        </table>
      </div>
    </>
  )
}
