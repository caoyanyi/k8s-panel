import { render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import type { Cluster } from '../types'
import { ClustersPage } from './ClustersPage'

const cluster: Cluster = {
  id: 'clu_1', name: 'production-east', environment: 'production', server: 'https://api.example.com',
  status: 'connected', version: 'v1.36.2', credentials_configured: true,
  last_checked_at: '2026-07-27T08:00:00Z', created_at: '2026-07-24T08:00:00Z', updated_at: '2026-07-27T08:00:00Z',
}

describe('ClustersPage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('rotates credentials only after cluster-name confirmation and clears secrets', async () => {
    const refreshClusters = vi.fn().mockResolvedValue(undefined)
    const notify = vi.fn()
    const fetchMock = vi.fn().mockResolvedValue(dataResponse({ ...cluster, version: 'v1.36.3' }))
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()
    renderPage({ refreshClusters }, notify)

    await user.click(screen.getByRole('button', { name: '轮换 production-east 凭据' }))
    const dialog = screen.getByRole('dialog', { name: '轮换集群凭据' })
    const submit = within(dialog).getByRole('button', { name: '验证并轮换' })
    expect(submit).toBeDisabled()
    expect(within(dialog).getByLabelText('Bearer Token')).toHaveAttribute('maxlength', '65536')
    expect(within(dialog).getByLabelText('CA 证书（PEM，可选）')).toHaveAttribute('maxlength', '262144')
    await user.type(within(dialog).getByLabelText('Bearer Token'), 'new-service-account-token')
    await user.type(within(dialog).getByLabelText('CA 证书（PEM，可选）'), 'new-test-ca')
    await user.type(within(dialog).getByLabelText('输入集群名称确认'), 'production-east')
    expect(submit).toBeEnabled()
    await user.click(submit)

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/credential-rotations',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          bearer_token: 'new-service-account-token', ca_cert: 'new-test-ca', confirmation: 'production-east',
        }),
      }),
    )
    expect(refreshClusters).toHaveBeenCalledTimes(1)
    expect(notify).toHaveBeenCalledWith('success', 'production-east 凭据已轮换')
    expect(screen.queryByRole('dialog', { name: '轮换集群凭据' })).not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain('new-service-account-token')
  })

  it('clears rejected candidate secrets and keeps the rotation dialog open', async () => {
    const notify = vi.fn()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(errorResponse(503, 'server_busy', '服务繁忙，请稍后重试')))
    const user = userEvent.setup()
    renderPage({}, notify)

    await user.click(screen.getByRole('button', { name: '轮换 production-east 凭据' }))
    const dialog = screen.getByRole('dialog', { name: '轮换集群凭据' })
    await user.type(within(dialog).getByLabelText('Bearer Token'), 'rejected-token')
    await user.type(within(dialog).getByLabelText('CA 证书（PEM，可选）'), 'rejected-ca')
    await user.type(within(dialog).getByLabelText('输入集群名称确认'), 'production-east')
    await user.click(within(dialog).getByRole('button', { name: '验证并轮换' }))

    expect(await within(dialog).findByRole('alert')).toHaveTextContent('服务繁忙，请稍后重试')
    expect(within(dialog).getByLabelText('Bearer Token')).toHaveValue('')
    expect(within(dialog).getByLabelText('CA 证书（PEM，可选）')).toHaveValue('')
    expect(notify).not.toHaveBeenCalled()
  })
})

function renderPage(overrides: Partial<PanelContextValue>, notify = vi.fn()) {
  const context: PanelContextValue = {
    clusters: [cluster], clustersLoading: false, clustersError: null, selectedClusterId: cluster.id,
    selectedNamespace: '', setSelectedClusterId: vi.fn(), setSelectedNamespace: vi.fn(),
    refreshClusters: vi.fn().mockResolvedValue(undefined), ...overrides,
  }
  render(<PanelContext.Provider value={context}><ClustersPage notify={notify} /></PanelContext.Provider>)
}

function dataResponse(data: unknown) {
  return new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function errorResponse(status: number, code: string, message: string) {
  return new Response(JSON.stringify({ error: { code, message, request_id: 'req_rotate' } }), {
    status, headers: { 'Content-Type': 'application/json' },
  })
}
