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

func TestServiceReusesAndInvalidatesKubernetesGatewayCache(t *testing.T) {
	t.Parallel()

	baseCipher, err := secure.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secure.NewCipher() error = %v", err)
	}
	countedCipher := &countingSecretCipher{delegate: baseCipher}
	first := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	second := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	third := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{first, second, third}}
	service, _, _ := newTestService(t, serviceFakes{
		cipher: countedCipher, factory: factory, cacheSize: 2, cacheTTL: 10 * time.Minute,
	})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "development", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := service.Summary(context.Background(), cluster.ID); err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if calls := factory.Calls(); calls != 1 {
		t.Fatalf("factory calls after cache hit = %d, want 1", calls)
	}
	if opens := countedCipher.openCalls.Load(); opens != 1 {
		t.Fatalf("credential decryptions after cache hit = %d, want 1", opens)
	}
	capacity := service.OperationCapacity().KubernetesClients
	if capacity.Entries != 1 || capacity.Capacity != 2 || capacity.Maximum != 2 || capacity.Building != 0 {
		t.Fatalf("Kubernetes client capacity = %#v", capacity)
	}

	if _, err := service.TestClusterConnection(context.Background(), "admin", "req_test", cluster.ID); err != nil {
		t.Fatalf("TestClusterConnection() error = %v", err)
	}
	if factory.Calls() != 2 || first.idleCloseCalls.Load() != 1 {
		t.Fatalf("factory calls = %d, first closes = %d", factory.Calls(), first.idleCloseCalls.Load())
	}
	if opens := countedCipher.openCalls.Load(); opens != 2 {
		t.Fatalf("credential decryptions after explicit test = %d, want 2", opens)
	}
	if _, err := service.SetClusterEnabled(context.Background(), "admin", "req_disable", cluster.ID, false); err != nil {
		t.Fatalf("SetClusterEnabled(false) error = %v", err)
	}
	if second.idleCloseCalls.Load() != 1 || service.OperationCapacity().KubernetesClients.Entries != 0 {
		t.Fatalf("second closes = %d, capacity = %#v", second.idleCloseCalls.Load(), service.OperationCapacity().KubernetesClients)
	}
	if _, err := service.SetClusterEnabled(context.Background(), "admin", "req_enable", cluster.ID, true); err != nil {
		t.Fatalf("SetClusterEnabled(true) error = %v", err)
	}
	if factory.Calls() != 3 {
		t.Fatalf("factory calls after enable = %d, want 3", factory.Calls())
	}
	if err := service.DeleteCluster(context.Background(), "admin", "req_delete", cluster.ID, cluster.Name); err != nil {
		t.Fatalf("DeleteCluster() error = %v", err)
	}
	if third.idleCloseCalls.Load() != 1 || service.OperationCapacity().KubernetesClients.Entries != 0 {
		t.Fatalf("third closes = %d, capacity = %#v", third.idleCloseCalls.Load(), service.OperationCapacity().KubernetesClients)
	}
}

func TestServiceRotatesClusterCredentialsAfterCandidateValidation(t *testing.T) {
	t.Parallel()

	cipher, err := secure.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("secure.NewCipher() error = %v", err)
	}
	current := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	candidate := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.3"}}
	rebuilt := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.3"}}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{current, candidate, rebuilt}}
	service, fileStore, _ := newTestService(t, serviceFakes{cipher: cipher, factory: factory})
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com",
		CACert: "old-ca", BearerToken: "old-token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	before, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster(before) error = %v", err)
	}

	rotated, err := service.RotateClusterCredentials(context.Background(), "admin", "req_rotate", created.ID, domain.ClusterCredentialRotationInput{
		CACert: "new-ca", BearerToken: "new-token", Confirmation: created.Name,
	})
	if err != nil {
		t.Fatalf("RotateClusterCredentials() error = %v", err)
	}
	if rotated.Status != domain.ClusterConnected || rotated.Version != "v1.36.3" {
		t.Fatalf("rotated cluster = %#v", rotated)
	}
	after, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster(after) error = %v", err)
	}
	if after.BearerTokenCiphertext == before.BearerTokenCiphertext || after.CACertCiphertext == before.CACertCiphertext {
		t.Fatal("credential ciphertext was not replaced")
	}
	token, err := cipher.OpenString(after.BearerTokenCiphertext, clusterAAD(created.ID, "bearer_token"))
	if err != nil || token != "new-token" {
		t.Fatalf("rotated token = %q, error = %v", token, err)
	}
	ca, err := cipher.OpenString(after.CACertCiphertext, clusterAAD(created.ID, "ca_cert"))
	if err != nil || ca != "new-ca" {
		t.Fatalf("rotated CA = %q, error = %v", ca, err)
	}
	connections := factory.Connections()
	if len(connections) != 2 || connections[1].BearerToken != "new-token" || connections[1].CACert != "new-ca" {
		t.Fatalf("candidate connections = %#v", connections)
	}
	if current.idleCloseCalls.Load() != 1 || candidate.idleCloseCalls.Load() != 1 {
		t.Fatalf("current closes = %d, candidate closes = %d", current.idleCloseCalls.Load(), candidate.idleCloseCalls.Load())
	}
	if _, err := service.Summary(context.Background(), created.ID); err != nil {
		t.Fatalf("Summary() after rotation error = %v", err)
	}
	if factory.Calls() != 3 {
		t.Fatalf("factory calls after lazy rebuild = %d, want 3", factory.Calls())
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	found := false
	for _, event := range audits {
		if event.Action != "cluster.credentials.rotate" || event.Result != "succeeded" {
			continue
		}
		found = true
		if strings.Contains(event.Summary, "new-token") || strings.Contains(event.Summary, "new-ca") {
			t.Fatalf("rotation audit leaked credentials: %#v", event)
		}
	}
	if !found {
		t.Fatalf("rotation audit not found: %#v", audits)
	}
}

