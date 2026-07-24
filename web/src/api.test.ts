import { afterEach, describe, expect, it, vi } from 'vitest'
import { ApiError, api } from './api'

describe('api', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('returns the data envelope and sends same-origin credentials', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ data: [{ id: 'clu_1', name: 'production' }] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    vi.stubGlobal('fetch', fetchMock)

    const result = await api.get<Array<{ id: string; name: string }>>('/api/v1/clusters')

    expect(result).toEqual([{ id: 'clu_1', name: 'production' }])
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/clusters',
      expect.objectContaining({ credentials: 'same-origin', headers: expect.any(Headers) }),
    )
  })

  it('throws a typed error without exposing an arbitrary response body', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(
        new Response(
          JSON.stringify({
            error: {
              code: 'validation_error',
              message: '请求参数不合法',
              request_id: 'req_123',
              details: [{ field: 'name', message: 'is required' }],
            },
          }),
          { status: 422, headers: { 'Content-Type': 'application/json' } },
        ),
      ),
    )

    await expect(api.post('/api/v1/clusters', { name: '' })).rejects.toEqual(
      expect.objectContaining({
        name: 'ApiError',
        status: 422,
        code: 'validation_error',
        requestId: 'req_123',
      }),
    )
  })
})
