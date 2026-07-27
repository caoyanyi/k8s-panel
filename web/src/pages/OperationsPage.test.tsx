import { act, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { OperationsPage } from './OperationsPage'

describe('OperationsPage', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('shows adaptive operation capacity without exposing host details', async () => {
    let canceled = false
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST' && String(input).endsWith('/operations/op_1/cancellations')) {
        canceled = true
        return Promise.resolve(dataResponse({
          id: 'op_1', request_id: 'req_1', kind: 'workload.scale', state: 'canceled',
          cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
          summary: 'replicas=5, resource_version=42', created_at: '2026-07-25T08:00:00Z',
          finished_at: '2026-07-25T08:02:00Z', updated_at: '2026-07-25T08:02:00Z',
        }))
      }
      if (String(input).endsWith('/system/resources')) {
        return Promise.resolve(dataResponse({
          adaptive: true,
          pressure: 'constrained',
          memory_ratio: 0.84,
          load_ratio: 0.62,
          active_operations: 1,
          operation_limit: 1,
          maximum_operations: 2,
          queue_depth: 3,
          queue_capacity: 64,
          kubernetes_reads: { adaptive: true, pressure: 'constrained', active: 2, limit: 2, maximum: 4 },
          kubernetes_clients: { entries: 3, capacity: 4, maximum: 8, building: 1 },
          sampled_at: '2026-07-25T08:00:00Z',
        }))
      }
      return Promise.resolve(dataResponse([{
        id: 'op_1', request_id: 'req_1', kind: 'workload.scale', state: canceled ? 'canceled' : 'queued',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
        summary: 'replicas=5, resource_version=42', created_at: '2026-07-25T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
      }, {
        id: 'op_2', request_id: 'req_2', kind: 'workload.image_update', state: 'running',
        cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
        summary: 'container=app, fields=1, resource_version=42', created_at: '2026-07-25T08:01:00Z', updated_at: '2026-07-25T08:01:00Z',
      }]))
    })
    vi.stubGlobal('fetch', fetchMock)
    const notify = vi.fn()
    const user = userEvent.setup()

    render(<OperationsPage notify={notify} />)

    expect(await screen.findByText('工作负载扩缩容')).toBeInTheDocument()
    expect(screen.getByText('工作负载镜像更新')).toBeInTheDocument()
    expect(screen.getByRole('region', { name: '操作记录' })).toHaveAttribute('tabindex', '0')
    expect(screen.getByText('资源承压')).toBeInTheDocument()
    expect(screen.getByText('内存 84%')).toBeInTheDocument()
    expect(screen.getByText('负载 62%')).toBeInTheDocument()
    expect(screen.getByText('执行槽 1 / 1')).toBeInTheDocument()
    expect(screen.getByText('读取槽 2 / 2')).toBeInTheDocument()
    expect(screen.getByText('连接缓存 3 / 4')).toBeInTheDocument()
    expect(screen.getByText('队列 3 / 64')).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('/proc')
    expect(document.body.textContent).not.toContain('/sys/fs/cgroup')
    expect(screen.queryByRole('button', { name: '取消任务 op_2' })).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '取消任务 op_1' }))
    expect(await screen.findByText('已取消')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/operations/op_1/cancellations', expect.objectContaining({
      method: 'POST', body: '{}',
    }))
    expect(notify).toHaveBeenCalledWith('success', '任务 op_1 已取消')
    expect(screen.queryByRole('button', { name: '取消任务 op_1' })).not.toBeInTheDocument()
  })

  it('pauses polling while hidden and refreshes after becoming visible', async () => {
    vi.useFakeTimers()
    const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      if (String(input).endsWith('/system/resources')) {
        return Promise.resolve(dataResponse({
          adaptive: false, pressure: 'unknown', active_operations: 0, operation_limit: 2,
          maximum_operations: 2, queue_depth: 0, queue_capacity: 64, sampled_at: '2026-07-25T08:00:00Z',
          kubernetes_reads: { adaptive: false, pressure: 'normal', active: 0, limit: 4, maximum: 4 },
          kubernetes_clients: { entries: 0, capacity: 8, maximum: 8, building: 0 },
        }))
      }
      return Promise.resolve(dataResponse([]))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(<OperationsPage notify={vi.fn()} />)
    await act(async () => { await Promise.resolve(); await Promise.resolve() })
    expect(screen.getByText('内存 -')).toBeInTheDocument()
    expect(screen.getByText('自适应已关闭')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)

    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(fetchMock).toHaveBeenCalledTimes(2)

    hidden.mockReturnValue(false)
    await act(async () => {
      document.dispatchEvent(new Event('visibilitychange'))
      document.dispatchEvent(new Event('visibilitychange'))
      await Promise.resolve()
      await Promise.resolve()
    })
    expect(fetchMock).toHaveBeenCalledTimes(4)
    vi.useRealTimers()
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
