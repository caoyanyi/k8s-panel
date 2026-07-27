package platform

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
)

func TestGatewayCacheCoalescesConcurrentBuilds(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	cache, err := newGatewayCache(2, 10*time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	t.Cleanup(cache.Close)

	gateway := &fakeKubeGateway{}
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var builds atomic.Int64
	build := func(ctx context.Context) (KubeGateway, error) {
		builds.Add(1)
		started <- struct{}{}
		select {
		case <-release:
			return gateway, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	type result struct {
		gateway KubeGateway
		err     error
	}
	results := make(chan result, 2)
	get := func() {
		resolved, getErr := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), build)
		results <- result{gateway: resolved, err: getErr}
	}
	go get()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("gateway build did not start")
	}
	go get()
	close(release)

	first := <-results
	second := <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("Get() errors = %v, %v", first.err, second.err)
	}
	if first.gateway != gateway || second.gateway != gateway || builds.Load() != 1 {
		t.Fatalf("gateways = %p, %p; builds = %d", first.gateway, second.gateway, builds.Load())
	}
	if snapshot := cache.Snapshot(); snapshot.Entries != 1 || snapshot.Building != 0 {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
}

func TestGatewayCacheWaitHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	cache, err := newGatewayCache(2, 10*time.Minute, time.Now)
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	t.Cleanup(cache.Close)

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, getErr := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), func(context.Context) (KubeGateway, error) {
			started <- struct{}{}
			<-release
			return &fakeKubeGateway{}, nil
		})
		firstResult <- getErr
	}()
	<-started

	waitContext, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cache.Get(waitContext, "clu_1", testGatewayFingerprint(1), func(context.Context) (KubeGateway, error) {
		t.Fatal("waiting request started a duplicate build")
		return nil, nil
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting Get() error = %v, want context canceled", err)
	}
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
}

func TestGatewayCacheInvalidatesChangedCredentialsAndClusterLifecycle(t *testing.T) {
	t.Parallel()

	cache, err := newGatewayCache(2, 10*time.Minute, time.Now)
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	t.Cleanup(cache.Close)

	first := &fakeKubeGateway{}
	second := &fakeKubeGateway{}
	var builds atomic.Int64
	build := func(gateway KubeGateway) func(context.Context) (KubeGateway, error) {
		return func(context.Context) (KubeGateway, error) {
			builds.Add(1)
			return gateway, nil
		}
	}
	if _, err := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), build(first)); err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if _, err := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), build(second)); err != nil {
		t.Fatalf("cached Get() error = %v", err)
	}
	if builds.Load() != 1 {
		t.Fatalf("builds after cache hit = %d", builds.Load())
	}
	if _, err := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(2), build(second)); err != nil {
		t.Fatalf("credential-change Get() error = %v", err)
	}
	if builds.Load() != 2 || first.idleCloseCalls.Load() != 1 {
		t.Fatalf("builds = %d, first closes = %d", builds.Load(), first.idleCloseCalls.Load())
	}

	cache.Invalidate("clu_1")
	if second.idleCloseCalls.Load() != 1 || cache.Snapshot().Entries != 0 {
		t.Fatalf("second closes = %d, snapshot = %#v", second.idleCloseCalls.Load(), cache.Snapshot())
	}
}

func TestGatewayCacheAppliesLRUTTLAndResourcePressureBounds(t *testing.T) {
	t.Parallel()

	var clockMu sync.Mutex
	now := time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return now
	}
	advance := func(duration time.Duration) {
		clockMu.Lock()
		now = now.Add(duration)
		clockMu.Unlock()
	}
	cache, err := newGatewayCache(4, 10*time.Minute, clock)
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	t.Cleanup(cache.Close)

	gateways := make(map[string]*fakeKubeGateway, 5)
	for index, id := range []string{"clu_1", "clu_2", "clu_3", "clu_4"} {
		gateway := &fakeKubeGateway{}
		gateways[id] = gateway
		if _, err := cache.Get(context.Background(), id, testGatewayFingerprint(byte(index+1)), func(context.Context) (KubeGateway, error) {
			return gateway, nil
		}); err != nil {
			t.Fatalf("Get(%s) error = %v", id, err)
		}
		advance(time.Second)
	}

	cache.Reconcile(resourceguard.Snapshot{Adaptive: true, Pressure: resourceguard.PressureConstrained})
	if snapshot := cache.Snapshot(); snapshot.Entries != 2 || snapshot.Capacity != 2 || snapshot.Maximum != 4 {
		t.Fatalf("constrained Snapshot() = %#v", snapshot)
	}
	if gateways["clu_1"].idleCloseCalls.Load() != 1 || gateways["clu_2"].idleCloseCalls.Load() != 1 {
		t.Fatal("constrained reconciliation did not evict least recently used gateways")
	}

	cache.Reconcile(resourceguard.Snapshot{Adaptive: true, Pressure: resourceguard.PressureCritical})
	if snapshot := cache.Snapshot(); snapshot.Entries != 1 || snapshot.Capacity != 1 {
		t.Fatalf("critical Snapshot() = %#v", snapshot)
	}
	if gateways["clu_3"].idleCloseCalls.Load() != 1 {
		t.Fatal("critical reconciliation did not shrink cache to one gateway")
	}

	cache.Reconcile(resourceguard.Snapshot{Adaptive: true, Pressure: resourceguard.PressureNormal})
	advance(11 * time.Minute)
	fifth := &fakeKubeGateway{}
	gateways["clu_5"] = fifth
	if _, err := cache.Get(context.Background(), "clu_5", testGatewayFingerprint(5), func(context.Context) (KubeGateway, error) {
		return fifth, nil
	}); err != nil {
		t.Fatalf("Get(clu_5) error = %v", err)
	}
	if gateways["clu_4"].idleCloseCalls.Load() != 1 {
		t.Fatal("idle gateway was not evicted after TTL")
	}
	if snapshot := cache.Snapshot(); snapshot.Entries != 1 || snapshot.Capacity != 4 {
		t.Fatalf("recovered Snapshot() = %#v", snapshot)
	}
}

