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
	credentialRotationLock    = "cluster-credentials"
	capabilityScanLock        = "cluster-capabilities"
	capabilityScanTimeout     = 20 * time.Second
)

type Service struct {
	store               Store
	cipher              SecretCipher
	targetValidator     TargetValidator
	kubeFactory         KubeFactory
	repositoryChecker   RepositoryChecker
	helm                HelmGateway
	operationGovernor   OperationGovernor
	readGovernor        ReadGovernor
	gatewayCache        *gatewayCache
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
		dependencies.OperationGovernor == nil || dependencies.ReadGovernor == nil {
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
	if dependencies.KubernetesClientCacheSize == 0 {
		dependencies.KubernetesClientCacheSize = defaultGatewayCacheSize
	}
	if dependencies.KubernetesClientCacheTTL == 0 {
		dependencies.KubernetesClientCacheTTL = defaultGatewayCacheTTL
	}
	gatewayCache, err := newGatewayCache(
		dependencies.KubernetesClientCacheSize, dependencies.KubernetesClientCacheTTL, dependencies.Clock,
	)
	if err != nil {
		return nil, err
	}
	service := &Service{
		store:             dependencies.Store,
		cipher:            dependencies.Cipher,
		targetValidator:   dependencies.TargetValidator,
		kubeFactory:       dependencies.KubeFactory,
		repositoryChecker: dependencies.RepositoryChecker,
		helm:              dependencies.Helm,
		operationGovernor: dependencies.OperationGovernor,
		readGovernor:      dependencies.ReadGovernor,
		gatewayCache:      gatewayCache,
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return ClusterView{}, err
	}
	defer release()
	s.gatewayCache.Invalidate(id)
	return s.testClusterConnection(ctx, actor, requestID, id)
}

func (s *Service) RotateClusterCredentials(
	ctx context.Context,
	actor, requestID, id string,
	input domain.ClusterCredentialRotationInput,
) (ClusterView, error) {
	if err := domain.ValidateClusterCredentialRotationInput(input); err != nil {
		return ClusterView{}, err
	}
	unlock, err := s.tryAcquireTargetLock(ctx, id, "", credentialRotationLock)
	if err != nil {
		return ClusterView{}, err
	}
	defer unlock()
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return ClusterView{}, err
	}
	defer release()

	current, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return ClusterView{}, err
	}
	if current.Status == domain.ClusterDisabled {
		return ClusterView{}, domain.ErrInvalidState
	}
	if input.Confirmation != current.Name {
		return ClusterView{}, domain.Invalid("confirmation", "must match the cluster name")
	}

	candidate, buildErr := s.kubeFactory.New(ctx, kubernetes.Connection{
		Server: current.Server, CACert: input.CACert, BearerToken: input.BearerToken,
	})
	if buildErr != nil {
		if candidate != nil {
			closeIdleGateways([]KubeGateway{candidate})
		}
		return ClusterView{}, s.recordCredentialRotationFailure(ctx, actor, requestID, current, buildErr)
	}
	if candidate == nil {
		return ClusterView{}, s.recordCredentialRotationFailure(
			ctx, actor, requestID, current, errors.New("Kubernetes gateway builder returned no gateway"),
		)
	}
	defer closeIdleGateways([]KubeGateway{candidate})
	probe, probeErr := candidate.Probe(ctx)
	if probeErr != nil {
		return ClusterView{}, s.recordCredentialRotationFailure(ctx, actor, requestID, current, probeErr)
	}

	tokenCiphertext, err := s.cipher.SealString(input.BearerToken, clusterAAD(id, "bearer_token"))
	if err != nil {
		return ClusterView{}, s.recordCredentialRotationFailure(ctx, actor, requestID, current, fmt.Errorf("encrypt cluster token: %w", err))
	}
	var caCiphertext string
	if input.CACert != "" {
		caCiphertext, err = s.cipher.SealString(input.CACert, clusterAAD(id, "ca_cert"))
		if err != nil {
			return ClusterView{}, s.recordCredentialRotationFailure(ctx, actor, requestID, current, fmt.Errorf("encrypt cluster CA: %w", err))
		}
	}

	updated := current
	updated.CACertCiphertext = caCiphertext
	updated.BearerTokenCiphertext = tokenCiphertext
	updated.Status = domain.ClusterConnected
	updated.Version = probe.Version
	updated.LastErrorCode = ""
	updated.LastCheckedAt = s.now()
	updated.UpdatedAt = updated.LastCheckedAt
	audit, err := s.newAuditEvent(
		actor, requestID, "cluster.credentials.rotate", "succeeded", id, "", current.Name,
		"connection credentials rotated", "",
	)
	if err != nil {
		return ClusterView{}, err
	}
	if err := s.store.RotateClusterCredentials(ctx, current, updated, audit); err != nil {
		return ClusterView{}, err
	}
	s.gatewayCache.Invalidate(id)
	return clusterView(updated), nil
}

