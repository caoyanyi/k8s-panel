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

    render(<WorkloadDetailModal clusterId="clu_1" clusterName="development" environment="development" workload={workload} open onClose={vi.fn()} notify={vi.fn()} openOperations={vi.fn()} />)
    const dialog = await screen.findByRole('dialog', { name: 'Pod · gateway-0' })

    expect(within(dialog).getByText('uid-gateway-0')).toBeInTheDocument()
    expect(within(dialog).getByText('2')).toBeInTheDocument()
    expect(fetchMock.mock.calls.some(([input]) => String(input).endsWith('/events?limit=50'))).toBe(false)

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

    render(<WorkloadDetailModal clusterId="clu_1" clusterName="development" environment="development" workload={workload} open onClose={vi.fn()} notify={vi.fn()} openOperations={vi.fn()} />)

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

    render(<WorkloadDetailModal clusterId="clu_1" clusterName="development" environment="development" workload={workload} open onClose={vi.fn()} notify={vi.fn()} openOperations={vi.fn()} />)
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

  it('submits a production deployment scale with resource-version confirmation', async () => {
    const workload: Workload = {
      kind: 'Deployment', namespace: 'payments', name: 'gateway', ready: 2, desired: 3,
      status: 'Progressing', images: ['gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    const notify = vi.fn()
    const openOperations = vi.fn()
    const onClose = vi.fn()
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (init?.method === 'POST' && path.endsWith('/scales')) {
        return Promise.resolve(dataResponse({
          id: 'op_scale', request_id: 'req_scale', kind: 'workload.scale', state: 'queued',
          cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
          created_at: '2026-07-25T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
        }))
      }
      if (path.endsWith('/events?limit=50')) return Promise.resolve(dataResponse([]))
      return Promise.resolve(dataResponse({
        ...workload, uid: 'uid-gateway', resource_version: '42', labels: {}, containers: [], conditions: [], yaml: 'kind: Deployment\n',
      }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<WorkloadDetailModal
      clusterId="clu_1"
      clusterName="production-east"
      environment="production"
      workload={workload}
      open
      onClose={onClose}
      notify={notify}
      openOperations={openOperations}
    />)
    const dialog = await screen.findByRole('dialog', { name: 'Deployment · gateway' })
    await within(dialog).findByText('uid-gateway')
    await user.click(within(dialog).getByRole('button', { name: '扩缩容' }))
    await user.clear(within(dialog).getByLabelText('副本数'))
    await user.click(within(dialog).getByRole('button', { name: '提交扩缩容' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent('请输入副本数')
    expect(fetchMock.mock.calls.some(([input, init]) => init?.method === 'POST' && String(input).endsWith('/scales'))).toBe(false)
    await user.type(within(dialog).getByLabelText('副本数'), '5')
    await user.type(within(dialog).getByLabelText('输入集群名称确认'), 'production-east')
    await user.click(within(dialog).getByRole('button', { name: '提交扩缩容' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/workloads/deployment/payments/gateway/scales',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ resource_version: '42', confirmation: 'production-east', replicas: 5 }),
      }),
    ))
    expect(notify).toHaveBeenCalledWith('success', '扩缩容任务 op_scale 已提交')
    expect(onClose).toHaveBeenCalled()
    expect(openOperations).toHaveBeenCalled()
  })

  it('submits a deployment rolling restart without replica changes', async () => {
    const workload: Workload = {
      kind: 'Deployment', namespace: 'payments', name: 'gateway', ready: 3, desired: 3,
      status: 'Ready', images: ['gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    const notify = vi.fn()
    const openOperations = vi.fn()
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (init?.method === 'POST' && path.endsWith('/restarts')) {
        return Promise.resolve(dataResponse({
          id: 'op_restart', request_id: 'req_restart', kind: 'workload.restart', state: 'queued',
          cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
          created_at: '2026-07-25T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
        }))
      }
      return Promise.resolve(dataResponse({
        ...workload, uid: 'uid-gateway', resource_version: '51', labels: {}, containers: [], conditions: [], yaml: 'kind: Deployment\n',
      }))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<WorkloadDetailModal
      clusterId="clu_1"
      clusterName="development"
      environment="development"
      workload={workload}
      open
      onClose={vi.fn()}
      notify={notify}
      openOperations={openOperations}
    />)
    const dialog = await screen.findByRole('dialog', { name: 'Deployment · gateway' })
    await within(dialog).findByText('uid-gateway')
    await user.click(within(dialog).getByRole('button', { name: /^滚动重启$/ }))
    await user.click(within(dialog).getByRole('button', { name: '提交滚动重启' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/workloads/deployment/payments/gateway/restarts',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({ resource_version: '51', confirmation: '' }),
      }),
    ))
    expect(notify).toHaveBeenCalledWith('success', '滚动重启任务 op_restart 已提交')
    expect(openOperations).toHaveBeenCalled()
  })

  it('previews and submits a production deployment image update', async () => {
    const workload: Workload = {
      kind: 'Deployment', namespace: 'payments', name: 'gateway', ready: 3, desired: 3,
      status: 'Ready', images: ['registry.example.com/gateway:1.4.0'], created_at: '2026-07-24T08:00:00Z',
    }
    const detail: WorkloadDetail = {
      ...workload,
      uid: 'uid-gateway',
      resource_version: '61',
      labels: {},
      containers: [
        { name: 'app', image: workload.images[0], type: 'container', ready: true, restart_count: 0, state: 'Running' },
        { name: 'setup', image: 'registry.example.com/setup:1.0.0', type: 'init', ready: true, restart_count: 0, state: 'Terminated' },
      ],
      conditions: [],
      yaml: 'kind: Deployment\n',
    }
    const notify = vi.fn()
    const openOperations = vi.fn()
    const onClose = vi.fn()
    const fetchMock = vi.fn().mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const path = String(input)
      if (init?.method === 'POST' && path.endsWith('/image-previews')) {
        const body = JSON.parse(String(init.body)) as { image: string }
        return Promise.resolve(dataResponse({
          kind: 'Deployment', namespace: 'payments', name: 'gateway', container: 'app', resource_version: '61',
          changes: [{
            field: 'spec.template.spec.containers[name=app].image',
            before: 'registry.example.com/gateway:1.4.0',
            after: body.image,
          }],
        }))
      }
      if (init?.method === 'POST' && path.endsWith('/image-updates')) {
        return Promise.resolve(dataResponse({
          id: 'op_image', request_id: 'req_image', kind: 'workload.image_update', state: 'queued',
          cluster_id: 'clu_1', namespace: 'payments', target: 'gateway', submitted_by: 'admin',
          created_at: '2026-07-25T08:00:00Z', updated_at: '2026-07-25T08:00:00Z',
        }))
      }
      return Promise.resolve(dataResponse(detail))
    })
    vi.stubGlobal('fetch', fetchMock)
    const user = userEvent.setup()

    render(<WorkloadDetailModal
      clusterId="clu_1"
      clusterName="production-east"
      environment="production"
      workload={workload}
      open
      onClose={onClose}
      notify={notify}
      openOperations={openOperations}
    />)
    const dialog = await screen.findByRole('dialog', { name: 'Deployment · gateway' })
    await within(dialog).findByText('uid-gateway')
    await user.click(within(dialog).getByRole('button', { name: '更新镜像' }))
    expect(within(dialog).getByLabelText('容器')).toHaveValue('app')
    expect(within(dialog).queryByRole('option', { name: 'setup' })).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '提交镜像更新' })).toBeDisabled()

    await user.clear(within(dialog).getByLabelText('新镜像'))
    await user.type(within(dialog).getByLabelText('新镜像'), 'registry.example.com/gateway:1.5.0')
    await user.click(within(dialog).getByRole('button', { name: '预览变更' }))
    expect(await within(dialog).findByText('服务端 dry-run 通过')).toBeInTheDocument()
    expect(within(dialog).getByText('registry.example.com/gateway:1.4.0')).toBeInTheDocument()
    expect(within(dialog).getByText('registry.example.com/gateway:1.5.0')).toBeInTheDocument()

    await user.type(within(dialog).getByLabelText('新镜像'), '-candidate')
    expect(within(dialog).queryByText('服务端 dry-run 通过')).not.toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: '提交镜像更新' })).toBeDisabled()
    await user.clear(within(dialog).getByLabelText('新镜像'))
    await user.type(within(dialog).getByLabelText('新镜像'), 'registry.example.com/gateway:1.5.0')
    await user.click(within(dialog).getByRole('button', { name: '预览变更' }))
    await within(dialog).findByText('服务端 dry-run 通过')
    await user.type(within(dialog).getByLabelText('输入集群名称确认'), 'production-east')
    await user.click(within(dialog).getByRole('button', { name: '提交镜像更新' }))

    await waitFor(() => expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters/clu_1/workloads/deployment/payments/gateway/image-updates',
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          container: 'app',
          current_image: 'registry.example.com/gateway:1.4.0',
          image: 'registry.example.com/gateway:1.5.0',
          resource_version: '61',
          confirmation: 'production-east',
        }),
      }),
    ))
    expect(notify).toHaveBeenCalledWith('success', '镜像更新任务 op_image 已提交')
    expect(onClose).toHaveBeenCalled()
    expect(openOperations).toHaveBeenCalled()
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}
