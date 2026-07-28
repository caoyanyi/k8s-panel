import { LoaderCircle } from 'lucide-react'
import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, ApiError } from './api'
import { PanelContext } from './context'
import { Layout, type RouteName } from './Layout'
import { LoginPage } from './LoginPage'
import { useResource } from './hooks'
import { AccessPage } from './pages/AccessPage'
import { AuditPage } from './pages/AuditPage'
import { ClusterResourcesPage } from './pages/ClusterResourcesPage'
import { ClustersPage } from './pages/ClustersPage'
import { ConfigurationPage } from './pages/ConfigurationPage'
import { DashboardPage } from './pages/DashboardPage'
import { EventsPage } from './pages/EventsPage'
import { GovernancePage } from './pages/GovernancePage'
import { HelmPage } from './pages/HelmPage'
import { NetworkPage } from './pages/NetworkPage'
import { OperationsPage } from './pages/OperationsPage'
import { StoragePage } from './pages/StoragePage'
import { WorkloadsPage } from './pages/WorkloadsPage'
import type { Cluster, Principal } from './types'

type AuthState =
  | { status: 'checking'; principal: null }
  | { status: 'anonymous'; principal: null }
  | { status: 'authenticated'; principal: Principal }

export function App() {
  const [auth, setAuth] = useState<AuthState>({ status: 'checking', principal: null })

  useEffect(() => {
    const unauthorized = () => setAuth({ status: 'anonymous', principal: null })
    window.addEventListener('panel:unauthorized', unauthorized)
    api.get<Principal>('/api/v1/session')
      .then((principal) => setAuth({ status: 'authenticated', principal }))
      .catch((error: unknown) => {
        if (error instanceof ApiError && error.status === 401) {
          setAuth({ status: 'anonymous', principal: null })
        } else {
          setAuth({ status: 'anonymous', principal: null })
        }
      })
    return () => window.removeEventListener('panel:unauthorized', unauthorized)
  }, [])

  if (auth.status === 'checking') {
    return <div className="app-loading" role="status"><LoaderCircle className="spin" size={24} /><span>正在连接控制面</span></div>
  }
  if (auth.status === 'anonymous') {
    return <LoginPage onAuthenticated={(principal) => setAuth({ status: 'authenticated', principal })} />
  }
  return <AuthenticatedApp principal={auth.principal} onAnonymous={() => setAuth({ status: 'anonymous', principal: null })} />
}

function AuthenticatedApp({ principal, onAnonymous }: { principal: Principal; onAnonymous: () => void }) {
  const clusters = useResource((signal) => api.get<Cluster[]>('/api/v1/clusters', signal), [])
  const [selectedClusterId, setSelectedClusterId] = useState('')
  const [selectedNamespace, setSelectedNamespace] = useState('')
  const [route, setRoute] = useState<RouteName>(readRoute)
  const [notice, setNotice] = useState<{ tone: 'success' | 'error'; message: string } | null>(null)

  useEffect(() => {
    const onHashChange = () => setRoute(readRoute())
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])
  useEffect(() => {
    const items = clusters.data ?? []
    if (items.length === 0) {
      setSelectedClusterId('')
      setSelectedNamespace('')
      return
    }
    if (!items.some((item) => item.id === selectedClusterId)) {
      setSelectedClusterId(items[0].id)
      setSelectedNamespace('')
    }
  }, [clusters.data, selectedClusterId])
  useEffect(() => {
    if (!notice) return
    const timer = window.setTimeout(() => setNotice(null), 4500)
    return () => window.clearTimeout(timer)
  }, [notice])

  const navigate = useCallback((next: RouteName) => {
    window.location.hash = next
    setRoute(next)
  }, [])
  const notify = useCallback((tone: 'success' | 'error', message: string) => setNotice({ tone, message }), [])
  const logout = async () => {
    try {
      await api.delete('/api/v1/session')
    } finally {
      onAnonymous()
    }
  }
  const contextValue = useMemo(() => ({
    clusters: clusters.data ?? [],
    clustersLoading: clusters.loading,
    clustersError: clusters.error,
    selectedClusterId,
    selectedNamespace,
    setSelectedClusterId: (id: string) => { setSelectedClusterId(id); setSelectedNamespace('') },
    setSelectedNamespace,
    refreshClusters: clusters.refresh,
  }), [clusters.data, clusters.loading, clusters.error, clusters.refresh, selectedClusterId, selectedNamespace])

  return (
    <PanelContext.Provider value={contextValue}>
      <Layout principal={principal} route={route} onNavigate={navigate} onLogout={logout} notice={notice}>
        {route === 'dashboard' && <DashboardPage onOpenClusters={() => navigate('clusters')} />}
        {route === 'clusters' && <ClustersPage notify={notify} />}
        {route === 'resources' && <ClusterResourcesPage />}
        {route === 'workloads' && <WorkloadsPage notify={notify} openOperations={() => navigate('operations')} />}
        {route === 'network' && <NetworkPage />}
        {route === 'configuration' && <ConfigurationPage />}
        {route === 'storage' && <StoragePage />}
        {route === 'governance' && <GovernancePage />}
        {route === 'access' && <AccessPage />}
        {route === 'events' && <EventsPage />}
        {route === 'helm' && <HelmPage notify={notify} openOperations={() => navigate('operations')} />}
        {route === 'operations' && <OperationsPage notify={notify} />}
        {route === 'audit' && <AuditPage />}
      </Layout>
    </PanelContext.Provider>
  )
}

function readRoute(): RouteName {
  const candidate = window.location.hash.replace(/^#\/?/, '') as RouteName
  return ['dashboard', 'clusters', 'resources', 'workloads', 'network', 'configuration', 'storage', 'governance', 'access', 'events', 'helm', 'operations', 'audit'].includes(candidate) ? candidate : 'dashboard'
}
