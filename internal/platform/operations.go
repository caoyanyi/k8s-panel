package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/caoyanyi/k8s-panel/internal/domain"
)

func (s *Service) Run(ctx context.Context, workers int) {
	if workers < 1 {
		workers = 1
	}
	started := false
	s.runOnce.Do(func() {
		started = true
		for range workers {
			go s.worker(ctx)
		}
	})
	<-ctx.Done()
	if started {
		s.cancelAllOperationControls()
	}
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
	return s.createAndEnqueueOperation(ctx, operation, operationJob{operationID: id, helmInput: &input})
}

func (s *Service) SubmitWorkloadOperation(
	ctx context.Context,
	actor string,
	requestID string,
	kind domain.OperationKind,
	input domain.WorkloadOperationInput,
) (domain.Operation, error) {
	input.Reference.Kind = strings.ToLower(strings.TrimSpace(input.Reference.Kind))
	input.ResourceVersion = strings.TrimSpace(input.ResourceVersion)
	if input.Replicas != nil {
		replicas := *input.Replicas
		input.Replicas = &replicas
	}
	if err := domain.ValidateWorkloadOperationInput(kind, input); err != nil {
		return domain.Operation{}, err
	}
	cluster, err := s.store.GetCluster(ctx, input.ClusterID)
	if err != nil {
		return domain.Operation{}, err
	}
	if cluster.Status != domain.ClusterConnected && cluster.Status != domain.ClusterDegraded {
		return domain.Operation{}, domain.ErrInvalidState
	}
	if cluster.Environment == domain.EnvironmentProduction && input.Confirmation != cluster.Name {
		return domain.Operation{}, domain.Invalid("confirmation", "must match the production cluster name")
	}
	input.Confirmation = ""
	id, err := s.newID("op")
	if err != nil {
		return domain.Operation{}, fmt.Errorf("create operation ID: %w", err)
	}
	now := s.now()
	operation := domain.Operation{
		ID: id, RequestID: requestID, Kind: kind, State: domain.OperationQueued,
		ClusterID: input.ClusterID, Namespace: input.Reference.Namespace, Target: input.Reference.Name,
		SubmittedBy: actor, Summary: workloadOperationSummary(kind, input), CreatedAt: now, UpdatedAt: now,
	}
	return s.createAndEnqueueOperation(ctx, operation, operationJob{operationID: id, workloadInput: &input})
}

func (s *Service) PreviewWorkloadImage(
	ctx context.Context,
	actor string,
	requestID string,
	input domain.WorkloadImageOperationInput,
) (domain.WorkloadImagePreview, error) {
	input = normalizeWorkloadImageInput(input)
	if err := domain.ValidateWorkloadImageOperationInput(input); err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	cluster, err := s.store.GetCluster(ctx, input.ClusterID)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	if cluster.Status != domain.ClusterConnected && cluster.Status != domain.ClusterDegraded {
		return domain.WorkloadImagePreview{}, domain.ErrInvalidState
	}
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	defer release()
	connection, err := s.clusterConnection(cluster)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	gateway, err := s.kubeFactory.New(connection)
	if err != nil {
		return domain.WorkloadImagePreview{}, err
	}
	preview, previewErr := gateway.PreviewWorkloadImage(ctx, input.Change)
	result := "succeeded"
	summary := workloadImageSummary(input.Change)
	if previewErr != nil {
		result = "failed"
		summary = "image preview failed"
	}
	if err := s.audit(
		ctx, actor, requestID, "workload.image_preview", result, input.ClusterID,
		input.Change.Reference.Namespace, input.Change.Reference.Name, summary, "",
	); err != nil {
		return domain.WorkloadImagePreview{}, fmt.Errorf("write workload image preview audit: %w", err)
	}
	return preview, previewErr
}

