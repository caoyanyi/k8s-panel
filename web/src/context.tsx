import { createContext, useContext } from 'react'
import type { Cluster } from './types'

export interface PanelContextValue {
  clusters: Cluster[]
  clustersLoading: boolean
  clustersError: unknown
  selectedClusterId: string
  selectedNamespace: string
  setSelectedClusterId: (id: string) => void
  setSelectedNamespace: (namespace: string) => void
  refreshClusters: () => Promise<void>
}

export const PanelContext = createContext<PanelContextValue | null>(null)

export function usePanel() {
  const value = useContext(PanelContext)
  if (!value) throw new Error('usePanel must be used inside PanelContext')
  return value
}
