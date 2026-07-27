import { useCallback, useEffect, useRef, useState } from 'react'
import { ApiError } from './api'

export interface ResourceState<T> {
  data: T | null
  loading: boolean
  error: unknown
  refresh: () => Promise<void>
}

export function useResource<T>(loader: (signal: AbortSignal) => Promise<T>, dependencies: unknown[] = []): ResourceState<T> {
  const [data, setData] = useState<T | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<unknown>(null)
  const activeController = useRef<AbortController | null>(null)
  const requestSequence = useRef(0)

  const refresh = useCallback(async () => {
    activeController.current?.abort()
    const controller = new AbortController()
    const sequence = ++requestSequence.current
    activeController.current = controller
    setLoading(true)
    setError(null)
    try {
      const nextData = await loader(controller.signal)
      if (requestSequence.current === sequence) setData(nextData)
    } catch (caught) {
      if (requestSequence.current === sequence && !(caught instanceof DOMException && caught.name === 'AbortError')) {
        setError(caught)
      }
    } finally {
      if (requestSequence.current === sequence) {
        activeController.current = null
        setLoading(false)
      }
    }
  }, dependencies)

  useEffect(() => {
    void refresh()
    return () => {
      requestSequence.current++
      activeController.current?.abort()
      activeController.current = null
    }
  }, [refresh])

  return { data, loading, error, refresh }
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}
