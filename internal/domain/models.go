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

type Namespace struct {
	Name       string            `json:"name"`
	Status     string            `json:"status"`
	Labels     map[string]string `json:"labels"`
	Finalizers []string          `json:"finalizers"`
	CreatedAt  time.Time         `json:"created_at"`
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
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	Source    string    `json:"source,omitempty"`
	Count     int32     `json:"count"`
	FirstSeen time.Time `json:"first_seen,omitzero"`
	LastSeen  time.Time `json:"last_seen,omitzero"`
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
