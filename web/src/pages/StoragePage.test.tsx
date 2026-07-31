import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { type ReactNode, useState } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { PanelContext, type PanelContextValue } from '../context'
import { StoragePage } from './StoragePage'

const baseContext: PanelContextValue = {
  clusters: [{
    id: 'clu_1', name: 'production-cn', environment: 'production', server: 'https://api.example.com',
    status: 'connected', version: 'v1.36.2', credentials_configured: true,
    created_at: '2026-07-20T08:00:00Z', updated_at: '2026-07-24T08:00:00Z',
  }],
  clustersLoading: false,
  clustersError: null,
  selectedClusterId: 'clu_1',
  selectedNamespace: '',
  setSelectedClusterId: vi.fn(),
  setSelectedNamespace: vi.fn(),
  refreshClusters: vi.fn().mockResolvedValue(undefined),
}

describe('StoragePage', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('loads one storage kind at a time and keeps cluster resources namespace independent', async () => {
    const requested: string[] = []
    let namespaceRequests = 0
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) {
        namespaceRequests++
        return Promise.resolve(dataResponse([namespace]))
      }
      if (path.includes('/persistent-volume-claims')) {
        requested.push(path)
        return Promise.resolve(dataResponse([claim]))
      }
      if (path.includes('/persistent-volumes')) {
        requested.push(path)
        return Promise.resolve(dataResponse([volume]))
      }
      if (path.includes('/storage-classes')) {
        requested.push(path)
        return Promise.resolve(dataResponse([storageClass]))
      }
      if (path.endsWith('/csi-drivers/ebs.csi.example.com')) {
        requested.push(path)
        return Promise.resolve(dataResponse({
          ...csiDrivers[0], attach_required: true, pod_info_on_mount: true, storage_capacity: true,
          requires_republish: false, se_linux_mount: true, fs_group_policy: 'File',
          volume_lifecycle_modes: ['Persistent', 'Ephemeral'], token_request_count: 2,
          tokenRequests: [{ audience: 'private-storage-api' }], annotations: { private: 'value' },
        }))
      }
      if (path.endsWith('/csi-drivers')) {
        requested.push(path)
        return Promise.resolve(dataResponse(csiDrivers))
      }
      if (path.endsWith('/volume-attachments')) {
        requested.push(path)
        return Promise.resolve(dataResponse(volumeAttachments))
      }
      if (path.endsWith('/csi-nodes/worker-01')) {
        requested.push(path)
        return Promise.resolve(dataResponse({
          ...csiNodes[0],
          drivers: [
            {
              name: 'ebs.csi.example.com', allocatable_count: 12, topology_key_count: 2,
              node_id: 'private-storage-node-01', topology_keys: ['topology.example.com/zone'],
            },
            { name: 'local.csi.example.com', topology_key_count: 0 },
          ],
          annotations: { private: 'private-value' },
        }))
      }
      if (path.endsWith('/csi-nodes')) {
        requested.push(path)
        return Promise.resolve(dataResponse(csiNodes))
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage({ selectedNamespace: 'payments' })

    expect(await screen.findByText('payments-data')).toBeInTheDocument()
    expect(requested).toEqual(['/api/v1/clusters/clu_1/persistent-volume-claims?namespace=payments'])
    await user.click(screen.getByRole('button', { name: /^PV$/ }))
    expect(await screen.findByText('pv-payments-data')).toBeInTheDocument()
    expect(screen.getByLabelText('命名空间')).toBeDisabled()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/persistent-volumes')

    await user.click(screen.getByRole('button', { name: 'StorageClass' }))
    expect(await screen.findByText('csi.example.com')).toBeInTheDocument()
    expect(screen.getByText('默认')).toBeInTheDocument()
    expect(requested.at(-1)).toBe('/api/v1/clusters/clu_1/storage-classes')

    expect(requested.some((path) => path.includes('/csi-drivers'))).toBe(false)
    await user.click(screen.getByRole('button', { name: 'CSIDriver' }))
    expect(await screen.findByText('ebs.csi.example.com')).toBeInTheDocument()
    expect(screen.getByLabelText('命名空间')).toBeDisabled()
    expect(namespaceRequests).toBe(1)
    expect(requested.filter((path) => path.endsWith('/csi-drivers'))).toHaveLength(1)
    expect(requested.some((path) => path.endsWith('/volume-attachments'))).toBe(false)

    await user.click(screen.getByRole('button', { name: '查看 ebs.csi.example.com' }))
    const dialog = await screen.findByRole('dialog', { name: 'CSIDriver · ebs.csi.example.com' })
    expect(within(dialog).getByText('File')).toBeInTheDocument()
    expect(within(dialog).getByText('Persistent · Ephemeral')).toBeInTheDocument()
    expect(within(dialog).getByText('2 项')).toBeInTheDocument()
    expect(screen.queryByText('private-storage-api')).not.toBeInTheDocument()
    expect(requested.filter((path) => path.endsWith('/csi-drivers/ebs.csi.example.com'))).toHaveLength(1)

    await user.click(within(dialog).getByRole('button', { name: '关闭' }))
    await user.click(screen.getByRole('button', { name: '卷挂接' }))
    expect(await screen.findByText('attach-a')).toBeInTheDocument()
    expect(screen.getByText('已挂接')).toBeInTheDocument()
    expect(screen.getByText('正在分离')).toBeInTheDocument()
    expect(screen.getByText('内联迁移卷')).toBeInTheDocument()
    expect(screen.queryByText('private-attach-error')).not.toBeInTheDocument()
    expect(screen.getByLabelText('命名空间')).toBeDisabled()
    expect(namespaceRequests).toBe(1)
    expect(requested.filter((path) => path.endsWith('/volume-attachments'))).toHaveLength(1)

    expect(requested.some((path) => path.includes('/csi-nodes'))).toBe(false)
    await user.click(screen.getByRole('button', { name: 'CSI节点' }))
    const nodeTable = await screen.findByRole('region', { name: 'CSINode 清单' })
    const nodeRow = within(nodeTable).getByText('worker-01').closest('tr')
    expect(nodeRow).not.toBeNull()
    expect(within(nodeRow as HTMLElement).getByText('2')).toBeInTheDocument()
    expect(screen.getByLabelText('命名空间')).toBeDisabled()
    expect(namespaceRequests).toBe(1)
    expect(requested.filter((path) => path.endsWith('/csi-nodes'))).toHaveLength(1)

    await user.click(within(nodeRow as HTMLElement).getByRole('button', { name: '查看 worker-01' }))
    const nodeDialog = await screen.findByRole('dialog', { name: 'CSINode · worker-01' })
    expect(within(nodeDialog).getByText('ebs.csi.example.com')).toBeInTheDocument()
    expect(within(nodeDialog).getByText('未声明上限')).toBeInTheDocument()
    expect(within(nodeDialog).queryByText('private-storage-node-01')).not.toBeInTheDocument()
    expect(within(nodeDialog).queryByText('topology.example.com/zone')).not.toBeInTheDocument()
    expect(within(nodeDialog).queryByText('private-value')).not.toBeInTheDocument()
    expect(requested.filter((path) => path.endsWith('/csi-nodes/worker-01'))).toHaveLength(1)
  })

  it('aborts an active claim refresh when the storage kind changes', async () => {
    let claimRequests = 0
    let refreshSignal: AbortSignal | null = null
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(dataResponse([namespace]))
      if (path.includes('/persistent-volumes')) return Promise.resolve(dataResponse([volume]))
      if (path.includes('/persistent-volume-claims')) {
        claimRequests++
        if (claimRequests === 1) return Promise.resolve(dataResponse([claim]))
        refreshSignal = init?.signal ?? null
        return new Promise<Response>((_resolve, reject) => {
          refreshSignal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')), { once: true })
        })
      }
      return Promise.resolve(errorResponse(404, 'not_found'))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    renderPage()
    expect(await screen.findByText('payments-data')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新' }))
    await waitFor(() => expect(claimRequests).toBe(2))
    await user.click(screen.getByRole('button', { name: /^PV$/ }))

    await waitFor(() => expect(refreshSignal?.aborted).toBe(true))
    expect(await screen.findByText('pv-payments-data')).toBeInTheDocument()
  })

  it('searches and paginates the active claim inventory locally', async () => {
    const claims = Array.from({ length: 101 }, (_, index) => ({
      ...claim,
      name: `claim-${String(index).padStart(3, '0')}`,
    }))
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => (
      Promise.resolve(dataResponse(String(input).endsWith('/namespaces') ? [namespace] : claims))
    )))
    const user = userEvent.setup()

    renderPage()

    expect(await screen.findByText('claim-000')).toBeInTheDocument()
    expect(screen.queryByText('claim-100')).not.toBeInTheDocument()
    expect(screen.getByText('第 1 / 2 页 · 101 条')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '下一页' }))
    expect(await screen.findByText('claim-100')).toBeInTheDocument()

    await user.type(screen.getByLabelText('搜索存储资源'), 'missing')
    expect(screen.getByText('没有匹配的 PVC')).toBeInTheDocument()
  })

  it('keeps cluster-scoped storage available when namespace discovery fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/namespaces')) return Promise.resolve(errorResponse(403, 'forbidden'))
      if (path.includes('/persistent-volume-claims')) return Promise.resolve(dataResponse([claim]))
      if (path.includes('/persistent-volumes')) return Promise.resolve(dataResponse([volume]))
      return Promise.resolve(errorResponse(404, 'not_found'))
    }))
    const user = userEvent.setup()

    renderPage()
    await user.click(screen.getByRole('button', { name: /^PV$/ }))

    expect(await screen.findByText('pv-payments-data')).toBeInTheDocument()
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

