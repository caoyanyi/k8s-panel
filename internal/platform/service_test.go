package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
	"github.com/caoyanyi/k8s-panel/internal/secure"
	"github.com/caoyanyi/k8s-panel/internal/store"
)

func TestServiceCreatesEncryptedClusterAndReadsNamespaces(t *testing.T) {
	t.Parallel()

	service, fileStore, dataPath := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{
			probe:      domain.ClusterProbe{Version: "v1.36.2", NamespaceCount: 2, NodeCount: 3},
			namespaces: []domain.Namespace{{Name: "default"}, {Name: "payments"}},
		},
	})
	input := domain.ClusterInput{
		Name:        "production-east",
		Environment: domain.EnvironmentProduction,
		Server:      "https://api.example.com:6443",
		CACert:      "test-ca",
		BearerToken: "plain-service-account-token",
	}
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", input)
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if created.Status != domain.ClusterConnected || created.Version != "v1.36.2" {
		t.Errorf("created cluster = %#v", created)
	}
	if created.CredentialsConfigured != true {
		t.Error("CredentialsConfigured = false")
	}

	stored, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	if stored.BearerTokenCiphertext == input.BearerToken || stored.CACertCiphertext == input.CACert {
		t.Fatal("cluster credentials were stored as plaintext")
	}
	contents, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(contents), input.BearerToken) || strings.Contains(string(contents), input.CACert) {
		t.Fatal("store file contains plaintext credentials")
	}

	namespaces, err := service.Namespaces(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("Namespaces() error = %v", err)
	}
	if len(namespaces) != 2 || namespaces[1].Name != "payments" {
		t.Errorf("Namespaces() = %#v", namespaces)
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(audits) < 2 || audits[0].RequestID != "req_create" {
		t.Errorf("audit events = %#v", audits)
	}
}

