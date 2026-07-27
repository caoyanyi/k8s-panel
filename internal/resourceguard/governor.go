package resourceguard

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"
)

type Pressure string

const (
	PressureUnknown     Pressure = "unknown"
	PressureNormal      Pressure = "normal"
	PressureConstrained Pressure = "constrained"
	PressureCritical    Pressure = "critical"
)

const (
	DefaultHighWatermark     = 0.80
	DefaultCriticalWatermark = 0.95
)

type Sample struct {
	MemoryRatio *float64
	LoadRatio   *float64
}

type Sampler interface {
	Sample() Sample
}

type Snapshot struct {
	Adaptive          bool      `json:"adaptive"`
	Pressure          Pressure  `json:"pressure"`
	MemoryRatio       *float64  `json:"memory_ratio,omitempty"`
	LoadRatio         *float64  `json:"load_ratio,omitempty"`
	ActiveOperations  int       `json:"active_operations"`
	OperationLimit    int       `json:"operation_limit"`
	MaximumOperations int       `json:"maximum_operations"`
	SampledAt         time.Time `json:"sampled_at"`
}

type Config struct {
	Enabled           bool
	MaxConcurrent     int
	HighWatermark     float64
	CriticalWatermark float64
	RetryInterval     time.Duration
	Sampler           Sampler
	Clock             func() time.Time
}

type Governor struct {
	enabled           bool
	maxConcurrent     int
	highWatermark     float64
	criticalWatermark float64
	retryInterval     time.Duration
	sampler           Sampler
	clock             func() time.Time

	mu     sync.Mutex
	active int
}

func New(config Config) (*Governor, error) {
	if config.MaxConcurrent < 1 {
		return nil, errors.New("maximum concurrent operations must be positive")
	}
	if config.HighWatermark <= 0 || config.HighWatermark >= 1 {
		return nil, errors.New("high watermark must be between zero and one")
	}
	if config.CriticalWatermark <= config.HighWatermark || config.CriticalWatermark > 1 {
		return nil, errors.New("critical watermark must be above the high watermark and at most one")
	}
	if config.RetryInterval <= 0 {
		config.RetryInterval = 2 * time.Second
	}
	if config.Sampler == nil {
		config.Sampler = NewSystemSampler()
	}
	if config.Clock == nil {
		config.Clock = time.Now
	}
	return &Governor{
		enabled:           config.Enabled,
		maxConcurrent:     config.MaxConcurrent,
		highWatermark:     config.HighWatermark,
		criticalWatermark: config.CriticalWatermark,
		retryInterval:     config.RetryInterval,
		sampler:           config.Sampler,
		clock:             config.Clock,
	}, nil
}

func (g *Governor) Acquire(ctx context.Context) (Snapshot, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		if snapshot, release, ok := g.TryAcquire(); ok {
			return snapshot, release, nil
		}

		timer := time.NewTimer(g.retryInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return Snapshot{}, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *Governor) TryAcquire() (Snapshot, func(), bool) {
	sample := g.sampler.Sample()
	g.mu.Lock()
	snapshot := g.snapshotLocked(sample)
	if g.active >= snapshot.OperationLimit {
		g.mu.Unlock()
		return snapshot, nil, false
	}
	g.active++
	snapshot.ActiveOperations = g.active
	g.mu.Unlock()

	var once sync.Once
	return snapshot, func() {
		once.Do(func() {
			g.mu.Lock()
			if g.active > 0 {
				g.active--
			}
			g.mu.Unlock()
		})
	}, true
}

func (g *Governor) Snapshot() Snapshot {
	sample := g.sampler.Sample()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.snapshotLocked(sample)
}

func (g *Governor) snapshotLocked(sample Sample) Snapshot {
	memoryRatio := safeRatio(sample.MemoryRatio)
	loadRatio := safeRatio(sample.LoadRatio)
	pressure, limit := g.capacity(memoryRatio, loadRatio)
	return Snapshot{
		Adaptive:          g.enabled,
		Pressure:          pressure,
		MemoryRatio:       memoryRatio,
		LoadRatio:         loadRatio,
		ActiveOperations:  g.active,
		OperationLimit:    limit,
		MaximumOperations: g.maxConcurrent,
		SampledAt:         g.clock().UTC(),
	}
}

func (g *Governor) capacity(memoryRatio, loadRatio *float64) (Pressure, int) {
	if !g.enabled {
		return PressureNormal, g.maxConcurrent
	}
	maximum, available := maximumRatio(memoryRatio, loadRatio)
	if !available {
		return PressureUnknown, g.maxConcurrent
	}
	if maximum >= g.criticalWatermark {
		return PressureCritical, 0
	}
	if maximum >= g.highWatermark {
		return PressureConstrained, max(1, (g.maxConcurrent+1)/2)
	}
	return PressureNormal, g.maxConcurrent
}

func safeRatio(value *float64) *float64 {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
		return nil
	}
	cloned := *value
	return &cloned
}

func maximumRatio(left, right *float64) (float64, bool) {
	switch {
	case left == nil && right == nil:
		return 0, false
	case left == nil:
		return *right, true
	case right == nil:
		return *left, true
	default:
		return max(*left, *right), true
	}
}
