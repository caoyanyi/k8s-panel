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
	OperationHelmInstall   OperationKind = "helm.install"
	OperationHelmUpgrade   OperationKind = "helm.upgrade"
	OperationHelmRollback  OperationKind = "helm.rollback"
	OperationHelmUninstall OperationKind = "helm.uninstall"
)

type OperationState string

const (
	OperationQueued    OperationState = "queued"
	OperationRunning   OperationState = "running"
	OperationSucceeded OperationState = "succeeded"
	OperationFailed    OperationState = "failed"
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
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
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

type HelmRelease struct {
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	Revision   int       `json:"revision"`
	Status     string    `json:"status"`
	Chart      string    `json:"chart"`
	AppVersion string    `json:"app_version,omitempty"`
	UpdatedAt  time.Time `json:"updated_at,omitempty"`
}