func TestServiceKeepsCurrentCredentialsWhenCandidateValidationFails(t *testing.T) {
	t.Parallel()

	current := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	candidate := &fakeKubeGateway{probeErr: domain.ErrUnauthorized}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{current, candidate}}
	service, fileStore, _ := newTestService(t, serviceFakes{factory: factory})
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "old-token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	before, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster(before) error = %v", err)
	}

	_, err = service.RotateClusterCredentials(context.Background(), "admin", "req_rotate", created.ID, domain.ClusterCredentialRotationInput{
		BearerToken: "rejected-token", Confirmation: created.Name,
	})
	if !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("RotateClusterCredentials() error = %v, want unauthorized", err)
	}
	after, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil || after != before {
		t.Fatalf("cluster changed after rejected rotation: before = %#v, after = %#v, error = %v", before, after, err)
	}
	if current.idleCloseCalls.Load() != 0 || candidate.idleCloseCalls.Load() != 1 {
		t.Fatalf("current closes = %d, candidate closes = %d", current.idleCloseCalls.Load(), candidate.idleCloseCalls.Load())
	}
	if _, err := service.Summary(context.Background(), created.ID); err != nil {
		t.Fatalf("Summary() with current client error = %v", err)
	}
	if factory.Calls() != 2 {
		t.Fatalf("factory calls = %d, want cached current client", factory.Calls())
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	foundFailure := false
	for _, event := range audits {
		if event.Action == "cluster.credentials.rotate" && event.Result == "failed" {
			foundFailure = true
			if strings.Contains(event.Summary, "rejected-token") {
				t.Fatalf("failed rotation audit leaked token: %#v", event)
			}
		}
	}
	if !foundFailure {
		t.Fatalf("failed rotation audit not found: %#v", audits)
	}
}

func TestServiceRejectsCredentialRotationAndConnectionTestUnderCriticalPressure(t *testing.T) {
	t.Parallel()

	pressure := 0.97
	readGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled: true, MaxConcurrent: 4, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticResourceSampler{sample: resourceguard.Sample{MemoryRatio: &pressure}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	current := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{current}}
	service, fileStore, _ := newTestService(t, serviceFakes{factory: factory, readGovernor: readGovernor})
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "old-token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	before, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster(before) error = %v", err)
	}

	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_rotate", created.ID, domain.ClusterCredentialRotationInput{
		BearerToken: "new-token", Confirmation: created.Name,
	}); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("RotateClusterCredentials() error = %v, want busy", err)
	}
	if _, err := service.TestClusterConnection(context.Background(), "admin", "req_test", created.ID); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("TestClusterConnection() error = %v, want busy", err)
	}
	after, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil || after != before {
		t.Fatalf("cluster changed under critical pressure: before = %#v, after = %#v, error = %v", before, after, err)
	}
	if factory.Calls() != 1 || current.idleCloseCalls.Load() != 0 {
		t.Fatalf("factory calls = %d, current closes = %d", factory.Calls(), current.idleCloseCalls.Load())
	}
}

func TestServiceRejectsConcurrentCredentialRotationForSameCluster(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	current := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	candidate := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.3"}, probeStarted: started, probeRelease: release,
	}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{current, candidate}}
	service, _, _ := newTestService(t, serviceFakes{factory: factory})
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "old-token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, rotateErr := service.RotateClusterCredentials(context.Background(), "admin", "req_rotate_1", created.ID, domain.ClusterCredentialRotationInput{
			BearerToken: "new-token-1", Confirmation: created.Name,
		})
		result <- rotateErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("candidate validation did not start")
	}
	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_rotate_2", created.ID, domain.ClusterCredentialRotationInput{
		BearerToken: "new-token-2", Confirmation: created.Name,
	}); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("concurrent RotateClusterCredentials() error = %v, want busy", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("first RotateClusterCredentials() error = %v", err)
	}
	if factory.Calls() != 2 {
		t.Fatalf("factory calls = %d, want one candidate validation", factory.Calls())
	}
}

func TestServiceRejectsCredentialRotationBeforeCandidateValidation(t *testing.T) {
	t.Parallel()

	current := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	factory := &sequenceKubeFactory{gateways: []KubeGateway{current}}
	service, fileStore, _ := newTestService(t, serviceFakes{factory: factory})
	created, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "old-token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	valid := domain.ClusterCredentialRotationInput{BearerToken: "new-token", Confirmation: created.Name}
	invalid := valid
	invalid.BearerToken = "invalid token"
	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_invalid", created.ID, invalid); err == nil {
		t.Fatal("RotateClusterCredentials() accepted whitespace in the token")
	}
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.RotateClusterCredentials(canceledContext, "admin", "req_canceled", created.ID, valid); !errors.Is(err, context.Canceled) {
		t.Fatalf("RotateClusterCredentials(canceled) error = %v, want context canceled", err)
	}
	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_missing", "clu_missing", valid); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("RotateClusterCredentials(missing) error = %v, want not found", err)
	}

	stored, err := fileStore.GetCluster(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("GetCluster() error = %v", err)
	}
	stored.Status = domain.ClusterDisabled
	if err := fileStore.UpdateCluster(context.Background(), stored); err != nil {
		t.Fatalf("UpdateCluster(disabled) error = %v", err)
	}
	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_disabled", created.ID, valid); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("RotateClusterCredentials(disabled) error = %v, want invalid state", err)
	}
	stored.Status = domain.ClusterConnected
	if err := fileStore.UpdateCluster(context.Background(), stored); err != nil {
		t.Fatalf("UpdateCluster(connected) error = %v", err)
	}
	wrongConfirmation := valid
	wrongConfirmation.Confirmation = "production-west"
	if _, err := service.RotateClusterCredentials(context.Background(), "admin", "req_confirmation", created.ID, wrongConfirmation); err == nil {
		t.Fatal("RotateClusterCredentials() accepted a mismatched confirmation")
	}
	if factory.Calls() != 1 {
		t.Fatalf("factory calls = %d, want no candidate builds", factory.Calls())
	}
}

func TestServiceChecksClusterCapabilitiesWithinResourceGuard(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.2"},
		capabilities: []domain.KubernetesCapability{
			{Key: "namespaces.list", State: domain.KubernetesCapabilityAllowed},
			{Key: "pods.logs.get", State: domain.KubernetesCapabilityDenied},
		},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	result, err := service.ClusterCapabilities(context.Background(), cluster.ID, "payments")
	if err != nil {
		t.Fatalf("ClusterCapabilities() error = %v", err)
	}
	if result.Namespace != "payments" || !result.CheckedAt.Equal(time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)) ||
		len(result.Checks) != 2 || result.Checks[1].State != domain.KubernetesCapabilityDenied {
		t.Fatalf("capability result = %#v", result)
	}
	if gateway.capabilityCalls.Load() != 1 || gateway.capabilityNamespace != "payments" {
		t.Fatalf("capability calls = %d, namespace = %q", gateway.capabilityCalls.Load(), gateway.capabilityNamespace)
	}
}