func TestGatewayCacheRejectsInvalidConfigurationAndInvalidatedBuild(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		maximum int
		ttl     time.Duration
	}{
		{maximum: 0, ttl: time.Minute},
		{maximum: 65, ttl: time.Minute},
		{maximum: 1, ttl: time.Second},
		{maximum: 1, ttl: 2 * time.Hour},
	} {
		if _, err := newGatewayCache(test.maximum, test.ttl, time.Now); err == nil {
			t.Fatalf("newGatewayCache(%d, %s) error = nil", test.maximum, test.ttl)
		}
	}

	cache, err := newGatewayCache(2, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	t.Cleanup(cache.Close)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	gateway := &fakeKubeGateway{}
	result := make(chan error, 1)
	go func() {
		_, getErr := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), func(context.Context) (KubeGateway, error) {
			started <- struct{}{}
			<-release
			return gateway, nil
		})
		result <- getErr
	}()
	<-started
	cache.Invalidate("clu_1")
	close(release)
	if err := <-result; !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("invalidated build error = %v, want invalid state", err)
	}
	if gateway.idleCloseCalls.Load() != 1 || cache.Snapshot().Entries != 0 {
		t.Fatalf("gateway closes = %d, snapshot = %#v", gateway.idleCloseCalls.Load(), cache.Snapshot())
	}
}

func TestGatewayCacheCloseReleasesEntriesAndRejectsNewBuilds(t *testing.T) {
	t.Parallel()

	cache, err := newGatewayCache(2, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("newGatewayCache() error = %v", err)
	}
	gateway := &fakeKubeGateway{}
	if _, err := cache.Get(context.Background(), "clu_1", testGatewayFingerprint(1), func(context.Context) (KubeGateway, error) {
		return gateway, nil
	}); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	cache.Close()
	cache.Close()
	if gateway.idleCloseCalls.Load() != 1 || cache.Snapshot().Entries != 0 {
		t.Fatalf("gateway closes = %d, snapshot = %#v", gateway.idleCloseCalls.Load(), cache.Snapshot())
	}
	if _, err := cache.Get(context.Background(), "clu_2", testGatewayFingerprint(2), func(context.Context) (KubeGateway, error) {
		return &fakeKubeGateway{}, nil
	}); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("Get() after Close() error = %v, want invalid state", err)
	}
}

func TestClusterGatewayFingerprintTracksOnlyConnectionMaterial(t *testing.T) {
	t.Parallel()

	cluster := domain.Cluster{
		ID: "clu_1", Server: "https://api.example.com", CACertCiphertext: "encrypted-ca", BearerTokenCiphertext: "encrypted-token",
		Status: domain.ClusterConnected, UpdatedAt: time.Date(2026, 7, 27, 9, 0, 0, 0, time.UTC),
	}
	initial := clusterGatewayFingerprint(cluster)
	cluster.Status = domain.ClusterDegraded
	cluster.UpdatedAt = cluster.UpdatedAt.Add(time.Hour)
	if got := clusterGatewayFingerprint(cluster); got != initial {
		t.Fatal("status-only change invalidated gateway fingerprint")
	}
	cluster.BearerTokenCiphertext = "rotated-encrypted-token"
	if got := clusterGatewayFingerprint(cluster); got == initial {
		t.Fatal("credential change did not invalidate gateway fingerprint")
	}
}

func testGatewayFingerprint(seed byte) gatewayFingerprint {
	var fingerprint gatewayFingerprint
	fingerprint[0] = seed
	return fingerprint
}
