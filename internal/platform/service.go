package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
)

const (
	defaultOperationQueueSize = 64
	maxOperationQueueSize     = 128
	targetLockStripes         = 64
)

type Service struct {
	store               Store
	cipher              SecretCipher
	targetValidator     TargetValidator
	kubeFactory         KubeFactory
	repositoryChecker   RepositoryChecker
	helm                HelmGateway
	operationGovernor   OperationGovernor
	clock               func() time.Time
	newID               func(string) (string, error)
	queue               chan operationJob
	runOnce             sync.Once
	targetLocks         [targetLockStripes]chan struct{}
	operationControlsMu sync.Mutex
	operationControls   map[string]*operationControl
}

type operationJob struct {
	operationID        string
	helmInput          *domain.HelmOperationInput
	workloadInput      *domain.WorkloadOperationInput
	workloadImageInput *domain.WorkloadImageOperationInput
	control            *operationControl
}

type operationControl struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func New(dependencies Dependencies) (*Service, error) {
	if dependencies.Store == nil || dependencies.Cipher == nil || dependencies.TargetValidator == nil ||
		dependencies.KubeFactory == nil || dependencies.RepositoryChecker == nil || dependencies.Helm == nil ||
		dependencies.OperationGovernor == nil {
		return nil, errors.New("all platform dependencies are required")
	}
	if dependencies.Clock == nil {
		dependencies.Clock = time.Now
	}
	if dependencies.NewID == nil {
		return nil, errors.New("ID generator is required")
	}
	if dependencies.OperationQueueSize == 0 {
		dependencies.OperationQueueSize = defaultOperationQueueSize
	}
	if dependencies.OperationQueueSize < 1 || dependencies.OperationQueueSize > maxOperationQueueSize {
		return nil, errors.New("operation queue size must be between 1 and 128")
	}
	service := &Service{
		store:             dependencies.Store,
		cipher:            dependencies.Cipher,
		targetValidator:   dependencies.TargetValidator,
		kubeFactory:       dependencies.KubeFactory,
		repositoryChecker: dependencies.RepositoryChecker,
		helm:              dependencies.Helm,
		operationGovernor: dependencies.OperationGovernor,
		clock:             dependencies.Clock,
		newID:             dependencies.NewID,
		queue:             make(chan operationJob, dependencies.OperationQueueSize),
		operationControls: make(map[string]*operationControl, dependencies.OperationQueueSize),
	}
	for index := range service.targetLocks {
		service.targetLocks[index] = make(chan struct{}, 1)
	}
	return service, nil
}

func (s *Service) CreateCluster(
	ctx context.Context,
	actor string,
	requestID string,
	input domain.ClusterInput,
) (ClusterView, error) {
	if err := domain.ValidateClusterInput(input); err != nil {
		return ClusterView{}, err
	}
	if err := s.targetValidator.Validate(ctx, input.Server); err != nil {
		return ClusterView{}, domain.Invalid("server", "target is blocked or cannot be resolved")
	}
	id, err := s.newID("clu")
	if err != nil {
		return ClusterView{}, fmt.Errorf("create cluster ID: %w", err)
	}
	tokenCiphertext, err := s.cipher.SealString(input.BearerToken, clusterAAD(id, "bearer_token"))
	if err != nil {
		return ClusterView{}, fmt.Errorf("encrypt cluster token: %w", err)
	}
	var caCiphertext string
	if input.CACert != "" {
		caCiphertext, err = s.cipher.SealString(input.CACert, clusterAAD(id, "ca_cert"))
		if err != nil {
			return ClusterView{}, fmt.Errorf("encrypt cluster CA: %w", err)
		}
	}
	now := s.now()
	cluster := domain.Cluster{
		ID:                    id,
		Name:                  input.Name,
		Environment:           input.Environment,
		Server:                strings.TrimRight(input.Server, "/"),
		Status:                domain.ClusterPending,
		CACertCiphertext:      caCiphertext,
		BearerTokenCiphertext: tokenCiphertext,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.store.CreateCluster(ctx, cluster); err != nil {
		return ClusterView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "cluster.create", "succeeded", id, "", input.Name, "connection metadata created", ""); err != nil {
		return ClusterView{}, fmt.Errorf("write cluster audit: %w", err)
	}
	view, _ := s.testClusterConnection(ctx, actor, requestID, id)
	return view, nil
}

func (s *Service) ListClusters(ctx context.Context) ([]ClusterView, error) {
	clusters, err := s.store.ListClusters(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]ClusterView, 0, len(clusters))
	for _, cluster := range clusters {
		views = append(views, clusterView(cluster))
	}
	return views, nil
}