func TestServicePersistsUnreachableProbeWithoutLeakingError(t *testing.T) {
	t.Parallel()

	service, fileStore, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probeErr: fmt.Errorf("token=secret: %w", domain.ErrUpstream)},
	})
	created, err := service.CreateCluster(context.Background(), "admin", "req_failure", domain.ClusterInput{
		Name:        "staging",
		Environment: domain.EnvironmentStaging,
		Server:      "https://api.example.com",
		BearerToken: "secret",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if created.Status != domain.ClusterUnreachable || created.LastErrorCode != "upstream_unavailable" {
		t.Errorf("created cluster = %#v", created)
	}
	stored, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	if strings.Contains(stored.LastErrorCode, "secret") {
		t.Errorf("LastErrorCode leaked secret: %q", stored.LastErrorCode)
	}
}

func TestServiceReadsWorkloadDiagnosticsAndAuditsPodLogs(t *testing.T) {
	t.Parallel()

	reference := domain.WorkloadReference{Kind: "pod", Namespace: "payments", Name: "gateway-0"}
	logRequest := domain.PodLogRequest{Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 200, Timestamps: true}
	gateway := &fakeKubeGateway{
		probe:  domain.ClusterProbe{Version: "v1.36.2"},
		detail: domain.WorkloadDetail{Workload: domain.Workload{Kind: "Pod", Namespace: "payments", Name: "gateway-0"}, UID: "uid-1"},
		events: []domain.KubernetesEvent{{Type: "Warning", Reason: "BackOff"}},
		logs:   domain.PodLogs{Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 200, Content: "sensitive application output"},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	detail, err := service.WorkloadDetail(context.Background(), cluster.ID, reference)
	if err != nil || detail.UID != "uid-1" {
		t.Fatalf("WorkloadDetail() = %#v, %v", detail, err)
	}
	events, err := service.WorkloadEvents(context.Background(), cluster.ID, reference, 20)
	if err != nil || len(events) != 1 || events[0].Reason != "BackOff" {
		t.Fatalf("WorkloadEvents() = %#v, %v", events, err)
	}
	logs, err := service.PodLogs(context.Background(), "admin", "req_logs", cluster.ID, logRequest)
	if err != nil || logs.Content != "sensitive application output" {
		t.Fatalf("PodLogs() = %#v, %v", logs, err)
	}
	if gateway.detailReference != reference || gateway.eventLimit != 20 || gateway.logRequest != logRequest {
		t.Errorf("gateway calls = %#v, %d, %#v", gateway.detailReference, gateway.eventLimit, gateway.logRequest)
	}

	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	found := false
	for _, event := range audits {
		if event.Action == "pod.logs.read" {
			found = true
			if event.RequestID != "req_logs" || event.Target != "gateway-0/app" || strings.Contains(event.Summary, logs.Content) {
				t.Errorf("pod log audit = %#v", event)
			}
		}
	}
	if !found {
		t.Fatal("pod.logs.read audit event was not written")
	}
}

func TestServiceReadsNodeDiagnostics(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe:      domain.ClusterProbe{Version: "v1.36.2"},
		nodes:      []domain.Node{{Name: "worker-01", Status: "Ready"}},
		nodeDetail: domain.NodeDetail{Node: domain.Node{Name: "worker-01", Status: "Ready"}, UID: "uid-worker-01"},
		nodeEvents: []domain.KubernetesEvent{{Type: "Warning", Reason: "NodeNotReady"}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	nodes, err := service.Nodes(context.Background(), cluster.ID)
	if err != nil || len(nodes) != 1 || nodes[0].Name != "worker-01" {
		t.Fatalf("Nodes() = %#v, %v", nodes, err)
	}
	detail, err := service.NodeDetail(context.Background(), cluster.ID, "worker-01")
	if err != nil || detail.UID != "uid-worker-01" {
		t.Fatalf("NodeDetail() = %#v, %v", detail, err)
	}
	events, err := service.NodeEvents(context.Background(), cluster.ID, "worker-01", 25)
	if err != nil || len(events) != 1 || events[0].Reason != "NodeNotReady" {
		t.Fatalf("NodeEvents() = %#v, %v", events, err)
	}
	if gateway.nodeName != "worker-01" || gateway.nodeEventLimit != 25 {
		t.Errorf("gateway calls = %q, %d", gateway.nodeName, gateway.nodeEventLimit)
	}

	if _, err := service.NodeDetail(context.Background(), cluster.ID, "../nodes"); err == nil {
		t.Fatal("NodeDetail() accepted an invalid node name")
	}
	if _, err := service.NodeEvents(context.Background(), cluster.ID, "worker-01", domain.MaxNodeEventLimit+1); err == nil {
		t.Fatal("NodeEvents() accepted an excessive limit")
	}
}

func TestServiceSerializesHelmOperationsForSameRelease(t *testing.T) {
	t.Parallel()

	started := make(chan string, 2)
	release := make(chan struct{}, 2)
	helm := &blockingHelmGateway{started: started, release: release}
	service, _, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}},
		helm: helm,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 2)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	repository, err := service.CreateRepository(context.Background(), "admin", "req_repo", domain.RepositoryInput{
		Name: "stable", URL: "https://charts.example.com",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	operationInput := domain.HelmOperationInput{
		ClusterID: cluster.ID, Namespace: "payments", ReleaseName: "gateway", Chart: "gateway", RepositoryID: repository.ID,
	}
	first, err := service.SubmitHelmOperation(context.Background(), "admin", "req_first", domain.OperationHelmInstall, operationInput)
	if err != nil {
		t.Fatalf("SubmitHelmOperation(first) error = %v", err)
	}
	second, err := service.SubmitHelmOperation(context.Background(), "admin", "req_second", domain.OperationHelmUpgrade, operationInput)
	if err != nil {
		t.Fatalf("SubmitHelmOperation(second) error = %v", err)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first Helm operation did not start")
	}
	select {
	case name := <-started:
		t.Fatalf("second operation for %q started concurrently", name)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("second Helm operation did not start after lock release")
	}
	release <- struct{}{}

	waitForOperationState(t, service, first.ID, domain.OperationSucceeded)
	waitForOperationState(t, service, second.ID, domain.OperationSucceeded)
	if max := helm.maxActive.Load(); max != 1 {
		t.Errorf("max concurrent calls = %d, want 1", max)
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(audits) < 8 {
		t.Errorf("audit event count = %d, want at least 8", len(audits))
	}
}

func TestServiceExecutesControlledWorkloadOperations(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 2)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	replicas := int32(5)
	scaleInput := domain.WorkloadOperationInput{
		ClusterID:       cluster.ID,
		Reference:       domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42",
		Replicas:        &replicas,
	}
	if _, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_rejected", domain.OperationWorkloadScale, scaleInput,
	); err == nil {
		t.Fatal("production scale without confirmation succeeded")
	} else {
		var validationErr *domain.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "confirmation" {
			t.Fatalf("confirmation error = %v", err)
		}
	}

	scaleInput.Confirmation = "production-east"
	scale, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_scale", domain.OperationWorkloadScale, scaleInput,
	)
	if err != nil {
		t.Fatalf("SubmitWorkloadOperation(scale) error = %v", err)
	}
	restart, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_restart", domain.OperationWorkloadRestart, domain.WorkloadOperationInput{
			ClusterID: cluster.ID, Reference: scaleInput.Reference, ResourceVersion: "43", Confirmation: "production-east",
		},
	)
	if err != nil {
		t.Fatalf("SubmitWorkloadOperation(restart) error = %v", err)
	}
	waitForOperationState(t, service, scale.ID, domain.OperationSucceeded)
	waitForOperationState(t, service, restart.ID, domain.OperationSucceeded)

	gateway.mutationMu.Lock()
	defer gateway.mutationMu.Unlock()
	if gateway.scaledVersion != "42" || gateway.scaledReplicas != 5 {
		t.Errorf("scale call = version %q, replicas %d", gateway.scaledVersion, gateway.scaledReplicas)
	}
	if gateway.restartedVersion != "43" || gateway.restartedAt.IsZero() {
		t.Errorf("restart call = version %q, time %v", gateway.restartedVersion, gateway.restartedAt)
	}
	if strings.Contains(scale.Summary, "production-east") || scale.Kind != domain.OperationWorkloadScale {
		t.Errorf("scale operation = %#v", scale)
	}
}

