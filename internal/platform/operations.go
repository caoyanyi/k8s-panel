package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func (s *Service) Run(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	s.runOnce.Do(func() {
		for range workers {
			go s.worker(ctx)
		}
	})
	<-ctx.Done()
}

func (s *Service) SubmitHelmOperation(
	ctx context.Context,
	actor string,
	requestID string,
	kind domain.OperationKind,
	input domain.HelmOperationInput,
) (domain.Operation, error) {
	if kind != domain.OperationHelmInstall && kind != domain.OperationHelmUpgrade &&
		kind != domain.OperationHelmRollback && kind != domain.OperationHelmUninstall {
		return domain.Operation{}, domain.Invalid("kind", "unsupported Helm operation")
	}
	if kind == domain.OperationHelmInstall || kind == domain.OperationHelmUpgrade {
		if err := domain.ValidateHelmOperationInput(input); err != nil {
			return domain.Operation{}, err
		}
	} else if err := validateHelmTarget(input); err != nil {
		return domain.Operation{}, err
	}
	cluster, err := s.store.GetCluster(ctx, input.ClusterID)
	if err != nil {
		return domain.Operation{}, err
	}
	if cluster.Status != domain.ClusterConnected && cluster.Status != domain.ClusterDegraded {
		return domain.Operation{}, domain.ErrInvalidState
	}
	if input.RepositoryID != "" {
		repository, err := s.store.GetRepository(ctx, input.RepositoryID)
		if err != nil {
			return domain.Operation{}, err
		}
		if !repository.Enabled || repository.Status != "connected" {
			return domain.Operation{}, domain.ErrInvalidState
		}
	}
	id, err := s.newID("op")
	if err != nil {
		return domain.Operation{}, fmt.Errorf("create operation ID: %w", err)
	}
	now := s.now()
	operation := domain.Operation{
		ID:          id,
		RequestID:   requestID,
		Kind:        kind,
		State:       domain.OperationQueued,
		ClusterID:   input.ClusterID,
		Namespace:   input.Namespace,
		Target:      input.ReleaseName,
		SubmittedBy: actor,
		Summary:     operationSummary(input),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := s.store.CreateOperation(ctx, operation); err != nil {
		return domain.Operation{}, err
	}
	if err := s.audit(ctx, actor, requestID, string(kind), "submitted", input.ClusterID, input.Namespace, input.ReleaseName, operation.Summary, id); err != nil {
		return domain.Operation{}, fmt.Errorf("write operation audit: %w", err)
	}
	select {
	case s.queue <- operationJob{operationID: id, input: input}:
		return operation, nil
	default:
		operation.State = domain.OperationFailed
		operation.ErrorCode = "queue_full"
		operation.ErrorMessage = "操作队列已满，请稍后重试"
		operation.FinishedAt = now
		operation.UpdatedAt = now
		if err := s.store.UpdateOperation(ctx, operation); err != nil {
			return domain.Operation{}, err
		}
		return domain.Operation{}, errors.New("operation queue is full")
	}
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-s.queue:
			s.executeOperation(ctx, job)
		}
	}
}

func (s *Service) executeOperation(ctx context.Context, job operationJob) {
	operation, err := s.store.GetOperation(ctx, job.operationID)
	if err != nil {
		return
	}
	lock := s.releaseLock(operation.ClusterID, operation.Namespace, operation.Target)
	lock.Lock()
	defer lock.Unlock()

	operation.State = domain.OperationRunning
	operation.StartedAt = s.now()
	operation.UpdatedAt = operation.StartedAt
	if err := s.store.UpdateOperation(ctx, operation); err != nil {
		return
	}
	request, err := s.helmRequest(ctx, job.input)
	if err == nil {
		err = s.helm.Execute(ctx, operation.Kind, request)
	}
	now := s.now()
	operation.FinishedAt = now
	operation.UpdatedAt = now
	result := "succeeded"
	if err == nil {
		operation.State = domain.OperationSucceeded
	} else {
		operation.State = domain.OperationFailed
		operation.ErrorCode = operationErrorCode(err)
		operation.ErrorMessage = "Helm 操作执行失败，请查看错误码和目标集群状态"
		result = "failed"
	}
	if updateErr := s.store.UpdateOperation(context.WithoutCancel(ctx), operation); updateErr != nil {
		return
	}
	_ = s.audit(
		context.WithoutCancel(ctx),
		operation.SubmittedBy,
		operation.RequestID,
		string(operation.Kind),
		result,
		operation.ClusterID,
		operation.Namespace,
		operation.Target,
		operation.ErrorCode,
		operation.ID,
	)
}