func (s *Service) GetCluster(ctx context.Context, id string) (ClusterView, error) {
	cluster, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return ClusterView{}, err
	}
	return clusterView(cluster), nil
}

func (s *Service) TestClusterConnection(ctx context.Context, actor, requestID, id string) (ClusterView, error) {
	return s.testClusterConnection(ctx, actor, requestID, id)
}

func (s *Service) testClusterConnection(ctx context.Context, actor, requestID, id string) (ClusterView, error) {
	cluster, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return ClusterView{}, err
	}
	if cluster.Status == domain.ClusterDisabled {
		return ClusterView{}, domain.ErrInvalidState
	}
	connection, err := s.clusterConnection(cluster)
	if err != nil {
		return ClusterView{}, err
	}
	gateway, err := s.kubeFactory.New(connection)
	if err != nil {
		return ClusterView{}, fmt.Errorf("create Kubernetes gateway: %w", err)
	}
	probe, probeErr := gateway.Probe(ctx)
	now := s.now()
	cluster.LastCheckedAt = now
	cluster.UpdatedAt = now
	result := "succeeded"
	if probeErr == nil {
		cluster.Status = domain.ClusterConnected
		cluster.Version = probe.Version
		cluster.LastErrorCode = ""
	} else {
		cluster.Status, cluster.LastErrorCode = clusterFailure(probeErr)
		result = "failed"
	}
	if err := s.store.UpdateCluster(ctx, cluster); err != nil {
		return ClusterView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "cluster.connection_test", result, id, "", cluster.Name, cluster.LastErrorCode, ""); err != nil {
		return ClusterView{}, fmt.Errorf("write connection test audit: %w", err)
	}
	return clusterView(cluster), probeErr
}

func (s *Service) SetClusterEnabled(ctx context.Context, actor, requestID, id string, enabled bool) (ClusterView, error) {
	cluster, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return ClusterView{}, err
	}
	if enabled {
		cluster.Status = domain.ClusterPending
	} else {
		cluster.Status = domain.ClusterDisabled
	}
	cluster.UpdatedAt = s.now()
	if err := s.store.UpdateCluster(ctx, cluster); err != nil {
		return ClusterView{}, err
	}
	if err := s.audit(ctx, actor, requestID, "cluster.update", "succeeded", id, "", cluster.Name, fmt.Sprintf("enabled=%t", enabled), ""); err != nil {
		return ClusterView{}, err
	}
	if enabled {
		view, _ := s.testClusterConnection(ctx, actor, requestID, id)
		return view, nil
	}
	return clusterView(cluster), nil
}

func (s *Service) DeleteCluster(ctx context.Context, actor, requestID, id, expectedName string) error {
	cluster, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return err
	}
	if expectedName != cluster.Name {
		return domain.Invalid("confirmation", "must match the cluster name")
	}
	if err := s.audit(ctx, actor, requestID, "cluster.delete", "succeeded", id, "", cluster.Name, "connection metadata deleted", ""); err != nil {
		return err
	}
	return s.store.DeleteCluster(ctx, id)
}

func (s *Service) Summary(ctx context.Context, clusterID string) (domain.ClusterSummary, error) {
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.ClusterSummary{}, err
	}
	return gateway.Summary(ctx)
}

func (s *Service) Namespaces(ctx context.Context, clusterID string) ([]domain.Namespace, error) {
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Namespaces(ctx)
}

func (s *Service) Nodes(ctx context.Context, clusterID string) ([]domain.Node, error) {
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Nodes(ctx)
}