func TestServiceRejectsCapabilityChecksBeforeCallingGateway(t *testing.T) {
	t.Parallel()

	critical := 0.97
	readGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled: true, MaxConcurrent: 4, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticResourceSampler{sample: resourceguard.Sample{MemoryRatio: &critical}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, readGovernor: readGovernor})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	if _, err := service.ClusterCapabilities(context.Background(), cluster.ID, "Invalid_Namespace"); err == nil {
		t.Fatal("ClusterCapabilities() accepted an invalid namespace")
	}
	if _, err := service.ClusterCapabilities(context.Background(), cluster.ID, "payments"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("ClusterCapabilities() error = %v, want busy", err)
	}
	if gateway.capabilityCalls.Load() != 0 {
		t.Fatalf("capability calls = %d, want 0", gateway.capabilityCalls.Load())
	}
}

func TestServiceRejectsConcurrentCapabilityChecksForSameTarget(t *testing.T) {
	t.Parallel()

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	gateway := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.2"}, capabilityStarted: started, capabilityRelease: release,
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_create", domain.ClusterInput{
		Name: "production-east", Environment: domain.EnvironmentProduction, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	result := make(chan error, 1)
	go func() {
		_, capabilityErr := service.ClusterCapabilities(context.Background(), cluster.ID, "payments")
		result <- capabilityErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("capability check did not start")
	}
	if _, err := service.ClusterCapabilities(context.Background(), cluster.ID, "payments"); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("concurrent ClusterCapabilities() error = %v, want busy", err)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("first ClusterCapabilities() error = %v", err)
	}
	if gateway.capabilityCalls.Load() != 1 {
		t.Fatalf("capability calls = %d, want 1", gateway.capabilityCalls.Load())
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

func TestServiceRejectsKubernetesReadsDuringCriticalResourcePressure(t *testing.T) {
	t.Parallel()

	critical := 0.97
	readGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled: true, MaxConcurrent: 4, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticResourceSampler{sample: resourceguard.Sample{MemoryRatio: &critical}},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	gateway := &fakeKubeGateway{probe: domain.ClusterProbe{Version: "v1.36.2"}}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, readGovernor: readGovernor})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	reference := domain.WorkloadReference{Kind: "deployment", Namespace: "payments", Name: "gateway"}
	checks := []struct {
		name string
		call func() error
	}{
		{name: "summary", call: func() error { _, err := service.Summary(context.Background(), cluster.ID); return err }},
		{name: "namespaces", call: func() error { _, err := service.Namespaces(context.Background(), cluster.ID); return err }},
		{name: "nodes", call: func() error { _, err := service.Nodes(context.Background(), cluster.ID); return err }},
		{name: "node detail", call: func() error { _, err := service.NodeDetail(context.Background(), cluster.ID, "worker-01"); return err }},
		{name: "node events", call: func() error {
			_, err := service.NodeEvents(context.Background(), cluster.ID, "worker-01", 20)
			return err
		}},
		{name: "cluster events", call: func() error {
			_, err := service.Events(context.Background(), cluster.ID, "payments", "Warning", 200)
			return err
		}},
		{name: "workloads", call: func() error {
			_, err := service.Workloads(context.Background(), cluster.ID, "payments", "deployment")
			return err
		}},
		{name: "services", call: func() error {
			_, err := service.Services(context.Background(), cluster.ID, "payments")
			return err
		}},
		{name: "ingresses", call: func() error {
			_, err := service.Ingresses(context.Background(), cluster.ID, "payments")
			return err
		}},
		{name: "config maps", call: func() error {
			_, err := service.ConfigMaps(context.Background(), cluster.ID, "payments")
			return err
		}},
		{name: "secrets", call: func() error {
			_, err := service.Secrets(context.Background(), "admin", "req_secrets", cluster.ID, "payments")
			return err
		}},
		{name: "persistent volume claims", call: func() error {
			_, err := service.PersistentVolumeClaims(context.Background(), cluster.ID, "payments")
			return err
		}},
		{name: "persistent volumes", call: func() error {
			_, err := service.PersistentVolumes(context.Background(), cluster.ID)
			return err
		}},
		{name: "storage classes", call: func() error {
			_, err := service.StorageClasses(context.Background(), cluster.ID)
			return err
		}},
		{name: "access resources", call: func() error {
			_, err := service.AccessResources(context.Background(), cluster.ID, domain.AccessResourceRoles, "payments")
			return err
		}},
		{name: "access resource detail", call: func() error {
			_, err := service.AccessResourceDetail(context.Background(), cluster.ID, domain.KubernetesAccessResourceReference{
				Kind: domain.AccessResourceRoles, Namespace: "payments", Name: "reader",
			})
			return err
		}},
		{name: "service account access review", call: func() error {
			_, err := service.ReviewServiceAccountAccess(
				context.Background(), "admin", "req_review", cluster.ID, validServiceAccountAccessReviewInput(),
			)
			return err
		}},
		{name: "workload detail", call: func() error {
			_, err := service.WorkloadDetail(context.Background(), cluster.ID, reference)
			return err
		}},
		{name: "workload events", call: func() error {
			_, err := service.WorkloadEvents(context.Background(), cluster.ID, reference, 20)
			return err
		}},
		{name: "pod logs", call: func() error {
			_, err := service.PodLogs(context.Background(), "admin", "req_logs", cluster.ID, domain.PodLogRequest{
				Namespace: "payments", Pod: "gateway-0", Container: "app", TailLines: 100,
			})
			return err
		}},
		{name: "image preview", call: func() error {
			_, err := service.PreviewWorkloadImage(context.Background(), "admin", "req_preview", domain.WorkloadImageOperationInput{
				ClusterID: cluster.ID,
				Change: domain.WorkloadImageChange{
					Reference: reference, Container: "app", CurrentImage: "gateway:1.0.0", Image: "gateway:1.1.0", ResourceVersion: "42",
				},
			})
			return err
		}},
		{name: "Helm releases", call: func() error {
			_, err := service.ListHelmReleases(context.Background(), cluster.ID, "payments")
			return err
		}},
	}
	for _, check := range checks {
		if err := check.call(); !errors.Is(err, domain.ErrBusy) {
			t.Errorf("%s error = %v, want busy", check.name, err)
		}
	}
	if calls := gateway.summaryCalls.Load(); calls != 0 {
		t.Fatalf("Summary() reached Kubernetes %d times under critical pressure", calls)
	}
	if serviceCalls, ingressCalls := gateway.serviceCalls.Load(), gateway.ingressCalls.Load(); serviceCalls != 0 || ingressCalls != 0 {
		t.Fatalf("network reads reached Kubernetes under critical pressure: services=%d ingresses=%d", serviceCalls, ingressCalls)
	}
	if configMapCalls, secretCalls := gateway.configMapCalls.Load(), gateway.secretCalls.Load(); configMapCalls != 0 || secretCalls != 0 {
		t.Fatalf("configuration reads reached Kubernetes under critical pressure: configmaps=%d secrets=%d", configMapCalls, secretCalls)
	}
	if claimCalls, volumeCalls, classCalls := gateway.persistentVolumeClaimCalls.Load(), gateway.persistentVolumeCalls.Load(), gateway.storageClassCalls.Load(); claimCalls != 0 || volumeCalls != 0 || classCalls != 0 {
		t.Fatalf("storage reads reached Kubernetes under critical pressure: claims=%d volumes=%d classes=%d", claimCalls, volumeCalls, classCalls)
	}
	if calls := gateway.clusterEventCalls.Load(); calls != 0 {
		t.Fatalf("event reads reached Kubernetes %d times under critical pressure", calls)
	}
	if listCalls, detailCalls, reviewCalls := gateway.accessListCalls.Load(), gateway.accessDetailCalls.Load(), gateway.accessReviewCalls.Load(); listCalls != 0 || detailCalls != 0 || reviewCalls != 0 {
		t.Fatalf("access reads reached Kubernetes under critical pressure: lists=%d details=%d reviews=%d", listCalls, detailCalls, reviewCalls)
	}
	capacity := service.OperationCapacity().KubernetesReads
	if !capacity.Adaptive || capacity.Pressure != resourceguard.PressureCritical || capacity.Limit != 0 || capacity.Maximum != 4 {
		t.Fatalf("Kubernetes read capacity = %#v", capacity)
	}
}

