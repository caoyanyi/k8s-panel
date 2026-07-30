import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { HelmReleaseHistoryModal } from './HelmReleaseHistoryModal'

const release = {
  name: 'gateway', namespace: 'payments', revision: 4, status: 'deployed', chart: 'gateway-1.4.0',
}

describe('HelmReleaseHistoryModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('renders bounded revision metadata and selects only a non-current revision', async () => {
    const fetchMock = vi.fn().mockResolvedValue(dataResponse({
      name: 'gateway',
      namespace: 'payments',
      revisions: [
        { revision: 4, status: 'deployed', created_at: '2026-07-30T09:04:00Z' },
        { revision: 3, status: 'failed', created_at: '2026-07-30T09:03:00Z' },
        { revision: 2, status: 'superseded', created_at: '2026-07-30T09:02:00Z' },
      ],
      truncated: true,
      storage_secret: 'sh.helm.release.v1.gateway.v4',
      values: 'private-value',
      manifest: 'private-manifest',
    }))
    vi.stubGlobal('fetch', fetchMock)
    const onRollback = vi.fn()
    const user = userEvent.setup()

    render(<HelmReleaseHistoryModal clusterId="clu_1" release={release} onClose={vi.fn()} onRollback={onRollback} />)

    const dialog = await screen.findByRole('dialog', { name: '修订历史 · gateway' })
    expect(within(dialog).getByText('仅显示最近 10 个修订')).toBeInTheDocument()
    expect(within(dialog).getByText('已部署')).toBeInTheDocument()
    expect(within(dialog).getByText('失败')).toBeInTheDocument()
    expect(within(dialog).getByText('已替代')).toBeInTheDocument()
    expect(within(dialog).getByText('当前')).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', { name: '选择 revision 4 回滚' })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '选择 revision 3 回滚' })).toBeInTheDocument()
    expect(within(dialog).queryByText('private-value')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('private-manifest')).not.toBeInTheDocument()
    expect(within(dialog).queryByText('sh.helm.release.v1.gateway.v4')).not.toBeInTheDocument()
    expect(String(fetchMock.mock.calls[0]?.[0])).toBe(
      '/api/v1/helm-releases/gateway/history?cluster_id=clu_1&namespace=payments',
    )

    await user.click(within(dialog).getByRole('button', { name: '选择 revision 3 回滚' }))
    expect(onRollback).toHaveBeenCalledWith(3)
  })

  it('retries a resource-pressure response', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'server_busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({
        name: 'gateway', namespace: 'payments', truncated: false,
        revisions: [{ revision: 4, status: 'deployed', created_at: '2026-07-30T09:04:00Z' }],
      }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<HelmReleaseHistoryModal clusterId="clu_1" release={release} onClose={vi.fn()} onRollback={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    expect(await screen.findByText('当前')).toBeInTheDocument()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('aborts the history request when it unmounts', async () => {
    let signal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((_input: RequestInfo | URL, init?: RequestInit) => {
      signal = init?.signal ?? null
      return new Promise<Response>((_resolve, reject) => {
        signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
      })
    }))

    const rendered = render(
      <HelmReleaseHistoryModal clusterId="clu_1" release={release} onClose={vi.fn()} onRollback={vi.fn()} />,
    )
    expect(await screen.findByText('正在读取 Release 修订历史')).toBeInTheDocument()
    rendered.unmount()

    await waitFor(() => expect((signal as AbortSignal | null)?.aborted).toBe(true))
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