const claim = {
  namespace: 'payments', name: 'payments-data', status: 'Bound', volume: 'pv-payments-data', capacity: '20Gi',
  access_modes: 'RWO', storage_class: 'standard', volume_mode: 'Filesystem', created_at: '2026-07-24T08:00:00Z',
}

const volume = {
  name: 'pv-payments-data', status: 'Bound', claim: 'payments/payments-data', capacity: '20Gi', access_modes: 'RWO',
  storage_class: 'standard', reclaim_policy: 'Delete', volume_mode: 'Filesystem', created_at: '2026-07-23T08:00:00Z',
}

const storageClass = {
  name: 'standard', provisioner: 'csi.example.com', reclaim_policy: 'Delete', volume_binding_mode: 'WaitForFirstConsumer',
  allow_volume_expansion: true, default: true, created_at: '2026-07-22T08:00:00Z',
}

const csiDrivers = [
  { name: 'ebs.csi.example.com', created_at: '2026-07-22T08:05:00Z' },
  { name: 'local.csi.example.com', created_at: '2026-07-22T08:10:00Z' },
]

const volumeAttachments = [
  {
    name: 'attach-a', attacher: 'ebs.csi.example.com', persistent_volume: 'pv-payments-data',
    node: 'worker-01', status: 'attached', created_at: '2026-07-31T08:00:00Z',
  },
  {
    name: 'attach-inline', attacher: 'kubernetes.io/csi-migrated', node: 'worker-02', status: 'detaching',
    created_at: '2026-07-31T08:02:00Z', attach_error: 'private-attach-error',
  },
]

const csiNodes = [
  { name: 'worker-01', driver_count: 2, created_at: '2026-07-31T08:00:00Z' },
  { name: 'worker-02', driver_count: 0, created_at: '2026-07-31T08:02:00Z' },
]

function renderPage(overrides: Partial<PanelContextValue> = {}) {
  return render(<ContextHarness overrides={overrides}><StoragePage /></ContextHarness>)
}

function ContextHarness({ overrides, children }: { overrides: Partial<PanelContextValue>; children: ReactNode }) {
  const [selectedNamespace, setSelectedNamespace] = useState(overrides.selectedNamespace ?? baseContext.selectedNamespace)
  const value = {
    ...baseContext,
    ...overrides,
    selectedNamespace,
    setSelectedNamespace,
  }
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
