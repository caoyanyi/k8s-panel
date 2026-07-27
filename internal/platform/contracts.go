package platform

import (
	"context"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
)

type Store interface {
	CreateCluster(context.Context, domain.Cluster) error
	ListClusters(context.Context) ([]domain.Cluster, error)
	GetCluster(context.Context, string) (domain.Cluster, error)
	UpdateCluster(context.Context, domain.Cluster) error
	RotateClusterCredentials(context.Context, domain.Cluster, domain.Cluster, domain.AuditEvent) error
	DeleteCluster(context.Context, string) error

	CreateRepository(context.Context, domain.Repository) error
	ListRepositories(context.Context) ([]domain.Repository, error)
	GetRepository(context.Context, string) (domain.Repository, error)
	UpdateRepository(context.Context, domain.Repository) error
	DeleteRepository(context.Context, string) error

	CreateOperation(context.Context, domain.Operation) error
	GetOperation(context.Context, string) (domain.Operation, error)
	UpdateOperation(context.Context, domain.Operation) error
	TransitionOperation(context.Context, domain.OperationState, domain.Operation, *domain.AuditEvent) error
	ListOperations(context.Context, int) ([]domain.Operation, error)

	CreateAuditEvent(context.Context, domain.AuditEvent) error
	ListAuditEvents(context.Context, int) ([]domain.AuditEvent, error)
}

type SecretCipher interface {
	SealString(plaintext, associatedData string) (string, error)
	OpenString(encoded, associatedData string) (string, error)
}

type TargetValidator interface {
	Validate(ctx context.Context, rawURL string) error
}

type KubeGateway interface {
	Probe(context.Context) (domain.ClusterProbe, error)
	Capabilities(context.Context, string) ([]domain.KubernetesCapability, error)
	Summary(context.Context) (domain.ClusterSummary, error)
	Namespaces(context.Context) ([]domain.Namespace, error)
	Nodes(context.Context) ([]domain.Node, error)
	NodeDetail(context.Context, string) (domain.NodeDetail, error)
	NodeEvents(context.Context, string, int) ([]domain.KubernetesEvent, error)
	Workloads(context.Context, string, string) ([]domain.Workload, error)
	WorkloadDetail(context.Context, domain.WorkloadReference) (domain.WorkloadDetail, error)
	WorkloadEvents(context.Context, domain.WorkloadReference, int) ([]domain.KubernetesEvent, error)
	PodLogs(context.Context, domain.PodLogRequest) (domain.PodLogs, error)
	ScaleWorkload(context.Context, domain.WorkloadReference, string, int32) (domain.Workload, error)
	RestartWorkload(context.Context, domain.WorkloadReference, string, time.Time) (domain.Workload, error)
	PreviewWorkloadImage(context.Context, domain.WorkloadImageChange) (domain.WorkloadImagePreview, error)
	UpdateWorkloadImage(context.Context, domain.WorkloadImageChange) (domain.Workload, error)
}

type KubeFactory interface {
	New(context.Context, kubernetes.Connection) (KubeGateway, error)
}

type RepositoryConnection struct {
	URL      string
	Username string
	Password string
}

type RepositoryChecker interface {
	Check(context.Context, RepositoryConnection) error
}

type HelmRequest struct {
	Connection kubernetes.Connection
	Repository *RepositoryConnection
	Input      domain.HelmOperationInput
}

type HelmGateway interface {
	List(context.Context, kubernetes.Connection, string) ([]domain.HelmRelease, error)
	Execute(context.Context, domain.OperationKind, HelmRequest) error
}

type OperationGovernor interface {
	Acquire(context.Context) (resourceguard.Snapshot, func(), error)
	Snapshot() resourceguard.Snapshot
}

type ReadGovernor interface {
	TryAcquire() (resourceguard.Snapshot, func(), bool)
	Snapshot() resourceguard.Snapshot
}

type KubernetesReadCapacity struct {
	Adaptive bool                   `json:"adaptive"`
	Pressure resourceguard.Pressure `json:"pressure"`
	Active   int                    `json:"active"`
	Limit    int                    `json:"limit"`
	Maximum  int                    `json:"maximum"`
}

type KubernetesClientCacheCapacity struct {
	Entries  int `json:"entries"`
	Capacity int `json:"capacity"`
	Maximum  int `json:"maximum"`
	Building int `json:"building"`
}

type OperationCapacity struct {
	resourceguard.Snapshot
	QueueDepth        int                           `json:"queue_depth"`
	QueueCapacity     int                           `json:"queue_capacity"`
	KubernetesReads   KubernetesReadCapacity        `json:"kubernetes_reads"`
	KubernetesClients KubernetesClientCacheCapacity `json:"kubernetes_clients"`
}

type Dependencies struct {
	Store                     Store
	Cipher                    SecretCipher
	TargetValidator           TargetValidator
	KubeFactory               KubeFactory
	RepositoryChecker         RepositoryChecker
	Helm                      HelmGateway
	OperationGovernor         OperationGovernor
	ReadGovernor              ReadGovernor
	OperationQueueSize        int
	KubernetesClientCacheSize int
	KubernetesClientCacheTTL  time.Duration
	Clock                     func() time.Time
	NewID                     func(prefix string) (string, error)
}