func TestServicePreviewsAndExecutesControlledImageUpdate(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 1)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	input := domain.WorkloadImageOperationInput{
		ClusterID: cluster.ID,
		Change: domain.WorkloadImageChange{
			Reference:       domain.WorkloadReference{Kind: "Deployment", Namespace: "payments", Name: "gateway"},
			ResourceVersion: "42",
			Container:       "app",
			CurrentImage:    "registry.example.com/gateway:1.4.0",
			Image:           "registry.example.com/gateway:1.5.0",
		},
	}
	preview, err := service.PreviewWorkloadImage(context.Background(), "admin", "req_preview", input)
	if err != nil {
		t.Fatalf("PreviewWorkloadImage() error = %v", err)
	}
	if preview.Container != "app" || len(preview.Changes) != 1 {
		t.Errorf("preview = %#v", preview)
	}
	if _, err := service.SubmitWorkloadImageUpdate(context.Background(), "admin", "req_rejected", input); err == nil {
		t.Fatal("production image update without confirmation succeeded")
	} else {
		var validationErr *domain.ValidationError
		if !errors.As(err, &validationErr) || validationErr.Field != "confirmation" {
			t.Fatalf("confirmation error = %v", err)
		}
	}
	input.Confirmation = "production-east"
	operation, err := service.SubmitWorkloadImageUpdate(context.Background(), "admin", "req_image", input)
	if err != nil {
		t.Fatalf("SubmitWorkloadImageUpdate() error = %v", err)
	}
	waitForOperationState(t, service, operation.ID, domain.OperationSucceeded)

	gateway.mutationMu.Lock()
	previewed := gateway.previewedImage
	updated := gateway.updatedImage
	gateway.mutationMu.Unlock()
	if previewed.Container != "app" || updated.Image != input.Change.Image || updated.ResourceVersion != "42" {
		t.Errorf("previewed = %#v, updated = %#v", previewed, updated)
	}
	if operation.Kind != domain.OperationWorkloadImage || strings.Contains(operation.Summary, input.Change.CurrentImage) ||
		strings.Contains(operation.Summary, input.Change.Image) {
		t.Errorf("operation = %#v", operation)
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	var foundPreview bool
	for _, audit := range audits {
		if audit.Action == "workload.image_preview" && audit.Result == "succeeded" {
			foundPreview = true
			if strings.Contains(audit.Summary, input.Change.Image) {
				t.Errorf("preview audit leaked image: %#v", audit)
			}
		}
	}
	if !foundPreview {
		t.Error("successful image preview audit not found")
	}
}

