import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { HelmPage } from './HelmPage'

const baseContext: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'development', environment: 'development', server: 'https://api.example.com',
    status: 'connected', version: 'v1.36.2', credentials_configured: true,
    created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-24T08:00:00Z',
  }],
  clustersLoading: false,
  clustersError: null,
  selectedClusterId: 'clu_1',
  selectedNamespace: 'payments',
  setSelectedClusterId: vi.fn(),
  setSelectedNamespace: vi.fn(),
  refreshClusters: vi.fn().mockResolvedValue(undefined),
}

describe('HelmPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads history on demand and feeds the selected revision into rollback confirmation', async () => {
    const requested: string[] = []
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      requested.push(path)
      if (path === '/api/v1/chart-repositories') return Promise.resolve(dataResponse([]))
      if (path === '/api/v1/clusters/clu_1/namespaces') return Promise.resolve(dataResponse([namespace]))
      if (path === '/api/v1/helm-releases?cluster_id=clu_1&namespace=payments') return Promise.resolve(dataResponse([release]))
      if (path === '/api/v1/helm-releases/gateway/history?cluster_id=clu_1&namespace=payments') {
        return Promise.resolve(dataResponse({
          name: 'gateway', namespace: 'payments', truncated: false,
          revisions: [
            { revision: 4, status: 'deployed', created_at: '2026-07-30T09:04:00Z' },
            { revision: 2, status: 'superseded', created_at: '2026-07-30T09:02:00Z' },
          ],
        }))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('gateway-1.4.0')).toBeInTheDocument()
    expect(requested.some((path) => path.includes('/history'))).toBe(false)
    await user.click(screen.getByRole('button', { name: '查看 gateway 修订历史' }))
    const historyDialog = await screen.findByRole('dialog', { name: '修订历史 · gateway' })
    expect(within(historyDialog).getByText('已替代')).toBeInTheDocument()
    expect(requested.filter((path) => path.includes('/history'))).toHaveLength(1)

    await user.click(within(historyDialog).getByRole('button', { name: '选择 revision 2 回滚' }))
    const rollbackDialog = await screen.findByRole('dialog', { name: '回滚 gateway' })
    expect(within(rollbackDialog).getByLabelText('目标 Revision')).toHaveValue(2)
    expect(fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'POST')).toBe(false)
  })
})

const namespace = {
  name: 'payments', status: 'Active', labels: {}, finalizers: [], created_at: '2026-07-20T08:00:00Z',
}

const release = {
  name: 'gateway', namespace: 'payments', revision: 4, status: 'deployed', chart: 'gateway-1.4.0',
  updated_at: '2026-07-30T09:04:00Z',
}

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(
    <ContextHarness overrides={overrides}>
      <HelmPage notify={vi.fn()} openOperations={vi.fn()} />
    </ContextHarness>,
  )
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