func (s *Service) NodeDetail(ctx context.Context, clusterID, name string) (domain.NodeDetail, error) {
	if err := domain.ValidateNodeName(name); err != nil {
		return domain.NodeDetail{}, err
	}
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.NodeDetail{}, err
	}
	return gateway.NodeDetail(ctx, name)
}

func (s *Service) NodeEvents(ctx context.Context, clusterID, name string, limit int) ([]domain.KubernetesEvent, error) {
	if err := domain.ValidateNodeName(name); err != nil {
		return nil, err
	}
	if limit < 1 || limit > domain.MaxNodeEventLimit {
		return nil, domain.Invalid("limit", "must be between 1 and 100")
	}
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.NodeEvents(ctx, name, limit)
}

func (s *Service) Workloads(ctx context.Context, clusterID, namespace, kind string) ([]domain.Workload, error) {
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Workloads(ctx, namespace, kind)
}

func (s *Service) WorkloadDetail(ctx context.Context, clusterID string, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return domain.WorkloadDetail{}, err
	}
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.WorkloadDetail{}, err
	}
	return gateway.WorkloadDetail(ctx, reference)
}

func (s *Service) WorkloadEvents(ctx context.Context, clusterID string, reference domain.WorkloadReference, limit int) ([]domain.KubernetesEvent, error) {
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return nil, err
	}
	if limit < 1 || limit > domain.MaxWorkloadEventLimit {
		return nil, domain.Invalid("limit", "must be between 1 and 100")
	}
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.WorkloadEvents(ctx, reference, limit)
}

func (s *Service) PodLogs(
	ctx context.Context,
	actor, requestID, clusterID string,
	input domain.PodLogRequest,
) (domain.PodLogs, error) {
	if err := domain.ValidatePodLogRequest(input); err != nil {
		return domain.PodLogs{}, err
	}
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.PodLogs{}, err
	}
	logs, logsErr := gateway.PodLogs(ctx, input)
	result := "succeeded"
	summary := fmt.Sprintf("container=%s, previous=%t, tail_lines=%d", input.Container, input.Previous, input.TailLines)
	if logsErr != nil {
		result = "failed"
		summary = "pod log request failed"
	}
	if err := s.audit(ctx, actor, requestID, "pod.logs.read", result, clusterID, input.Namespace, input.Pod+"/"+input.Container, summary, ""); err != nil {
		return domain.PodLogs{}, fmt.Errorf("write pod log audit: %w", err)
	}
	return logs, logsErr
}

func (s *Service) kubeGateway(ctx context.Context, clusterID string) (KubeGateway, error) {
	cluster, err := s.store.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if cluster.Status == domain.ClusterDisabled {
		return nil, domain.ErrInvalidState
	}
	connection, err := s.clusterConnection(cluster)
	if err != nil {
		return nil, err
	}
	return s.kubeFactory.New(connection)
}

func (s *Service) clusterConnection(cluster domain.Cluster) (kubernetes.Connection, error) {
	token, err := s.cipher.OpenString(cluster.BearerTokenCiphertext, clusterAAD(cluster.ID, "bearer_token"))
	if err != nil {
		return kubernetes.Connection{}, fmt.Errorf("decrypt cluster token: %w", err)
	}
	var ca string
	if cluster.CACertCiphertext != "" {
		ca, err = s.cipher.OpenString(cluster.CACertCiphertext, clusterAAD(cluster.ID, "ca_cert"))
		if err != nil {
			return kubernetes.Connection{}, fmt.Errorf("decrypt cluster CA: %w", err)
		}
	}
	return kubernetes.Connection{Server: cluster.Server, CACert: ca, BearerToken: token}, nil
}

func clusterAAD(id, field string) string {
	return "cluster:" + id + ":" + field
}

func clusterFailure(err error) (domain.ClusterStatus, string) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		return domain.ClusterDegraded, "permission_denied"
	case errors.Is(err, domain.ErrUnauthorized):
		return domain.ClusterUnreachable, "credentials_rejected"
	case errors.Is(err, domain.ErrTimeout):
		return domain.ClusterUnreachable, "upstream_timeout"
	default:
		return domain.ClusterUnreachable, "upstream_unavailable"
	}
}

func (s *Service) now() time.Time {
	return s.clock().UTC()
}
