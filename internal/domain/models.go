package domain

import "time"

type Environment string

const (
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)

type ClusterStatus string

const (
	ClusterPending     ClusterStatus = "pending"
	ClusterConnected   ClusterStatus = "connected"
	ClusterDegraded    ClusterStatus = "degraded"
	ClusterUnreachable ClusterStatus = "unreachable"
	ClusterDisabled    ClusterStatus = "disabled"
)

type Cluster struct {
	ID                    string        `json:"id"`
	Name                  string        `json:"name"`
	Environment           Environment   `json:"environment"`
	Server                string        `json:"server"`
	Status                ClusterStatus `json:"status"`
	Version               string        `json:"version,omitempty"`
	LastErrorCode         string        `json:"last_error_code,omitempty"`
	CACertCiphertext      string        `json:"ca_cert_ciphertext,omitempty"`
	BearerTokenCiphertext string        `json:"bearer_token_ciphertext"`
	LastCheckedAt         time.Time     `json:"last_checked_at,omitempty"`
	CreatedAt             time.Time     `json:"created_at"`
	UpdatedAt             time.Time     `json:"updated_at"`
}

type ClusterInput struct {
	Name        string
	Environment Environment
	Server      string
	CACert      string
	BearerToken string
}

const (
	MaxClusterBearerTokenBytes = 64 * 1024
	MaxClusterCACertBytes      = 256 * 1024
)

type ClusterCredentialRotationInput struct {
	CACert       string
	BearerToken  string
	Confirmation string
}

