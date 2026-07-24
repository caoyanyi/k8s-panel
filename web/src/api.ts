export interface ApiErrorDetail {
  field: string
  message: string
}

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
    request_id?: string
    details?: ApiErrorDetail[]
  }
}

interface DataEnvelope<T> {
  data: T
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly details: ApiErrorDetail[]

  constructor(status: number, code: string, message: string, requestId = '', details: ApiErrorDetail[] = []) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.code = code
    this.requestId = requestId
    this.details = details
  }
}

async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers = new Headers({ Accept: 'application/json' })
  const init: RequestInit = {
    method,
    credentials: 'same-origin',
    headers,
    signal,
  }
  if (body !== undefined) {
    headers.set('Content-Type', 'application/json')
    init.body = JSON.stringify(body)
  }

  let response: Response
  try {
    response = await fetch(path, init)
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw error
    }
    throw new ApiError(0, 'network_error', '无法连接管理服务')
  }
  if (response.status === 204) {
    return undefined as T
  }

  const contentType = response.headers.get('Content-Type') ?? ''
  let payload: unknown
  if (contentType.includes('application/json')) {
    try {
      payload = await response.json()
    } catch {
      payload = undefined
    }
  }
  if (!response.ok) {
    const envelope = (payload ?? {}) as ErrorEnvelope
    const serverError = envelope.error
    const apiError = new ApiError(
      response.status,
      serverError?.code ?? 'request_failed',
      serverError?.message ?? '请求处理失败',
      serverError?.request_id ?? response.headers.get('X-Request-ID') ?? '',
      serverError?.details ?? [],
    )
    if (response.status === 401) {
      window.dispatchEvent(new CustomEvent('panel:unauthorized'))
    }
    throw apiError
  }
  if (payload === undefined || typeof payload !== 'object' || payload === null || !('data' in payload)) {
    throw new ApiError(response.status, 'invalid_response', '服务端返回了无法识别的响应')
  }
  return (payload as DataEnvelope<T>).data
}

export const api = {
  get: <T>(path: string, signal?: AbortSignal) => request<T>('GET', path, undefined, signal),
  post: <T>(path: string, body?: unknown, signal?: AbortSignal) => request<T>('POST', path, body, signal),
  patch: <T>(path: string, body: unknown, signal?: AbortSignal) => request<T>('PATCH', path, body, signal),
  delete: <T>(path: string, body?: unknown, signal?: AbortSignal) => request<T>('DELETE', path, body, signal),
}

export function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    const fieldMessage = error.details[0]
    const suffix = error.requestId ? `（请求 ${error.requestId}）` : ''
    return `${fieldMessage ? `${fieldMessage.field}: ${fieldMessage.message}` : error.message}${suffix}`
  }
  return '操作失败，请稍后重试'
}
