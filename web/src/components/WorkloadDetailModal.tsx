import { ArrowLeft, Copy, Download, LoaderCircle, RefreshCw, RotateCw, Scaling, ScanSearch } from 'lucide-react'
import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { api, errorMessage } from '../api'
import type { Environment, KubernetesEvent, Operation, PodLogs, Workload, WorkloadDetail, WorkloadImagePreview } from '../types'
import { formatDateTime } from '../utils'
import { EmptyState, ErrorState, LoadingState } from './DataState'
import { KubernetesEvents } from './KubernetesEvents'
import { Modal } from './Modal'
import { StatusBadge } from './StatusBadge'

type DetailTab = 'overview' | 'events' | 'yaml' | 'logs'
type BasicWorkloadAction = 'scale' | 'restart'
type WorkloadAction = BasicWorkloadAction | 'image'

interface WorkloadDetailModalProps {
  clusterId: string
  clusterName: string
  environment: Environment
  workload: Workload
  open: boolean
  onClose: () => void
  notify: (tone: 'success' | 'error', message: string) => void
  openOperations: () => void
}

export function WorkloadDetailModal({ clusterId, clusterName, environment, workload, open, onClose, notify, openOperations }: WorkloadDetailModalProps) {
  const [tab, setTab] = useState<DetailTab>('overview')
  const [detail, setDetail] = useState<WorkloadDetail | null>(null)
  const [detailLoading, setDetailLoading] = useState(false)
  const [detailError, setDetailError] = useState<unknown>(null)
  const [events, setEvents] = useState<KubernetesEvent[]>([])
  const [eventsLoading, setEventsLoading] = useState(false)
  const [eventsError, setEventsError] = useState<unknown>(null)
  const [eventsLoaded, setEventsLoaded] = useState(false)
  const [selectedContainer, setSelectedContainer] = useState('')
  const [tailLines, setTailLines] = useState(200)
  const [previous, setPrevious] = useState(false)
  const [logs, setLogs] = useState<PodLogs | null>(null)
  const [logsLoading, setLogsLoading] = useState(false)
  const [logsError, setLogsError] = useState<unknown>(null)
  const [copied, setCopied] = useState(false)
  const [action, setAction] = useState<WorkloadAction | null>(null)
  const [replicas, setReplicas] = useState('0')
  const [confirmation, setConfirmation] = useState('')
  const [actionError, setActionError] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [imageContainer, setImageContainer] = useState('')
  const [image, setImage] = useState('')
  const [imagePreview, setImagePreview] = useState<WorkloadImagePreview | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const logRequestRef = useRef<AbortController | null>(null)
  const imageRequestRef = useRef<AbortController | null>(null)
  const isPod = workload.kind.toLowerCase() === 'pod'
  const isDeployment = workload.kind.toLowerCase() === 'deployment'

  const resourcePath = useMemo(() => {
    const segments = [clusterId, workload.kind.toLowerCase(), workload.namespace, workload.name].map(encodeURIComponent)
    return `/api/v1/clusters/${segments[0]}/workloads/${segments[1]}/${segments[2]}/${segments[3]}`
  }, [clusterId, workload.kind, workload.name, workload.namespace])

  useEffect(() => {
    if (!open) {
      logRequestRef.current?.abort()
      logRequestRef.current = null
      imageRequestRef.current?.abort()
      imageRequestRef.current = null
      return
    }
    const controller = new AbortController()
    let active = true
    logRequestRef.current?.abort()
    logRequestRef.current = null
    setTab('overview')
    setDetail(null)
    setDetailLoading(true)
    setDetailError(null)
    setEvents([])
    setEventsLoading(false)
    setEventsError(null)
    setEventsLoaded(false)
    setLogs(null)
    setLogsError(null)
    setSelectedContainer('')
    setPrevious(false)
    setTailLines(200)
    setCopied(false)
    setAction(null)
    setConfirmation('')
    setActionError('')
    setSubmitting(false)
    setImageContainer('')
    setImage('')
    setImagePreview(null)
    setPreviewing(false)
    imageRequestRef.current?.abort()
    imageRequestRef.current = null

    api.get<WorkloadDetail>(resourcePath, controller.signal)
      .then((value) => { if (active) setDetail(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setDetailError(error)
      })
      .finally(() => { if (active) setDetailLoading(false) })
    return () => {
      active = false
      controller.abort()
    }
  }, [open, resourcePath])

  useEffect(() => {
    if (!open || tab !== 'events' || eventsLoaded) return
    const controller = new AbortController()
    let active = true
    setEventsLoading(true)
    setEventsError(null)
    api.get<KubernetesEvent[]>(`${resourcePath}/events?limit=50`, controller.signal)
      .then((value) => { if (active) setEvents(value) })
      .catch((error: unknown) => {
        if (active && !(error instanceof DOMException && error.name === 'AbortError')) setEventsError(error)
      })
      .finally(() => {
        if (active) {
          setEventsLoading(false)
          setEventsLoaded(true)
        }
      })
    return () => {
      active = false
      controller.abort()
    }
  }, [eventsLoaded, open, resourcePath, tab])

  useEffect(() => () => {
    logRequestRef.current?.abort()
    logRequestRef.current = null
    imageRequestRef.current?.abort()
    imageRequestRef.current = null
  }, [])

  useEffect(() => {
    if (!detail?.containers.length) return
    if (!detail.containers.some((container) => container.name === selectedContainer)) {
      setSelectedContainer((detail.containers.find((container) => container.type === 'container') ?? detail.containers[0]).name)
    }
  }, [detail, selectedContainer])

  const loadLogs = useCallback(async () => {
    if (!open || !isPod || !selectedContainer) return
    logRequestRef.current?.abort()
    const controller = new AbortController()
    logRequestRef.current = controller
    const query = new URLSearchParams({
      container: selectedContainer,
      tail_lines: String(tailLines),
      previous: String(previous),
      timestamps: 'true',
    })
    const segments = [clusterId, workload.namespace, workload.name].map(encodeURIComponent)
    setLogsLoading(true)
    setLogsError(null)
    try {
      const response = await api.get<PodLogs>(
        `/api/v1/clusters/${segments[0]}/pods/${segments[1]}/${segments[2]}/logs?${query}`,
        controller.signal,
      )
      if (logRequestRef.current === controller) setLogs(response)
    } catch (error) {
      if (logRequestRef.current === controller && !(error instanceof DOMException && error.name === 'AbortError')) {
        setLogsError(error)
      }
    } finally {
      if (logRequestRef.current === controller) {
        logRequestRef.current = null
        setLogsLoading(false)
      }
    }
  }, [clusterId, isPod, open, previous, selectedContainer, tailLines, workload.name, workload.namespace])

  useEffect(() => {
    if (tab === 'logs') {
      void loadLogs()
      return
    }
    logRequestRef.current?.abort()
    logRequestRef.current = null
  }, [loadLogs, tab])

  const tabs: Array<{ id: DetailTab; label: string }> = [
    { id: 'overview', label: '概览' },
    { id: 'events', label: '事件' },
    { id: 'yaml', label: 'YAML' },
  ]
  if (isPod) tabs.push({ id: 'logs', label: '日志' })

  const openAction = (nextAction: WorkloadAction) => {
    if (!detail) return
    setAction(nextAction)
    setReplicas(String(detail.desired))
    setConfirmation('')
    setActionError('')
    setImagePreview(null)
    setPreviewing(false)
    imageRequestRef.current?.abort()
    imageRequestRef.current = null
    if (nextAction === 'image') {
      const container = detail.containers.find((item) => item.type === 'container')
      setImageContainer(container?.name ?? '')
      setImage(container?.image ?? '')
    }
  }

  const submitAction = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!detail || !action || action === 'image' || submitting) return

    let replicaCount: number | undefined
    if (action === 'scale') {
      if (replicas.trim() === '') {
        setActionError('请输入副本数')
        return
      }
      replicaCount = Number(replicas)
      if (!Number.isInteger(replicaCount) || replicaCount < 0 || replicaCount > 1000) {
        setActionError('副本数必须是 0 到 1000 之间的整数')
        return
      }
    }
    if (environment === 'production' && confirmation !== clusterName) {
      setActionError(`请输入集群名称 ${clusterName} 以确认生产操作`)
      return
    }

    const payload: { resource_version: string; confirmation: string; replicas?: number } = {
      resource_version: detail.resource_version,
      confirmation: environment === 'production' ? confirmation : '',
    }
    if (replicaCount !== undefined) payload.replicas = replicaCount

    setSubmitting(true)
    setActionError('')
    try {
      const endpoint = action === 'scale' ? 'scales' : 'restarts'
      const operation = await api.post<Operation>(`${resourcePath}/${endpoint}`, payload)
      notify('success', `${action === 'scale' ? '扩缩容' : '滚动重启'}任务 ${operation.id} 已提交`)
      onClose()
      openOperations()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  const invalidateImagePreview = () => {
    imageRequestRef.current?.abort()
    imageRequestRef.current = null
    setPreviewing(false)
    setImagePreview(null)
    setActionError('')
  }

  const selectImageContainer = (name: string) => {
    const container = detail?.containers.find((item) => item.type === 'container' && item.name === name)
    invalidateImagePreview()
    setImageContainer(name)
    setImage(container?.image ?? '')
  }

  const changeImage = (value: string) => {
    invalidateImagePreview()
    setImage(value)
  }

  const previewImageUpdate = async () => {
    if (!detail || action !== 'image' || previewing || submitting) return
    const container = detail.containers.find((item) => item.type === 'container' && item.name === imageContainer)
    const validationError = validateImageValue(image, container?.image ?? '')
    if (!container || validationError) {
      setActionError(container ? validationError : '请选择普通容器')
      return
    }
    const controller = new AbortController()
    imageRequestRef.current?.abort()
    imageRequestRef.current = controller
    setImagePreview(null)
    setPreviewing(true)
    setActionError('')
    try {
      const preview = await api.post<WorkloadImagePreview>(`${resourcePath}/image-previews`, {
        container: container.name,
        current_image: container.image,
        image,
        resource_version: detail.resource_version,
      }, controller.signal)
      if (imageRequestRef.current === controller) setImagePreview(preview)
    } catch (error) {
      if (imageRequestRef.current === controller && !(error instanceof DOMException && error.name === 'AbortError')) {
        setActionError(errorMessage(error))
      }
    } finally {
      if (imageRequestRef.current === controller) {
        imageRequestRef.current = null
        setPreviewing(false)
      }
    }
  }

  const submitImageUpdate = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!detail || action !== 'image' || !imagePreview || previewing || submitting) return
    const container = detail.containers.find((item) => item.type === 'container' && item.name === imageContainer)
    const change = imagePreview.changes[0]
    if (!container || imagePreview.resource_version !== detail.resource_version || imagePreview.container !== container.name ||
      imagePreview.changes.length !== 1 || change.before !== container.image || change.after !== image) {
      setImagePreview(null)
      setActionError('变更预览已失效，请重新预览')
      return
    }
    if (environment === 'production' && confirmation !== clusterName) {
      setActionError(`请输入集群名称 ${clusterName} 以确认生产操作`)
      return
    }
    setSubmitting(true)
    setActionError('')
    try {
      const operation = await api.post<Operation>(`${resourcePath}/image-updates`, {
        container: container.name,
        current_image: container.image,
        image,
        resource_version: detail.resource_version,
        confirmation: environment === 'production' ? confirmation : '',
      })
      notify('success', `镜像更新任务 ${operation.id} 已提交`)
      onClose()
      openOperations()
    } catch (error) {
      setActionError(errorMessage(error))
    } finally {
      setSubmitting(false)
    }
  }

  const copyYAML = async () => {
    if (!detail || !navigator.clipboard?.writeText) return
    try {
      await navigator.clipboard.writeText(detail.yaml)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }

  return (
    <Modal title={`${workload.kind} · ${workload.name}`} open={open} onClose={onClose} width="large">
      {detailLoading ? <LoadingState label="正在读取资源详情" /> : detailError ? (
        <ErrorState error={detailError} />
      ) : detail ? (
        <div className="workload-detail">
          <div className="detail-tabs" role="tablist" aria-label="资源详情视图">
            {tabs.map((item) => (
              <button
                type="button"
                role="tab"
                key={item.id}
                aria-selected={tab === item.id}
                className={tab === item.id ? 'active' : ''}
                onClick={() => setTab(item.id)}
              >
                {item.label}
              </button>
            ))}
          </div>
          {tab === 'overview' && (action ? (action === 'image' ? (
            <ImageUpdateForm
              clusterName={clusterName}
              detail={detail}
              environment={environment}
              container={imageContainer}
              image={image}
              preview={imagePreview}
              confirmation={confirmation}
              error={actionError}
              previewing={previewing}
              submitting={submitting}
              onContainer={selectImageContainer}
              onImage={changeImage}
              onConfirmation={(value) => { setConfirmation(value); setActionError('') }}
              onCancel={() => { invalidateImagePreview(); setAction(null) }}
              onPreview={() => void previewImageUpdate()}
              onSubmit={submitImageUpdate}
            />
          ) : (
            <WorkloadOperationForm
              action={action}
              clusterName={clusterName}
              detail={detail}
              environment={environment}
              replicas={replicas}
              confirmation={confirmation}
              error={actionError}
              submitting={submitting}
              onReplicas={(value) => { setReplicas(value); setActionError('') }}
              onConfirmation={(value) => { setConfirmation(value); setActionError('') }}
              onCancel={() => setAction(null)}
              onSubmit={submitAction}
            />
          )) : <><DeploymentActions visible={isDeployment} canUpdateImage={detail.containers.some((container) => container.type === 'container')} onAction={openAction} /><Overview detail={detail} /></>)}
          {tab === 'events' && (
            !eventsLoaded || eventsLoading ? <LoadingState label="正在读取事件" /> : eventsError ? <ErrorState error={eventsError} onRetry={() => setEventsLoaded(false)} /> : <KubernetesEvents events={events} />
          )}
          {tab === 'yaml' && (
            <div className="code-panel">
              <div className="code-toolbar">
                <span>脱敏清单</span>
                <div>
                  <button type="button" className="button button-secondary" onClick={() => void copyYAML()}><Copy size={15} /> {copied ? '已复制' : '复制'}</button>
                  <button type="button" className="button button-secondary" onClick={() => downloadText(`${detail.name}.yaml`, detail.yaml)}><Download size={15} /> 下载</button>
                </div>
              </div>
              <pre className="yaml-view">{detail.yaml}</pre>
            </div>
          )}
          {tab === 'logs' && (
            <div className="logs-panel">
              <div className="logs-toolbar">
                <div className="toolbar-field">
                  <label htmlFor="log-container">容器</label>
                  <select id="log-container" value={selectedContainer} onChange={(event) => setSelectedContainer(event.target.value)}>
                    {detail.containers.map((container) => <option key={`${container.type}:${container.name}`} value={container.name}>{container.name}{container.type === 'init' ? ' (init)' : ''}</option>)}
                  </select>
                </div>
                <div className="toolbar-field">
                  <label htmlFor="log-lines">行数</label>
                  <select id="log-lines" value={tailLines} onChange={(event) => setTailLines(Number(event.target.value))}>
                    {[100, 200, 500, 1000, 2000].map((value) => <option key={value} value={value}>{value}</option>)}
                  </select>
                </div>
                <label className="checkbox-field"><input type="checkbox" checked={previous} onChange={(event) => setPrevious(event.target.checked)} />上一实例</label>
                <div className="logs-actions">
                  <button type="button" className="icon-button" aria-label="刷新日志" title="刷新日志" disabled={logsLoading || !selectedContainer} onClick={() => void loadLogs()}><RefreshCw size={17} className={logsLoading ? 'spin' : ''} /></button>
                  <button type="button" className="icon-button" aria-label="下载日志" title="下载日志" disabled={!logs} onClick={() => logs && downloadText(`${logs.pod}-${logs.container}.log`, logs.content)}><Download size={17} /></button>
                </div>
              </div>
              {logsLoading ? <LoadingState label="正在读取日志" /> : logsError ? <ErrorState error={logsError} onRetry={() => void loadLogs()} /> : logs ? (
                <>
                  {logs.truncated && <div className="log-warning" role="status">日志达到 2 MiB 响应上限，已截断</div>}
                  <pre className="log-view">{logs.content || '当前范围没有日志'}</pre>
                </>
              ) : <EmptyState title="请选择容器" />}
            </div>
          )}
        </div>
      ) : null}
    </Modal>
  )
}