type Repository struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	URL                string    `json:"url"`
	Enabled            bool      `json:"enabled"`
	UsernameCiphertext string    `json:"username_ciphertext,omitempty"`
	PasswordCiphertext string    `json:"password_ciphertext,omitempty"`
	Status             string    `json:"status"`
	LastErrorCode      string    `json:"last_error_code,omitempty"`
	LastCheckedAt      time.Time `json:"last_checked_at,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type RepositoryInput struct {
	Name     string
	URL      string
	Username string
	Password string
}

type OperationKind string

const (
	OperationHelmInstall     OperationKind = "helm.install"
	OperationHelmUpgrade     OperationKind = "helm.upgrade"
	OperationHelmRollback    OperationKind = "helm.rollback"
	OperationHelmUninstall   OperationKind = "helm.uninstall"
	OperationWorkloadScale   OperationKind = "workload.scale"
	OperationWorkloadRestart OperationKind = "workload.restart"
	OperationWorkloadImage   OperationKind = "workload.image_update"
)

type OperationState string

const (
	OperationQueued    OperationState = "queued"
	OperationRunning   OperationState = "running"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
	OperationCanceled  OperationState = "canceled"
	OperationUnknown   OperationState = "unknown"
)

type Operation struct {
	ID           string         `json:"id"`
	RequestID    string         `json:"request_id"`
	Kind         OperationKind  `json:"kind"`
	State        OperationState `json:"state"`
	ClusterID    string         `json:"cluster_id"`
	Namespace    string         `json:"namespace"`
	Target       string         `json:"target"`
	SubmittedBy  string         `json:"submitted_by"`
	Summary      string         `json:"summary,omitempty"`
	ErrorCode    string         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	StartedAt    time.Time      `json:"started_at,omitempty"`
	FinishedAt   time.Time      `json:"finished_at,omitempty"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type AuditEvent struct {
	ID          string    `json:"id"`
	RequestID   string    `json:"request_id"`
	OperationID string    `json:"operation_id,omitempty"`
	Actor       string    `json:"actor"`
	Action      string    `json:"action"`
	Result      string    `json:"result"`
	ClusterID   string    `json:"cluster_id,omitempty"`
	Namespace   string    `json:"namespace,omitempty"`
	Target      string    `json:"target"`
	Summary     string    `json:"summary,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type HelmOperationInput struct {
	ClusterID    string
	Namespace    string
	ReleaseName  string
	Chart        string
	RepositoryID string
	Version      string
	Values       string
	Revision     int
}

type WorkloadOperationInput struct {
	ClusterID       string
	Reference       WorkloadReference
	ResourceVersion string
	Replicas        *int32
	Confirmation    string
}

type WorkloadImageChange struct {
	Reference       WorkloadReference
	ResourceVersion string
	Container       string
	CurrentImage    string
	Image           string
}

type WorkloadImageOperationInput struct {
	ClusterID    string
	Change       WorkloadImageChange
	Confirmation string
}

type WorkloadFieldChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type WorkloadImagePreview struct {
	Kind            string                `json:"kind"`
	Namespace       string                `json:"namespace"`
	Name            string                `json:"name"`
	Container       string                `json:"container"`
	ResourceVersion string                `json:"resource_version"`
	Changes         []WorkloadFieldChange `json:"changes"`
}

type ClusterProbe struct {
	Version        string `json:"version"`
	NamespaceCount int    `json:"namespace_count"`
	NodeCount      int    `json:"node_count"`
}

type ClusterSummary struct {
	Version        string `json:"version"`
	NamespaceCount int    `json:"namespace_count"`
	NodeCount      int    `json:"node_count"`
	ReadyNodeCount int    `json:"ready_node_count"`
	WorkloadCount  int    `json:"workload_count"`
	ReadyWorkloads int    `json:"ready_workloads"`
	UnhealthyPods  int    `json:"unhealthy_pods"`
}

type KubernetesCapabilityState string

const (
	KubernetesCapabilityAllowed       KubernetesCapabilityState = "allowed"
	KubernetesCapabilityDenied        KubernetesCapabilityState = "denied"
	KubernetesCapabilityIndeterminate KubernetesCapabilityState = "indeterminate"
)

type KubernetesCapability struct {
	Key   string                    `json:"key"`
	State KubernetesCapabilityState `json:"state"`
}

type ClusterCapabilities struct {
	Namespace string                 `json:"namespace"`
	CheckedAt time.Time              `json:"checked_at"`
	Checks    []KubernetesCapability `json:"checks"`
}

type Namespace struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	Finalizers []string          `json:"finalizers"`
	CreatedAt  time.Time         `json:"created_at"`
}

type KubernetesPodSecurityAdmissionModeStatus string

const (
	PodSecurityAdmissionModeInherited  KubernetesPodSecurityAdmissionModeStatus = "inherited"
	PodSecurityAdmissionModeConfigured KubernetesPodSecurityAdmissionModeStatus = "configured"
	PodSecurityAdmissionModeInvalid    KubernetesPodSecurityAdmissionModeStatus = "invalid"
)

type KubernetesPodSecurityAdmissionMode struct {
	Status           KubernetesPodSecurityAdmissionModeStatus `json:"status"`
	Level            string                                   `json:"level,omitempty"`
	Version          string                                   `json:"version,omitempty"`
	VersionDefaulted bool                                     `json:"version_defaulted"`
}

type KubernetesPodSecurityAdmissionNamespace struct {
	Name             string                             `json:"name"`
	Enforce          KubernetesPodSecurityAdmissionMode `json:"enforce"`
	Audit            KubernetesPodSecurityAdmissionMode `json:"audit"`
	Warn             KubernetesPodSecurityAdmissionMode `json:"warn"`
	InvalidModeCount int                                `json:"invalid_mode_count"`
	CreatedAt        time.Time                          `json:"created_at"`
}

type KubernetesNodeVersionSkewStatus string

const (
	NodeVersionSameMinor       KubernetesNodeVersionSkewStatus = "same-minor"
	NodeVersionWithinPolicy    KubernetesNodeVersionSkewStatus = "within-policy"
	NodeVersionUpgradeBlocking KubernetesNodeVersionSkewStatus = "upgrade-blocking"
	NodeVersionOutsidePolicy   KubernetesNodeVersionSkewStatus = "outside-policy"
	NodeVersionNewerThanServer KubernetesNodeVersionSkewStatus = "newer-than-server"
	NodeVersionMajorMismatch   KubernetesNodeVersionSkewStatus = "major-mismatch"
)

type KubernetesNodeVersionSkew struct {
	Name                string                          `json:"name"`
	KubeletVersion      string                          `json:"kubelet_version"`
	Status              KubernetesNodeVersionSkewStatus `json:"status"`
	MinorSkew           int                             `json:"minor_skew"`
	MaximumMinorSkew    int                             `json:"maximum_minor_skew"`
	MinorSkewComparable bool                            `json:"minor_skew_comparable"`
}

type KubernetesNodeVersionSkewReport struct {
	APIServerVersion string                      `json:"api_server_version"`
	Nodes            []KubernetesNodeVersionSkew `json:"nodes"`
}

type KubernetesDeprecatedAPIRequest struct {
	Group          string `json:"group"`
	Version        string `json:"version"`
	Resource       string `json:"resource"`
	Subresource    string `json:"subresource"`
	RemovedRelease string `json:"removed_release"`
}

type KubernetesEndpointCertificateStatus string

const (
	EndpointCertificateValid    KubernetesEndpointCertificateStatus = "valid"
	EndpointCertificateExpiring KubernetesEndpointCertificateStatus = "expiring"
	EndpointCertificateCritical KubernetesEndpointCertificateStatus = "critical"
	EndpointCertificateExpired  KubernetesEndpointCertificateStatus = "expired"
)

type KubernetesEndpointCertificate struct {
	ObservedAt       time.Time                           `json:"observed_at"`
	NotBefore        time.Time                           `json:"not_before"`
	NotAfter         time.Time                           `json:"not_after"`
	RemainingSeconds int64                               `json:"remaining_seconds"`
	Status           KubernetesEndpointCertificateStatus `json:"status"`
}

type NodeResources struct {
	CPU              string `json:"cpu,omitempty"`
	Memory           string `json:"memory,omitempty"`
	Pods             string `json:"pods,omitempty"`
	EphemeralStorage string `json:"ephemeral_storage,omitempty"`
}

type Node struct {
	Name          string        `json:"name"`
	Status        string        `json:"status"`
	Roles         []string      `json:"roles"`
	Version       string        `json:"version"`
	InternalIP    string        `json:"internal_ip,omitempty"`
	OSImage       string        `json:"os_image,omitempty"`
	Architecture  string        `json:"architecture,omitempty"`
	Capacity      NodeResources `json:"capacity"`
	Allocatable   NodeResources `json:"allocatable"`
	Unschedulable bool          `json:"unschedulable"`
	TaintCount    int           `json:"taint_count"`
	CreatedAt     time.Time     `json:"created_at"`
}

type NodeTaint struct {
	Key       string    `json:"key"`
	Value     string    `json:"value,omitempty"`
	Effect    string    `json:"effect"`
	TimeAdded time.Time `json:"time_added,omitzero"`
}

type NodeAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type NodeCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastHeartbeatTime  time.Time `json:"last_heartbeat_time,omitzero"`
	LastTransitionTime time.Time `json:"last_transition_time,omitzero"`
}

type NodeSystemInfo struct {
	OSImage                 string `json:"os_image,omitempty"`
	KernelVersion           string `json:"kernel_version,omitempty"`
	ContainerRuntimeVersion string `json:"container_runtime_version,omitempty"`
	KubeletVersion          string `json:"kubelet_version,omitempty"`
	OperatingSystem         string `json:"operating_system,omitempty"`
	Architecture            string `json:"architecture,omitempty"`
}

type NodeDetail struct {
	Node
	UID             string            `json:"uid"`
	ResourceVersion string            `json:"resource_version"`
	Labels          map[string]string `json:"labels"`
	Taints          []NodeTaint       `json:"taints"`
	Addresses       []NodeAddress     `json:"addresses"`
	Conditions      []NodeCondition   `json:"conditions"`
	SystemInfo      NodeSystemInfo    `json:"system_info"`
}

const (
	MaxNetworkAddresses = 32
	MaxServicePorts     = 64
	MaxIngressHosts     = 32
)

type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol"`
	Port       int32  `json:"port"`
	TargetPort string `json:"target_port,omitempty"`
	NodePort   int32  `json:"node_port,omitempty"`
}