func (s *Service) recordCredentialRotationFailure(
	ctx context.Context,
	actor, requestID string,
	cluster domain.Cluster,
	cause error,
) error {
	if err := s.audit(
		ctx, actor, requestID, "cluster.credentials.rotate", "failed", cluster.ID, "", cluster.Name,
		"candidate credential validation failed", "",
	); err != nil {
		return fmt.Errorf("write credential rotation audit: %w", err)
	}
	return cause
}

func (s *Service) testClusterConnection(ctx context.Context, actor, requestID, id string) (ClusterView, error) {
	cluster, err := s.store.GetCluster(ctx, id)
	if err != nil {
		return ClusterView{}, err
	}
	if cluster.Status == domain.ClusterDisabled {
		return ClusterView{}, domain.ErrInvalidState
	}
	gateway, err := s.gatewayForCluster(ctx, cluster)
	if err != nil {
		return ClusterView{}, err
	}
	probe, probeErr := gateway.Probe(ctx)
	if probeErr != nil {
		s.gatewayCache.Invalidate(id)
	}
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
	if !enabled {
		s.gatewayCache.Invalidate(id)
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
	if err := s.store.DeleteCluster(ctx, id); err != nil {
		return err
	}
	s.gatewayCache.Invalidate(id)
	return nil
}

func (s *Service) Summary(ctx context.Context, clusterID string) (domain.ClusterSummary, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.ClusterSummary{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.ClusterSummary{}, err
	}
	return gateway.Summary(ctx)
}

func (s *Service) ClusterCapabilities(ctx context.Context, clusterID, namespace string) (domain.ClusterCapabilities, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return domain.ClusterCapabilities{}, err
	}
	unlock, err := s.tryAcquireTargetLock(ctx, clusterID, namespace, capabilityScanLock)
	if err != nil {
		return domain.ClusterCapabilities{}, err
	}
	defer unlock()
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.ClusterCapabilities{}, err
	}
	defer release()
	scanContext, cancel := context.WithTimeout(ctx, capabilityScanTimeout)
	defer cancel()
	gateway, err := s.kubeGateway(scanContext, clusterID)
	if err != nil {
		return domain.ClusterCapabilities{}, err
	}
	checks, err := gateway.Capabilities(scanContext, namespace)
	if err != nil {
		return domain.ClusterCapabilities{}, err
	}
	return domain.ClusterCapabilities{
		Namespace: namespace,
		CheckedAt: s.now(),
		Checks:    append([]domain.KubernetesCapability(nil), checks...),
	}, nil
}

func (s *Service) Namespaces(ctx context.Context, clusterID string) ([]domain.Namespace, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Namespaces(ctx)
}

func (s *Service) Nodes(ctx context.Context, clusterID string) ([]domain.Node, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.NodeDetail{}, err
	}
	defer release()
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.NodeEvents(ctx, name, limit)
}

func (s *Service) CustomResourceDefinitions(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesCustomResourceDefinition, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.CustomResourceDefinitions(ctx)
}

