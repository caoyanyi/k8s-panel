import { CheckCircle2, CircleHelp, LoaderCircle, ShieldCheck, XCircle } from 'lucide-react'
import { type FormEvent, useEffect, useMemo, useRef, useState } from 'react'
import { api, errorMessage } from '../api'
import type {
  KubernetesAccessResourceDetail,
  KubernetesCapabilityState,
  KubernetesResourceAttributes,
  KubernetesServiceAccountAccessReview as AccessReviewResult,
  KubernetesServiceAccountAccessReviewInput,
  Namespace,
} from '../types'
import { formatDateTime } from '../utils'

interface ResourcePreset {
  key: string
  label: string
  group: string
  resource: string
  subresource: string
  namespaced: boolean
}

const resourcePresets: ResourcePreset[] = [
  { key: 'pods', label: 'Pod', group: '', resource: 'pods', subresource: '', namespaced: true },
  { key: 'pod-logs', label: 'Pod 日志', group: '', resource: 'pods', subresource: 'log', namespaced: true },
  { key: 'deployments', label: 'Deployment', group: 'apps', resource: 'deployments', subresource: '', namespaced: true },
  { key: 'services', label: 'Service', group: '', resource: 'services', subresource: '', namespaced: true },
  { key: 'configmaps', label: 'ConfigMap', group: '', resource: 'configmaps', subresource: '', namespaced: true },
  { key: 'secrets', label: 'Secret', group: '', resource: 'secrets', subresource: '', namespaced: true },
  { key: 'roles', label: 'Role', group: 'rbac.authorization.k8s.io', resource: 'roles', subresource: '', namespaced: true },
  { key: 'rolebindings', label: 'RoleBinding', group: 'rbac.authorization.k8s.io', resource: 'rolebindings', subresource: '', namespaced: true },
  { key: 'namespaces', label: 'Namespace', group: '', resource: 'namespaces', subresource: '', namespaced: false },
  { key: 'nodes', label: 'Node', group: '', resource: 'nodes', subresource: '', namespaced: false },
  { key: 'persistentvolumes', label: 'PersistentVolume', group: '', resource: 'persistentvolumes', subresource: '', namespaced: false },
  { key: 'clusterroles', label: 'ClusterRole', group: 'rbac.authorization.k8s.io', resource: 'clusterroles', subresource: '', namespaced: false },
  { key: 'clusterrolebindings', label: 'ClusterRoleBinding', group: 'rbac.authorization.k8s.io', resource: 'clusterrolebindings', subresource: '', namespaced: false },
]

const accessReviewVerbs = [
  'get', 'list', 'watch', 'create', 'update', 'patch', 'delete', 'deletecollection',
  'proxy', 'use', 'bind', 'escalate', 'impersonate', 'approve', 'sign',
]

interface ServiceAccountAccessReviewProps {
  clusterId: string
  detail: KubernetesAccessResourceDetail
  namespaces: Namespace[]
}