func TestServiceDefersImageUpdateDuringCriticalResourcePressure(t *testing.T) {
	t.Parallel()

	critical := 0.97
	sampler := &mutableResourceSampler{sample: resourceguard.Sample{MemoryRatio: &critical}}
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: true, MaxConcurrent: 2, HighWatermark: 0.80, CriticalWatermark: 0.95,
		RetryInterval: time.Millisecond, Sampler: sampler,
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, governor: governor})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 1)
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	input := domain.WorkloadImageOperationInput{
		ClusterID: cluster.ID,
		Change: domain.WorkloadImageChange{
			Reference: domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"}, ResourceVersion: "42",
			Container: "app", CurrentImage: "gateway:1.4.0", Image: "gateway:1.5.0",
		},
	}
	if _, err := service.PreviewWorkloadImage(context.Background(), "admin", "req_preview", input); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("PreviewWorkloadImage() under critical pressure error = %v", err)
	}
	operation, err := service.SubmitWorkloadImageUpdate(context.Background(), "admin", "req_image", input)
	if err != nil {
		t.Fatalf("SubmitWorkloadImageUpdate() error = %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	queued, err := service.GetOperation(context.Background(), operation.ID)
	if err != nil || queued.State != domain.OperationQueued {
		t.Fatalf("operation under pressure = %#v, error = %v", queued, err)
	}
	gateway.mutationMu.Lock()
	calledUnderPressure := gateway.updatedImage.Container != ""
	gateway.mutationMu.Unlock()
	if calledUnderPressure {
		t.Fatal("image update ran during critical pressure")
	}
	normal := 0.20
	sampler.mu.Lock()
	sampler.sample = resourceguard.Sample{MemoryRatio: &normal}
	sampler.mu.Unlock()
	waitForOperationState(t, service, operation.ID, domain.OperationSucceeded)
}

func TestServiceDefersOperationsDuringCriticalResourcePressure(t *testing.T) {
	t.Parallel()

	critical := 0.97
	sampler := &mutableResourceSampler{sample: resourceguard.Sample{MemoryRatio: &critical}}
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: true, MaxConcurrent: 2, HighWatermark: 0.80, CriticalWatermark: 0.95,
		RetryInterval: time.Millisecond, Sampler: sampler,
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	started := make(chan string, 1)
	release := make(chan struct{}, 1)
	helm := &blockingHelmGateway{started: started, release: release}
	service, _, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}, helm: helm, governor: governor,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 2)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	repository, err := service.CreateRepository(context.Background(), "admin", "req_repo", domain.RepositoryInput{
		Name: "stable", URL: "https://charts.example.com",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	operation, err := service.SubmitHelmOperation(context.Background(), "admin", "req_install", domain.OperationHelmInstall, domain.HelmOperationInput{
		ClusterID: cluster.ID, Namespace: "payments", ReleaseName: "gateway", Chart: "gateway", RepositoryID: repository.ID,
	})
	if err != nil {
		t.Fatalf("SubmitHelmOperation() error = %v", err)
	}
	select {
	case target := <-started:
		t.Fatalf("operation %q started under critical resource pressure", target)
	case <-time.After(30 * time.Millisecond):
	}
	queued, err := service.GetOperation(context.Background(), operation.ID)
	if err != nil || queued.State != domain.OperationQueued {
		t.Fatalf("queued operation = %#v, %v", queued, err)
	}
	capacity := service.OperationCapacity()
	if capacity.Pressure != resourceguard.PressureCritical || capacity.OperationLimit != 0 {
		t.Fatalf("critical capacity = %#v", capacity)
	}

	normal := 0.20
	sampler.Set(resourceguard.Sample{MemoryRatio: &normal})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start after resource pressure recovered")
	}
	release <- struct{}{}
	waitForOperationState(t, service, operation.ID, domain.OperationSucceeded)
}