func (s *Service) CustomResourceDefinition(
	ctx context.Context,
	clusterID, name string,
) (domain.KubernetesCustomResourceDefinitionDetail, error) {
	if err := domain.ValidateCustomResourceDefinitionName(name); err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesCustomResourceDefinitionDetail{}, err
	}
	return gateway.CustomResourceDefinition(ctx, name)
}

func (s *Service) APIServices(ctx context.Context, clusterID string) ([]domain.KubernetesAPIService, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.APIServices(ctx)
}

func (s *Service) AdmissionWebhookConfigurations(
	ctx context.Context,
	clusterID string,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
) ([]domain.KubernetesAdmissionWebhookConfiguration, error) {
	if err := domain.ValidateAdmissionWebhookConfigurationKind(kind); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.AdmissionWebhookConfigurations(ctx, kind)
}

func (s *Service) AdmissionWebhookConfiguration(
	ctx context.Context,
	clusterID string,
	kind domain.KubernetesAdmissionWebhookConfigurationKind,
	name string,
) (domain.KubernetesAdmissionWebhookConfigurationDetail, error) {
	if err := domain.ValidateAdmissionWebhookConfigurationKind(kind); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	if err := domain.ValidateAdmissionWebhookConfigurationName(name); err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesAdmissionWebhookConfigurationDetail{}, err
	}
	return gateway.AdmissionWebhookConfiguration(ctx, kind, name)
}

func (s *Service) ValidatingAdmissionPolicies(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesAdmissionPolicyResource, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.ValidatingAdmissionPolicies(ctx)
}

func (s *Service) ValidatingAdmissionPolicy(
	ctx context.Context,
	clusterID, name string,
) (domain.KubernetesValidatingAdmissionPolicyDetail, error) {
	if err := domain.ValidateAdmissionPolicyResourceName(name); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyDetail{}, err
	}
	return gateway.ValidatingAdmissionPolicy(ctx, name)
}

func (s *Service) ValidatingAdmissionPolicyBindings(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesAdmissionPolicyResource, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.ValidatingAdmissionPolicyBindings(ctx)
}

func (s *Service) ValidatingAdmissionPolicyBinding(
	ctx context.Context,
	clusterID, name string,
) (domain.KubernetesValidatingAdmissionPolicyBindingDetail, error) {
	if err := domain.ValidateAdmissionPolicyResourceName(name); err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesValidatingAdmissionPolicyBindingDetail{}, err
	}
	return gateway.ValidatingAdmissionPolicyBinding(ctx, name)
}

func (s *Service) Events(
	ctx context.Context,
	clusterID, namespace, eventType string,
	limit int,
) ([]domain.KubernetesEvent, error) {
	if err := domain.ValidateKubernetesEventList(namespace, eventType, limit); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Events(ctx, namespace, eventType, limit)
}

func (s *Service) Workloads(ctx context.Context, clusterID, namespace, kind string) ([]domain.Workload, error) {
	if err := domain.ValidateWorkloadList(namespace, kind); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Workloads(ctx, namespace, kind)
}

func (s *Service) Services(ctx context.Context, clusterID, namespace string) ([]domain.KubernetesService, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Services(ctx, namespace)
}

func (s *Service) Ingresses(ctx context.Context, clusterID, namespace string) ([]domain.KubernetesIngress, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.Ingresses(ctx, namespace)
}

func (s *Service) EndpointSlices(ctx context.Context, clusterID, namespace string) ([]domain.KubernetesEndpointSlice, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.EndpointSlices(ctx, namespace)
}

func (s *Service) NetworkPolicies(ctx context.Context, clusterID, namespace string) ([]domain.KubernetesNetworkPolicy, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.NetworkPolicies(ctx, namespace)
}

func (s *Service) ConfigMaps(ctx context.Context, clusterID, namespace string) ([]domain.KubernetesConfigMap, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.ConfigMaps(ctx, namespace)
}