export function ServiceAccountAccessReview({
  clusterId,
  detail,
  namespaces,
}: ServiceAccountAccessReviewProps) {
  const serviceAccountNamespace = detail.namespace ?? ''
  const [presetKey, setPresetKey] = useState(resourcePresets[0].key)
  const [verb, setVerb] = useState('get')
  const [targetNamespace, setTargetNamespace] = useState(serviceAccountNamespace)
  const [objectName, setObjectName] = useState('')
  const [result, setResult] = useState<AccessReviewResult | null>(null)
  const [reviewError, setReviewError] = useState<unknown>(null)
  const [loading, setLoading] = useState(false)
  const controllerRef = useRef<AbortController | null>(null)
  const preset = resourcePresets.find((candidate) => candidate.key === presetKey) ?? resourcePresets[0]
  const namespaceNames = useMemo(() => (
    Array.from(new Set([serviceAccountNamespace, ...namespaces.map((item) => item.name)])).filter(Boolean).sort()
  ), [namespaces, serviceAccountNamespace])

  useEffect(() => () => {
    controllerRef.current?.abort()
    controllerRef.current = null
  }, [])

  function invalidateResult() {
    controllerRef.current?.abort()
    controllerRef.current = null
    setResult(null)
    setReviewError(null)
    setLoading(false)
  }

  function changePreset(nextKey: string) {
    const nextPreset = resourcePresets.find((candidate) => candidate.key === nextKey) ?? resourcePresets[0]
    invalidateResult()
    setPresetKey(nextPreset.key)
    setTargetNamespace((current) => nextPreset.namespaced ? current || serviceAccountNamespace : '')
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    setLoading(true)
    setReviewError(null)
    setResult(null)

    const attributes: KubernetesResourceAttributes = {
      ...(preset.group ? { group: preset.group } : {}),
      resource: preset.resource,
      ...(preset.subresource ? { subresource: preset.subresource } : {}),
      verb,
      ...(preset.namespaced && targetNamespace ? { namespace: targetNamespace } : {}),
      ...(objectName ? { name: objectName } : {}),
    }
    const input: KubernetesServiceAccountAccessReviewInput = {
      service_account: { namespace: serviceAccountNamespace, name: detail.name },
      resource_attributes: attributes,
    }
    api.post<AccessReviewResult>(
      `/api/v1/clusters/${encodeURIComponent(clusterId)}/service-account-access-reviews`,
      input,
      controller.signal,
    )
      .then((value) => {
        if (controllerRef.current === controller) setResult(value)
      })
      .catch((caught: unknown) => {
        if (controllerRef.current === controller && !(caught instanceof DOMException && caught.name === 'AbortError')) {
          setReviewError(caught)
        }
      })
      .finally(() => {
        if (controllerRef.current === controller) {
          controllerRef.current = null
          setLoading(false)
        }
      })
  }

  return (
    <section className="detail-section service-account-access-review">
      <h3>权限模拟</h3>
      <form className="access-review-form" onSubmit={submit} noValidate>
        <div className="form-grid access-review-fields">
          <div className="field">
            <label htmlFor="access-review-resource">目标资源</label>
            <select
              id="access-review-resource"
              value={preset.key}
              onChange={(event) => changePreset(event.target.value)}
            >
              {resourcePresets.map((item) => <option key={item.key} value={item.key}>{item.label}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="access-review-verb">动作</label>
            <select
              id="access-review-verb"
              value={verb}
              onChange={(event) => {
                invalidateResult()
                setVerb(event.target.value)
              }}
            >
              {accessReviewVerbs.map((item) => <option key={item} value={item}>{item}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="access-review-namespace">目标命名空间</label>
            <select
              id="access-review-namespace"
              value={preset.namespaced ? targetNamespace : ''}
              disabled={!preset.namespaced}
              onChange={(event) => {
                invalidateResult()
                setTargetNamespace(event.target.value)
              }}
            >
              {!preset.namespaced && <option value="">集群级资源</option>}
              {preset.namespaced && namespaceNames.map((name) => <option key={name} value={name}>{name}</option>)}
            </select>
          </div>
          <div className="field">
            <label htmlFor="access-review-name">对象名称（可选）</label>
            <input
              id="access-review-name"
              maxLength={253}
              value={objectName}
              onChange={(event) => {
                invalidateResult()
                setObjectName(event.target.value)
              }}
              autoComplete="off"
              spellCheck={false}
            />
          </div>
        </div>
        {reviewError !== null && <div className="form-error" role="alert">{errorMessage(reviewError)}</div>}
        <div className="access-review-footer">
          <div className="access-review-result" aria-live="polite">
            {result && (
              <>
                <AccessReviewState state={result.state} />
                <time dateTime={result.checked_at}>{formatDateTime(result.checked_at)}</time>
              </>
            )}
          </div>
          <button
            type="submit"
            className="button button-primary"
            disabled={!serviceAccountNamespace || (preset.namespaced && !targetNamespace)}
          >
            {loading ? <LoaderCircle className="spin" size={16} /> : <ShieldCheck size={16} />}
            检查权限
          </button>
        </div>
      </form>
    </section>
  )
}

function AccessReviewState({ state }: { state: KubernetesCapabilityState }) {
  if (state === 'allowed') {
    return <span className="capability-state capability-allowed"><CheckCircle2 size={15} />允许</span>
  }
  if (state === 'denied') {
    return <span className="capability-state capability-denied"><XCircle size={15} />拒绝</span>
  }
  return <span className="capability-state capability-indeterminate"><CircleHelp size={15} />无法判定</span>
}
