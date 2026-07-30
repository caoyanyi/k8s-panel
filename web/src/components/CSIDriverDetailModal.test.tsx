import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CSIDriverDetailModal } from './CSIDriverDetailModal'

const resource = { name: 'ebs.csi.example.com', created_at: '2026-07-30T09:00:00Z' }

describe('CSIDriverDetailModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders bounded CSI configuration without token request or metadata contents', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse({
      ...resource,
      attach_required: true,
      pod_info_on_mount: true,
      storage_capacity: true,
      requires_republish: false,
      se_linux_mount: true,
      fs_group_policy: 'File',
      volume_lifecycle_modes: ['Persistent', 'Ephemeral'],
      token_request_count: 2,
      tokenRequests: [{ audience: 'private-storage-api', expirationSeconds: 3600 }],
      annotations: { private: 'value' },
    })))

    render(<CSIDriverDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: `CSIDriver · ${resource.name}` })
    expect(within(dialog).getByText('File')).toBeInTheDocument()
    expect(within(dialog).getByText('Persistent · Ephemeral')).toBeInTheDocument()
    expect(within(dialog).getByText('2 项')).toBeInTheDocument()
    expect(within(dialog).getAllByText('是')).toHaveLength(4)
    expect(within(dialog).getByText('否')).toBeInTheDocument()
    expect(within(dialog).queryByText('private-storage-api')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('3600')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('value')).not.toBeInTheDocument()
  })

  it('retries a resource-pressure response and displays a persistent-only driver', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({
        ...resource,
        attach_required: false,
        pod_info_on_mount: false,
        storage_capacity: false,
        requires_republish: false,
        se_linux_mount: false,
        fs_group_policy: 'ReadWriteOnceWithFSType',
        volume_lifecycle_modes: ['Persistent'],
        token_request_count: 0,
      }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<CSIDriverDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    const dialog = await screen.findByRole('dialog', { name: `CSIDriver · ${resource.name}` })
    expect(await within(dialog).findByText('ReadWriteOnceWithFSType')).toBeInTheDocument()
    expect(within(dialog).getByText('Persistent')).toBeInTheDocument()
    expect(within(dialog).getByText('0 项')).toBeInTheDocument()
    expect(within(dialog).getAllByText('否')).toHaveLength(5)
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
      <CSIDriverDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />,
    )
    expect(await screen.findByText('正在读取 CSIDriver 详情')).toBeInTheDocument()
    rendered.unmount()

    expect((signal as AbortSignal | null)?.aborted).toBe(true)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