func TestServiceListsNetworkResourcesAndValidatesNamespaceBeforeGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe:     domain.ClusterProbe{Version: "v1.36.2"},
		services:  []domain.KubernetesService{{Namespace: "payments", Name: "gateway"}},
		ingresses: []domain.KubernetesIngress{{Namespace: "payments", Name: "gateway"}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	services, err := service.Services(context.Background(), cluster.ID, "payments")
	if err != nil || len(services) != 1 || services[0].Name != "gateway" {
		t.Fatalf("Services() = %#v, %v", services, err)
	}
	ingresses, err := service.Ingresses(context.Background(), cluster.ID, "")
	if err != nil || len(ingresses) != 1 || ingresses[0].Name != "gateway" {
		t.Fatalf("Ingresses() = %#v, %v", ingresses, err)
	}
	if gateway.serviceNamespace != "payments" || gateway.ingressNamespace != "" {
		t.Fatalf("network namespaces = service %q, ingress %q", gateway.serviceNamespace, gateway.ingressNamespace)
	}

	serviceCalls := gateway.serviceCalls.Load()
	ingressCalls := gateway.ingressCalls.Load()
	if _, err := service.Services(context.Background(), cluster.ID, "bad/namespace"); err == nil {
		t.Fatal("Services() accepted an invalid namespace")
	}
	if _, err := service.Ingresses(context.Background(), cluster.ID, "bad/namespace"); err == nil {
		t.Fatal("Ingresses() accepted an invalid namespace")
	}
	if gateway.serviceCalls.Load() != serviceCalls || gateway.ingressCalls.Load() != ingressCalls {
		t.Fatal("invalid namespace reached Kubernetes gateway")
	}
}

func TestServiceListsConfigurationResourcesAndValidatesSecretScopeBeforeGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe:      domain.ClusterProbe{Version: "v1.36.2"},
		configMaps: []domain.KubernetesConfigMap{{Namespace: "payments", Name: "settings", DataCount: 3}},
		secrets:    []domain.KubernetesSecret{{Namespace: "payments", Name: "registry", Type: "Opaque", DataCount: 1}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	configMaps, err := service.ConfigMaps(context.Background(), cluster.ID, "")
	if err != nil || len(configMaps) != 1 || configMaps[0].Name != "settings" {
		t.Fatalf("ConfigMaps() = %#v, %v", configMaps, err)
	}
	secrets, err := service.Secrets(context.Background(), "admin", "req_secrets", cluster.ID, "payments")
	if err != nil || len(secrets) != 1 || secrets[0].Name != "registry" {
		t.Fatalf("Secrets() = %#v, %v", secrets, err)
	}
	if gateway.configMapNamespace != "" || gateway.secretNamespace != "payments" {
		t.Fatalf("configuration namespaces = configmaps %q, secrets %q", gateway.configMapNamespace, gateway.secretNamespace)
	}

	configMapCalls := gateway.configMapCalls.Load()
	secretCalls := gateway.secretCalls.Load()
	if _, err := service.ConfigMaps(context.Background(), cluster.ID, "bad/namespace"); err == nil {
		t.Fatal("ConfigMaps() accepted an invalid namespace")
	}
	if _, err := service.Secrets(context.Background(), "admin", "req_empty", cluster.ID, ""); err == nil {
		t.Fatal("Secrets() accepted an empty namespace")
	}
	if _, err := service.Secrets(context.Background(), "admin", "req_invalid", cluster.ID, "bad/namespace"); err == nil {
		t.Fatal("Secrets() accepted an invalid namespace")
	}
	if gateway.configMapCalls.Load() != configMapCalls || gateway.secretCalls.Load() != secretCalls {
		t.Fatal("invalid configuration scope reached Kubernetes gateway")
	}

	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	found := false
	for _, event := range audits {
		if event.Action != "secret.metadata.list" {
			continue
		}
		found = true
		if event.RequestID != "req_secrets" || event.Namespace != "payments" || event.Target != "secrets" ||
			event.Summary != "count=1" || strings.Contains(event.Summary, "registry") {
			t.Errorf("secret metadata audit = %#v", event)
		}
	}
	if !found {
		t.Fatal("secret.metadata.list audit event was not written")
	}
}

