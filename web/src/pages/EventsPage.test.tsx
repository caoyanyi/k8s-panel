import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { EventsPage } from './EventsPage'

const baseContext: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
    status: 'connected', version: 'v1.36.2', credentials_configured: true,
    created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-28T08:00:00Z',
  }],
  clustersLoading: false,
  clustersError: null,
  selectedClusterId: 'clu_1',
  selectedNamespace: '',
  setSelectedClusterId: vi.fn(),
  setSelectedNamespace: vi.fn(),
  refreshClusters: vi.fn().mockResolvedValue(undefined),
}

describe('EventsPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('defaults to upstream Warning filtering and can explicitly load all events', async () => {
    const requested: string[] = []
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/events')) {
        requested.push(path)
        return Promise.resolve(dataResponse(path.includes('type=Warning') ? [warningEvent] : [warningEvent, normalEvent]))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('BackOff')).toBeInTheDocument()
    expect(requested).toEqual(['/api/v1/clusters/clu_1/events?type=Warning&limit=200'])
    expect(screen.queryByText('Scheduled')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '全部事件' }))
    expect(await screen.findByText('Scheduled')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/events?limit=200')

    await user.selectOptions(screen.getByLabelText('命名空间'), 'payments')
    await waitFor(() => expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/events?namespace=payments&limit=200'))
  })

  it('aborts an active refresh when the event filter changes', async () => {
    let eventRequests = 0
    let refreshSignal: AbortSignal | null = null
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/events')) {
        eventRequests++
        if (eventRequests === 1) return Promise.resolve(dataResponse([warningEvent]))
        if (!path.includes('type=Warning')) return Promise.resolve(dataResponse([normalEvent]))
        refreshSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          refreshSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('BackOff')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(eventRequests).toBe(2))
    await user.click(screen.getByRole('button', { name: '全部事件' }))

    await waitFor(() => expect(refreshSignal?.aborted).toBe(true))
    expect(await screen.findByText('Scheduled')).toBeInTheDocument()
  })

  it('searches and paginates events locally', async () => {
    const events = Array.from({ length: 101 }, (_, index) => ({
      ...warningEvent,
      name: `warning-${String(index).padStart(3, '0')}`,
      reason: `Reason-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : events))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('Reason-000')).toBeInTheDocument()
    expect(screen.queryByText('Reason-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('Reason-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索事件'), 'not-found')
    expect(screen.getByText('没有匹配的事件')).toBeInTheDocument()
  })

  it('keeps cluster-wide events available when namespace discovery fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(String(input).endsWith('/namespaces')
        ? errorResponse(403, 'forbidden')
        : dataResponse([warningEvent]))
    )))

    renderPage()

    expect(await screen.findByText('BackOff')).toBeInTheDocument()
    expect(screen.getByText('命名空间列表不可用')).toBeInTheDocument()
  })

  it('does not request Kubernetes resources without a selected cluster', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    renderPage({ clusters: [], selectedClusterId: '' })

    expect(screen.getByText('尚未选择集群')).toBeInTheDocument()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})

const namespace = {
  name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
}

const warningEvent = {
  namespace: 'payments', name: 'gateway-warning', type: 'Warning', reason: 'BackOff',
  message: 'Back-off restarting container', message_truncated: false,
  source: 'kubelet', object_kind: 'Pod', object_name: 'gateway-0', count: 3,
  first_seen: '2026-07-28T07:50:00Z', last_seen: '2026-07-28T08:03:00Z', created_at: '2026-07-28T07:50:00Z',
}

const normalEvent = {
  namespace: 'payments', name: 'gateway-scheduled', type: 'Normal', reason: 'Scheduled',
  message: 'Successfully assigned pod', message_truncated: false,
  source: 'default-scheduler', object_kind: 'Pod', object_name: 'gateway-0', count: 1,
  first_seen: '2026-07-28T08:04:00Z', last_seen: '2026-07-28T08:04:00Z', created_at: '2026-07-28T08:04:00Z',
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><EventsPage /></ContextHarness>)
}

function ContextHarness({ overrides, children }: { overrides: Partial<PanelContextValue>; children: ReactNode }) {
  const [selectedNamespace, setSelectedNamespace] = useState(overrides.selectedNamespace ?? baseContext.selectedNamespace)
  const value = { ...baseContext, ...overrides, selectedNamespace, setSelectedNamespace }
  return <PanelContext.Provider value={value}>{children}</PanelContext.Provider>
}

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function errorResponse(status: number, code: string): Response {
  return new Response(JSON.stringify({ error: { code, message: code } }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}