function DeploymentActions({ visible, canUpdateImage, onAction }: { visible: boolean; canUpdateImage: boolean; onAction: (action: WorkloadAction) => void }) {
  if (!visible) return null
  return (
    <div className="workload-actions" aria-label="Deployment 操作">
      <button type="button" className="button button-secondary" onClick={() => onAction('scale')}><Scaling size={16} /> 扩缩容</button>
      <button type="button" className="button button-secondary" onClick={() => onAction('restart')}><RotateCw size={16} /> 滚动重启</button>
      <button type="button" className="button button-secondary" disabled={!canUpdateImage} onClick={() => onAction('image')}><RefreshCw size={16} /> 更新镜像</button>
    </div>
  )
}

function WorkloadOperationForm({
  action,
  clusterName,
  detail,
  environment,
  replicas,
  confirmation,
  error,
  submitting,
  onReplicas,
  onConfirmation,
  onCancel,
  onSubmit,
}: {
  action: BasicWorkloadAction
  clusterName: string
  detail: WorkloadDetail
  environment: Environment
  replicas: string
  confirmation: string
  error: string
  submitting: boolean
  onReplicas: (value: string) => void
  onConfirmation: (value: string) => void
  onCancel: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  const isScale = action === 'scale'
  return (
    <form className="workload-operation" onSubmit={onSubmit}>
      <div className="workload-operation-heading">
        <button type="button" className="icon-button" aria-label="返回概览" title="返回概览" disabled={submitting} onClick={onCancel}><ArrowLeft size={18} /></button>
        <div><h3>{isScale ? 'Deployment 扩缩容' : 'Deployment 滚动重启'}</h3><p>{detail.namespace} / {detail.name}</p></div>
      </div>
      <dl className="operation-target">
        <div><dt>当前副本</dt><dd>{detail.ready}/{detail.desired}</dd></div>
        <div><dt>Resource Version</dt><dd className="mono">{detail.resource_version}</dd></div>
      </dl>
      {isScale ? (
        <div className="field">
          <label htmlFor="workload-replicas">副本数</label>
          <input id="workload-replicas" type="number" min={0} max={1000} step={1} value={replicas} onChange={(event) => onReplicas(event.target.value)} autoFocus />
          <small>允许 0 到 1000 个副本</small>
        </div>
      ) : <p className="operation-description">将更新 Pod 模板注解，由 Kubernetes 按 Deployment 策略逐步替换实例。</p>}
      {environment === 'production' && (
        <div className="field">
          <label htmlFor="workload-confirmation">输入集群名称确认</label>
          <input id="workload-confirmation" value={confirmation} onChange={(event) => onConfirmation(event.target.value)} placeholder={clusterName} autoComplete="off" />
          <small>生产操作必须完整输入 {clusterName}</small>
        </div>
      )}
      {error && <div className="form-error" role="alert">{error}</div>}
      <div className="form-actions">
        <button type="button" className="button button-secondary" disabled={submitting} onClick={onCancel}>取消</button>
        <button type="submit" className="button button-primary" disabled={submitting}>
          {submitting ? <LoaderCircle className="spin" size={16} /> : isScale ? <Scaling size={16} /> : <RotateCw size={16} />}
          {isScale ? '提交扩缩容' : '提交滚动重启'}
        </button>
      </div>
    </form>
  )
}

function ImageUpdateForm({
  clusterName,
  detail,
  environment,
  container,
  image,
  preview,
  confirmation,
  error,
  previewing,
  submitting,
  onContainer,
  onImage,
  onConfirmation,
  onCancel,
  onPreview,
  onSubmit,
}: {
  clusterName: string
  detail: WorkloadDetail
  environment: Environment
  container: string
  image: string
  preview: WorkloadImagePreview | null
  confirmation: string
  error: string
  previewing: boolean
  submitting: boolean
  onContainer: (value: string) => void
  onImage: (value: string) => void
  onConfirmation: (value: string) => void
  onCancel: () => void
  onPreview: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  const containers = detail.containers.filter((item) => item.type === 'container')
  const current = containers.find((item) => item.name === container)
  const busy = previewing || submitting
  return (
    <form className="workload-operation" onSubmit={onSubmit}>
      <div className="workload-operation-heading">
        <button type="button" className="icon-button" aria-label="返回概览" title="返回概览" disabled={busy} onClick={onCancel}><ArrowLeft size={18} /></button>
        <div><h3>Deployment 镜像更新</h3><p>{detail.namespace} / {detail.name}</p></div>
      </div>
      <dl className="operation-target">
        <div><dt>目标容器</dt><dd className="mono">{container || '-'}</dd></div>
        <div><dt>Resource Version</dt><dd className="mono">{detail.resource_version}</dd></div>
      </dl>
      <div className="form-grid image-update-fields">
        <div className="field">
          <label htmlFor="image-container">容器</label>
          <select id="image-container" value={container} disabled={busy} onChange={(event) => onContainer(event.target.value)}>
            {containers.map((item) => <option key={item.name} value={item.name}>{item.name}</option>)}
          </select>
        </div>
        <div className="field">
          <label htmlFor="current-container-image">当前镜像</label>
          <input id="current-container-image" className="mono" value={current?.image ?? ''} readOnly />
        </div>
        <div className="field field-full">
          <label htmlFor="next-container-image">新镜像</label>
          <input id="next-container-image" className="mono" value={image} maxLength={1024} disabled={busy || !current} onChange={(event) => onImage(event.target.value)} autoComplete="off" autoFocus />
        </div>
      </div>
      {preview && (
        <section className="image-change-preview" aria-live="polite">
          <div className="image-change-preview-heading"><h4>服务端 dry-run 通过</h4><span>{preview.changes.length} 个字段</span></div>
          {preview.changes.map((change) => (
            <div className="image-change-row" key={change.field}>
              <code>{change.field}</code>
              <dl>
                <div><dt>变更前</dt><dd>{change.before}</dd></div>
                <div><dt>变更后</dt><dd>{change.after}</dd></div>
              </dl>
            </div>
          ))}
        </section>
      )}
      {environment === 'production' && (
        <div className="field">
          <label htmlFor="image-update-confirmation">输入集群名称确认</label>
          <input id="image-update-confirmation" value={confirmation} disabled={busy} onChange={(event) => onConfirmation(event.target.value)} placeholder={clusterName} autoComplete="off" />
          <small>生产操作必须完整输入 {clusterName}</small>
        </div>
      )}
      {error && <div className="form-error" role="alert">{error}</div>}
      <div className="form-actions">
        <button type="button" className="button button-secondary" disabled={busy} onClick={onCancel}>取消</button>
        <button type="button" className="button button-secondary" disabled={busy || !current} onClick={onPreview}>
          {previewing ? <LoaderCircle className="spin" size={16} /> : <ScanSearch size={16} />} 预览变更
        </button>
        <button type="submit" className="button button-primary" disabled={busy || !preview}>
          {submitting ? <LoaderCircle className="spin" size={16} /> : <RefreshCw size={16} />} 提交镜像更新
        </button>
      </div>
    </form>
  )
}

function Overview({ detail }: { detail: WorkloadDetail }) {
  const labels = Object.entries(detail.labels)
  const kind = detail.kind.toLowerCase()
  const progressLabel = kind === 'job' ? '完成进度' : kind === 'cronjob' ? '活动任务' : '就绪'
  const progress = kind === 'cronjob' ? `${detail.ready} 个活动任务` : `${detail.ready}/${detail.desired}`
  return (
    <div className="detail-overview">
      <dl className="detail-grid">
        <div><dt>状态</dt><dd><StatusBadge status={detail.status} /></dd></div>
        <div><dt>命名空间</dt><dd className="mono">{detail.namespace}</dd></div>
        <div><dt>{progressLabel}</dt><dd>{progress}</dd></div>
        <div><dt>创建时间</dt><dd>{formatDateTime(detail.created_at)}</dd></div>
        <div><dt>UID</dt><dd className="mono">{detail.uid}</dd></div>
        <div><dt>Resource Version</dt><dd className="mono">{detail.resource_version}</dd></div>
      </dl>
      <section className="detail-section">
        <h3>标签</h3>
        {labels.length ? <div className="label-list">{labels.map(([key, value]) => <span key={key}><strong>{key}</strong>={value}</span>)}</div> : <span className="detail-muted">无标签</span>}
      </section>
      <section className="detail-section">
        <h3>容器</h3>
        {detail.containers.length ? <div className="table-wrap"><table className="detail-table">
          <thead><tr><th>名称</th><th>类型</th><th>状态</th><th>重启</th><th>镜像</th></tr></thead>
          <tbody>{detail.containers.map((container) => <tr key={`${container.type}:${container.name}`}>
            <td><strong>{container.name}</strong></td><td>{container.type}</td><td>{container.state || '-'}</td><td>{container.restart_count}</td><td className="mono clipped-cell" title={container.image}>{container.image}</td>
          </tr>)}</tbody>
        </table></div> : <span className="detail-muted">无容器</span>}
      </section>
      <section className="detail-section">
        <h3>条件</h3>
        {detail.conditions.length ? <div className="condition-list">{detail.conditions.map((condition) => (
          <div key={`${condition.type}:${condition.last_transition_time ?? ''}`}>
            <StatusBadge status={condition.status} /><strong>{condition.type}</strong><span>{condition.reason || '-'}</span><time>{formatDateTime(condition.last_transition_time)}</time>
            {condition.message && <p>{condition.message}</p>}
          </div>
        ))}</div> : <span className="detail-muted">无条件记录</span>}
      </section>
    </div>
  )
}

function downloadText(filename: string, content: string) {
  const url = URL.createObjectURL(new Blob([content], { type: 'text/plain;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = filename
  anchor.click()
  URL.revokeObjectURL(url)
}

function validateImageValue(image: string, currentImage: string): string {
  if (!image || image !== image.trim() || /\s/u.test(image)) return '镜像引用不能为空或包含空白字符'
  if (new TextEncoder().encode(image).length > 1024) return '镜像引用不能超过 1024 字节'
  if (image === currentImage) return '新镜像必须与当前镜像不同'
  return ''
}