type KubernetesService struct {
	Namespace         string        `json:"namespace"`
	Name              string        `json:"name"`
	Type              string        `json:"type"`
	ClusterIP         string        `json:"cluster_ip,omitempty"`
	ExternalName      string        `json:"external_name,omitempty"`
	ExternalAddresses []string      `json:"external_addresses"`
	AddressCount      int           `json:"address_count"`
	Ports             []ServicePort `json:"ports"`
	PortCount         int           `json:"port_count"`
	CreatedAt         time.Time     `json:"created_at"`
}

type KubernetesIngress struct {
	Namespace    string    `json:"namespace"`
	Name         string    `json:"name"`
	ClassName    string    `json:"class_name,omitempty"`
	Hosts        []string  `json:"hosts"`
	HostCount    int       `json:"host_count"`
	Addresses    []string  `json:"addresses"`
	AddressCount int       `json:"address_count"`
	TLS          bool      `json:"tls"`
	RuleCount    int       `json:"rule_count"`
	PathCount    int       `json:"path_count"`
	CreatedAt    time.Time `json:"created_at"`
}

type KubernetesEndpointSlice struct {
	Namespace                 string    `json:"namespace"`
	Name                      string    `json:"name"`
	ServiceName               string    `json:"service_name"`
	AddressType               string    `json:"address_type"`
	EndpointCount             int       `json:"endpoint_count"`
	ReadyEndpointCount        int       `json:"ready_endpoint_count"`
	ReadyDefaultedCount       int       `json:"ready_defaulted_count"`
	ServingEndpointCount      int       `json:"serving_endpoint_count"`
	ServingDefaultedCount     int       `json:"serving_defaulted_count"`
	TerminatingEndpointCount  int       `json:"terminating_endpoint_count"`
	TerminatingDefaultedCount int       `json:"terminating_defaulted_count"`
	PortCount                 int       `json:"port_count"`
	CreatedAt                 time.Time `json:"created_at"`
}

