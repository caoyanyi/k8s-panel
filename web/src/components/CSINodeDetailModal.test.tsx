import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CSINodeDetailModal } from './CSINodeDetailModal'

const resource = { name: 'worker-01', driver_count: 2, created_at: '2026-07-31T08:00:00Z' }

describe('CSINodeDetailModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders bounded driver summaries without node IDs, topology keys, or metadata', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse({
      ...resource,
      drivers: [
        {
          name: 'ebs.csi.example.com', allocatable_count: 12, topology_key_count: 2,
          node_id: 'private-storage-node-01',
          topology_keys: ['topology.example.com/zone', 'topology.kubernetes.io/region'],
        },
        { name: 'local.csi.example.com', topology_key_count: 0, node_id: 'private-local-node-01' },
      ],
      annotations: { private: 'private-value' },
    })))

    render(<CSINodeDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: `CSINode · ${resource.name}` })
    const ebsRow = within(dialog).getByText('ebs.csi.example.com').closest('tr')
    const localRow = within(dialog).getByText('local.csi.example.com').closest('tr')
    expect(ebsRow).not.toBeNull()
    expect(localRow).not.toBeNull()
    expect(within(ebsRow as HTMLElement).getByText('12')).toBeInTheDocument()
    expect(within(ebsRow as HTMLElement).getByText('2')).toBeInTheDocument()
    expect(within(localRow as HTMLElement).getByText('未声明上限')).toBeInTheDocument()
    expect(within(localRow as HTMLElement).getByText('0')).toBeInTheDocument()
    expect(within(dialog).queryByText('private-storage-node-01')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('topology.example.com/zone')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('private-value')).not.toBeInTheDocument()
  })

  it('retries a resource-pressure response and shows an empty driver inventory', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({ ...resource, driver_count: 0, drivers: [] }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<CSINodeDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('未记录 CSI 驱动')).toBeInTheDocument()
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
      <CSINodeDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />,
    )
    expect(await screen.findByText('正在读取 CSINode 详情')).toBeInTheDocument()
    rendered.unmount()

    expect((signal as AbortSignal | null)?.aborted).toBe(true)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
