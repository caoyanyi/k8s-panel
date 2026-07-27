import { CheckCircle2, CircleHelp, KeyRound, LoaderCircle, Plus, Power, RefreshCw, ShieldCheck, Trash2, XCircle } from 'lucide-react'
import { type FormEvent, useCallback, useRef, useState } from 'react'
import { api, errorMessage } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { Modal } from '../components/Modal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { usePanel } from '../context'
import type { Cluster, ClusterCapabilities, Environment, KubernetesCapabilityState } from '../types'
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

const initialRotationForm = {
  bearer_token: '',
  ca_cert: '',
  confirmation: '',
}

const maxBearerTokenLength = 64 * 1024
const maxCACertLength = 256 * 1024

export function ClustersPage({ notify }: ClustersPageProps) {
  const { clusters, clustersLoading, clustersError, refreshClusters, selectedNamespace, setSelectedClusterId } = usePanel()
  const [createOpen, setCreateOpen] = useState(false)
  const [form, setForm] = useState(initialForm)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<Cluster | null>(null)
  const [confirmation, setConfirmation] = useState('')
  const [rotationTarget, setRotationTarget] = useState<Cluster | null>(null)
  const [rotationForm, setRotationForm] = useState(initialRotationForm)
  const [capabilityTarget, setCapabilityTarget] = useState<Cluster | null>(null)
  const [capabilityNamespace, setCapabilityNamespace] = useState('default')
  const [capabilityResult, setCapabilityResult] = useState<ClusterCapabilities | null>(null)
  const [capabilityLoading, setCapabilityLoading] = useState(false)
  const [capabilityError, setCapabilityError] = useState('')
  const capabilityAbortRef = useRef<AbortController | null>(null)
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

  const closeRotation = useCallback(() => {
    setRotationTarget(null)
    setRotationForm(initialRotationForm)
    setFormError('')
  }, [])

  const rotateCredentials = async (event: FormEvent) => {
    event.preventDefault()
    if (!rotationTarget) return
    setSubmitting(true)
    setFormError('')
    try {
      await api.post<Cluster>(`/api/v1/clusters/${rotationTarget.id}/credential-rotations`, {
        bearer_token: rotationForm.bearer_token,
        ca_cert: rotationForm.ca_cert,
        confirmation: rotationForm.confirmation,
      })
      await refreshClusters()
      notify('success', `${rotationTarget.name} 凭据已轮换`)
      closeRotation()
    } catch (error) {
      setFormError(errorMessage(error))
    } finally {
      setSubmitting(false)
      setRotationForm((current) => ({ ...current, bearer_token: '', ca_cert: '' }))
    }
  }

  const closeCapabilities = useCallback(() => {
    const controller = capabilityAbortRef.current
    capabilityAbortRef.current = null
    controller?.abort()
    setCapabilityTarget(null)
    setCapabilityResult(null)
    setCapabilityLoading(false)
    setCapabilityError('')
  }, [])

  const openCapabilities = (cluster: Cluster) => {
    setCapabilityTarget(cluster)
    setCapabilityNamespace(selectedNamespace || 'default')
    setCapabilityResult(null)
    setCapabilityError('')
  }

  const checkCapabilities = async (event: FormEvent) => {
    event.preventDefault()
    if (!capabilityTarget) return
    const namespace = capabilityNamespace.trim()
    if (!namespace) {
      setCapabilityError('命名空间为必填项')
      return
    }
    capabilityAbortRef.current?.abort()
    const controller = new AbortController()
    capabilityAbortRef.current = controller
    setCapabilityNamespace(namespace)
    setCapabilityLoading(true)
    setCapabilityResult(null)
    setCapabilityError('')
    try {
      const result = await api.get<ClusterCapabilities>(
        `/api/v1/clusters/${capabilityTarget.id}/capabilities?namespace=${encodeURIComponent(namespace)}`,
        controller.signal,
      )
      if (capabilityAbortRef.current === controller) setCapabilityResult(result)
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError') && capabilityAbortRef.current === controller) {
        setCapabilityError(errorMessage(error))
      }
    } finally {
      if (capabilityAbortRef.current === controller) {
        capabilityAbortRef.current = null
        setCapabilityLoading(false)
      }
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
            <table className="cluster-table">
              <thead><tr><th>名称</th><th>环境</th><th>连接状态</th><th>版本</th><th>API Server</th><th>最近检测</th><th className="actions-column cluster-actions-column">操作</th></tr></thead>
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
                      <button
                        type="button"
                        className="icon-button"
                        title="轮换凭据"
                        aria-label={`轮换 ${cluster.name} 凭据`}
                        disabled={busyID === cluster.id || cluster.status === 'disabled'}
                        onClick={() => { setRotationTarget(cluster); setRotationForm(initialRotationForm); setFormError('') }}
                      >
                        <KeyRound size={16} />
                      </button>
                      <button
                        type="button"
                        className="icon-button"
                        title="权限检测"
                        aria-label={`检测 ${cluster.name} 权限`}
                        disabled={busyID === cluster.id || cluster.status === 'disabled'}
                        onClick={() => openCapabilities(cluster)}
                      >
                        <ShieldCheck size={16} />
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
          <div className="field field-full"><label htmlFor="cluster-token">Bearer Token</label><textarea id="cluster-token" className="secret-input" rows={3} maxLength={maxBearerTokenLength} value={form.bearer_token} onChange={(event) => setForm({ ...form, bearer_token: event.target.value })} autoComplete="off" spellCheck={false} /></div>
          <div className="field field-full"><label htmlFor="cluster-ca">CA 证书（PEM，可选）</label><textarea id="cluster-ca" className="mono" rows={5} maxLength={maxCACertLength} value={form.ca_cert} onChange={(event) => setForm({ ...form, ca_cert: event.target.value })} spellCheck={false} /></div>
          {formError && <div className="form-error field-full" role="alert">{formError}</div>}
          <div className="form-actions field-full"><button type="button" className="button button-secondary" onClick={closeCreate}>取消</button><button type="submit" className="button button-primary" disabled={submitting}>{submitting && <LoaderCircle className="spin" size={16} />} 保存并检测</button></div>
        </form>
      </Modal>

      <Modal title="轮换集群凭据" open={Boolean(rotationTarget)} onClose={closeRotation}>
        {rotationTarget && (
          <form className="credential-dialog" onSubmit={rotateCredentials} noValidate>
            <div className="credential-target">
              <span>集群</span>
              <strong>{rotationTarget.name}</strong>
              <small>{rotationTarget.server}</small>
            </div>
            <div className="field">
              <label htmlFor="rotation-token">Bearer Token</label>
              <textarea
                id="rotation-token"
                className="secret-input"
                rows={3}
                maxLength={maxBearerTokenLength}
                value={rotationForm.bearer_token}
                onChange={(event) => setRotationForm((current) => ({ ...current, bearer_token: event.target.value }))}
                autoComplete="off"
                spellCheck={false}
                autoFocus
              />
            </div>
            <div className="field">
              <label htmlFor="rotation-ca">CA 证书（PEM，可选）</label>
              <textarea
                id="rotation-ca"
                className="mono"
                rows={5}
                maxLength={maxCACertLength}
                value={rotationForm.ca_cert}
                onChange={(event) => setRotationForm((current) => ({ ...current, ca_cert: event.target.value }))}
                spellCheck={false}
              />
            </div>
            <div className="field">
              <label htmlFor="rotation-confirmation">输入集群名称确认</label>
              <input
                id="rotation-confirmation"
                maxLength={64}
                value={rotationForm.confirmation}
                onChange={(event) => setRotationForm((current) => ({ ...current, confirmation: event.target.value }))}
                autoComplete="off"
              />
            </div>
            {formError && <div className="form-error" role="alert">{formError}</div>}
            <div className="form-actions">
              <button type="button" className="button button-secondary" onClick={closeRotation}>取消</button>
              <button
                type="submit"
                className="button button-primary"
                disabled={!rotationForm.bearer_token || rotationForm.confirmation !== rotationTarget.name || submitting}
              >
                {submitting ? <LoaderCircle className="spin" size={16} /> : <KeyRound size={16} />}
                验证并轮换
              </button>
            </div>
          </form>
        )}
      </Modal>

      <Modal title="权限能力检测" open={Boolean(capabilityTarget)} onClose={closeCapabilities} width="wide">
        {capabilityTarget && (
          <form className="capability-dialog" onSubmit={checkCapabilities} noValidate>
            <div className="capability-target">
              <span>集群</span>
              <strong>{capabilityTarget.name}</strong>
              <small>{capabilityTarget.server}</small>
            </div>
            <div className="capability-controls">
              <div className="field">
                <label htmlFor="capability-namespace">命名空间</label>
                <input
                  id="capability-namespace"
                  maxLength={63}
                  value={capabilityNamespace}
                  onChange={(event) => setCapabilityNamespace(event.target.value)}
                  disabled={capabilityLoading}
                  autoComplete="off"
                  autoFocus
                />
              </div>
              <button type="submit" className="button button-primary" disabled={capabilityLoading || !capabilityNamespace.trim()}>
                {capabilityLoading ? <LoaderCircle className="spin" size={16} /> : <ShieldCheck size={16} />}
                检测权限
              </button>
            </div>
            {capabilityError && <div className="form-error" role="alert">{capabilityError}</div>}
            {capabilityResult && (
              <div className="capability-results" aria-live="polite">
                <div className="capability-result-meta">
                  <span>命名空间 <strong>{capabilityResult.namespace}</strong></span>
                  <span>检测时间 {formatDateTime(capabilityResult.checked_at)}</span>
                </div>
                <div className="table-wrap capability-table" role="region" aria-label="权限能力检测结果" tabIndex={0}>
                  <table>
                    <thead><tr><th>能力</th><th>范围</th><th>结果</th></tr></thead>
                    <tbody>{capabilityResult.checks.map((check) => (
                      <tr key={check.key}>
                        <td><strong>{capabilityLabel(check.key)}</strong></td>
                        <td>{capabilityScope(check.key, capabilityResult.namespace)}</td>
                        <td><CapabilityState state={check.state} /></td>
                      </tr>
                    ))}</tbody>
                  </table>
                </div>
              </div>
            )}
          </form>
        )}
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

const capabilityLabels: Record<string, string> = {
  'namespaces.list': '命名空间列表',
  'nodes.list': '节点列表',
  'pods.list': 'Pod 列表',
  'pods.logs.get': 'Pod 日志',
  'events.list': '事件列表',
  'deployments.list': 'Deployment 列表',
  'statefulsets.list': 'StatefulSet 列表',
  'daemonsets.list': 'DaemonSet 列表',
  'deployments.patch': 'Deployment 变更',
  'deployments.scale.patch': 'Deployment 扩缩容',
}

function capabilityLabel(key: string) {
  return capabilityLabels[key] ?? key
}

function capabilityScope(key: string, namespace: string) {
  return key === 'namespaces.list' || key === 'nodes.list' ? '集群' : namespace
}

function CapabilityState({ state }: { state: KubernetesCapabilityState }) {
  if (state === 'allowed') {
    return <span className="capability-state capability-allowed"><CheckCircle2 size={15} />允许</span>
  }
  if (state === 'denied') {
    return <span className="capability-state capability-denied"><XCircle size={15} />拒绝</span>
  }
  return <span className="capability-state capability-indeterminate"><CircleHelp size={15} />无法判定</span>
}