type KubernetesNetworkPolicy struct {
	Namespace                  string                 `json:"namespace"`
	Name                       string                 `json:"name"`
	PodSelectorMode            KubernetesSelectorMode `json:"pod_selector_mode"`
	PodSelectorLabelCount      int                    `json:"pod_selector_label_count"`
	PodSelectorExpressionCount int                    `json:"pod_selector_expression_count"`
	PolicyTypes                []string               `json:"policy_types"`
	PolicyTypesDefaulted       bool                   `json:"policy_types_defaulted"`
	IngressRuleCount           int                    `json:"ingress_rule_count"`
	IngressPeerCount           int                    `json:"ingress_peer_count"`
	IngressPortCount           int                    `json:"ingress_port_count"`
	EgressRuleCount            int                    `json:"egress_rule_count"`
	EgressPeerCount            int                    `json:"egress_peer_count"`
	EgressPortCount            int                    `json:"egress_port_count"`
	CreatedAt                  time.Time              `json:"created_at"`
}

type KubernetesConfigMap struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	DataCount int       `json:"data_count"`
	CreatedAt time.Time `json:"created_at"`
}

type KubernetesSecret struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	DataCount int       `json:"data_count"`
	CreatedAt time.Time `json:"created_at"`
}

type KubernetesPersistentVolumeClaim struct {
	Namespace    string    `json:"namespace"`
	Name         string    `json:"name"`
	Status       string    `json:"status"`
	Volume       string    `json:"volume,omitempty"`
	Capacity     string    `json:"capacity,omitempty"`
	AccessModes  string    `json:"access_modes,omitempty"`
	StorageClass string    `json:"storage_class,omitempty"`
	VolumeMode   string    `json:"volume_mode,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type KubernetesPersistentVolume struct {
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Claim         string    `json:"claim,omitempty"`
	Capacity      string    `json:"capacity,omitempty"`
	AccessModes   string    `json:"access_modes,omitempty"`
	StorageClass  string    `json:"storage_class,omitempty"`
	ReclaimPolicy string    `json:"reclaim_policy,omitempty"`
	VolumeMode    string    `json:"volume_mode,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type KubernetesStorageClass struct {
	Name                 string    `json:"name"`
	Provisioner          string    `json:"provisioner"`
	ReclaimPolicy        string    `json:"reclaim_policy,omitempty"`
	VolumeBindingMode    string    `json:"volume_binding_mode,omitempty"`
	AllowVolumeExpansion bool      `json:"allow_volume_expansion"`
	Default              bool      `json:"default"`
	CreatedAt            time.Time `json:"created_at"`
}

type KubernetesQuotaResource struct {
	Name     string `json:"name"`
	Hard     string `json:"hard,omitempty"`
	Used     string `json:"used,omitempty"`
	Observed bool   `json:"observed"`
}

type KubernetesResourceQuota struct {
	Namespace          string                    `json:"namespace"`
	Name               string                    `json:"name"`
	Scopes             []string                  `json:"scopes"`
	ScopeCount         int                       `json:"scope_count"`
	ScopesTruncated    bool                      `json:"scopes_truncated"`
	ScopeSelectorCount int                       `json:"scope_selector_count"`
	Resources          []KubernetesQuotaResource `json:"resources"`
	ResourceCount      int                       `json:"resource_count"`
	ResourcesTruncated bool                      `json:"resources_truncated"`
	CreatedAt          time.Time                 `json:"created_at"`
}

type KubernetesLimitRangeConstraint struct {
	Type                 string `json:"type"`
	Resource             string `json:"resource"`
	DefaultRequest       string `json:"default_request,omitempty"`
	Default              string `json:"default,omitempty"`
	Min                  string `json:"min,omitempty"`
	Max                  string `json:"max,omitempty"`
	MaxLimitRequestRatio string `json:"max_limit_request_ratio,omitempty"`
}

