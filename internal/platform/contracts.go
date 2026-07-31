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
	PodSecurityAdmissionNamespaces(context.Context) ([]domain.KubernetesPodSecurityAdmissionNamespace, error)
	NodeVersionSkew(context.Context) (domain.KubernetesNodeVersionSkewReport, error)
	DeprecatedAPIRequests(context.Context) ([]domain.KubernetesDeprecatedAPIRequest, error)
	EndpointCertificate(context.Context) (domain.KubernetesEndpointCertificate, error)
	APIServerReadiness(context.Context) (domain.KubernetesAPIServerReadiness, error)
	DisruptionBudgets(context.Context) ([]domain.KubernetesPodDisruptionBudget, error)
	Nodes(context.Context) ([]domain.Node, error)
	NodeDetail(context.Context, string) (domain.NodeDetail, error)
	NodeEvents(context.Context, string, int) ([]domain.KubernetesEvent, error)
	CustomResourceDefinitions(context.Context) ([]domain.KubernetesCustomResourceDefinition, error)
	CustomResourceDefinition(context.Context, string) (domain.KubernetesCustomResourceDefinitionDetail, error)
	CertificateSigningRequests(context.Context) ([]domain.KubernetesCertificateSigningRequest, error)
	CertificateSigningRequest(context.Context, string) (domain.KubernetesCertificateSigningRequestDetail, error)
	PriorityClasses(context.Context) ([]domain.KubernetesPriorityClass, error)
	PriorityClass(context.Context, string) (domain.KubernetesPriorityClassDetail, error)
	RuntimeClasses(context.Context) ([]domain.KubernetesRuntimeClass, error)
	RuntimeClass(context.Context, string) (domain.KubernetesRuntimeClassDetail, error)
	APIServices(context.Context) ([]domain.KubernetesAPIService, error)
	AdmissionWebhookConfigurations(context.Context, domain.KubernetesAdmissionWebhookConfigurationKind) ([]domain.KubernetesAdmissionWebhookConfiguration, error)
	AdmissionWebhookConfiguration(context.Context, domain.KubernetesAdmissionWebhookConfigurationKind, string) (domain.KubernetesAdmissionWebhookConfigurationDetail, error)
	ValidatingAdmissionPolicies(context.Context) ([]domain.KubernetesAdmissionPolicyResource, error)
	ValidatingAdmissionPolicy(context.Context, string) (domain.KubernetesValidatingAdmissionPolicyDetail, error)
	ValidatingAdmissionPolicyBindings(context.Context) ([]domain.KubernetesAdmissionPolicyResource, error)
	ValidatingAdmissionPolicyBinding(context.Context, string) (domain.KubernetesValidatingAdmissionPolicyBindingDetail, error)
	Events(context.Context, string, string, int) ([]domain.KubernetesEvent, error)
	Workloads(context.Context, string, string) ([]domain.Workload, error)
	Services(context.Context, string) ([]domain.KubernetesService, error)
	Ingresses(context.Context, string) ([]domain.KubernetesIngress, error)
	EndpointSlices(context.Context, string) ([]domain.KubernetesEndpointSlice, error)
	NetworkPolicies(context.Context, string) ([]domain.KubernetesNetworkPolicy, error)
	ConfigMaps(context.Context, string) ([]domain.KubernetesConfigMap, error)
	Secrets(context.Context, string) ([]domain.KubernetesSecret, error)
	PersistentVolumeClaims(context.Context, string) ([]domain.KubernetesPersistentVolumeClaim, error)
	PersistentVolumes(context.Context) ([]domain.KubernetesPersistentVolume, error)
	StorageClasses(context.Context) ([]domain.KubernetesStorageClass, error)
	VolumeAttachments(context.Context) ([]domain.KubernetesVolumeAttachment, error)
	CSIDrivers(context.Context) ([]domain.KubernetesCSIDriver, error)
	CSIDriver(context.Context, string) (domain.KubernetesCSIDriverDetail, error)
	CSINodes(context.Context) ([]domain.KubernetesCSINode, error)
	CSINode(context.Context, string) (domain.KubernetesCSINodeDetail, error)
	HelmReleaseHistory(context.Context, string, string) (domain.HelmReleaseHistory, error)
	ResourceQuotas(context.Context, string) ([]domain.KubernetesResourceQuota, error)
	LimitRanges(context.Context, string) ([]domain.KubernetesLimitRange, error)
	HorizontalPodAutoscalers(context.Context, string) ([]domain.KubernetesHorizontalPodAutoscaler, error)
	PodDisruptionBudgets(context.Context, string) ([]domain.KubernetesPodDisruptionBudget, error)
	AccessResources(context.Context, domain.KubernetesAccessResourceKind, string) ([]domain.KubernetesAccessResource, error)
	AccessResourceDetail(context.Context, domain.KubernetesAccessResourceReference) (domain.KubernetesAccessResourceDetail, error)
	ReviewServiceAccountAccess(context.Context, domain.KubernetesServiceAccountAccessReviewInput) (domain.KubernetesCapabilityState, error)
	WorkloadDetail(context.Context, domain.WorkloadReference) (domain.WorkloadDetail, error)
	DeploymentRevisionHistory(context.Context, domain.WorkloadReference) (domain.DeploymentRevisionHistory, error)
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