func (s *Service) SubmitWorkloadImageUpdate(
	ctx context.Context,
	actor string,
	requestID string,
	input domain.WorkloadImageOperationInput,
) (domain.Operation, error) {
	input = normalizeWorkloadImageInput(input)
	if err := domain.ValidateWorkloadImageOperationInput(input); err != nil {
		return domain.Operation{}, err
	}
	cluster, err := s.store.GetCluster(ctx, input.ClusterID)
	if err != nil {
		return domain.Operation{}, err
	}
	if cluster.Status != domain.ClusterConnected && cluster.Status != domain.ClusterDegraded {
		return domain.Operation{}, domain.ErrInvalidState
	}
	if cluster.Environment == domain.EnvironmentProduction && input.Confirmation != cluster.Name {
		return domain.Operation{}, domain.Invalid("confirmation", "must match the production cluster name")
	}
	input.Confirmation = ""
	id, err := s.newID("op")
	if err != nil {
		return domain.Operation{}, fmt.Errorf("create operation ID: %w", err)
	}
	now := s.now()
	operation := domain.Operation{
		ID: id, RequestID: requestID, Kind: domain.OperationWorkloadImage, State: domain.OperationQueued,
		ClusterID: input.ClusterID, Namespace: input.Change.Reference.Namespace, Target: input.Change.Reference.Name,
		SubmittedBy: actor, Summary: workloadImageSummary(input.Change), CreatedAt: now, UpdatedAt: now,
	}
	return s.createAndEnqueueOperation(ctx, operation, operationJob{operationID: id, workloadImageInput: &input})
}

func (s *Service) CancelOperation(
	ctx context.Context,
	actor string,
	requestID string,
	operationID string,
) (domain.Operation, error) {
	if err := domain.ValidateOperationID(operationID); err != nil {
		return domain.Operation{}, err
	}
	operation, err := s.store.GetOperation(ctx, operationID)
	if err != nil {
		return domain.Operation{}, err
	}
	if operation.State != domain.OperationQueued {
		return domain.Operation{}, domain.ErrInvalidState
	}
	auditID, err := s.newID("audit")
	if err != nil {
		return domain.Operation{}, fmt.Errorf("create cancellation audit ID: %w", err)
	}
	now := s.now()
	operation.State = domain.OperationCanceled
	operation.ErrorCode = ""
	operation.ErrorMessage = ""
	operation.FinishedAt = now
	operation.UpdatedAt = now
	audit := domain.AuditEvent{
		ID: auditID, RequestID: requestID, OperationID: operation.ID, Actor: actor,
		Action: "operation.cancel", Result: "succeeded", ClusterID: operation.ClusterID,
		Namespace: operation.Namespace, Target: operation.Target, Summary: "kind=" + string(operation.Kind), CreatedAt: now,
	}
	if err := s.store.TransitionOperation(ctx, domain.OperationQueued, operation, &audit); err != nil {
		return domain.Operation{}, err
	}
	s.signalOperationCancellation(operation.ID)
	return operation, nil
}