func TestServiceCancelsQueuedOperationAndInterruptsResourceWait(t *testing.T) {
	t.Parallel()

	governor := &cancelAwareGovernor{
		started:  make(chan struct{}, 1),
		canceled: make(chan struct{}, 1),
	}
	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, governor: governor})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 1)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	replicas := int32(2)
	operation, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_scale", domain.OperationWorkloadScale, domain.WorkloadOperationInput{
			ClusterID: cluster.ID, Reference: domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
			ResourceVersion: "42", Replicas: &replicas,
		},
	)
	if err != nil {
		t.Fatalf("SubmitWorkloadOperation() error = %v", err)
	}
	select {
	case <-governor.started:
	case <-time.After(time.Second):
		t.Fatal("operation did not start waiting for a resource slot")
	}

	canceled, err := service.CancelOperation(context.Background(), "admin", "req_cancel", operation.ID)
	if err != nil {
		t.Fatalf("CancelOperation() error = %v", err)
	}
	if canceled.State != domain.OperationCanceled || canceled.FinishedAt.IsZero() || canceled.ErrorCode != "" {
		t.Fatalf("canceled operation = %#v", canceled)
	}
	select {
	case <-governor.canceled:
	case <-time.After(time.Second):
		t.Fatal("resource wait was not interrupted after cancellation")
	}
	if _, err := service.CancelOperation(context.Background(), "admin", "req_duplicate", operation.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("duplicate CancelOperation() error = %v, want ErrInvalidState", err)
	}
	gateway.mutationMu.Lock()
	mutated := gateway.scaledVersion != ""
	gateway.mutationMu.Unlock()
	if mutated {
		t.Fatal("canceled workload operation reached Kubernetes")
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	cancelAudits := 0
	for _, audit := range audits {
		if audit.Action == "operation.cancel" && audit.Result == "succeeded" && audit.OperationID == operation.ID {
			cancelAudits++
		}
	}
	if cancelAudits != 1 {
		t.Fatalf("successful cancellation audits = %d, want 1", cancelAudits)
	}
}