func (s *Service) Secrets(
	ctx context.Context,
	actor, requestID, clusterID, namespace string,
) ([]domain.KubernetesSecret, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, listErr := s.kubeGateway(ctx, clusterID)
	var items []domain.KubernetesSecret
	if listErr == nil {
		items, listErr = gateway.Secrets(ctx, namespace)
	}
	result := "failed"
	summary := "secret metadata list failed"
	if listErr == nil {
		result = "succeeded"
		summary = fmt.Sprintf("count=%d", len(items))
	}
	if err := s.audit(ctx, actor, requestID, "secret.metadata.list", result, clusterID, namespace, "secrets", summary, ""); err != nil {
		return nil, fmt.Errorf("write secret metadata audit: %w", err)
	}
	return items, listErr
}

func (s *Service) PersistentVolumeClaims(
	ctx context.Context,
	clusterID, namespace string,
) ([]domain.KubernetesPersistentVolumeClaim, error) {
	if namespace != "" {
		if err := domain.ValidateNamespace(namespace); err != nil {
			return nil, err
		}
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.PersistentVolumeClaims(ctx, namespace)
}

func (s *Service) PersistentVolumes(ctx context.Context, clusterID string) ([]domain.KubernetesPersistentVolume, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.PersistentVolumes(ctx)
}

func (s *Service) StorageClasses(ctx context.Context, clusterID string) ([]domain.KubernetesStorageClass, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.StorageClasses(ctx)
}

func (s *Service) ResourceQuotas(
	ctx context.Context,
	clusterID, namespace string,
) ([]domain.KubernetesResourceQuota, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.ResourceQuotas(ctx, namespace)
}

func (s *Service) PodSecurityAdmissionNamespaces(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesPodSecurityAdmissionNamespace, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.PodSecurityAdmissionNamespaces(ctx)
}

func (s *Service) NodeVersionSkew(
	ctx context.Context,
	clusterID string,
) (domain.KubernetesNodeVersionSkewReport, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesNodeVersionSkewReport{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesNodeVersionSkewReport{}, err
	}
	return gateway.NodeVersionSkew(ctx)
}

func (s *Service) DeprecatedAPIRequests(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesDeprecatedAPIRequest, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.DeprecatedAPIRequests(ctx)
}

func (s *Service) EndpointCertificate(
	ctx context.Context,
	clusterID string,
) (domain.KubernetesEndpointCertificate, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesEndpointCertificate{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesEndpointCertificate{}, err
	}
	return gateway.EndpointCertificate(ctx)
}

func (s *Service) DisruptionBudgets(
	ctx context.Context,
	clusterID string,
) ([]domain.KubernetesPodDisruptionBudget, error) {
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.DisruptionBudgets(ctx)
}

func (s *Service) LimitRanges(
	ctx context.Context,
	clusterID, namespace string,
) ([]domain.KubernetesLimitRange, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.LimitRanges(ctx, namespace)
}

func (s *Service) HorizontalPodAutoscalers(
	ctx context.Context,
	clusterID, namespace string,
) ([]domain.KubernetesHorizontalPodAutoscaler, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.HorizontalPodAutoscalers(ctx, namespace)
}

func (s *Service) PodDisruptionBudgets(
	ctx context.Context,
	clusterID, namespace string,
) ([]domain.KubernetesPodDisruptionBudget, error) {
	if err := domain.ValidateNamespace(namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.PodDisruptionBudgets(ctx, namespace)
}

func (s *Service) AccessResources(
	ctx context.Context,
	clusterID string,
	kind domain.KubernetesAccessResourceKind,
	namespace string,
) ([]domain.KubernetesAccessResource, error) {
	if err := domain.ValidateAccessResourceScope(kind, namespace); err != nil {
		return nil, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return gateway.AccessResources(ctx, kind, namespace)
}

func (s *Service) AccessResourceDetail(
	ctx context.Context,
	clusterID string,
	reference domain.KubernetesAccessResourceReference,
) (domain.KubernetesAccessResourceDetail, error) {
	if err := domain.ValidateAccessResourceReference(reference); err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	defer release()
	gateway, err := s.kubeGateway(ctx, clusterID)
	if err != nil {
		return domain.KubernetesAccessResourceDetail{}, err
	}
	return gateway.AccessResourceDetail(ctx, reference)
}

func (s *Service) ReviewServiceAccountAccess(
	ctx context.Context,
	actor, requestID, clusterID string,
	input domain.KubernetesServiceAccountAccessReviewInput,
) (domain.KubernetesServiceAccountAccessReview, error) {
	if err := domain.ValidateServiceAccountAccessReviewInput(input); err != nil {
		return domain.KubernetesServiceAccountAccessReview{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.KubernetesServiceAccountAccessReview{}, err
	}
	gateway, reviewErr := s.kubeGateway(ctx, clusterID)
	var state domain.KubernetesCapabilityState
	if reviewErr == nil {
		state, reviewErr = gateway.ReviewServiceAccountAccess(ctx, input)
	}
	if reviewErr == nil && state != domain.KubernetesCapabilityAllowed && state != domain.KubernetesCapabilityDenied &&
		state != domain.KubernetesCapabilityIndeterminate {
		reviewErr = fmt.Errorf("invalid Kubernetes service account access review state: %w", domain.ErrUpstream)
	}
	checkedAt := s.now()
	release()
	result := "failed"
	summary := "service account access review failed"
	if reviewErr == nil {
		result = "succeeded"
		attributes := input.ResourceAttributes
		summary = fmt.Sprintf(
			"state=%s, verb=%s, group=%s, resource=%s, subresource=%s, target_namespace=%s",
			state,
			attributes.Verb,
			accessReviewAuditValue(attributes.Group, "core"),
			attributes.Resource,
			accessReviewAuditValue(attributes.Subresource, "-"),
			accessReviewAuditValue(attributes.Namespace, "cluster"),
		)
	}
	serviceAccount := input.ServiceAccount
	if err := s.audit(
		ctx,
		actor,
		requestID,
		"service_account.access_review",
		result,
		clusterID,
		serviceAccount.Namespace,
		serviceAccount.Namespace+"/"+serviceAccount.Name,
		summary,
		"",
	); err != nil {
		return domain.KubernetesServiceAccountAccessReview{}, fmt.Errorf("write service account access review audit: %w", err)
	}
	if reviewErr != nil {
		return domain.KubernetesServiceAccountAccessReview{}, reviewErr
	}
	return domain.KubernetesServiceAccountAccessReview{
		ServiceAccount:     serviceAccount,
		ResourceAttributes: input.ResourceAttributes,
		State:              state,
		CheckedAt:          checkedAt,
	}, nil
}

func accessReviewAuditValue(value, empty string) string {
	if value == "" {
		return empty
	}
	return value
}

func (s *Service) WorkloadDetail(ctx context.Context, clusterID string, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	if err := domain.ValidateWorkloadReference(reference); err != nil {
		return domain.WorkloadDetail{}, err
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.WorkloadDetail{}, err
	}
	defer release()
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.PodLogs{}, err
	}
	defer release()
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

func (s *Service) acquireKubernetesRead(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	snapshot, release, ok := s.readGovernor.TryAcquire()
	s.gatewayCache.Reconcile(snapshot)
	if !ok {
		return nil, domain.ErrBusy
	}
	return release, nil
}

func (s *Service) kubeGateway(ctx context.Context, clusterID string) (KubeGateway, error) {
	cluster, err := s.store.GetCluster(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	if cluster.Status == domain.ClusterDisabled {
		return nil, domain.ErrInvalidState
	}
	return s.gatewayForCluster(ctx, cluster)
}

func (s *Service) gatewayForCluster(ctx context.Context, cluster domain.Cluster) (KubeGateway, error) {
	gateway, err := s.gatewayCache.Get(
		ctx,
		cluster.ID,
		clusterGatewayFingerprint(cluster),
		func(buildContext context.Context) (KubeGateway, error) {
			connection, err := s.clusterConnection(cluster)
			if err != nil {
				return nil, err
			}
			return s.kubeFactory.New(buildContext, connection)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes gateway: %w", err)
	}
	return gateway, nil
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
