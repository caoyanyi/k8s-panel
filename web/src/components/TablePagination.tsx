import { ChevronLeft, ChevronRight } from 'lucide-react'

export const TABLE_PAGE_SIZE = 100

interface TablePaginationProps {
  page: number
  totalItems: number
  onPage: (page: number) => void
  pageSize?: number
}

export function TablePagination({ page, totalItems, onPage, pageSize = TABLE_PAGE_SIZE }: TablePaginationProps) {
  const totalPages = Math.ceil(totalItems / pageSize)
  if (totalPages <= 1) return null

  return (
    <nav className="table-pagination" aria-label="资源清单分页">
      <span>第 {page + 1} / {totalPages} 页 · {totalItems} 条</span>
      <button type="button" className="icon-button" aria-label="上一页" title="上一页" disabled={page === 0} onClick={() => onPage(page - 1)}><ChevronLeft size={17} /></button>
      <button type="button" className="icon-button" aria-label="下一页" title="下一页" disabled={page + 1 >= totalPages} onClick={() => onPage(page + 1)}><ChevronRight size={17} /></button>
    </nav>
  )
}