func (s *Service) helmRequest(ctx context.Context, input domain.HelmOperationInput) (HelmRequest, error) {
	cluster, err := s.store.GetCluster(ctx, input.ClusterID)
	if err != nil {
		return HelmRequest{}, err
	}
	connection, err := s.clusterConnection(cluster)
	if err != nil {
		return HelmRequest{}, err
	}
	request := HelmRequest{Connection: connection, Input: input}
	if input.RepositoryID != "" {
		repository, err := s.store.GetRepository(ctx, input.RepositoryID)
		if err != nil {
			return HelmRequest{}, err
		}
		repositoryConnection, err := s.repositoryConnection(repository)
		if err != nil {
			return HelmRequest{}, err
		}
		request.Repository = &repositoryConnection
	}
	return request, nil
}

func (s *Service) ListHelmReleases(ctx context.Context, clusterID, namespace string) ([]domain.HelmRelease, error) {
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
	return s.helm.List(ctx, connection, namespace)
}

func (s *Service) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	return s.store.GetOperation(ctx, id)
}

func (s *Service) ListOperations(ctx context.Context, limit int) ([]domain.Operation, error) {
	return s.store.ListOperations(ctx, limit)
}

func (s *Service) ListAuditEvents(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	return s.store.ListAuditEvents(ctx, limit)
}

func (s *Service) releaseLock(clusterID, namespace, name string) *sync.Mutex {
	key := clusterID + "\x00" + namespace + "\x00" + name
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	lock := s.releaseLocks[key]
	if lock == nil {
		lock = &sync.Mutex{}
		s.releaseLocks[key] = lock
	}
	return lock
}

func (s *Service) audit(
	ctx context.Context,
	actor, requestID, action, result, clusterID, namespace, target, summary, operationID string,
) error {
	id, err := s.newID("audit")
	if err != nil {
		return fmt.Errorf("create audit ID: %w", err)
	}
	return s.store.CreateAuditEvent(ctx, domain.AuditEvent{
		ID:          id,
		RequestID:   requestID,
		OperationID: operationID,
		Actor:       actor,
		Action:      action,
		Result:      result,
		ClusterID:   clusterID,
		Namespace:   namespace,
		Target:      target,
		Summary:     summary,
		CreatedAt:   s.now(),
	})
}

func validateHelmTarget(input domain.HelmOperationInput) error {
	if input.ClusterID == "" {
		return domain.Invalid("cluster_id", "is required")
	}
	if input.Namespace == "" {
		return domain.Invalid("namespace", "is required")
	}
	if input.ReleaseName == "" {
		return domain.Invalid("release_name", "is required")
	}
	if strings.ContainsAny(input.Namespace+input.ReleaseName, " /\\") {
		return domain.Invalid("release_name", "contains invalid characters")
	}
	return nil
}

func operationSummary(input domain.HelmOperationInput) string {
	parts := make([]string, 0, 2)
	if input.Chart != "" {
		parts = append(parts, "chart="+input.Chart)
	}
	if input.Version != "" {
		parts = append(parts, "version="+input.Version)
	}
	return strings.Join(parts, ", ")
}

func operationErrorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		return "permission_denied"
	case errors.Is(err, domain.ErrUnauthorized):
		return "credentials_rejected"
	case errors.Is(err, domain.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "upstream_timeout"
	case errors.Is(err, domain.ErrConflict):
		return "release_conflict"
	default:
		return "helm_operation_failed"
	}
}
