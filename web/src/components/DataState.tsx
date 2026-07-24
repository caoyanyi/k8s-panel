import { AlertTriangle, Inbox, RefreshCw } from 'lucide-react'
import type { ReactNode } from 'react'
import { errorMessage } from '../api'

export function LoadingState({ label = '正在加载' }: { label?: string }) {
  return (
    <div className="data-state" role="status">
      <RefreshCw className="spin" size={20} aria-hidden="true" />
      <span>{label}</span>
    </div>
  )
}

export function EmptyState({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <div className="data-state empty-state">
      <Inbox size={24} aria-hidden="true" />
      <strong>{title}</strong>
      {action}
    </div>
  )
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  return (
    <div className="data-state error-state" role="alert">
      <AlertTriangle size={22} aria-hidden="true" />
      <span>{errorMessage(error)}</span>
      {onRetry && (
        <button type="button" className="button button-secondary" onClick={onRetry}>
          <RefreshCw size={16} /> 重试
        </button>
      )}
    </div>
  )
}
