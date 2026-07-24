import type { ReactNode } from 'react'

export function PageHeader({ title, meta, actions }: { title: string; meta?: string; actions?: ReactNode }) {
  return (
    <div className="page-header">
      <div>
        <h1>{title}</h1>
        {meta && <p>{meta}</p>}
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  )
}
