import { useCallback, useEffect, useState } from 'react'
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

  const refresh = useCallback(async () => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    try {
      setData(await loader(controller.signal))
    } catch (caught) {
      if (!(caught instanceof DOMException && caught.name === 'AbortError')) {
        setError(caught)
      }
    } finally {
      setLoading(false)
    }
  }, dependencies)

  useEffect(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(null)
    loader(controller.signal)
      .then(setData)
      .catch((caught: unknown) => {
        if (!(caught instanceof DOMException && caught.name === 'AbortError')) {
          setError(caught)
        }
      })
      .finally(() => setLoading(false))
    return () => controller.abort()
  }, dependencies)

  return { data, loading, error, refresh }
}

export function isUnauthorized(error: unknown): boolean {
  return error instanceof ApiError && error.status === 401
}
