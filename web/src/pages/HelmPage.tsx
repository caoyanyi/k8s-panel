import { ArrowDownToLine, History, LoaderCircle, Plus, Power, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { type FormEvent, useEffect, useState } from 'react'
import { api, errorMessage } from '../api'
import { EmptyState, ErrorState, LoadingState } from '../components/DataState'
import { Modal } from '../components/Modal'
import { PageHeader } from '../components/PageHeader'
import { StatusBadge } from '../components/StatusBadge'
import { usePanel } from '../context'
import { useResource } from '../hooks'
import type { ChartRepository, HelmRelease, Namespace, Operation } from '../types'
import { formatDateTime } from '../utils'

interface HelmPageProps {
  notify: (tone: 'success' | 'error', message: string) => void
  openOperations: () => void
}

type ReleaseAction = { type: 'upgrade' | 'rollback' | 'uninstall'; release: HelmRelease } | null

export function HelmPage({ notify, openOperations }: HelmPageProps) {
  const { clusters, selectedClusterId, selectedNamespace, setSelectedNamespace } = usePanel()
  const [tab, setTab] = useState<'releases' | 'repositories'>('releases')
  const [repositoryOpen, setRepositoryOpen] = useState(false)
  const [installOpen, setInstallOpen] = useState(false)
  const [releaseAction, setReleaseAction] = useState<ReleaseAction>(null)
  const [submitting, setSubmitting] = useState(false)
  const [formError, setFormError] = useState('')
  const [repositoryForm, setRepositoryForm] = useState({ name: '', url: '', username: '', password: '' })
  const [releaseForm, setReleaseForm] = useState({ release_name: '', chart: '', repository_id: '', version: '', values: '', revision: 1 })
  const selectedCluster = clusters.find((cluster) => cluster.id === selectedClusterId)
  const repositories = useResource((signal) => api.get<ChartRepository[]>('/api/v1/chart-repositories', signal), [])
  const namespaces = useResource(
    (signal) => selectedClusterId ? api.get<Namespace[]>(`/api/v1/clusters/${selectedClusterId}/namespaces`, signal) : Promise.resolve([]),
    [selectedClusterId],
  )
  const releaseQuery = new URLSearchParams({ cluster_id: selectedClusterId })
  if (selectedNamespace) releaseQuery.set('namespace', selectedNamespace)
  const releases = useResource(
    (signal) => selectedClusterId ? api.get<HelmRelease[]>(`/api/v1/helm-releases?${releaseQuery}`, signal) : Promise.resolve([]),
    [selectedClusterId, selectedNamespace],
  )

  useEffect(() => {
    if (!selectedNamespace && namespaces.data?.length) setSelectedNamespace(namespaces.data[0].name)
  }, [namespaces.data, selectedNamespace, setSelectedNamespace])

  const closeRepository = () => {
    setRepositoryOpen(false)
    setRepositoryForm({ name: '', url: '', username: '', password: '' })
    setFormError('')
  }

  const createRepository = async (event: FormEvent) => {
    event.preventDefault()
    if (!repositoryForm.name.trim() || !repositoryForm.url.trim()) {
      setFormError('名称和仓库地址为必填项')
      return
    }
    setSubmitting(true)
    setFormError('')
    try {
      await api.post('/api/v1/chart-repositories', repositoryForm)
      await repositories.refresh()
      closeRepository()
      notify('success', 'Chart 仓库已保存并完成检测')
    } catch (error) {
      setFormError(errorMessage(error))
      setRepositoryForm((current) => ({ ...current, password: '' }))
    } finally {
      setSubmitting(false)
    }
  }

  const repositoryAction = async (repository: ChartRepository, action: 'test' | 'toggle' | 'delete') => {
    try {
      if (action === 'test') await api.post(`/api/v1/chart-repositories/${repository.id}/connection-tests`)
      if (action === 'toggle') await api.patch(`/api/v1/chart-repositories/${repository.id}`, { enabled: !repository.enabled })
      if (action === 'delete') await api.delete(`/api/v1/chart-repositories/${repository.id}`)
      notify('success', `${repository.name} 已${action === 'delete' ? '删除' : action === 'test' ? '完成检测' : repository.enabled ? '停用' : '启用'}`)
      await repositories.refresh()
    } catch (error) {
      notify('error', errorMessage(error))
      await repositories.refresh()
    }
  }

  const openInstall = () => {
    setReleaseForm({ release_name: '', chart: '', repository_id: repositories.data?.find((item) => item.enabled)?.id ?? '', version: '', values: '', revision: 1 })
    setFormError('')
    setInstallOpen(true)
  }

  const submitInstall = async (event: FormEvent) => {
    event.preventDefault()
    if (!selectedClusterId || !selectedNamespace || !releaseForm.release_name || !releaseForm.chart) {
      setFormError('集群、命名空间、Release 名称和 Chart 为必填项')
      return
    }
    setSubmitting(true)
    try {
      const operation = await api.post<Operation>('/api/v1/helm-releases', {
        cluster_id: selectedClusterId,
        namespace: selectedNamespace,
        ...releaseForm,
      })
      setInstallOpen(false)
      setReleaseForm({ release_name: '', chart: '', repository_id: '', version: '', values: '', revision: 1 })
      notify('success', `安装任务 ${operation.id} 已提交`)
      openOperations()
    } catch (error) {
      setFormError(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  const openReleaseAction = (type: NonNullable<ReleaseAction>['type'], release: HelmRelease) => {
    setReleaseAction({ type, release })
    setReleaseForm({
      release_name: release.name,
      chart: type === 'upgrade' ? release.chart.replace(/-[0-9][^-]*$/, '') : '',
      repository_id: repositories.data?.find((item) => item.enabled)?.id ?? '',
      version: '',
      values: '',
      revision: Math.max(1, release.revision - 1),
    })
    setFormError('')
  }

  const submitReleaseAction = async () => {
    if (!releaseAction || !selectedClusterId) return
    setSubmitting(true)
    const base = `/api/v1/helm-releases/${encodeURIComponent(releaseAction.release.name)}`
    const payload = {
      cluster_id: selectedClusterId,
      namespace: releaseAction.release.namespace,
      chart: releaseForm.chart,
      repository_id: releaseForm.repository_id,
      version: releaseForm.version,
      values: releaseForm.values,
      revision: releaseForm.revision,
    }
    try {
      let operation: Operation
      if (releaseAction.type === 'upgrade') operation = await api.post<Operation>(`${base}/upgrades`, payload)
      else if (releaseAction.type === 'rollback') operation = await api.post<Operation>(`${base}/rollbacks`, payload)
      else operation = await api.delete<Operation>(base, payload)
      setReleaseAction(null)
      notify('success', `任务 ${operation.id} 已提交`)
      openOperations()
    } catch (error) {
      setFormError(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="page">
      <PageHeader
        title="Helm"
        meta={selectedCluster ? `${selectedCluster.name} · ${selectedNamespace || '全部命名空间'}` : '选择一个集群'}
        actions={tab === 'releases'
          ? <button className="button button-primary" disabled={!selectedClusterId} onClick={openInstall}><ArrowDownToLine size={17} /> 安装 Release</button>
          : <button className="button button-primary" onClick={() => setRepositoryOpen(true)}><Plus size={17} /> 添加仓库</button>}
      />
      <div className="segmented-control" role="tablist" aria-label="Helm 视图">
        <button role="tab" aria-selected={tab === 'releases'} className={tab === 'releases' ? 'active' : ''} onClick={() => setTab('releases')}>Releases</button>
        <button role="tab" aria-selected={tab === 'repositories'} className={tab === 'repositories' ? 'active' : ''} onClick={() => setTab('repositories')}>Chart 仓库</button>
      </div>

      {tab === 'releases' ? (
        <>
          {selectedClusterId && <section className="toolbar"><div className="toolbar-field"><label htmlFor="helm-namespace">命名空间</label><select id="helm-namespace" value={selectedNamespace} onChange={(event) => setSelectedNamespace(event.target.value)}><option value="">全部命名空间</option>{namespaces.data?.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}</select></div><button className="icon-button toolbar-refresh" title="刷新" aria-label="刷新 Release" onClick={() => void releases.refresh()}><RefreshCw size={17} className={releases.loading ? 'spin' : ''} /></button></section>}
          <section className="section-block table-section">
            {!selectedClusterId ? <EmptyState title="尚未选择集群" /> : releases.loading ? <LoadingState label="正在读取 Helm Release" /> : releases.error ? <ErrorState error={releases.error} onRetry={() => void releases.refresh()} /> : !releases.data?.length ? <EmptyState title="当前范围没有 Helm Release" action={<button className="button button-primary" onClick={openInstall}>安装 Release</button>} /> : (
              <div className="table-wrap"><table><thead><tr><th>Release</th><th>命名空间</th><th>状态</th><th>Chart</th><th>Revision</th><th>更新时间</th><th className="actions-column">操作</th></tr></thead><tbody>{releases.data.map((release) => <tr key={`${release.namespace}:${release.name}`}>
                <td><strong>{release.name}</strong></td><td className="mono">{release.namespace}</td><td><StatusBadge status={release.status} /></td><td className="mono">{release.chart || '-'}</td><td>{release.revision}</td><td>{formatDateTime(release.updated_at)}</td>
                <td><div className="row-actions"><button className="icon-button" title="升级" aria-label={`升级 ${release.name}`} onClick={() => openReleaseAction('upgrade', release)}><RefreshCw size={16} /></button><button className="icon-button" title="回滚" aria-label={`回滚 ${release.name}`} onClick={() => openReleaseAction('rollback', release)}><RotateCcw size={16} /></button><button className="icon-button icon-danger" title="卸载" aria-label={`卸载 ${release.name}`} onClick={() => openReleaseAction('uninstall', release)}><Trash2 size={16} /></button></div></td>
              </tr>)}</tbody></table></div>
            )}
          </section>
        </>
      ) : (
        <section className="section-block table-section">
          {repositories.loading ? <LoadingState label="正在读取 Chart 仓库" /> : repositories.error ? <ErrorState error={repositories.error} onRetry={() => void repositories.refresh()} /> : !repositories.data?.length ? <EmptyState title="尚未配置 Chart 仓库" action={<button className="button button-primary" onClick={() => setRepositoryOpen(true)}>添加仓库</button>} /> : (
            <div className="table-wrap"><table><thead><tr><th>名称</th><th>地址</th><th>状态</th><th>认证</th><th>最近检测</th><th className="actions-column">操作</th></tr></thead><tbody>{repositories.data.map((repository) => <tr key={repository.id}>
              <td><strong>{repository.name}</strong></td><td className="mono clipped-cell" title={repository.url}>{repository.url}</td><td><StatusBadge status={repository.status} /></td><td>{repository.credentials_configured ? '已配置' : '匿名'}</td><td>{formatDateTime(repository.last_checked_at)}</td>
              <td><div className="row-actions"><button className="icon-button" title="检测" aria-label={`检测 ${repository.name}`} disabled={!repository.enabled} onClick={() => void repositoryAction(repository, 'test')}><RefreshCw size={16} /></button><button className="icon-button" title={repository.enabled ? '停用' : '启用'} aria-label={`${repository.enabled ? '停用' : '启用'} ${repository.name}`} onClick={() => void repositoryAction(repository, 'toggle')}><Power size={16} /></button><button className="icon-button icon-danger" title="删除" aria-label={`删除 ${repository.name}`} onClick={() => void repositoryAction(repository, 'delete')}><Trash2 size={16} /></button></div></td>
            </tr>)}</tbody></table></div>
          )}
        </section>
      )}

      <Modal title="添加 Chart 仓库" open={repositoryOpen} onClose={closeRepository}>
        <form onSubmit={createRepository} className="form-grid">
          <div className="field field-full"><label htmlFor="repo-name">名称</label><input id="repo-name" value={repositoryForm.name} onChange={(event) => setRepositoryForm({ ...repositoryForm, name: event.target.value })} autoFocus /></div>
          <div className="field field-full"><label htmlFor="repo-url">仓库地址</label><input id="repo-url" type="url" placeholder="https://charts.example.com" value={repositoryForm.url} onChange={(event) => setRepositoryForm({ ...repositoryForm, url: event.target.value })} /></div>
          <div className="field"><label htmlFor="repo-user">用户名（可选）</label><input id="repo-user" autoComplete="off" value={repositoryForm.username} onChange={(event) => setRepositoryForm({ ...repositoryForm, username: event.target.value })} /></div>
          <div className="field"><label htmlFor="repo-password">密码（可选）</label><input id="repo-password" type="password" autoComplete="new-password" value={repositoryForm.password} onChange={(event) => setRepositoryForm({ ...repositoryForm, password: event.target.value })} /></div>
          {formError && <div className="form-error field-full" role="alert">{formError}</div>}
          <div className="form-actions field-full"><button type="button" className="button button-secondary" onClick={closeRepository}>取消</button><button type="submit" className="button button-primary" disabled={submitting}>{submitting && <LoaderCircle className="spin" size={16} />} 保存并检测</button></div>
        </form>
      </Modal>

      <Modal title="安装 Helm Release" open={installOpen} onClose={() => { setInstallOpen(false); setFormError('') }} width="wide">
        <form onSubmit={submitInstall} className="form-grid">
          <ReleaseFields form={releaseForm} setForm={setReleaseForm} repositories={repositories.data ?? []} includeName />
          {formError && <div className="form-error field-full" role="alert">{formError}</div>}
          <div className="form-actions field-full"><button type="button" className="button button-secondary" onClick={() => setInstallOpen(false)}>取消</button><button type="submit" className="button button-primary" disabled={submitting}>{submitting && <LoaderCircle className="spin" size={16} />} 提交安装</button></div>
        </form>
      </Modal>

      <Modal title={releaseAction ? actionTitle(releaseAction.type, releaseAction.release.name) : ''} open={Boolean(releaseAction)} onClose={() => setReleaseAction(null)} width={releaseAction?.type === 'upgrade' ? 'wide' : 'normal'}>
        {releaseAction?.type === 'upgrade' && <div className="form-grid"><ReleaseFields form={releaseForm} setForm={setReleaseForm} repositories={repositories.data ?? []} /></div>}
        {releaseAction?.type === 'rollback' && <div className="field"><label htmlFor="rollback-revision">目标 Revision</label><input id="rollback-revision" type="number" min={1} value={releaseForm.revision} onChange={(event) => setReleaseForm({ ...releaseForm, revision: Number(event.target.value) })} /></div>}
        {releaseAction?.type === 'uninstall' && <div className="danger-target"><span>Release</span><strong>{releaseAction.release.name}</strong><small>{releaseAction.release.namespace} · {selectedCluster?.name}</small></div>}
        {formError && <div className="form-error" role="alert">{formError}</div>}
        <div className="form-actions"><button className="button button-secondary" onClick={() => setReleaseAction(null)}>取消</button><button className={releaseAction?.type === 'uninstall' ? 'button button-danger' : 'button button-primary'} disabled={submitting} onClick={() => void submitReleaseAction()}>{releaseAction?.type === 'rollback' ? <History size={16} /> : releaseAction?.type === 'uninstall' ? <Trash2 size={16} /> : <RefreshCw size={16} />}{releaseAction?.type === 'upgrade' ? '提交升级' : releaseAction?.type === 'rollback' ? '提交回滚' : '确认卸载'}</button></div>
      </Modal>
    </div>
  )
}

function ReleaseFields({ form, setForm, repositories, includeName = false }: {
  form: { release_name: string; chart: string; repository_id: string; version: string; values: string; revision: number }
  setForm: (value: typeof form) => void
  repositories: ChartRepository[]
  includeName?: boolean
}) {
  return <>
    {includeName && <div className="field"><label htmlFor="release-name">Release 名称</label><input id="release-name" value={form.release_name} onChange={(event) => setForm({ ...form, release_name: event.target.value })} /></div>}
    <div className="field"><label htmlFor="release-repository">Chart 仓库</label><select id="release-repository" value={form.repository_id} onChange={(event) => setForm({ ...form, repository_id: event.target.value })}><option value="">OCI 引用</option>{repositories.filter((item) => item.enabled).map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></div>
    <div className="field"><label htmlFor="release-chart">Chart</label><input id="release-chart" placeholder={form.repository_id ? 'gateway' : 'oci://registry.example.com/charts/gateway'} value={form.chart} onChange={(event) => setForm({ ...form, chart: event.target.value })} /></div>
    <div className="field"><label htmlFor="release-version">版本（可选）</label><input id="release-version" value={form.version} onChange={(event) => setForm({ ...form, version: event.target.value })} /></div>
    <div className="field field-full"><label htmlFor="release-values">Values YAML（可选）</label><textarea id="release-values" className="mono" rows={9} value={form.values} onChange={(event) => setForm({ ...form, values: event.target.value })} spellCheck={false} /></div>
  </>
}

function actionTitle(type: NonNullable<ReleaseAction>['type'], name: string) {
  return `${type === 'upgrade' ? '升级' : type === 'rollback' ? '回滚' : '卸载'} ${name}`
}
