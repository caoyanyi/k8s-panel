package platform

import (
	"context"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
)

type Store interface {
	CreateCluster(context.Context, domain.Cluster) error
	ListClusters(context.Context) ([]domain.Cluster, error)
	GetCluster(context.Context, string) (domain.Cluster, error)
	UpdateCluster(context.Context, domain.Cluster) error
	DeleteCluster(context.Context, string) error

	CreateRepository(context.Context, domain.Repository) error
	ListRepositories(context.Context) ([]domain.Repository, error)
	GetRepository(context.Context, string) (domain.Repository, error)
	UpdateRepository(context.Context, domain.Repository) error
	DeleteRepository(context.Context, string) error

	CreateOperation(context.Context, domain.Operation) error
	GetOperation(context.Context, string) (domain.Operation, error)
	UpdateOperation(context.Context, domain.Operation) error
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
	Summary(context.Context) (domain.ClusterSummary, error)
	Namespaces(context.Context) ([]domain.Namespace, error)
	Workloads(context.Context, string, string) ([]domain.Workload, error)
}

type KubeFactory interface {
	New(kubernetes.Connection) (KubeGateway, error)
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

type Dependencies struct {
	Store             Store
	Cipher            SecretCipher
	TargetValidator   TargetValidator
	KubeFactory       KubeFactory
	RepositoryChecker RepositoryChecker
	Helm              HelmGateway
	Clock             func() time.Time
	NewID             func(prefix string) (string, error)
}
