export type Environment = 'development' | 'staging' | 'production'
export type ClusterStatus = 'pending' | 'connected' | 'degraded' | 'unreachable' | 'disabled'
export type OperationState = 'queued' | 'running' | 'succeeded' | 'failed' | 'unknown'
export type ResourcePressure = 'unknown' | 'normal' | 'constrained' | 'critical'

export interface Principal {
  username: string
  role: string
  expires_at: string
}

export interface Cluster {
  id: string
  name: string
  environment: Environment
  server: string
  status: ClusterStatus
  version?: string
  last_error_code?: string
  credentials_configured: boolean
  last_checked_at?: string
  created_at: string
  updated_at: string
}

export interface ClusterSummary {
  version: string
  namespace_count: number
  node_count: number
  ready_node_count: number
  workload_count: number
  ready_workloads: number
  unhealthy_pods: number
}

export interface Namespace {
  name: string
  status: string
  labels: Record<string, string>
  finalizers: string[]
  created_at: string
}

export interface NodeResources {
  cpu?: string
  memory?: string
  pods?: string
  ephemeral_storage?: string
}

export interface ClusterNode {
  name: string
  status: string
  roles: string[]
  version: string
  internal_ip?: string
  os_image?: string
  architecture?: string
  capacity: NodeResources
  allocatable: NodeResources
  unschedulable: boolean
  taint_count: number
  created_at: string
}

export interface NodeTaint {
  key: string
  value?: string
  effect: string
  time_added?: string
}

export interface NodeAddress {
  type: string
  address: string
}

export interface NodeCondition {
  type: string
  status: string
  reason?: string
  message?: string
  last_heartbeat_time?: string
  last_transition_time?: string
}

export interface NodeSystemInfo {
  os_image?: string
  kernel_version?: string
  container_runtime_version?: string
  kubelet_version?: string
  operating_system?: string
  architecture?: string
}

export interface NodeDetail extends ClusterNode {
  uid: string
  resource_version: string
  labels: Record<string, string>
  taints: NodeTaint[]
  addresses: NodeAddress[]
  conditions: NodeCondition[]
  system_info: NodeSystemInfo
}

export interface Workload {
  kind: string
  namespace: string
  name: string
  ready: number
  desired: number
  status: string
  images: string[]
  created_at: string
}

export interface WorkloadContainer {
  name: string
  image: string
  type: 'container' | 'init' | 'ephemeral'
  ready: boolean
  restart_count: number
  state?: string
}

export interface WorkloadCondition {
  type: string
  status: string
  reason?: string
  message?: string
  last_transition_time?: string
}

export interface WorkloadDetail extends Workload {
  uid: string
  resource_version: string
  labels: Record<string, string>
  containers: WorkloadContainer[]
  conditions: WorkloadCondition[]
  yaml: string
}

export interface KubernetesEvent {
  name: string
  type: string
  reason: string
  message: string
  source?: string
  count: number
  first_seen?: string
  last_seen?: string
}

export interface PodLogs {
  namespace: string
  pod: string
  container: string
  tail_lines: number
  previous: boolean
  timestamps: boolean
  truncated: boolean
  content: string
}

export interface ChartRepository {
  id: string
  name: string
  url: string
  enabled: boolean
  status: string
  last_error_code?: string
  credentials_configured: boolean
  last_checked_at?: string
  created_at: string
  updated_at: string
}

export interface HelmRelease {
  name: string
  namespace: string
  revision: number
  status: string
  chart: string
  app_version?: string
  updated_at?: string
}

export interface Operation {
  id: string
  request_id: string
  kind: string
  state: OperationState
  cluster_id: string
  namespace: string
  target: string
  submitted_by: string
  summary?: string
  error_code?: string
  error_message?: string
  created_at: string
  started_at?: string
  finished_at?: string
  updated_at: string
}

export interface OperationCapacity {
  adaptive: boolean
  pressure: ResourcePressure
  memory_ratio?: number
  load_ratio?: number
  active_operations: number
  operation_limit: number
  maximum_operations: number
  queue_depth: number
  queue_capacity: number
  sampled_at: string
}

export interface AuditEvent {
  id: string
  request_id: string
  operation_id?: string
  actor: string
  action: string
  result: string
  cluster_id?: string
  namespace?: string
  target: string
  summary?: string
  created_at: string
}
