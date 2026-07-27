package platform

import (
	"context"
	"crypto/sha256"
	"errors"
	"sync"
	"time"

	"github.com/caoyanyi/k8s-panel/internal/domain"
	"github.com/caoyanyi/k8s-panel/internal/resourceguard"
)

const (
	defaultGatewayCacheSize = 8
	maximumGatewayCacheSize = 64
	defaultGatewayCacheTTL  = 10 * time.Minute
	minimumGatewayCacheTTL  = time.Minute
	maximumGatewayCacheTTL  = time.Hour
)

type gatewayFingerprint [sha256.Size]byte

type gatewayCacheEntry struct {
	fingerprint gatewayFingerprint
	gateway     KubeGateway
	lastUsed    time.Time
}

type gatewayCacheBuild struct {
	fingerprint gatewayFingerprint
	done        chan struct{}
	err         error
}

type gatewayCacheSnapshot struct {
	Entries  int
	Capacity int
	Maximum  int
	Building int
}

type gatewayCache struct {
	mu          sync.Mutex
	entries     map[string]gatewayCacheEntry
	builds      map[string]*gatewayCacheBuild
	generations map[string]uint64
	maximum     int
	capacity    int
	ttl         time.Duration
	clock       func() time.Time
	closed      bool
}

func newGatewayCache(maximum int, ttl time.Duration, clock func() time.Time) (*gatewayCache, error) {
	if maximum < 1 || maximum > maximumGatewayCacheSize {
		return nil, errors.New("Kubernetes client cache size must be between 1 and 64")
	}
	if ttl < minimumGatewayCacheTTL || ttl > maximumGatewayCacheTTL {
		return nil, errors.New("Kubernetes client cache TTL must be between 1m and 1h")
	}
	if clock == nil {
		clock = time.Now
	}
	return &gatewayCache{
		entries:     make(map[string]gatewayCacheEntry, maximum),
		builds:      make(map[string]*gatewayCacheBuild, maximum),
		generations: make(map[string]uint64, maximum),
		maximum:     maximum,
		capacity:    maximum,
		ttl:         ttl,
		clock:       clock,
	}, nil
}

func (c *gatewayCache) Get(
	ctx context.Context,
	clusterID string,
	fingerprint gatewayFingerprint,
	build func(context.Context) (KubeGateway, error),
) (KubeGateway, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if build == nil {
		return nil, errors.New("Kubernetes gateway builder is required")
	}

	for {
		now := c.clock().UTC()
		var retired []KubeGateway

		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return nil, domain.ErrInvalidState
		}
		retired = append(retired, c.expireLocked(now)...)
		if entry, found := c.entries[clusterID]; found {
			if entry.fingerprint == fingerprint {
				entry.lastUsed = now
				c.entries[clusterID] = entry
				c.mu.Unlock()
				closeIdleGateways(retired)
				return entry.gateway, nil
			}
			delete(c.entries, clusterID)
			retired = append(retired, entry.gateway)
		}
		if pending, found := c.builds[clusterID]; found {
			c.mu.Unlock()
			closeIdleGateways(retired)
			select {
			case <-pending.done:
				if pending.fingerprint == fingerprint && pending.err != nil {
					return nil, pending.err
				}
				continue
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		generation := c.generations[clusterID]
		pending := &gatewayCacheBuild{fingerprint: fingerprint, done: make(chan struct{})}
		c.builds[clusterID] = pending
		c.mu.Unlock()
		closeIdleGateways(retired)

		gateway, buildErr := build(ctx)
		retired = retired[:0]
		c.mu.Lock()
		delete(c.builds, clusterID)
		resultErr := buildErr
		switch {
		case buildErr != nil:
			if gateway != nil {
				retired = append(retired, gateway)
			}
		case gateway == nil:
			resultErr = errors.New("Kubernetes gateway builder returned no gateway")
		case c.closed || c.generations[clusterID] != generation:
			resultErr = domain.ErrInvalidState
			retired = append(retired, gateway)
		default:
			if existing, found := c.entries[clusterID]; found {
				delete(c.entries, clusterID)
				retired = append(retired, existing.gateway)
			}
			c.entries[clusterID] = gatewayCacheEntry{
				fingerprint: fingerprint,
				gateway:     gateway,
				lastUsed:    c.clock().UTC(),
			}
			retired = append(retired, c.evictLocked()...)
		}
		pending.err = resultErr
		close(pending.done)
		c.mu.Unlock()
		closeIdleGateways(retired)
		if resultErr != nil {
			return nil, resultErr
		}
		return gateway, nil
	}
}

func (c *gatewayCache) Invalidate(clusterID string) {
	var retired []KubeGateway
	c.mu.Lock()
	c.generations[clusterID]++
	if entry, found := c.entries[clusterID]; found {
		delete(c.entries, clusterID)
		retired = append(retired, entry.gateway)
	}
	c.mu.Unlock()
	closeIdleGateways(retired)
}

func (c *gatewayCache) Reconcile(snapshot resourceguard.Snapshot) {
	var retired []KubeGateway
	c.mu.Lock()
	if !c.closed {
		c.capacity = adaptiveGatewayCacheCapacity(c.maximum, snapshot)
		retired = append(retired, c.expireLocked(c.clock().UTC())...)
		retired = append(retired, c.evictLocked()...)
	}
	c.mu.Unlock()
	closeIdleGateways(retired)
}

func (c *gatewayCache) Snapshot() gatewayCacheSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return gatewayCacheSnapshot{
		Entries:  len(c.entries),
		Capacity: c.capacity,
		Maximum:  c.maximum,
		Building: len(c.builds),
	}
}

func (c *gatewayCache) Close() {
	var retired []KubeGateway
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		for clusterID, entry := range c.entries {
			delete(c.entries, clusterID)
			retired = append(retired, entry.gateway)
		}
	}
	c.mu.Unlock()
	closeIdleGateways(retired)
}

func (c *gatewayCache) expireLocked(now time.Time) []KubeGateway {
	retired := make([]KubeGateway, 0)
	for clusterID, entry := range c.entries {
		if now.Before(entry.lastUsed.Add(c.ttl)) {
			continue
		}
		delete(c.entries, clusterID)
		retired = append(retired, entry.gateway)
	}
	return retired
}

func (c *gatewayCache) evictLocked() []KubeGateway {
	retired := make([]KubeGateway, 0)
	for len(c.entries) > c.capacity {
		var oldestID string
		var oldestTime time.Time
		found := false
		for clusterID, entry := range c.entries {
			if !found || entry.lastUsed.Before(oldestTime) ||
				(entry.lastUsed.Equal(oldestTime) && clusterID < oldestID) {
				oldestID = clusterID
				oldestTime = entry.lastUsed
				found = true
			}
		}
		if !found {
			break
		}
		retired = append(retired, c.entries[oldestID].gateway)
		delete(c.entries, oldestID)
	}
	return retired
}

func adaptiveGatewayCacheCapacity(maximum int, snapshot resourceguard.Snapshot) int {
	if !snapshot.Adaptive {
		return maximum
	}
	switch snapshot.Pressure {
	case resourceguard.PressureConstrained:
		return max(1, (maximum+1)/2)
	case resourceguard.PressureCritical:
		return 1
	default:
		return maximum
	}
}

func closeIdleGateways(gateways []KubeGateway) {
	for _, gateway := range gateways {
		if closer, ok := gateway.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func clusterGatewayFingerprint(cluster domain.Cluster) gatewayFingerprint {
	return sha256.Sum256([]byte(
		cluster.Server + "\x00" + cluster.CACertCiphertext + "\x00" + cluster.BearerTokenCiphertext,
	))
}
