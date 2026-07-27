package resourceguard

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestGovernorAdaptsOperationCapacityToPressure(t *testing.T) {
	t.Parallel()

	sampler := &mutableSampler{sample: Sample{MemoryRatio: ratio(0.40), LoadRatio: ratio(0.30)}}
	governor, err := New(Config{
		Enabled:           true,
		MaxConcurrent:     4,
		HighWatermark:     0.80,
		CriticalWatermark: 0.95,
		RetryInterval:     time.Millisecond,
		Sampler:           sampler,
		Clock:             func() time.Time { return time.Date(2026, 7, 25, 8, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	releases := acquirePermits(t, governor, 4)
	status := governor.Snapshot()
	if status.Pressure != PressureNormal || status.OperationLimit != 4 || status.ActiveOperations != 4 {
		t.Fatalf("normal status = %#v", status)
	}
	for _, release := range releases {
		release()
	}

	sampler.Set(Sample{MemoryRatio: ratio(0.84), LoadRatio: ratio(0.20)})
	releases = acquirePermits(t, governor, 2)
	status = governor.Snapshot()
	if status.Pressure != PressureConstrained || status.OperationLimit != 2 || status.ActiveOperations != 2 {
		t.Fatalf("constrained status = %#v", status)
	}
	assertAcquireTimesOut(t, governor)
	for _, release := range releases {
		release()
	}

	sampler.Set(Sample{MemoryRatio: ratio(0.97), LoadRatio: ratio(0.20)})
	status = governor.Snapshot()
	if status.Pressure != PressureCritical || status.OperationLimit != 0 {
		t.Fatalf("critical status = %#v", status)
	}
	assertAcquireTimesOut(t, governor)

	sampler.Set(Sample{MemoryRatio: ratio(0.30), LoadRatio: ratio(0.20)})
	_, release, err := governor.Acquire(context.Background())
	if err != nil {
		t.Fatalf("Acquire() after recovery error = %v", err)
	}
	release()
	release()
	if active := governor.Snapshot().ActiveOperations; active != 0 {
		t.Fatalf("idempotent release active operations = %d", active)
	}
}

func TestGovernorHandlesUnavailableMetricsAndDisabledAdaptation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		sample   Sample
		pressure Pressure
	}{
		{name: "unavailable metrics", enabled: true, sample: Sample{}, pressure: PressureUnknown},
		{name: "load metric only", enabled: true, sample: Sample{LoadRatio: ratio(0.2)}, pressure: PressureNormal},
		{name: "disabled adaptation", enabled: false, sample: Sample{MemoryRatio: ratio(1)}, pressure: PressureNormal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			governor, err := New(Config{
				Enabled: true, MaxConcurrent: 2, HighWatermark: 0.80, CriticalWatermark: 0.95,
				RetryInterval: time.Millisecond, Sampler: &mutableSampler{sample: tt.sample},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			governor.enabled = tt.enabled
			status := governor.Snapshot()
			if status.Pressure != tt.pressure || status.OperationLimit != 2 {
				t.Fatalf("Snapshot() = %#v", status)
			}
		})
	}
}

func TestGovernorTryAcquireIsNonBlockingAndReleasesCapacity(t *testing.T) {
	t.Parallel()

	governor, err := New(Config{
		Enabled: false, MaxConcurrent: 1, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticSampler{sample: Sample{MemoryRatio: ratio(0.20)}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, release, ok := governor.TryAcquire()
	if !ok || release == nil || status.ActiveOperations != 1 {
		t.Fatalf("first TryAcquire() = %#v, release nil = %t, ok = %t", status, release == nil, ok)
	}
	full, blockedRelease, ok := governor.TryAcquire()
	if ok || blockedRelease != nil || full.ActiveOperations != 1 || full.OperationLimit != 1 {
		t.Fatalf("full TryAcquire() = %#v, release nil = %t, ok = %t", full, blockedRelease == nil, ok)
	}

	release()
	release()
	status, release, ok = governor.TryAcquire()
	if !ok || status.ActiveOperations != 1 {
		t.Fatalf("TryAcquire() after release = %#v, ok = %t", status, ok)
	}
	release()
}

func TestGovernorTryAcquireRejectsCriticalPressure(t *testing.T) {
	t.Parallel()

	governor, err := New(Config{
		Enabled: true, MaxConcurrent: 4, HighWatermark: 0.80, CriticalWatermark: 0.95,
		Sampler: staticSampler{sample: Sample{LoadRatio: ratio(0.97)}},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	status, release, ok := governor.TryAcquire()
	if ok || release != nil || status.Pressure != PressureCritical || status.OperationLimit != 0 {
		t.Fatalf("TryAcquire() = %#v, release nil = %t, ok = %t", status, release == nil, ok)
	}
}

func TestGovernorRejectsInvalidConfiguration(t *testing.T) {
	t.Parallel()

	tests := []Config{
		{MaxConcurrent: 0, HighWatermark: 0.8, CriticalWatermark: 0.95},
		{MaxConcurrent: 2, HighWatermark: 0, CriticalWatermark: 0.95},
		{MaxConcurrent: 2, HighWatermark: 0.95, CriticalWatermark: 0.90},
	}
	for _, config := range tests {
		if _, err := New(config); err == nil {
			t.Fatalf("New(%#v) error = nil", config)
		}
	}
}

func acquirePermits(t *testing.T, governor *Governor, count int) []func() {
	t.Helper()
	releases := make([]func(), 0, count)
	for range count {
		_, release, err := governor.Acquire(context.Background())
		if err != nil {
			t.Fatalf("Acquire() error = %v", err)
		}
		releases = append(releases, release)
	}
	return releases
}

func assertAcquireTimesOut(t *testing.T, governor *Governor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	if _, _, err := governor.Acquire(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Acquire() error = %v, want deadline exceeded", err)
	}
}

func ratio(value float64) *float64 { return &value }

type mutableSampler struct {
	mu     sync.RWMutex
	sample Sample
}

type staticSampler struct{ sample Sample }

func (s staticSampler) Sample() Sample { return s.sample }

func (s *mutableSampler) Sample() Sample {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sample
}

func (s *mutableSampler) Set(sample Sample) {
	s.mu.Lock()
	s.sample = sample
	s.mu.Unlock()
}
