import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'

describe('App', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
    window.location.hash = ''
  })

  it('shows login after an unauthenticated session check', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(errorResponse(401, 'unauthorized')))

    render(<App />)

    expect(await screen.findByRole('heading', { name: '登录 KubePanel' })).toBeInTheDocument()
    expect(screen.getByLabelText('用户名')).toBeInTheDocument()
    expect(screen.getByLabelText('密码')).toHaveAttribute('type', 'password')
  })

  it('logs in and renders the operational dashboard', async () => {
    const responses = [
      errorResponse(401, 'unauthorized'),
      dataResponse({ username: 'admin', role: 'system-admin', expires_at: '2026-07-24T18:00:00Z' }),
      dataResponse([]),
      dataResponse([]),
      dataResponse([]),
    ]
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(responses.shift() ?? dataResponse([]))))
    const user = userEvent.setup()

    render(<App />)
    await user.type(await screen.findByLabelText('用户名'), 'admin')
    await user.type(screen.getByLabelText('密码'), 'admin-password')
    await user.click(screen.getByRole('button', { name: '登录' }))

    expect(await screen.findByRole('heading', { name: '运行总览' })).toBeInTheDocument()
    expect(screen.getByRole('navigation', { name: '主导航' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'admin 账户菜单' })).toBeInTheDocument()
    await waitFor(() => expect(screen.getByText('尚未接入集群')).toBeInTheDocument())
  })
})

function dataResponse(data: unknown): Response {
  return new Response(JSON.stringify({ data }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  })
}

function errorResponse(status: number, code: string): Response {
  return new Response(
    JSON.stringify({ error: { code, message: '请先登录或重新登录', request_id: 'req_test' } }),
    { status, headers: { 'Content-Type': 'application/json' } },
  )
}