type KubernetesLimitRange struct {
	Namespace            string                           `json:"namespace"`
	Name                 string                           `json:"name"`
	Constraints          []KubernetesLimitRangeConstraint `json:"constraints"`
	ConstraintCount      int                              `json:"constraint_count"`
	ConstraintsTruncated bool                             `json:"constraints_truncated"`
	CreatedAt            time.Time                        `json:"created_at"`
}

type KubernetesPolicyCondition struct {
	Type   string `json:"type"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type KubernetesHorizontalPodAutoscaler struct {
	Namespace            string                      `json:"namespace"`
	Name                 string                      `json:"name"`
	TargetAPIVersion     string                      `json:"target_api_version,omitempty"`
	TargetKind           string                      `json:"target_kind"`
	TargetName           string                      `json:"target_name"`
	MinReplicas          int32                       `json:"min_replicas"`
	MinReplicasDefaulted bool                        `json:"min_replicas_defaulted"`
	MaxReplicas          int32                       `json:"max_replicas"`
	CurrentReplicas      int32                       `json:"current_replicas"`
	DesiredReplicas      int32                       `json:"desired_replicas"`
	MetricCount          int                         `json:"metric_count"`
	CurrentMetricCount   int                         `json:"current_metric_count"`
	Observed             bool                        `json:"observed"`
	Conditions           []KubernetesPolicyCondition `json:"conditions"`
	ConditionCount       int                         `json:"condition_count"`
	ConditionsTruncated  bool                        `json:"conditions_truncated"`
	LastScaleTime        *time.Time                  `json:"last_scale_time,omitempty"`
	CreatedAt            time.Time                   `json:"created_at"`
}

type KubernetesSelectorMode string

const (
	KubernetesSelectorNone     KubernetesSelectorMode = "none"
	KubernetesSelectorAll      KubernetesSelectorMode = "all"
	KubernetesSelectorFiltered KubernetesSelectorMode = "filtered"
)

type KubernetesPodDisruptionBudget struct {
	Namespace                           string                      `json:"namespace"`
	Name                                string                      `json:"name"`
	SelectorMode                        KubernetesSelectorMode      `json:"selector_mode"`
	SelectorLabelCount                  int                         `json:"selector_label_count"`
	SelectorExpressionCount             int                         `json:"selector_expression_count"`
	MinAvailable                        string                      `json:"min_available,omitempty"`
	MaxUnavailable                      string                      `json:"max_unavailable,omitempty"`
	CurrentHealthy                      int32                       `json:"current_healthy"`
	DesiredHealthy                      int32                       `json:"desired_healthy"`
	DisruptionsAllowed                  int32                       `json:"disruptions_allowed"`
	ExpectedPods                        int32                       `json:"expected_pods"`
	Observed                            bool                        `json:"observed"`
	UnhealthyPodEvictionPolicy          string                      `json:"unhealthy_pod_eviction_policy"`
	UnhealthyPodEvictionPolicyDefaulted bool                        `json:"unhealthy_pod_eviction_policy_defaulted"`
	Conditions                          []KubernetesPolicyCondition `json:"conditions"`
	ConditionCount                      int                         `json:"condition_count"`
	ConditionsTruncated                 bool                        `json:"conditions_truncated"`
	CreatedAt                           time.Time                   `json:"created_at"`
}

type KubernetesCustomResourceDefinition struct {
	Name      string    `json:"name"`
	Resource  string    `json:"resource"`
	Group     string    `json:"group"`
	CreatedAt time.Time `json:"created_at"`
}

type KubernetesAPIService struct {
	Name                       string     `json:"name"`
	Group                      string     `json:"group"`
	Version                    string     `json:"version"`
	Local                      bool       `json:"local"`
	ServiceNamespace           string     `json:"service_namespace,omitempty"`
	ServiceName                string     `json:"service_name,omitempty"`
	ServicePort                int32      `json:"service_port,omitempty"`
	ServicePortDefaulted       bool       `json:"service_port_defaulted"`
	AvailabilityObserved       bool       `json:"availability_observed"`
	AvailabilityStatus         string     `json:"availability_status,omitempty"`
	AvailabilityReason         string     `json:"availability_reason,omitempty"`
	AvailabilityTransitionTime *time.Time `json:"availability_transition_time,omitempty"`
	ConditionCount             int        `json:"condition_count"`
	InsecureSkipTLSVerify      bool       `json:"insecure_skip_tls_verify"`
	GroupPriorityMinimum       int32      `json:"group_priority_minimum"`
	VersionPriority            int32      `json:"version_priority"`
	CreatedAt                  time.Time  `json:"created_at"`
}

type KubernetesAdmissionWebhookConfigurationKind string

const (
	AdmissionWebhookConfigurationValidating KubernetesAdmissionWebhookConfigurationKind = "validating"
	AdmissionWebhookConfigurationMutating   KubernetesAdmissionWebhookConfigurationKind = "mutating"
)

type KubernetesAdmissionWebhookConfiguration struct {
	Kind      KubernetesAdmissionWebhookConfigurationKind `json:"kind"`
	Name      string                                      `json:"name"`
	CreatedAt time.Time                                   `json:"created_at"`
}

type KubernetesAdmissionWebhook struct {
	Name                             string   `json:"name"`
	TargetType                       string   `json:"target_type"`
	ServiceNamespace                 string   `json:"service_namespace,omitempty"`
	ServiceName                      string   `json:"service_name,omitempty"`
	ServicePort                      int32    `json:"service_port,omitempty"`
	ServicePortDefaulted             bool     `json:"service_port_defaulted"`
	CABundleConfigured               bool     `json:"ca_bundle_configured"`
	FailurePolicy                    string   `json:"failure_policy"`
	FailurePolicyDefaulted           bool     `json:"failure_policy_defaulted"`
	MatchPolicy                      string   `json:"match_policy"`
	MatchPolicyDefaulted             bool     `json:"match_policy_defaulted"`
	SideEffects                      string   `json:"side_effects"`
	TimeoutSeconds                   int32    `json:"timeout_seconds"`
	TimeoutSecondsDefaulted          bool     `json:"timeout_seconds_defaulted"`
	ReinvocationPolicy               string   `json:"reinvocation_policy,omitempty"`
	ReinvocationPolicyDefaulted      bool     `json:"reinvocation_policy_defaulted"`
	AdmissionReviewVersions          []string `json:"admission_review_versions"`
	RuleCount                        int      `json:"rule_count"`
	OperationCount                   int      `json:"operation_count"`
	APIGroupCount                    int      `json:"api_group_count"`
	APIVersionCount                  int      `json:"api_version_count"`
	ResourceCount                    int      `json:"resource_count"`
	NamespaceSelectorLabelCount      int      `json:"namespace_selector_label_count"`
	NamespaceSelectorExpressionCount int      `json:"namespace_selector_expression_count"`
	ObjectSelectorLabelCount         int      `json:"object_selector_label_count"`
	ObjectSelectorExpressionCount    int      `json:"object_selector_expression_count"`
	MatchConditionCount              int      `json:"match_condition_count"`
}

type KubernetesAdmissionWebhookConfigurationDetail struct {
	KubernetesAdmissionWebhookConfiguration
	Generation   int64                        `json:"generation"`
	Webhooks     []KubernetesAdmissionWebhook `json:"webhooks"`
	WebhookCount int                          `json:"webhook_count"`
}

type KubernetesAdmissionPolicyResourceKind string

const (
	AdmissionPolicyResourcePolicy  KubernetesAdmissionPolicyResourceKind = "policy"
	AdmissionPolicyResourceBinding KubernetesAdmissionPolicyResourceKind = "binding"
)

type KubernetesAdmissionPolicyResource struct {
	Kind      KubernetesAdmissionPolicyResourceKind `json:"kind"`
	Name      string                                `json:"name"`
	CreatedAt time.Time                             `json:"created_at"`
}

type KubernetesAdmissionMatchSummary struct {
	Configured                       bool   `json:"configured"`
	MatchPolicy                      string `json:"match_policy,omitempty"`
	MatchPolicyDefaulted             bool   `json:"match_policy_defaulted"`
	ResourceRuleCount                int    `json:"resource_rule_count"`
	ExcludeResourceRuleCount         int    `json:"exclude_resource_rule_count"`
	OperationCount                   int    `json:"operation_count"`
	APIGroupCount                    int    `json:"api_group_count"`
	APIVersionCount                  int    `json:"api_version_count"`
	ResourceCount                    int    `json:"resource_count"`
	NamespaceSelectorLabelCount      int    `json:"namespace_selector_label_count"`
	NamespaceSelectorExpressionCount int    `json:"namespace_selector_expression_count"`
	ObjectSelectorLabelCount         int    `json:"object_selector_label_count"`
	ObjectSelectorExpressionCount    int    `json:"object_selector_expression_count"`
}

type KubernetesValidatingAdmissionPolicyDetail struct {
	KubernetesAdmissionPolicyResource
	Generation             int64                           `json:"generation"`
	FailurePolicy          string                          `json:"failure_policy"`
	FailurePolicyDefaulted bool                            `json:"failure_policy_defaulted"`
	ParamKindConfigured    bool                            `json:"param_kind_configured"`
	ParamAPIVersion        string                          `json:"param_api_version,omitempty"`
	ParamKind              string                          `json:"param_kind,omitempty"`
	Match                  KubernetesAdmissionMatchSummary `json:"match"`
	ValidationCount        int                             `json:"validation_count"`
	AuditAnnotationCount   int                             `json:"audit_annotation_count"`
	MatchConditionCount    int                             `json:"match_condition_count"`
	VariableCount          int                             `json:"variable_count"`
	ObservedGeneration     int64                           `json:"observed_generation"`
	TypeCheckingObserved   bool                            `json:"type_checking_observed"`
	ExpressionWarningCount int                             `json:"expression_warning_count"`
	ConditionCount         int                             `json:"condition_count"`
}

type KubernetesValidatingAdmissionPolicyBindingDetail struct {
	KubernetesAdmissionPolicyResource
	Generation                   int64                           `json:"generation"`
	PolicyName                   string                          `json:"policy_name"`
	ValidationActions            []string                        `json:"validation_actions"`
	ParamRefConfigured           bool                            `json:"param_ref_configured"`
	ParamRefMode                 string                          `json:"param_ref_mode,omitempty"`
	ParamNamespace               string                          `json:"param_namespace,omitempty"`
	ParameterNotFoundAction      string                          `json:"parameter_not_found_action,omitempty"`
	ParamSelectorLabelCount      int                             `json:"param_selector_label_count"`
	ParamSelectorExpressionCount int                             `json:"param_selector_expression_count"`
	Match                        KubernetesAdmissionMatchSummary `json:"match"`
}

type KubernetesCustomResourceDefinitionVersion struct {
	Name       string `json:"name"`
	Served     bool   `json:"served"`
	Storage    bool   `json:"storage"`
	Deprecated bool   `json:"deprecated"`
}

type KubernetesCustomResourceDefinitionCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	ObservedGeneration int64     `json:"observed_generation"`
	LastTransitionTime time.Time `json:"last_transition_time"`
}

type KubernetesCustomResourceDefinitionDetail struct {
	KubernetesCustomResourceDefinition
	Scope                       string                                        `json:"scope"`
	Singular                    string                                        `json:"singular"`
	Kind                        string                                        `json:"kind"`
	ListKind                    string                                        `json:"list_kind"`
	ShortNames                  []string                                      `json:"short_names"`
	ShortNameCount              int                                           `json:"short_name_count"`
	ShortNamesTruncated         bool                                          `json:"short_names_truncated"`
	Categories                  []string                                      `json:"categories"`
	CategoryCount               int                                           `json:"category_count"`
	CategoriesTruncated         bool                                          `json:"categories_truncated"`
	Versions                    []KubernetesCustomResourceDefinitionVersion   `json:"versions"`
	VersionCount                int                                           `json:"version_count"`
	VersionsTruncated           bool                                          `json:"versions_truncated"`
	StoredVersions              []string                                      `json:"stored_versions"`
	StoredVersionCount          int                                           `json:"stored_version_count"`
	StoredVersionsTruncated     bool                                          `json:"stored_versions_truncated"`
	ConversionStrategy          string                                        `json:"conversion_strategy"`
	ConversionStrategyDefaulted bool                                          `json:"conversion_strategy_defaulted"`
	Generation                  int64                                         `json:"generation"`
	ObservedGeneration          int64                                         `json:"observed_generation"`
	Conditions                  []KubernetesCustomResourceDefinitionCondition `json:"conditions"`
	ConditionCount              int                                           `json:"condition_count"`
	ConditionsTruncated         bool                                          `json:"conditions_truncated"`
}

type KubernetesAccessResourceKind string

const (
	AccessResourceServiceAccounts     KubernetesAccessResourceKind = "serviceaccounts"
	AccessResourceRoles               KubernetesAccessResourceKind = "roles"
	AccessResourceRoleBindings        KubernetesAccessResourceKind = "rolebindings"
	AccessResourceClusterRoles        KubernetesAccessResourceKind = "clusterroles"
	AccessResourceClusterRoleBindings KubernetesAccessResourceKind = "clusterrolebindings"
)

type KubernetesAccessResourceReference struct {
	Kind      KubernetesAccessResourceKind
	Namespace string
	Name      string
}

type KubernetesAccessResource struct {
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type KubernetesRoleRule struct {
	APIGroups       []string `json:"api_groups"`
	Resources       []string `json:"resources"`
	ResourceNames   []string `json:"resource_names"`
	Verbs           []string `json:"verbs"`
	NonResourceURLs []string `json:"non_resource_urls"`
}

type KubernetesRoleReference struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type KubernetesAccessSubject struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

type KubernetesAccessResourceDetail struct {
	KubernetesAccessResource
	RoleRef                      *KubernetesRoleReference  `json:"role_ref,omitempty"`
	Rules                        []KubernetesRoleRule      `json:"rules"`
	RuleCount                    int                       `json:"rule_count"`
	RulesTruncated               bool                      `json:"rules_truncated"`
	Subjects                     []KubernetesAccessSubject `json:"subjects"`
	SubjectCount                 int                       `json:"subject_count"`
	SubjectsTruncated            bool                      `json:"subjects_truncated"`
	AutomountServiceAccountToken *bool                     `json:"automount_service_account_token,omitempty"`
	SecretCount                  int                       `json:"secret_count"`
	ImagePullSecretCount         int                       `json:"image_pull_secret_count"`
}

type KubernetesServiceAccountReference struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type KubernetesResourceAttributes struct {
	Group       string `json:"group,omitempty"`
	Resource    string `json:"resource"`
	Subresource string `json:"subresource,omitempty"`
	Verb        string `json:"verb"`
	Namespace   string `json:"namespace,omitempty"`
	Name        string `json:"name,omitempty"`
}

type KubernetesServiceAccountAccessReviewInput struct {
	ServiceAccount     KubernetesServiceAccountReference `json:"service_account"`
	ResourceAttributes KubernetesResourceAttributes      `json:"resource_attributes"`
}

type KubernetesServiceAccountAccessReview struct {
	ServiceAccount     KubernetesServiceAccountReference `json:"service_account"`
	ResourceAttributes KubernetesResourceAttributes      `json:"resource_attributes"`
	State              KubernetesCapabilityState         `json:"state"`
	CheckedAt          time.Time                         `json:"checked_at"`
}

type Workload struct {
	Kind      string    `json:"kind"`
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Ready     int32     `json:"ready"`
	Desired   int32     `json:"desired"`
	Status    string    `json:"status"`
	Images    []string  `json:"images"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	MaxPodLogTailLines     = 2000
	MaxWorkloadEventLimit  = 100
	MaxNodeEventLimit      = 100
	MaxClusterEventLimit   = 500
	MaxWorkloadReplicas    = 1000
	MaxContainerImageBytes = 1024
)