func TestServiceListsStorageResourcesAndValidatesClaimScopeBeforeGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe:                  domain.ClusterProbe{Version: "v1.36.2"},
		persistentVolumeClaims: []domain.KubernetesPersistentVolumeClaim{{Namespace: "payments", Name: "data", Status: "Bound"}},
		persistentVolumes:      []domain.KubernetesPersistentVolume{{Name: "pv-data", Status: "Bound"}},
		storageClasses:         []domain.KubernetesStorageClass{{Name: "standard", Provisioner: "csi.example.com"}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	claims, err := service.PersistentVolumeClaims(context.Background(), cluster.ID, "payments")
	if err != nil || len(claims) != 1 || claims[0].Name != "data" {
		t.Fatalf("PersistentVolumeClaims() = %#v, %v", claims, err)
	}
	volumes, err := service.PersistentVolumes(context.Background(), cluster.ID)
	if err != nil || len(volumes) != 1 || volumes[0].Name != "pv-data" {
		t.Fatalf("PersistentVolumes() = %#v, %v", volumes, err)
	}
	classes, err := service.StorageClasses(context.Background(), cluster.ID)
	if err != nil || len(classes) != 1 || classes[0].Name != "standard" {
		t.Fatalf("StorageClasses() = %#v, %v", classes, err)
	}
	if gateway.persistentVolumeClaimNamespace != "payments" {
		t.Fatalf("claim namespace = %q", gateway.persistentVolumeClaimNamespace)
	}

	claimCalls := gateway.persistentVolumeClaimCalls.Load()
	if _, err := service.PersistentVolumeClaims(context.Background(), cluster.ID, "bad/namespace"); err == nil {
		t.Fatal("PersistentVolumeClaims() accepted an invalid namespace")
	}
	if gateway.persistentVolumeClaimCalls.Load() != claimCalls {
		t.Fatal("invalid claim namespace reached Kubernetes gateway")
	}
}

func TestServiceListsNamespaceGovernanceAndValidatesScopeBeforeGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.2"},
		resourceQuotas: []domain.KubernetesResourceQuota{{
			Namespace: "payments", Name: "compute-quota",
			Resources: []domain.KubernetesQuotaResource{{Name: "requests.cpu", Hard: "4", Used: "2", Observed: true}},
		}},
		limitRanges: []domain.KubernetesLimitRange{{
			Namespace: "payments", Name: "namespace-defaults",
			Constraints: []domain.KubernetesLimitRangeConstraint{{Type: "Container", Resource: "cpu", Default: "500m"}},
		}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	quotas, err := service.ResourceQuotas(context.Background(), cluster.ID, "payments")
	if err != nil || len(quotas) != 1 || quotas[0].Name != "compute-quota" {
		t.Fatalf("ResourceQuotas() = %#v, %v", quotas, err)
	}
	limitRanges, err := service.LimitRanges(context.Background(), cluster.ID, "payments")
	if err != nil || len(limitRanges) != 1 || limitRanges[0].Name != "namespace-defaults" {
		t.Fatalf("LimitRanges() = %#v, %v", limitRanges, err)
	}
	if gateway.resourceQuotaNamespace != "payments" || gateway.limitRangeNamespace != "payments" ||
		gateway.resourceQuotaCalls.Load() != 1 || gateway.limitRangeCalls.Load() != 1 {
		t.Fatalf("governance gateway inputs = quota %q/%d, limit %q/%d",
			gateway.resourceQuotaNamespace, gateway.resourceQuotaCalls.Load(),
			gateway.limitRangeNamespace, gateway.limitRangeCalls.Load())
	}

	quotaCalls := gateway.resourceQuotaCalls.Load()
	limitCalls := gateway.limitRangeCalls.Load()
	for _, namespace := range []string{"", "bad/namespace"} {
		if _, err := service.ResourceQuotas(context.Background(), cluster.ID, namespace); err == nil {
			t.Errorf("ResourceQuotas(%q) succeeded", namespace)
		}
		if _, err := service.LimitRanges(context.Background(), cluster.ID, namespace); err == nil {
			t.Errorf("LimitRanges(%q) succeeded", namespace)
		}
	}
	if gateway.resourceQuotaCalls.Load() != quotaCalls || gateway.limitRangeCalls.Load() != limitCalls {
		t.Fatal("invalid governance namespace reached Kubernetes gateway")
	}
}

func TestServiceListsEventsAndValidatesFiltersBeforeGateway(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.2"},
		clusterEvents: []domain.KubernetesEvent{{
			Namespace: "payments", Name: "gateway-warning", Type: "Warning", Reason: "BackOff",
		}},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	events, err := service.Events(context.Background(), cluster.ID, "payments", "Warning", 200)
	if err != nil || len(events) != 1 || events[0].Reason != "BackOff" {
		t.Fatalf("Events() = %#v, %v", events, err)
	}
	if gateway.clusterEventNamespace != "payments" || gateway.clusterEventType != "Warning" || gateway.clusterEventLimit != 200 {
		t.Fatalf("event filters = namespace %q, type %q, limit %d", gateway.clusterEventNamespace, gateway.clusterEventType, gateway.clusterEventLimit)
	}

	calls := gateway.clusterEventCalls.Load()
	checks := []struct {
		name      string
		namespace string
		eventType string
		limit     int
	}{
		{name: "namespace", namespace: "bad/namespace", eventType: "Warning", limit: 200},
		{name: "type", namespace: "payments", eventType: "warning", limit: 200},
		{name: "limit", namespace: "payments", eventType: "Warning", limit: domain.MaxClusterEventLimit + 1},
	}
	for _, check := range checks {
		if _, err := service.Events(context.Background(), cluster.ID, check.namespace, check.eventType, check.limit); err == nil {
			t.Errorf("Events() accepted invalid %s", check.name)
		}
	}
	if gateway.clusterEventCalls.Load() != calls {
		t.Fatal("invalid event filters reached Kubernetes gateway")
	}
}

