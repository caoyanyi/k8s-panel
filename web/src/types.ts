export type Environment = 'development' | 'staging' | 'production'
export type ClusterStatus = 'pending' | 'connected' | 'degraded' | 'unreachable' | 'disabled'
export type OperationState = 'queued' | 'running' | 'succeeded' | 'failed' | 'canceled' | 'unknown'
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

export type KubernetesCapabilityState = 'allowed' | 'denied' | 'indeterminate'

export interface KubernetesCapability {
  key: string
  state: KubernetesCapabilityState
}

export interface ClusterCapabilities {
  namespace: string
  checked_at: string
  checks: KubernetesCapability[]
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

export interface ServicePort {
  name?: string
  protocol: string
  port: number
  target_port?: string
  node_port?: number
}

export interface KubernetesService {
  namespace: string
  name: string
  type: string
  cluster_ip?: string
  external_name?: string
  external_addresses: string[]
  address_count: number
  ports: ServicePort[]
  port_count: number
  created_at: string
}

export interface KubernetesIngress {
  namespace: string
  name: string
  class_name?: string
  hosts: string[]
  host_count: number
  addresses: string[]
  address_count: number
  tls: boolean
  rule_count: number
  path_count: number
  created_at: string
}

export interface KubernetesEndpointSlice {
  namespace: string
  name: string
  service_name: string
  address_type: 'IPv4' | 'IPv6' | 'FQDN'
  endpoint_count: number
  ready_endpoint_count: number
  ready_defaulted_count: number
  serving_endpoint_count: number
  serving_defaulted_count: number
  terminating_endpoint_count: number
  terminating_defaulted_count: number
  port_count: number
  created_at: string
}

export interface KubernetesNetworkPolicy {
  namespace: string
  name: string
  pod_selector_mode: KubernetesSelectorMode
  pod_selector_label_count: number
  pod_selector_expression_count: number
  policy_types: Array<'Ingress' | 'Egress'>
  policy_types_defaulted: boolean
  ingress_rule_count: number
  ingress_peer_count: number
  ingress_port_count: number
  egress_rule_count: number
  egress_peer_count: number
  egress_port_count: number
  created_at: string
}

export interface KubernetesConfigMap {
  namespace: string
  name: string
  data_count: number
  created_at: string
}

export interface KubernetesSecret {
  namespace: string
  name: string
  type: string
  data_count: number
  created_at: string
}

export interface KubernetesPersistentVolumeClaim {
  namespace: string
  name: string
  status: string
  volume?: string
  capacity?: string
  access_modes?: string
  storage_class?: string
  volume_mode?: string
  created_at: string
}

export interface KubernetesPersistentVolume {
  name: string
  status: string
  claim?: string
  capacity?: string
  access_modes?: string
  storage_class?: string
  reclaim_policy?: string
  volume_mode?: string
  created_at: string
}

export interface KubernetesStorageClass {
  name: string
  provisioner: string
  reclaim_policy?: string
  volume_binding_mode?: string
  allow_volume_expansion: boolean
  default: boolean
  created_at: string
}

export interface KubernetesQuotaResource {
  name: string
  hard?: string
  used?: string
  observed: boolean
}

export interface KubernetesResourceQuota {
  namespace: string
  name: string
  scopes: string[]
  scope_count: number
  scopes_truncated: boolean
  scope_selector_count: number
  resources: KubernetesQuotaResource[]
  resource_count: number
  resources_truncated: boolean
  created_at: string
}

export interface KubernetesLimitRangeConstraint {
  type: string
  resource: string
  default_request?: string
  default?: string
  min?: string
  max?: string
  max_limit_request_ratio?: string
}

export interface KubernetesLimitRange {
  namespace: string
  name: string
  constraints: KubernetesLimitRangeConstraint[]
  constraint_count: number
  constraints_truncated: boolean
  created_at: string
}

export interface KubernetesPolicyCondition {
  type: string
  status: 'True' | 'False' | 'Unknown'
  reason?: string
}

export interface KubernetesHorizontalPodAutoscaler {
  namespace: string
  name: string
  target_api_version?: string
  target_kind: string
  target_name: string
  min_replicas: number
  min_replicas_defaulted: boolean
  max_replicas: number
  current_replicas: number
  desired_replicas: number
  metric_count: number
  current_metric_count: number
  observed: boolean
  conditions: KubernetesPolicyCondition[]
  condition_count: number
  conditions_truncated: boolean
  last_scale_time?: string
  created_at: string
}

export type KubernetesSelectorMode = 'none' | 'all' | 'filtered'

export interface KubernetesPodDisruptionBudget {
  namespace: string
  name: string
  selector_mode: KubernetesSelectorMode
  selector_label_count: number
  selector_expression_count: number
  min_available?: string
  max_unavailable?: string
  current_healthy: number
  desired_healthy: number
  disruptions_allowed: number
  expected_pods: number
  observed: boolean
  unhealthy_pod_eviction_policy: string
  unhealthy_pod_eviction_policy_defaulted: boolean
  conditions: KubernetesPolicyCondition[]
  condition_count: number
  conditions_truncated: boolean
  created_at: string
}

export type KubernetesAccessResourceKind =
  | 'serviceaccounts'
  | 'roles'
  | 'rolebindings'
  | 'clusterroles'
  | 'clusterrolebindings'

export interface KubernetesAccessResource {
  kind: 'ServiceAccount' | 'Role' | 'RoleBinding' | 'ClusterRole' | 'ClusterRoleBinding'
  namespace?: string
  name: string
  created_at: string
}

export interface KubernetesRoleRule {
  api_groups: string[]
  resources: string[]
  resource_names: string[]
  verbs: string[]
  non_resource_urls: string[]
}

export interface KubernetesRoleReference {
  kind: 'Role' | 'ClusterRole'
  name: string
}

export interface KubernetesAccessSubject {
  kind: 'User' | 'Group' | 'ServiceAccount'
  namespace?: string
  name: string
}

export interface KubernetesAccessResourceDetail extends KubernetesAccessResource {
  role_ref?: KubernetesRoleReference
  rules: KubernetesRoleRule[]
  rule_count: number
  rules_truncated: boolean
  subjects: KubernetesAccessSubject[]
  subject_count: number
  subjects_truncated: boolean
  automount_service_account_token?: boolean
  secret_count: number
  image_pull_secret_count: number
}

export interface KubernetesServiceAccountReference {
  namespace: string
  name: string
}

export interface KubernetesResourceAttributes {
  group?: string
  resource: string
  subresource?: string
  verb: string
  namespace?: string
  name?: string
}

export interface KubernetesServiceAccountAccessReviewInput {
  service_account: KubernetesServiceAccountReference
  resource_attributes: KubernetesResourceAttributes
}

export interface KubernetesServiceAccountAccessReview extends KubernetesServiceAccountAccessReviewInput {
  state: KubernetesCapabilityState
  checked_at: string
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

export interface WorkloadFieldChange {
  field: string
  before: string
  after: string
}

export interface WorkloadImagePreview {
  kind: string
  namespace: string
  name: string
  container: string
  resource_version: string
  changes: WorkloadFieldChange[]
}

export interface KubernetesEvent {
  namespace?: string
  name: string
  type: string
  reason: string
  message: string
  message_truncated?: boolean
  source?: string
  object_kind?: string
  object_name?: string
  count: number
  first_seen?: string
  last_seen?: string
  created_at?: string
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
  kubernetes_reads: KubernetesReadCapacity
  kubernetes_clients: KubernetesClientCacheCapacity
  sampled_at: string
}

export interface KubernetesReadCapacity {
  adaptive: boolean
  pressure: ResourcePressure
  active: number
  limit: number
  maximum: number
}

export interface KubernetesClientCacheCapacity {
  entries: number
  capacity: number
  maximum: number
  building: number
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
