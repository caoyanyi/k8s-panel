import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { CertificateSigningRequestDetailModal } from './CertificateSigningRequestDetailModal'

const resource = { name: 'worker-01', created_at: '2026-07-30T09:00:00Z' }

describe('CertificateSigningRequestDetailModal', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('shows a pending request without optional duration or conditions', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(dataResponse({
      ...resource,
      requester: 'system:node:worker-01', signer_name: 'example.com/node-client',
      usages: ['client auth'], state: 'pending', certificate_issued: false,
      conditions: [], condition_count: 0,
    })))

    const rendered = render(
      <CertificateSigningRequestDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />,
    )

    const dialog = await screen.findByRole('dialog', { name: `证书请求 · ${resource.name}` })
    expect(within(dialog).getByText('等待审批')).toBeInTheDocument()
    expect(within(dialog).getByText('由签名器决定')).toBeInTheDocument()
    expect(within(dialog).getByText('未写入')).toBeInTheDocument()
    expect(within(dialog).getByText('暂无条件')).toBeInTheDocument()
    rendered.unmount()
  })

  it('retries a failed request and renders issued evidence', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ error: { code: 'busy', message: '资源繁忙' } }), {
        status: 503,
        headers: { 'Content-Type': 'application/json' },
      }))
      .mockResolvedValueOnce(dataResponse({
        ...resource,
        requester: 'system:node:worker-01', signer_name: 'example.com/node-client',
        requested_expiration_seconds: 90061, usages: ['client auth'], state: 'issued', certificate_issued: true,
        conditions: [{ type: 'Approved', status: 'True' }], condition_count: 1,
      }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<CertificateSigningRequestDetailModal clusterId="clu_1" resource={resource} onClose={vi.fn()} />)

    expect(await screen.findByText('资源繁忙')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))
    const dialog = await screen.findByRole('dialog', { name: `证书请求 · ${resource.name}` })
    expect(await within(dialog).findByText('已签发')).toBeInTheDocument()
    expect(within(dialog).getByText('已写入')).toBeInTheDocument()
    expect(within(dialog).getByText('1 天 1 小时 1 分钟 1 秒（请求值）')).toBeInTheDocument()
    expect(within(dialog).getAllByText('-')).toHaveLength(3)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}