type WorkloadReference struct {
	Kind      string
	Namespace string
	Name      string
}

type WorkloadContainer struct {
	Name         string `json:"name"`
	Image        string `json:"image"`
	Type         string `json:"type"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restart_count"`
	State        string `json:"state,omitempty"`
}

type WorkloadCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"last_transition_time,omitzero"`
}

type WorkloadDetail struct {
	Workload
	UID             string              `json:"uid"`
	ResourceVersion string              `json:"resource_version"`
	Labels          map[string]string   `json:"labels"`
	Containers      []WorkloadContainer `json:"containers"`
	Conditions      []WorkloadCondition `json:"conditions"`
	YAML            string              `json:"yaml"`
}

type KubernetesEvent struct {
	Namespace        string    `json:"namespace,omitempty"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	Reason           string    `json:"reason"`
	Message          string    `json:"message"`
	MessageTruncated bool      `json:"message_truncated"`
	Source           string    `json:"source,omitempty"`
	ObjectKind       string    `json:"object_kind,omitempty"`
	ObjectName       string    `json:"object_name,omitempty"`
	Count            int32     `json:"count"`
	FirstSeen        time.Time `json:"first_seen,omitzero"`
	LastSeen         time.Time `json:"last_seen,omitzero"`
	CreatedAt        time.Time `json:"created_at,omitzero"`
}

type PodLogRequest struct {
	Namespace  string
	Pod        string
	Container  string
	TailLines  int
	Previous   bool
	Timestamps bool
}

type PodLogs struct {
	Namespace  string `json:"namespace"`
	Pod        string `json:"pod"`
	Container  string `json:"container"`
	TailLines  int    `json:"tail_lines"`
	Previous   bool   `json:"previous"`
	Timestamps bool   `json:"timestamps"`
	Truncated  bool   `json:"truncated"`
	Content    string `json:"content"`
}

type HelmRelease struct {
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	Revision   int       `json:"revision"`
	Status     string    `json:"status"`
	Chart      string    `json:"chart"`
	AppVersion string    `json:"app_version,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}
