import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { RuntimeClassDetailModal } from './RuntimeClassDetailModal'

const resource = { name: 'kata-containers', created_at: '2026-07-30T09:00:00Z' }

describe('RuntimeClassDetailModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders bounded runtime facts without scheduling or metadata contents', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse({
      ...resource,
      handler: 'kata-fc',
      overhead_configured: true,
      pod_overhead_cpu: '250m',
      pod_overhead_memory: '120Mi',
      overhead_resource_count: 3,
      scheduling_configured: true,
      node_selector_count: 2,
      toleration_count: 2,
      nodeSelector: { 'private.example.com/runtime': 'kata' },
      tolerations: [{ key: 'private-taint' }],
      annotations: { private: 'value' },
    })))

    render(<RuntimeClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: `RuntimeClass · ${resource.name}` })
    expect(within(dialog).getByText('kata-fc')).toBeInTheDocument()
    expect(within(dialog).getByText('250m')).toBeInTheDocument()
    expect(within(dialog).getByText('120Mi')).toBeInTheDocument()
    expect(within(dialog).getByText('3 项')).toBeInTheDocument()
    expect(within(dialog).getAllByText('2 项')).toHaveLength(2)
    expect(within(dialog).getAllByText('已配置')).toHaveLength(2)
    expect(within(dialog).queryByText('private.example.com/runtime')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('private-taint')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('value')).not.toBeInTheDocument()
  })

  it('retries a resource-pressure response and shows absent optional configuration', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({
        ...resource,
        handler: 'runc',
        overhead_configured: false,
        overhead_resource_count: 0,
        scheduling_configured: false,
        node_selector_count: 0,
        toleration_count: 0,
      }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<RuntimeClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    const dialog = await screen.findByRole('dialog', { name: `RuntimeClass · ${resource.name}` })
    expect(await within(dialog).findByText('runc')).toBeInTheDocument()
    expect(within(dialog).getAllByText('未配置')).toHaveLength(2)
    expect(within(dialog).getAllByText('未设置')).toHaveLength(2)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('aborts the detail request when it unmounts', async () => {
    let signal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      signal = init?.signal ?? null
      return new Promise<Response>((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
      })
    }))

    const rendered = render(
      <RuntimeClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />,
    )
    expect(await screen.findByText('正在读取 RuntimeClass 详情')).toBeInTheDocument()
    rendered.unmount()

    expect((signal as AbortSignal | null)?.aborted).toBe(true)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
