import { X } from 'lucide-react'
import { type ReactNode, useEffect, useRef } from 'react'

interface ModalProps {
  title: string
  open: boolean
  onClose: () => void
  children: ReactNode
  width?: 'normal' | 'wide'
}

export function Modal({ title, open, onClose, children, width = 'normal' }: ModalProps) {
  const dialogRef = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const previousFocus = document.activeElement as HTMLElement | null
    dialogRef.current?.focus()
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') onClose()
    }
    document.addEventListener('keydown', onKeyDown)
    return () => {
      document.removeEventListener('keydown', onKeyDown)
      previousFocus?.focus()
    }
  }, [open, onClose])

  if (!open) return null
  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <div
        ref={dialogRef}
        className={`modal modal-${width}`}
        role="dialog"
        aria-modal="true"
        aria-labelledby="modal-title"
        tabIndex={-1}
      >
        <div className="modal-header">
          <h2 id="modal-title">{title}</h2>
          <button type="button" className="icon-button" aria-label="关闭" title="关闭" onClick={onClose}>
            <X size={18} />
          </button>
        </div>
        <div className="modal-body">{children}</div>
      </div>
    </div>
  )
}
