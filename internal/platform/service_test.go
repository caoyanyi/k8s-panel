package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/kubernetes"
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

type serviceFakes struct {
	kube KubeGateway
	helm HelmGateway
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
	var idCounter atomic.Int64
	service, err := New(Dependencies{
		Store:             fileStore,
		Cipher:            cipher,
		TargetValidator:   allowAllValidator{},
		KubeFactory:       fakeKubeFactory{gateway: fakes.kube},
		RepositoryChecker: successfulRepositoryChecker{},
		Helm:              fakes.helm,
		Clock:             func() time.Time { return now },
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
	probe           domain.ClusterProbe
	probeErr        error
	namespaces      []domain.Namespace
	detail          domain.WorkloadDetail
	events          []domain.KubernetesEvent
	logs            domain.PodLogs
	detailReference domain.WorkloadReference
	eventLimit      int
	logRequest      domain.PodLogRequest
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