func TestServiceListsAccessResourcesAndValidatesScopeBeforeGateway(t *testing.T) {
	t.Parallel()

	reference := domain.KubernetesAccessResourceReference{
		Kind: domain.AccessResourceRoleBindings, Namespace: "payments", Name: "gateway-readers",
	}
	gateway := &fakeKubeGateway{
		probe:           domain.ClusterProbe{Version: "v1.36.2"},
		accessResources: []domain.KubernetesAccessResource{{Kind: "RoleBinding", Namespace: "payments", Name: "gateway-readers"}},
		accessDetail: domain.KubernetesAccessResourceDetail{
			KubernetesAccessResource: domain.KubernetesAccessResource{Kind: "RoleBinding", Namespace: "payments", Name: "gateway-readers"},
			RoleRef:                  &domain.KubernetesRoleReference{Kind: "Role", Name: "gateway-reader"},
		},
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	items, err := service.AccessResources(context.Background(), cluster.ID, reference.Kind, reference.Namespace)
	if err != nil || len(items) != 1 || items[0].Name != reference.Name {
		t.Fatalf("AccessResources() = %#v, %v", items, err)
	}
	detail, err := service.AccessResourceDetail(context.Background(), cluster.ID, reference)
	if err != nil || detail.RoleRef == nil || detail.RoleRef.Name != "gateway-reader" {
		t.Fatalf("AccessResourceDetail() = %#v, %v", detail, err)
	}
	if gateway.accessKind != reference.Kind || gateway.accessNamespace != reference.Namespace || gateway.accessReference != reference {
		t.Fatalf("access gateway inputs = kind %q, namespace %q, reference %#v", gateway.accessKind, gateway.accessNamespace, gateway.accessReference)
	}

	listCalls := gateway.accessListCalls.Load()
	detailCalls := gateway.accessDetailCalls.Load()
	invalidLists := []domain.KubernetesAccessResourceReference{
		{Kind: domain.AccessResourceRoles},
		{Kind: domain.AccessResourceClusterRoles, Namespace: "payments"},
		{Kind: "secrets", Namespace: "payments"},
	}
	for _, invalid := range invalidLists {
		if _, err := service.AccessResources(context.Background(), cluster.ID, invalid.Kind, invalid.Namespace); err == nil {
			t.Errorf("AccessResources() accepted %#v", invalid)
		}
	}
	if _, err := service.AccessResourceDetail(context.Background(), cluster.ID, domain.KubernetesAccessResourceReference{
		Kind: domain.AccessResourceRoles, Namespace: "payments", Name: "../reader",
	}); err == nil {
		t.Fatal("AccessResourceDetail() accepted an invalid name")
	}
	if gateway.accessListCalls.Load() != listCalls || gateway.accessDetailCalls.Load() != detailCalls {
		t.Fatal("invalid access scope reached Kubernetes gateway")
	}
}

func TestServiceReviewsServiceAccountAccessAndWritesBoundedAudit(t *testing.T) {
	t.Parallel()

	gateway := &fakeKubeGateway{
		probe:             domain.ClusterProbe{Version: "v1.36.2"},
		accessReviewState: domain.KubernetesCapabilityAllowed,
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}
	input := validServiceAccountAccessReviewInput()

	review, err := service.ReviewServiceAccountAccess(context.Background(), "admin", "req_review", cluster.ID, input)
	if err != nil || review.State != domain.KubernetesCapabilityAllowed || review.ServiceAccount != input.ServiceAccount ||
		review.ResourceAttributes != input.ResourceAttributes || review.CheckedAt.IsZero() {
		t.Fatalf("ReviewServiceAccountAccess() = %#v, %v", review, err)
	}
	if gateway.accessReviewCalls.Load() != 1 || gateway.accessReviewInput != input {
		t.Fatalf("access review gateway input = %#v, calls = %d", gateway.accessReviewInput, gateway.accessReviewCalls.Load())
	}

	invalid := input
	invalid.ResourceAttributes.Resource = "*"
	if _, err := service.ReviewServiceAccountAccess(context.Background(), "admin", "req_invalid", cluster.ID, invalid); err == nil {
		t.Fatal("ReviewServiceAccountAccess() accepted a wildcard resource")
	}
	if gateway.accessReviewCalls.Load() != 1 {
		t.Fatal("invalid access review reached Kubernetes gateway")
	}

	gateway.accessReviewErr = errors.New("private authorizer failure")
	if _, err := service.ReviewServiceAccountAccess(context.Background(), "admin", "req_failed", cluster.ID, input); err == nil {
		t.Fatal("ReviewServiceAccountAccess() succeeded after gateway failure")
	}
	audits, err := service.ListAuditEvents(context.Background(), 100)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	foundSuccess := false
	foundFailure := false
	for _, event := range audits {
		if event.Action != "service_account.access_review" {
			continue
		}
		if event.Target != "payments/gateway" || event.Namespace != "payments" || strings.Contains(event.Summary, "private") {
			t.Errorf("service account access review audit = %#v", event)
		}
		switch event.RequestID {
		case "req_review":
			foundSuccess = event.Result == "succeeded" && event.Summary == "state=allowed, verb=get, group=core, resource=pods, subresource=-, target_namespace=payments"
		case "req_failed":
			foundFailure = event.Result == "failed" && event.Summary == "service account access review failed"
		}
	}
	if !foundSuccess || !foundFailure {
		t.Fatalf("service account access review audits missing: success=%t failure=%t events=%#v", foundSuccess, foundFailure, audits)
	}
}

func TestServiceRejectsExcessKubernetesReadsWithoutWaiting(t *testing.T) {
	t.Parallel()

	readGovernor, err := resourceguard.New(resourceguard.Config{
		Enabled: false, MaxConcurrent: 1, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticResourceSampler{},
	})
	if err != nil {
		t.Fatalf("resourceguard.New() error = %v", err)
	}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	gateway := &fakeKubeGateway{
		probe: domain.ClusterProbe{Version: "v1.36.2"}, summaryStarted: started, summaryRelease: release,
	}
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, readGovernor: readGovernor})
	cluster, err := service.CreateCluster(context.Background(), "admin", "req_cluster", domain.ClusterInput{
		Name: "cluster", Environment: domain.EnvironmentDevelopment, Server: "https://api.example.com", BearerToken: "token",
	})
	if err != nil {
		t.Fatalf("CreateCluster() error = %v", err)
	}

	firstContext, cancelFirst := context.WithCancel(context.Background())
	t.Cleanup(cancelFirst)
	firstResult := make(chan error, 1)
	go func() {
		_, summaryErr := service.Summary(firstContext, cluster.ID)
		firstResult <- summaryErr
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first Summary() did not reach Kubernetes")
	}

	secondContext, cancelSecond := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelSecond()
	if _, err := service.Summary(secondContext, cluster.ID); !errors.Is(err, domain.ErrBusy) {
		t.Fatalf("second Summary() error = %v, want busy", err)
	}
	close(release)
	select {
	case err := <-firstResult:
		if err != nil {
			t.Fatalf("first Summary() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first Summary() did not finish after release")
	}
	if calls := gateway.summaryCalls.Load(); calls != 1 {
		t.Fatalf("Kubernetes Summary() calls = %d, want 1", calls)
	}
	if active := service.OperationCapacity().KubernetesReads.Active; active != 0 {
		t.Fatalf("active Kubernetes reads after completion = %d", active)
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
	service, _, _ := newTestService(t, serviceFakes{kube: gateway, governor: governor, readGovernor: governor})
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
	cipher       SecretCipher
	kube         KubeGateway
	factory      KubeFactory
	helm         HelmGateway
	governor     OperationGovernor
	readGovernor ReadGovernor
	queueSize    int
	cacheSize    int
	cacheTTL     time.Duration
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
	if fakes.cipher == nil {
		fakes.cipher = cipher
	}
	if fakes.kube == nil {
		fakes.kube = &fakeKubeGateway{}
	}
	if fakes.factory == nil {
		fakes.factory = fakeKubeFactory{gateway: fakes.kube}
	}
	if fakes.helm == nil {
		fakes.helm = &blockingHelmGateway{}
	}
	if fakes.governor == nil {
		fakes.governor = testOperationGovernor(t)
	}
	if fakes.readGovernor == nil {
		fakes.readGovernor = testReadGovernor(t)
	}
	if fakes.queueSize == 0 {
		fakes.queueSize = 16
	}
	var idCounter atomic.Int64
	service, err := New(Dependencies{
		Store:                     fileStore,
		Cipher:                    fakes.cipher,
		TargetValidator:           allowAllValidator{},
		KubeFactory:               fakes.factory,
		RepositoryChecker:         successfulRepositoryChecker{},
		Helm:                      fakes.helm,
		OperationGovernor:         fakes.governor,
		ReadGovernor:              fakes.readGovernor,
		OperationQueueSize:        fakes.queueSize,
		KubernetesClientCacheSize: fakes.cacheSize,
		KubernetesClientCacheTTL:  fakes.cacheTTL,
		Clock:                     func() time.Time { return now },
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

type countingSecretCipher struct {
	delegate  SecretCipher
	openCalls atomic.Int64
}

func (c *countingSecretCipher) SealString(plaintext, associatedData string) (string, error) {
	return c.delegate.SealString(plaintext, associatedData)
}

func (c *countingSecretCipher) OpenString(encoded, associatedData string) (string, error) {
	c.openCalls.Add(1)
	return c.delegate.OpenString(encoded, associatedData)
}

type fakeKubeFactory struct{ gateway KubeGateway }

func (f fakeKubeFactory) New(context.Context, kubernetes.Connection) (KubeGateway, error) {
	return f.gateway, nil
}

type sequenceKubeFactory struct {
	mu          sync.Mutex
	gateways    []KubeGateway
	connections []kubernetes.Connection
	calls       int
}

func (f *sequenceKubeFactory) New(_ context.Context, connection kubernetes.Connection) (KubeGateway, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls >= len(f.gateways) {
		return nil, errors.New("unexpected Kubernetes gateway build")
	}
	gateway := f.gateways[f.calls]
	f.connections = append(f.connections, connection)
	f.calls++
	return gateway, nil
}

func (f *sequenceKubeFactory) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *sequenceKubeFactory) Connections() []kubernetes.Connection {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]kubernetes.Connection(nil), f.connections...)
}

type fakeKubeGateway struct {
	probe                          domain.ClusterProbe
	probeErr                       error
	probeStarted                   chan<- struct{}
	probeRelease                   <-chan struct{}
	capabilities                   []domain.KubernetesCapability
	capabilityErr                  error
	capabilityCalls                atomic.Int64
	capabilityStarted              chan<- struct{}
	capabilityRelease              <-chan struct{}
	capabilityNamespace            string
	summaryCalls                   atomic.Int64
	summaryStarted                 chan struct{}
	summaryRelease                 <-chan struct{}
	namespaces                     []domain.Namespace
	detail                         domain.WorkloadDetail
	events                         []domain.KubernetesEvent
	logs                           domain.PodLogs
	detailReference                domain.WorkloadReference
	eventLimit                     int
	logRequest                     domain.PodLogRequest
	nodes                          []domain.Node
	nodeDetail                     domain.NodeDetail
	nodeEvents                     []domain.KubernetesEvent
	nodeName                       string
	nodeEventLimit                 int
	services                       []domain.KubernetesService
	ingresses                      []domain.KubernetesIngress
	configMaps                     []domain.KubernetesConfigMap
	secrets                        []domain.KubernetesSecret
	persistentVolumeClaims         []domain.KubernetesPersistentVolumeClaim
	persistentVolumes              []domain.KubernetesPersistentVolume
	storageClasses                 []domain.KubernetesStorageClass
	resourceQuotas                 []domain.KubernetesResourceQuota
	limitRanges                    []domain.KubernetesLimitRange
	clusterEvents                  []domain.KubernetesEvent
	accessResources                []domain.KubernetesAccessResource
	accessDetail                   domain.KubernetesAccessResourceDetail
	accessReviewState              domain.KubernetesCapabilityState
	accessReviewErr                error
	serviceCalls                   atomic.Int64
	ingressCalls                   atomic.Int64
	configMapCalls                 atomic.Int64
	secretCalls                    atomic.Int64
	persistentVolumeClaimCalls     atomic.Int64
	persistentVolumeCalls          atomic.Int64
	storageClassCalls              atomic.Int64
	resourceQuotaCalls             atomic.Int64
	limitRangeCalls                atomic.Int64
	clusterEventCalls              atomic.Int64
	accessListCalls                atomic.Int64
	accessDetailCalls              atomic.Int64
	accessReviewCalls              atomic.Int64
	serviceNamespace               string
	ingressNamespace               string
	configMapNamespace             string
	secretNamespace                string
	persistentVolumeClaimNamespace string
	resourceQuotaNamespace         string
	limitRangeNamespace            string
	clusterEventNamespace          string
	clusterEventType               string
	clusterEventLimit              int
	accessKind                     domain.KubernetesAccessResourceKind
	accessNamespace                string
	accessReference                domain.KubernetesAccessResourceReference
	accessReviewInput              domain.KubernetesServiceAccountAccessReviewInput
	mutationMu                     sync.Mutex
	scaledVersion                  string
	scaledReplicas                 int32
	restartedVersion               string
	restartedAt                    time.Time
	previewedImage                 domain.WorkloadImageChange
	updatedImage                   domain.WorkloadImageChange
	idleCloseCalls                 atomic.Int64
}

func (g *fakeKubeGateway) CloseIdleConnections() { g.idleCloseCalls.Add(1) }

func (g *fakeKubeGateway) Probe(ctx context.Context) (domain.ClusterProbe, error) {
	if g.probeStarted != nil {
		select {
		case g.probeStarted <- struct{}{}:
		default:
		}
	}
	if g.probeRelease != nil {
		select {
		case <-g.probeRelease:
		case <-ctx.Done():
			return domain.ClusterProbe{}, ctx.Err()
		}
	}
	return g.probe, g.probeErr
}
func (g *fakeKubeGateway) Capabilities(ctx context.Context, namespace string) ([]domain.KubernetesCapability, error) {
	g.capabilityCalls.Add(1)
	g.mutationMu.Lock()
	g.capabilityNamespace = namespace
	g.mutationMu.Unlock()
	if g.capabilityStarted != nil {
		select {
		case g.capabilityStarted <- struct{}{}:
		default:
		}
	}
	if g.capabilityRelease != nil {
		select {
		case <-g.capabilityRelease:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return append([]domain.KubernetesCapability(nil), g.capabilities...), g.capabilityErr
}
func (g *fakeKubeGateway) Summary(ctx context.Context) (domain.ClusterSummary, error) {
	g.summaryCalls.Add(1)
	if g.summaryStarted != nil {
		select {
		case g.summaryStarted <- struct{}{}:
		default:
		}
	}
	if g.summaryRelease != nil {
		select {
		case <-g.summaryRelease:
		case <-ctx.Done():
			return domain.ClusterSummary{}, ctx.Err()
		}
	}
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
func (g *fakeKubeGateway) Services(_ context.Context, namespace string) ([]domain.KubernetesService, error) {
	g.serviceCalls.Add(1)
	g.mutationMu.Lock()
	g.serviceNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesService(nil), g.services...), nil
}
func (g *fakeKubeGateway) Ingresses(_ context.Context, namespace string) ([]domain.KubernetesIngress, error) {
	g.ingressCalls.Add(1)
	g.mutationMu.Lock()
	g.ingressNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesIngress(nil), g.ingresses...), nil
}
func (g *fakeKubeGateway) ConfigMaps(_ context.Context, namespace string) ([]domain.KubernetesConfigMap, error) {
	g.configMapCalls.Add(1)
	g.mutationMu.Lock()
	g.configMapNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesConfigMap(nil), g.configMaps...), nil
}
func (g *fakeKubeGateway) Secrets(_ context.Context, namespace string) ([]domain.KubernetesSecret, error) {
	g.secretCalls.Add(1)
	g.mutationMu.Lock()
	g.secretNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesSecret(nil), g.secrets...), nil
}
func (g *fakeKubeGateway) PersistentVolumeClaims(_ context.Context, namespace string) ([]domain.KubernetesPersistentVolumeClaim, error) {
	g.persistentVolumeClaimCalls.Add(1)
	g.mutationMu.Lock()
	g.persistentVolumeClaimNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesPersistentVolumeClaim(nil), g.persistentVolumeClaims...), nil
}
func (g *fakeKubeGateway) PersistentVolumes(context.Context) ([]domain.KubernetesPersistentVolume, error) {
	g.persistentVolumeCalls.Add(1)
	return append([]domain.KubernetesPersistentVolume(nil), g.persistentVolumes...), nil
}
func (g *fakeKubeGateway) StorageClasses(context.Context) ([]domain.KubernetesStorageClass, error) {
	g.storageClassCalls.Add(1)
	return append([]domain.KubernetesStorageClass(nil), g.storageClasses...), nil
}
func (g *fakeKubeGateway) ResourceQuotas(_ context.Context, namespace string) ([]domain.KubernetesResourceQuota, error) {
	g.resourceQuotaCalls.Add(1)
	g.mutationMu.Lock()
	g.resourceQuotaNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesResourceQuota(nil), g.resourceQuotas...), nil
}
func (g *fakeKubeGateway) LimitRanges(_ context.Context, namespace string) ([]domain.KubernetesLimitRange, error) {
	g.limitRangeCalls.Add(1)
	g.mutationMu.Lock()
	g.limitRangeNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesLimitRange(nil), g.limitRanges...), nil
}
func (g *fakeKubeGateway) Events(_ context.Context, namespace, eventType string, limit int) ([]domain.KubernetesEvent, error) {
	g.clusterEventCalls.Add(1)
	g.mutationMu.Lock()
	g.clusterEventNamespace = namespace
	g.clusterEventType = eventType
	g.clusterEventLimit = limit
	g.mutationMu.Unlock()
	return append([]domain.KubernetesEvent(nil), g.clusterEvents...), nil
}
func (g *fakeKubeGateway) AccessResources(_ context.Context, kind domain.KubernetesAccessResourceKind, namespace string) ([]domain.KubernetesAccessResource, error) {
	g.accessListCalls.Add(1)
	g.mutationMu.Lock()
	g.accessKind = kind
	g.accessNamespace = namespace
	g.mutationMu.Unlock()
	return append([]domain.KubernetesAccessResource(nil), g.accessResources...), nil
}
func (g *fakeKubeGateway) AccessResourceDetail(_ context.Context, reference domain.KubernetesAccessResourceReference) (domain.KubernetesAccessResourceDetail, error) {
	g.accessDetailCalls.Add(1)
	g.mutationMu.Lock()
	g.accessReference = reference
	g.mutationMu.Unlock()
	return g.accessDetail, nil
}
func (g *fakeKubeGateway) ReviewServiceAccountAccess(_ context.Context, input domain.KubernetesServiceAccountAccessReviewInput) (domain.KubernetesCapabilityState, error) {
	g.accessReviewCalls.Add(1)
	g.mutationMu.Lock()
	g.accessReviewInput = input
	g.mutationMu.Unlock()
	return g.accessReviewState, g.accessReviewErr
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

func validServiceAccountAccessReviewInput() domain.KubernetesServiceAccountAccessReviewInput {
	return domain.KubernetesServiceAccountAccessReviewInput{
		ServiceAccount: domain.KubernetesServiceAccountReference{Namespace: "payments", Name: "gateway"},
		ResourceAttributes: domain.KubernetesResourceAttributes{
			Resource: "pods", Verb: "get", Namespace: "payments",
		},
	}
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

func testReadGovernor(t *testing.T) ReadGovernor {
	t.Helper()
	value := 0.10
	governor, err := resourceguard.New(resourceguard.Config{
		Enabled: false, MaxConcurrent: 8, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticResourceSampler{sample: resourceguard.Sample{MemoryRatio: &value}},
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