func TestServiceCancelsOperationWaitingForTargetLock(t *testing.T) {
	t.Parallel()

	started := make(chan string, 1)
	release := make(chan struct{}, 1)
	helm := &blockingHelmGateway{started: started, release: release}
	service, _, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}, helm: helm,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	repository, err := service.CreateRepository(context.Background(), "admin", "req_repo", domain.RepositoryInput{
		Name: "stable", URL: "https://charts.example.com",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	releaseTarget, err := service.acquireTargetLock(context.Background(), cluster.ID, "payments", "gateway")
	if err != nil {
		t.Fatalf("acquireTargetLock() error = %v", err)
	}
	t.Cleanup(releaseTarget)
	go service.Run(ctx, 1)

	submit := func(requestID, releaseName string) domain.Operation {
		t.Helper()
		operation, submitErr := service.SubmitHelmOperation(
			context.Background(), "admin", requestID, domain.OperationHelmInstall, domain.HelmOperationInput{
				ClusterID: cluster.ID, Namespace: "payments", ReleaseName: releaseName,
				Chart: "gateway", RepositoryID: repository.ID,
			},
		)
		if submitErr != nil {
			t.Fatalf("SubmitHelmOperation(%s) error = %v", releaseName, submitErr)
		}
		return operation
	}

	blocked := submit("req_blocked", "gateway")
	deadline := time.Now().Add(time.Second)
	for service.OperationCapacity().QueueDepth != 0 {
		if time.Now().After(deadline) {
			t.Fatal("blocked operation was not claimed by a worker")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := service.CancelOperation(context.Background(), "admin", "req_cancel", blocked.ID); err != nil {
		t.Fatalf("CancelOperation() error = %v", err)
	}

	otherTarget := "worker"
	for service.targetLock(cluster.ID, "payments", otherTarget) == service.targetLock(cluster.ID, "payments", "gateway") {
		otherTarget += "-x"
	}
	next := submit("req_next", otherTarget)
	select {
	case name := <-started:
		if name != otherTarget {
			t.Fatalf("next started release = %q, want %q", name, otherTarget)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled target-lock wait did not release its worker")
	}
	release <- struct{}{}
	waitForOperationState(t, service, next.ID, domain.OperationSucceeded)
}

func TestServiceTargetLockHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	service, _, _ := newTestService(t, serviceFakes{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.acquireTargetLock(ctx, "clu_1", "payments", "gateway"); !errors.Is(err, context.Canceled) {
		t.Fatalf("acquireTargetLock() error = %v, want context canceled", err)
	}
}

func TestServiceRejectsCancellationAfterOperationStarts(t *testing.T) {
	t.Parallel()

	started := make(chan string, 1)
	release := make(chan struct{}, 1)
	helm := &blockingHelmGateway{started: started, release: release}
	service, _, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}, helm: helm,
	})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go service.Run(ctx, 1)

	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	repository, err := service.CreateRepository(context.Background(), "admin", "req_repo", domain.RepositoryInput{
		Name: "stable", URL: "https://charts.example.com",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	operation, err := service.SubmitHelmOperation(context.Background(), "admin", "req_install", domain.OperationHelmInstall, domain.HelmOperationInput{
		ClusterID: cluster.ID, Namespace: "payments", ReleaseName: "gateway", Chart: "gateway", RepositoryID: repository.ID,
	})
	if err != nil {
		t.Fatalf("SubmitHelmOperation() error = %v", err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Helm operation did not start")
	}
	if _, err := service.CancelOperation(context.Background(), "admin", "req_cancel", operation.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("CancelOperation() error = %v, want ErrInvalidState", err)
	}
	release <- struct{}{}
	waitForOperationState(t, service, operation.ID, domain.OperationSucceeded)
}

func TestServiceRejectsOperationsWhenBoundedQueueIsFull(t *testing.T) {
	t.Parallel()

	service, _, _ := newTestService(t, serviceFakes{
		kube: &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}, queueSize: 1,
	})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	replicas := int32(2)
	input := domain.WorkloadOperationInput{
		ClusterID: cluster.ID, Reference: domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"},
		ResourceVersion: "42", Replicas: &replicas,
	}
	if _, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_first", domain.OperationWorkloadScale, input,
	); err != nil {
		t.Fatalf("SubmitWorkloadOperation(first) error = %v", err)
	}
	if _, err := service.SubmitWorkloadOperation(
		context.Background(), "admin", "req_second", domain.OperationWorkloadScale, input,
	); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("SubmitWorkloadOperation(second) error = %v, want busy", err)
	}

	capacity := service.OperationCapacity()
	if capacity.QueueDepth != 1 || capacity.QueueCapacity != 1 {
		t.Fatalf("operation capacity = %#v", capacity)
	}
	operations, err := service.ListOperations(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListOperations() error = %v", err)
	}
	foundQueueFailure := false
	for _, operation := range operations {
		if operation.RequestID == "req_second" && operation.State == domain.OperationFailed && operation.ErrorCode == "queue_full" {
			foundQueueFailure = true
		}
	}
	if !foundQueueFailure {
		t.Fatalf("queue-full operation not persisted: %#v", operations)
	}
}

type serviceFakes struct {
	kube      KubeGateway
	helm      HelmGateway
	governor  OperationGovernor
	queueSize int
}

func newTestService(t *testing.T, fakes serviceFakes) (*Service, *store.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panel.json")
	now := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	fileStore, err := store.Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	cipher, err := secure.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secure.NewCipher() error = %v", err)
	}
	if fakes.kube == nil {
		fakes.kube = &fakeKubeGateway{}
	}
	if fakes.helm == nil {
		fakes.helm = &blockingHelmGateway{}
	}
	if fakes.governor == nil {
		fakes.governor = testOperationGovernor(t)
	}
	if fakes.queueSize == 0 {
		fakes.queueSize = 16
	}
	var idCounter atomic.Int64
	service, err := New(Dependencies{
		Store:              fileStore,
		Cipher:             cipher,
		TargetValidator:    allowAllValidator{},
		KubeFactory:        fakeKubeFactory{gateway: fakes.kube},
		RepositoryChecker:  successfulRepositoryChecker{},
		Helm:               fakes.helm,
		OperationGovernor:  fakes.governor,
		OperationQueueSize: fakes.queueSize,
		Clock:              func() time.Time { return now },
		NewID: func(prefix string) (string, error) {
			return fmt.Sprintf("%s_%d", prefix, idCounter.Add(1)), nil
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service, fileStore, path
}

type allowAllValidator struct{}

func (allowAllValidator) Validate(context.Context, string) error { return nil }

type fakeKubeFactory struct{ gateway KubeGateway }

func (f fakeKubeFactory) New(kubernetes.Connection) (KubeGateway, error) { return f.gateway, nil }

type fakeKubeGateway struct {
	probe            domain.ClusterProbe
	probeErr         error
	namespaces       []domain.Namespace
	detail           domain.WorkloadDetail
	events           []domain.KubernetesEvent
	logs             domain.PodLogs
	detailReference  domain.WorkloadReference
	eventLimit       int
	logRequest       domain.PodLogRequest
	nodes            []domain.Node
	nodeDetail       domain.NodeDetail
	nodeEvents       []domain.KubernetesEvent
	nodeName         string
	nodeEventLimit   int
	mutationMu       sync.Mutex
	scaledVersion    string
	scaledReplicas   int32
	restartedVersion string
	restartedAt      time.Time
	previewedImage   domain.WorkloadImageChange
	updatedImage     domain.WorkloadImageChange
}

func (g *fakeKubeGateway) Probe(context.Context) (domain.ClusterProbe, error) {
	return g.probe, g.probeErr
}
func (g *fakeKubeGateway) Summary(context.Context) (domain.ClusterSummary, error) {
	return domain.ClusterSummary{Version: g.probe.Version}, g.probeErr
}
func (g *fakeKubeGateway) Namespaces(context.Context) ([]domain.Namespace, error) {
	return append([]domain.Namespace(nil), g.namespaces...), nil
}
func (g *fakeKubeGateway) Nodes(context.Context) ([]domain.Node, error) {
	return append([]domain.Node(nil), g.nodes...), nil
}
func (g *fakeKubeGateway) NodeDetail(_ context.Context, name string) (domain.NodeDetail, error) {
	g.nodeName = name
	return g.nodeDetail, nil
}
func (g *fakeKubeGateway) NodeEvents(_ context.Context, name string, limit int) ([]domain.KubernetesEvent, error) {
	g.nodeName = name
	g.nodeEventLimit = limit
	return append([]domain.KubernetesEvent(nil), g.nodeEvents...), nil
}
func (g *fakeKubeGateway) Workloads(context.Context, string, string) ([]domain.Workload, error) {
	return nil, nil
}
func (g *fakeKubeGateway) WorkloadDetail(_ context.Context, reference domain.WorkloadReference) (domain.WorkloadDetail, error) {
	g.detailReference = reference
	return g.detail, nil
}
func (g *fakeKubeGateway) WorkloadEvents(_ context.Context, reference domain.WorkloadReference, limit int) ([]domain.KubernetesEvent, error) {
	g.detailReference = reference
	g.eventLimit = limit
	return append([]domain.KubernetesEvent(nil), g.events...), nil
}
func (g *fakeKubeGateway) PodLogs(_ context.Context, request domain.PodLogRequest) (domain.PodLogs, error) {
	g.logRequest = request
	return g.logs, nil
}

func (g *fakeKubeGateway) ScaleWorkload(_ context.Context, reference domain.WorkloadReference, resourceVersion string, replicas int32) (domain.Workload, error) {
	g.mutationMu.Lock()
	defer g.mutationMu.Unlock()
	g.detailReference = reference
	g.scaledVersion = resourceVersion
	g.scaledReplicas = replicas
	return domain.Workload{Kind: "Deployment", Namespace: reference.Namespace, Name: reference.Name, Desired: replicas}, nil
}

func (g *fakeKubeGateway) RestartWorkload(_ context.Context, reference domain.WorkloadReference, resourceVersion string, restartedAt time.Time) (domain.Workload, error) {
	g.mutationMu.Lock()
	defer g.mutationMu.Unlock()
	g.detailReference = reference
	g.restartedVersion = resourceVersion
	g.restartedAt = restartedAt
	return domain.Workload{Kind: "Deployment", Namespace: reference.Namespace, Name: reference.Name}, nil
}

func (g *fakeKubeGateway) PreviewWorkloadImage(_ context.Context, change domain.WorkloadImageChange) (domain.WorkloadImagePreview, error) {
	g.mutationMu.Lock()
	defer g.mutationMu.Unlock()
	g.previewedImage = change
	return domain.WorkloadImagePreview{
		Kind: "Deployment", Namespace: change.Reference.Namespace, Name: change.Reference.Name,
		Container: change.Container, ResourceVersion: change.ResourceVersion,
		Changes: []domain.WorkloadFieldChange{{
			Field: "spec.template.spec.containers[name=" + change.Container + "].image", Before: change.CurrentImage, After: change.Image,
		}},
	}, nil
}

func (g *fakeKubeGateway) UpdateWorkloadImage(_ context.Context, change domain.WorkloadImageChange) (domain.Workload, error) {
	g.mutationMu.Lock()
	defer g.mutationMu.Unlock()
	g.updatedImage = change
	return domain.Workload{Kind: "Deployment", Namespace: change.Reference.Namespace, Name: change.Reference.Name, Images: []string{change.Image}}, nil
}

func testOperationGovernor(t *testing.T) OperationGovernor {
	t.Helper()
	value := 0.10
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: false, MaxConcurrent: 8, HighWatermark: 0.80, CriticalWatermark: 0.95,
		RetryInterval: time.Millisecond, Sampler: staticResourceSampler{sample: resourceguard.Sample{MemoryRatio: &value}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	return governor
}

type staticResourceSampler struct{ sample resourceguard.Sample }

func (s staticResourceSampler) Sample() resourceguard.Sample { return s.sample }

type mutableResourceSampler struct {
	mu     sync.RWMutex
	sample resourceguard.Sample
}

type cancelAwareGovernor struct {
	started  chan struct{}
	canceled chan struct{}
}

func (g *cancelAwareGovernor) Acquire(ctx context.Context) (resourceguard.Snapshot, func(), error) {
	g.started <- struct{}{}
	<-ctx.Done()
	g.canceled <- struct{}{}
	return resourceguard.Snapshot{}, nil, ctx.Err()
}

func (g *cancelAwareGovernor) Snapshot() resourceguard.Snapshot {
	return resourceguard.Snapshot{
		Adaptive: true, Pressure: resourceguard.PressureCritical, OperationLimit: 0, MaximumOperations: 1,
	}
}

func (s *mutableResourceSampler) Sample() resourceguard.Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sample
}

func (s *mutableResourceSampler) Set(sample resourceguard.Sample) {
	s.mu.Lock()
	s.sample = sample
	s.mu.Unlock()
}

type successfulRepositoryChecker struct{}

func (successfulRepositoryChecker) Check(context.Context, RepositoryConnection) error { return nil }

type blockingHelmGateway struct {
	started   chan string
	release   chan struct{}
	active    atomic.Int64
	maxActive atomic.Int64
}

func (g *blockingHelmGateway) List(context.Context, kubernetes.Connection, string) ([]domain.HelmRelease, error) {
	return nil, nil
}

func (g *blockingHelmGateway) Execute(_ context.Context, _ domain.OperationKind, request HelmRequest) error {
	active := g.active.Add(1)
	defer g.active.Add(-1)
	for {
		maximum := g.maxActive.Load()
		if active <= maximum || g.maxActive.CompareAndSwap(maximum, active) {
			break
		}
	}
	if g.started == nil {
		return nil
	}
	g.started <- request.Input.ReleaseName
	<-g.release
	return nil
}

func waitForOperationState(t *testing.T, service *Service, id string, want domain.OperationState) {
	t.Helper()
	deadline := time.After(time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("operation %s did not reach state %s", id, want)
		case <-ticker.C:
			operation, err := service.GetOperation(context.Background(), id)
			if err != nil && !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("GetOperation() error = %v", err)
			}
			if operation.State == want {
				return
			}
		}
	}
}
