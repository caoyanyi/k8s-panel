import { LoaderCircle, Plus, Power, RefreshCw, Trash2 } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { api, errorMessage } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { Modal } from '../components/Modal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { usePanel } from '../context'
import type { Cluster, Environment } from '../types'
import { formatDateTime } from '../utils'

interface ClustersPageProps {
  notify: (tone: 'success' | 'error', message: string) => void
  openCreateSignal?: number
}

const initialForm = {
  name: '',
  environment: 'development' as Environment,
  server: '',
  ca_cert: '',
  bearer_token: '',
}

export function ClustersPage({ notify }: ClustersPageProps) {
  const { clusters, clustersLoading, clustersError, refreshClusters, setSelectedClusterId } = usePanel()
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState(initialForm)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [busyID, setBusyID] = useState('')

  const closeCreate = () => {
    setCreateOpen(false)
    setForm(initialForm)
    setFormError('')
  }

  const createCluster = async (event: FormEvent) => {
    event.preventDefault()
    if (!form.name.trim() || !form.server.trim() || !form.bearer_token) {
      setFormError('名称、API Server 和 Bearer Token 为必填项')
      return
    }
    setSubmitting(true)
    setFormError('')
    try {
      const created = await api.post<Cluster>('/api/v1/clusters', {
        ...form,
        name: form.name.trim(),
        server: form.server.trim(),
      })
      setSelectedClusterId(created.id)
      await refreshClusters()
      closeCreate()
      notify('success', `集群 ${created.name} 已接入`)
    } catch (error) {
      setFormError(errorMessage(error))
    } finally {
      setSubmitting(false)
      setForm((current) => ({ ...current, bearer_token: '', ca_cert: '' }))
    }
  }

  const runAction = async (cluster: Cluster, action: 'test' | 'toggle') => {
    setBusyID(cluster.id)
    try {
      if (action === 'test') {
        await api.post(`/api/v1/clusters/${cluster.id}/connection-tests`)
        notify('success', `${cluster.name} 连接检测完成`)
      } else {
        await api.patch(`/api/v1/clusters/${cluster.id}`, { enabled: cluster.status === 'disabled' })
        notify('success', `${cluster.name} 已${cluster.status === 'disabled' ? '启用' : '停用'}`)
      }
      await refreshClusters()
    } catch (error) {
      notify('error', errorMessage(error))
      await refreshClusters()
    } finally {
      setBusyID('')
    }
  }

  const removeCluster = async () => {
    if (!deleteTarget) return
    setSubmitting(true)
    try {
      await api.delete(`/api/v1/clusters/${deleteTarget.id}`, { confirmation })
      notify('success', `连接 ${deleteTarget.name} 已删除`)
      setDeleteTarget(null)
      setConfirmation('')
      await refreshClusters()
    } catch (error) {
      setFormError(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="page">
      <PageHeader
        title="集群管理"
        meta={`${clusters.length} 个连接`}
        actions={<button type="button" className="button button-primary" onClick={() => setCreateOpen(true)}><Plus size={17} /> 接入集群</button>}
      />
      <section className="section-block table-section">
        {clustersLoading ? <LoadingState label="正在读取集群" /> : clustersError ? (
          <ErrorState error={clustersError} onRetry={() => void refreshClusters()} />
        ) : clusters.length === 0 ? (
          <EmptyState title="尚未接入集群" action={<button className="button button-primary" onClick={() => setCreateOpen(true)}>接入集群</button>} />
        ) : (
          <div className="table-wrap">
            <table>
              <thead><tr><th>名称</th><th>环境</th><th>连接状态</th><th>版本</th><th>API Server</th><th>最近检测</th><th className="actions-column">操作</th></tr></thead>
              <tbody>{clusters.map((cluster) => (
                <tr key={cluster.id}>
                  <td><div className="primary-cell"><strong>{cluster.name}</strong><span className="mono subtle-id">{cluster.id}</span></div></td>
                  <td><span className={`environment-label env-${cluster.environment}`}>{environmentLabel(cluster.environment)}</span></td>
                  <td><StatusBadge status={cluster.status} /></td>
                  <td className="mono">{cluster.version || '-'}</td>
                  <td className="mono clipped-cell" title={cluster.server}>{cluster.server}</td>
                  <td>{formatDateTime(cluster.last_checked_at)}</td>
                  <td>
                    <div className="row-actions">
                      <button type="button" className="icon-button" title="检测连接" aria-label={`检测 ${cluster.name} 连接`} disabled={busyID === cluster.id || cluster.status === 'disabled'} onClick={() => void runAction(cluster, 'test')}>
                        <RefreshCw size={16} className={busyID === cluster.id ? 'spin' : ''} />
                      </button>
                      <button type="button" className="icon-button" title={cluster.status === 'disabled' ? '启用' : '停用'} aria-label={`${cluster.status === 'disabled' ? '启用' : '停用'} ${cluster.name}`} disabled={busyID === cluster.id} onClick={() => void runAction(cluster, 'toggle')}>
                        <Power size={16} />
                      </button>
                      <button type="button" className="icon-button icon-danger" title="删除连接" aria-label={`删除 ${cluster.name}`} onClick={() => { setDeleteTarget(cluster); setFormError('') }}>
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}</tbody>
            </table>
          </div>
        )}
      </section>

      <Modal title="接入 Kubernetes 集群" open={createOpen} onClose={closeCreate} width="wide">
        <form onSubmit={createCluster} className="form-grid" noValidate>
          <div className="field"><label htmlFor="cluster-name">名称</label><input id="cluster-name" value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} autoFocus /></div>
          <div className="field"><label htmlFor="cluster-environment">环境</label><select id="cluster-environment" value={form.environment} onChange={(event) => setForm({ ...form, environment: event.target.value as Environment })}><option value="development">开发</option><option value="staging">预发</option><option value="production">生产</option></select></div>
          <div className="field field-full"><label htmlFor="cluster-server">API Server</label><input id="cluster-server" type="url" placeholder="https://api.example.com:6443" value={form.server} onChange={(event) => setForm({ ...form, server: event.target.value })} /></div>
          <div className="field field-full"><label htmlFor="cluster-token">Bearer Token</label><textarea id="cluster-token" className="secret-input" rows={3} value={form.bearer_token} onChange={(event) => setForm({ ...form, bearer_token: event.target.value })} autoComplete="off" spellCheck={false} /></div>
          <div className="field field-full"><label htmlFor="cluster-ca">CA 证书（PEM，可选）</label><textarea id="cluster-ca" className="mono" rows={5} value={form.ca_cert} onChange={(event) => setForm({ ...form, ca_cert: event.target.value })} spellCheck={false} /></div>
          {formError && <div className="form-error field-full" role="alert">{formError}</div>}
          <div className="form-actions field-full"><button type="button" className="button button-secondary" onClick={closeCreate}>取消</button><button type="submit" className="button button-primary" disabled={submitting}>{submitting && <LoaderCircle className="spin" size={16} />} 保存并检测</button></div>
        </form>
      </Modal>

      <Modal title="删除集群连接" open={Boolean(deleteTarget)} onClose={() => { setDeleteTarget(null); setConfirmation(''); setFormError('') }}>
        {deleteTarget && <div className="danger-dialog">
          <div className="danger-target"><span>集群</span><strong>{deleteTarget.name}</strong><small>{deleteTarget.server}</small></div>
          <div className="field"><label htmlFor="cluster-confirmation">输入集群名称确认</label><input id="cluster-confirmation" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} autoFocus /></div>
          {formError && <div className="form-error" role="alert">{formError}</div>}
          <div className="form-actions"><button className="button button-secondary" onClick={() => setDeleteTarget(null)}>取消</button><button className="button button-danger" disabled={confirmation !== deleteTarget.name || submitting} onClick={() => void removeCluster()}><Trash2 size={16} /> 删除连接</button></div>
        </div>}
      </Modal>
    </div>
  )
}

function environmentLabel(value: Environment) {
  return { development: '开发', staging: '预发', production: '生产' }[value]
}