func (s *Service) createAndEnqueueOperation(
	ctx context.Context,
	operation domain.Operation,
	job operationJob,
) (domain.Operation, error) {
	control := s.registerOperationControl(operation.ID)
	job.control = control
	if err := s.store.CreateOperation(ctx, operation); err != nil {
		s.releaseOperationControl(operation.ID, control)
		return domain.Operation{}, err
	}
	if err := s.audit(
		ctx, operation.SubmittedBy, operation.RequestID, string(operation.Kind), "submitted",
		operation.ClusterID, operation.Namespace, operation.Target, operation.Summary, operation.ID,
	); err != nil {
		operation.State = domain.OperationFailed
		operation.ErrorCode = "audit_failed"
		operation.ErrorMessage = "操作审计写入失败，任务未执行"
		operation.FinishedAt = s.now()
		operation.UpdatedAt = operation.FinishedAt
		_ = s.store.TransitionOperation(
			context.WithoutCancel(ctx), domain.OperationQueued, operation, nil,
		)
		s.releaseOperationControl(operation.ID, control)
		return domain.Operation{}, fmt.Errorf("write operation audit: %w", err)
	}
	select {
	case s.queue <- job:
		return operation, nil
	default:
		s.releaseOperationControl(operation.ID, control)
		operation.State = domain.OperationFailed
		operation.ErrorCode = "queue_full"
		operation.ErrorMessage = "操作队列已满，请稍后重试"
		operation.FinishedAt = s.now()
		operation.UpdatedAt = operation.FinishedAt
		if err := s.store.TransitionOperation(
			context.WithoutCancel(ctx), domain.OperationQueued, operation, nil,
		); err != nil {
			return domain.Operation{}, err
		}
		_ = s.audit(
			context.WithoutCancel(ctx), operation.SubmittedBy, operation.RequestID, string(operation.Kind), "failed",
			operation.ClusterID, operation.Namespace, operation.Target, operation.ErrorCode, operation.ID,
		)
		return domain.Operation{}, fmt.Errorf("operation queue is full: %w", domain.ErrBusy)
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
	executionContext, finish := s.operationExecutionContext(ctx, job)
	defer finish()

	operation, err := s.store.GetOperation(executionContext, job.operationID)
	if err != nil {
		return
	}
	if operation.State != domain.OperationQueued {
		return
	}
	releaseTarget, err := s.acquireTargetLock(
		executionContext, operation.ClusterID, operation.Namespace, operation.Target,
	)
	if err != nil {
		return
	}
	defer releaseTarget()
	_, release, err := s.operationGovernor.Acquire(executionContext)
	if err != nil {
		return
	}
	defer release()

	operation.State = domain.OperationRunning
	operation.StartedAt = s.now()
	operation.UpdatedAt = operation.StartedAt
	if err := s.store.TransitionOperation(
		executionContext, domain.OperationQueued, operation, nil,
	); err != nil {
		return
	}
	err = s.executeOperationJob(executionContext, operation, job)
	now := s.now()
	operation.FinishedAt = now
	operation.UpdatedAt = now
	result := "succeeded"
	if err == nil {
		operation.State = domain.OperationSucceeded
	} else if errors.Is(err, context.Canceled) && ctx.Err() != nil {
		operation.State = domain.OperationUnknown
		operation.ErrorCode = "operation_interrupted"
		operation.ErrorMessage = "操作执行被服务关闭中断，结果需要人工确认"
		result = "unknown"
	} else {
		operation.State = domain.OperationFailed
		operation.ErrorCode = operationErrorCode(operation.Kind, err)
		operation.ErrorMessage = operationFailureMessage(operation.Kind)
		result = "failed"
	}
	auditErr := s.audit(
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
	if auditErr != nil {
		operation.State = domain.OperationFailed
		operation.ErrorCode = "audit_failed"
		operation.ErrorMessage = "操作结果审计写入失败，请人工确认目标状态"
	}
	_ = s.store.UpdateOperation(context.WithoutCancel(ctx), operation)
}

func (s *Service) registerOperationControl(operationID string) *operationControl {
	controlContext, cancel := context.WithCancel(context.Background())
	control := &operationControl{ctx: controlContext, cancel: cancel}
	s.operationControlsMu.Lock()
	previous := s.operationControls[operationID]
	s.operationControls[operationID] = control
	s.operationControlsMu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	return control
}

func (s *Service) signalOperationCancellation(operationID string) {
	s.operationControlsMu.Lock()
	control := s.operationControls[operationID]
	delete(s.operationControls, operationID)
	s.operationControlsMu.Unlock()
	if control != nil {
		control.cancel()
	}
}

func (s *Service) releaseOperationControl(operationID string, control *operationControl) {
	if control == nil {
		return
	}
	control.cancel()
	s.operationControlsMu.Lock()
	if s.operationControls[operationID] == control {
		delete(s.operationControls, operationID)
	}
	s.operationControlsMu.Unlock()
}

func (s *Service) cancelAllOperationControls() {
	s.operationControlsMu.Lock()
	controls := make([]*operationControl, 0, len(s.operationControls))
	for operationID, control := range s.operationControls {
		controls = append(controls, control)
		delete(s.operationControls, operationID)
	}
	s.operationControlsMu.Unlock()
	for _, control := range controls {
		control.cancel()
	}
}

func (s *Service) operationExecutionContext(
	parent context.Context,
	job operationJob,
) (context.Context, func()) {
	if job.control == nil {
		return parent, func() {}
	}
	executionContext, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(job.control.ctx, cancel)
	return executionContext, func() {
		stop()
		cancel()
		s.releaseOperationControl(job.operationID, job.control)
	}
}

func (s *Service) executeOperationJob(ctx context.Context, operation domain.Operation, job operationJob) error {
	if job.helmInput != nil {
		request, err := s.helmRequest(ctx, *job.helmInput)
		if err != nil {
			return err
		}
		return s.helm.Execute(ctx, operation.Kind, request)
	}
	if job.workloadImageInput != nil {
		gateway, err := s.kubeGateway(ctx, job.workloadImageInput.ClusterID)
		if err != nil {
			return err
		}
		_, err = gateway.UpdateWorkloadImage(ctx, job.workloadImageInput.Change)
		return err
	}
	if job.workloadInput == nil {
		return errors.New("operation job has no input")
	}
	gateway, err := s.kubeGateway(ctx, job.workloadInput.ClusterID)
	if err != nil {
		return err
	}
	switch operation.Kind {
	case domain.OperationWorkloadScale:
		if job.workloadInput.Replicas == nil {
			return domain.Invalid("replicas", "is required")
		}
		_, err = gateway.ScaleWorkload(
			ctx, job.workloadInput.Reference, job.workloadInput.ResourceVersion, *job.workloadInput.Replicas,
		)
	case domain.OperationWorkloadRestart:
		_, err = gateway.RestartWorkload(
			ctx, job.workloadInput.Reference, job.workloadInput.ResourceVersion, s.now(),
		)
	default:
		err = domain.Invalid("kind", "unsupported workload operation")
	}
	return err
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
	release, err := s.acquireKubernetesRead(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
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

func (s *Service) OperationCapacity() OperationCapacity {
	readSnapshot := s.readGovernor.Snapshot()
	return OperationCapacity{
		Snapshot:      s.operationGovernor.Snapshot(),
		QueueDepth:    len(s.queue),
		QueueCapacity: cap(s.queue),
		KubernetesReads: KubernetesReadCapacity{
			Adaptive: readSnapshot.Adaptive,
			Pressure: readSnapshot.Pressure,
			Active:   readSnapshot.ActiveOperations,
			Limit:    readSnapshot.OperationLimit,
			Maximum:  readSnapshot.MaximumOperations,
		},
	}
}

func (s *Service) acquireTargetLock(
	ctx context.Context,
	clusterID, namespace, name string,
) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	lock := s.targetLock(clusterID, namespace, name)
	select {
	case lock <- struct{}{}:
		return func() { <-lock }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *Service) targetLock(clusterID, namespace, name string) chan struct{} {
	value := clusterID + "\x00" + namespace + "\x00" + name
	var hash uint64 = 14695981039346656037
	for index := range len(value) {
		hash ^= uint64(value[index])
		hash *= 1099511628211
	}
	return s.targetLocks[hash%targetLockStripes]
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

func workloadOperationSummary(kind domain.OperationKind, input domain.WorkloadOperationInput) string {
	if kind == domain.OperationWorkloadScale && input.Replicas != nil {
		return fmt.Sprintf("replicas=%d, resource_version=%s", *input.Replicas, input.ResourceVersion)
	}
	return "rolling restart, resource_version=" + input.ResourceVersion
}

func workloadImageSummary(change domain.WorkloadImageChange) string {
	return fmt.Sprintf("container=%s, fields=1, resource_version=%s", change.Container, change.ResourceVersion)
}

func normalizeWorkloadImageInput(input domain.WorkloadImageOperationInput) domain.WorkloadImageOperationInput {
	input.ClusterID = strings.TrimSpace(input.ClusterID)
	input.Change.Reference.Kind = strings.ToLower(strings.TrimSpace(input.Change.Reference.Kind))
	input.Change.ResourceVersion = strings.TrimSpace(input.Change.ResourceVersion)
	input.Change.Container = strings.TrimSpace(input.Change.Container)
	return input
}

func isWorkloadOperation(kind domain.OperationKind) bool {
	return kind == domain.OperationWorkloadScale || kind == domain.OperationWorkloadRestart ||
		kind == domain.OperationWorkloadImage
}

func operationErrorCode(kind domain.OperationKind, err error) string {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		return "permission_denied"
	case errors.Is(err, domain.ErrUnauthorized):
		return "credentials_rejected"
	case errors.Is(err, domain.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return "upstream_timeout"
	case errors.Is(err, domain.ErrConflict):
		if isWorkloadOperation(kind) {
			return "resource_version_conflict"
		}
		return "release_conflict"
	case errors.Is(err, domain.ErrNotFound):
		return "target_not_found"
	default:
		if isWorkloadOperation(kind) {
			return "workload_operation_failed"
		}
		return "helm_operation_failed"
	}
}

func operationFailureMessage(kind domain.OperationKind) string {
	if isWorkloadOperation(kind) {
		return "工作负载操作执行失败，请刷新资源并检查错误码"
	}
	return "Helm 操作执行失败，请查看错误码和目标集群状态"
}
