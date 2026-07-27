import {
  Boxes,
  ChevronDown,
  ClipboardList,
  Cpu,
  Gauge,
  History,
  Layers3,
  LogOut,
  Menu,
  Network,
  PackageOpen,
  Server,
  X,
} from 'lucide-react'
import { type ReactNode, useEffect, useRef, useState } from 'react'
import type { Principal } from './types'
import { usePanel } from './context'

export type RouteName = 'dashboard' | 'clusters' | 'resources' | 'workloads' | 'network' | 'helm' | 'operations' | 'audit'

const navigation: Array<{ route: RouteName; label: string; icon: typeof Gauge }> = [
  { route: 'dashboard', label: '总览', icon: Gauge },
  { route: 'clusters', label: '集群', icon: Server },
  { route: 'resources', label: '集群资源', icon: Cpu },
  { route: 'workloads', label: '工作负载', icon: Layers3 },
  { route: 'network', label: '网络', icon: Network },
  { route: 'helm', label: 'Helm', icon: PackageOpen },
  { route: 'operations', label: '操作中心', icon: ClipboardList },
  { route: 'audit', label: '审计', icon: History },
]

interface LayoutProps {
  principal: Principal
  route: RouteName
  onNavigate: (route: RouteName) => void
  onLogout: () => Promise<void>
  children: ReactNode
  notice?: { tone: 'success' | 'error'; message: string } | null
}

export function Layout({ principal, route, onNavigate, onLogout, children, notice }: LayoutProps) {
  const { clusters, selectedClusterId, setSelectedClusterId } = usePanel()
  const [mobileOpen, setMobileOpen] = useState(false)
  const [accountOpen, setAccountOpen] = useState(false)
  const accountRef = useRef<HTMLDivElement>(null)
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)

  useEffect(() => {
    const close = (event: MouseEvent) => {
      if (!accountRef.current?.contains(event.target as Node)) setAccountOpen(false)
    }
    document.addEventListener('mousedown', close)
    return () => document.removeEventListener('mousedown', close)
  }, [])

  const navigate = (next: RouteName) => {
    onNavigate(next)
    setMobileOpen(false)
  }

  return (
    <div className="app-shell">
      {mobileOpen && <button className="mobile-scrim" aria-label="关闭导航" onClick={() => setMobileOpen(false)} />}
      <aside className={`sidebar ${mobileOpen ? 'sidebar-open' : ''}`}>
        <div className="sidebar-brand">
          <span className="brand-mark"><Boxes size={22} /></span>
          <strong>KubePanel</strong>
          <button type="button" className="icon-button mobile-only" aria-label="关闭导航" onClick={() => setMobileOpen(false)}>
            <X size={18} />
          </button>
        </div>
        <nav aria-label="主导航" className="main-nav">
          {navigation.map((item) => {
            const Icon = item.icon
            return (
              <button
                type="button"
                key={item.route}
                className={route === item.route ? 'nav-item nav-item-active' : 'nav-item'}
                aria-current={route === item.route ? 'page' : undefined}
                onClick={() => navigate(item.route)}
              >
                <Icon size={18} aria-hidden="true" />
                <span>{item.label}</span>
              </button>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <span className="service-indicator"><span /> 控制面在线</span>
          <small>MVP 0.1.0</small>
        </div>
      </aside>
      <div className="workspace">
        <header className="topbar">
          <button type="button" className="icon-button mobile-only" aria-label="打开导航" onClick={() => setMobileOpen(true)}>
            <Menu size={20} />
          </button>
          <div className="context-control">
            <label htmlFor="global-cluster">当前集群</label>
            <select
              id="global-cluster"
              value={selectedClusterId}
              onChange={(event) => setSelectedClusterId(event.target.value)}
              disabled={clusters.length === 0}
            >
              {clusters.length === 0 && <option value="">未选择</option>}
              {clusters.map((cluster) => <option key={cluster.id} value={cluster.id}>{cluster.name}</option>)}
            </select>
            {selectedCluster?.environment === 'production' && <span className="environment-flag">生产</span>}
          </div>
          <div className="topbar-spacer" />
          <div className="account" ref={accountRef}>
            <button
              type="button"
              className="account-button"
              aria-label={`${principal.username} 账户菜单`}
              aria-expanded={accountOpen}
              onClick={() => setAccountOpen((open) => !open)}
            >
              <span className="avatar">{principal.username.slice(0, 1).toUpperCase()}</span>
              <span className="account-name">{principal.username}</span>
              <ChevronDown size={15} aria-hidden="true" />
            </button>
            {accountOpen && (
              <div className="account-menu">
                <div><strong>{principal.username}</strong><span>系统管理员</span></div>
                <button type="button" onClick={() => void onLogout()}><LogOut size={16} /> 退出登录</button>
              </div>
            )}
          </div>
        </header>
        {notice && <div className={`notice notice-${notice.tone}`} role="status">{notice.message}</div>}
        <main className="content">{children}</main>
      </div>
    </div>
  )
}
