import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PriorityClassDetailModal } from './PriorityClassDetailModal'

const resource = { name: 'workload-high', created_at: '2026-07-30T09:00:00Z' }

describe('PriorityClassDetailModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders explicit non-preempting policy facts without arbitrary upstream fields', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse({
      ...resource,
      value: -10,
      global_default: false,
      preemption_policy: 'Never',
      preemption_policy_defaulted: false,
      description: 'private scheduling guidance',
      annotations: { private: 'value' },
    })))

    render(<PriorityClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: `PriorityClass · ${resource.name}` })
    expect(within(dialog).getByText('-10')).toBeInTheDocument()
    expect(within(dialog).getByText('否')).toBeInTheDocument()
    expect(within(dialog).getByText('Never')).toBeInTheDocument()
    expect(within(dialog).getByText('显式配置')).toBeInTheDocument()
    expect(within(dialog).queryByText('private scheduling guidance')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('value')).not.toBeInTheDocument()
  })

  it('retries a resource-pressure response and renders the defaulted policy', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({
        ...resource,
        value: 2000000000,
        global_default: true,
        preemption_policy: 'PreemptLowerPriority',
        preemption_policy_defaulted: true,
      }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<PriorityClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    const dialog = await screen.findByRole('dialog', { name: `PriorityClass · ${resource.name}` })
    expect(await within(dialog).findByText('2,000,000,000')).toBeInTheDocument()
    expect(within(dialog).getByText('PreemptLowerPriority（默认）')).toBeInTheDocument()
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
      <PriorityClassDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />,
    )
    expect(await screen.findByText('正在读取 PriorityClass 详情')).toBeInTheDocument()
    rendered.unmount()

    expect((signal as AbortSignal | null)?.aborted).toBe(true)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
