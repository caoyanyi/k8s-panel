import { act, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { WorkloadDetailModal } from './WorkloadDetailModal'
import type { Workload, WorkloadDetail } from '../types'

describe('WorkloadDetailModal', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('shows pod overview, events, sanitized YAML and bounded logs', async () => {
    const workload: Workload = {
      kind: 'Pod', namespace: 'payments', name: 'gateway-0', ready: 1, desired: 1,
      status: 'Ready', images: ['registry.example.com/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    const detail: WorkloadDetail = {
      ...workload,
      uid: 'uid-gateway-0',
      resource_version: '42',
      labels: { app: 'gateway' },
      containers: [{ name: 'app', image: workload.images[0], type: 'container', ready: true, restart_count: 2, state: 'Running' }],
      conditions: [{ type: 'Ready', status: 'True', reason: 'ContainersReady', last_transition_time: '2026-07-24T08:01:00Z' }],
      yaml: 'apiVersion: v1\nkind: Pod\nspec:\n  containers:\n    env:\n      value: <redacted>\n',
    }
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/logs?')) {
        return Promise.resolve(dataResponse({
          namespace: 'payments', pod: 'gateway-0', container: 'app', tail_lines: 200,
          previous: path.includes('previous=true'), timestamps: true, truncated: true, content: '2026-07-24T08:04:00Z ready\n',
        }))
      }
      if (path.endsWith('/events?limit=50')) {
        return Promise.resolve(dataResponse([{
          name: 'gateway-warning', type: 'Warning', reason: 'BackOff', message: 'Back-off restarting container',
          source: 'kubelet', count: 3, last_seen: '2026-07-24T08:03:00Z',
        }]))
      }
      return Promise.resolve(dataResponse(detail))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<WorkloadDetailModal clusterId="clu_1" workload={workload} open onClose={vi.fn()} />)
    const dialog = await screen.findByRole('dialog', { name: 'Pod · gateway-0' })

    expect(within(dialog).getByText('uid-gateway-0')).toBeInTheDocument()
    expect(within(dialog).getByText('2')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('tab', { name: '事件' }))
    expect(await within(dialog).findByText('BackOff')).toBeInTheDocument()
    expect(within(dialog).getByText('Back-off restarting container')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('tab', { name: 'YAML' }))
    expect(within(dialog).getByText(/value: <redacted>/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '复制' }))
    expect(await within(dialog).findByRole('button', { name: '已复制' })).toBeInTheDocument()

    await user.click(within(dialog).getByRole('tab', { name: '日志' }))
    expect(await within(dialog).findByText(/08:04:00Z ready/)).toBeInTheDocument()
    expect(within(dialog).getByText('日志达到 2 MiB 响应上限，已截断')).toBeInTheDocument()
    await user.selectOptions(within(dialog).getByLabelText('行数'), '1000')
    await user.click(within(dialog).getByRole('checkbox', { name: '上一实例' }))
    await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => (
      String(input).includes('tail_lines=1000&previous=true')
    ))).toBe(true))
    await user.click(within(dialog).getByRole('button', { name: '刷新日志' }))
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining('/pods/payments/gateway-0/logs?container=app&tail_lines=200&previous=false&timestamps=true'),
      expect.objectContaining({ credentials: 'same-origin' }),
    )
  })

  it('does not offer pod logs for a deployment', async () => {
    const workload: Workload = {
      kind: 'Deployment', namespace: 'payments', name: 'gateway', ready: 2, desired: 3,
      status: 'Progressing', images: ['gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      if (String(input).endsWith('/events?limit=50')) return Promise.resolve(dataResponse([]))
      return Promise.resolve(dataResponse({
        ...workload, uid: 'uid-gateway', resource_version: '9', labels: {}, containers: [], conditions: [], yaml: 'kind: Deployment\n',
      }))
    }))

    render(<WorkloadDetailModal clusterId="clu_1" workload={workload} open onClose={vi.fn()} />)

    const dialog = await screen.findByRole('dialog', { name: 'Deployment · gateway' })
    expect(within(dialog).queryByRole('tab', { name: '日志' })).not.toBeInTheDocument()
  })

  it('ignores an older log response after the query changes', async () => {
    const workload: Workload = {
      kind: 'Pod', namespace: 'payments', name: 'gateway-0', ready: 1, desired: 1,
      status: 'Ready', images: ['gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    let resolveSlow: ((response: Response) => void) | undefined
    const slowResponse = new Promise<Response>((resolve) => { resolveSlow = resolve })
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.includes('/logs?') && path.includes('tail_lines=200')) return slowResponse
      if (path.includes('/logs?')) {
        return Promise.resolve(dataResponse({
          namespace: 'payments', pod: 'gateway-0', container: 'app', tail_lines: 1000,
          previous: false, timestamps: true, truncated: false, content: 'latest log response',
        }))
      }
      if (path.endsWith('/events?limit=50')) return Promise.resolve(dataResponse([]))
      return Promise.resolve(dataResponse({
        ...workload, uid: 'uid-gateway-0', resource_version: '42', labels: {},
        containers: [{ name: 'app', image: workload.images[0], type: 'container', ready: true, restart_count: 0, state: 'Running' }],
        conditions: [], yaml: 'kind: Pod\n',
      }))
    }))
    const user = userEvent.setup()

    render(<WorkloadDetailModal clusterId="clu_1" workload={workload} open onClose={vi.fn()} />)
    const dialog = await screen.findByRole('dialog', { name: 'Pod · gateway-0' })
    await within(dialog).findByText('uid-gateway-0')
    await user.click(within(dialog).getByRole('tab', { name: '日志' }))
    await waitFor(() => expect(resolveSlow).toBeDefined())
    await user.selectOptions(within(dialog).getByLabelText('行数'), '1000')
    expect(await within(dialog).findByText('latest log response')).toBeInTheDocument()

    await act(async () => resolveSlow?.(dataResponse({
      namespace: 'payments', pod: 'gateway-0', container: 'app', tail_lines: 200,
      previous: false, timestamps: true, truncated: false, content: 'stale log response',
    })))
    expect(within(dialog).getByText('latest log response')).toBeInTheDocument()
    expect(within(dialog).queryByText('stale log response')).not.toBeInTheDocument()
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
