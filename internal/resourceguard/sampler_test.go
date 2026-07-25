package resourceguard

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemSamplerUsesCgroupMemoryAndNormalizedLoad(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"/sys/fs/cgroup/memory.current": "840\n",
		"/sys/fs/cgroup/memory.max":     "1000\n",
		"/proc/loadavg":                 "1.50 1.20 1.00 2/100 99\n",
	}
	sampler := systemSampler{
		readFile: func(path string) ([]byte, error) {
			value, found := files[path]
			if !found {
				return nil, errors.New("missing fixture")
			}
			return []byte(value), nil
		},
		cpuCount: func() int { return 2 },
	}

	sample := sampler.Sample()
	if sample.MemoryRatio == nil || *sample.MemoryRatio != 0.84 {
		t.Fatalf("memory ratio = %#v", sample.MemoryRatio)
	}
	if sample.LoadRatio == nil || *sample.LoadRatio != 0.75 {
		t.Fatalf("load ratio = %#v", sample.LoadRatio)
	}
}

func TestSystemSamplerFallsBackToProcMemory(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"/proc/meminfo": "MemTotal:       1000 kB\nMemFree:         100 kB\nMemAvailable:    250 kB\n",
		"/proc/loadavg": "invalid\n",
	}
	sampler := systemSampler{
		readFile: func(path string) ([]byte, error) {
			value, found := files[path]
			if !found {
				return nil, errors.New("not available")
			}
			return []byte(value), nil
		},
		cpuCount: func() int { return 0 },
	}

	sample := sampler.Sample()
	if sample.MemoryRatio == nil || *sample.MemoryRatio != 0.75 {
		t.Fatalf("memory ratio = %#v", sample.MemoryRatio)
	}
	if sample.LoadRatio != nil {
		t.Fatalf("load ratio = %#v, want nil", sample.LoadRatio)
	}
}

func TestSystemSamplerIgnoresInvalidOrUnlimitedSignals(t *testing.T) {
	t.Parallel()

	sampler := systemSampler{
		readFile: func(path string) ([]byte, error) {
			switch path {
			case "/sys/fs/cgroup/memory.current":
				return []byte("100"), nil
			case "/sys/fs/cgroup/memory.max":
				return []byte("max"), nil
			default:
				return nil, errors.New("not available")
			}
		},
		cpuCount: func() int { return 2 },
	}

	sample := sampler.Sample()
	if sample.MemoryRatio != nil || sample.LoadRatio != nil {
		t.Fatalf("Sample() = %#v", sample)
	}
}

func TestSystemSamplerFallsBackToBoundedProcessMemory(t *testing.T) {
	t.Parallel()

	sampler := systemSampler{
		readFile: func(string) ([]byte, error) { return nil, errors.New("not available") },
		cpuCount: func() int { return 0 },
		processMemory: func() (uint64, uint64, bool) {
			return 400, 1000, true
		},
	}
	sample := sampler.Sample()
	if sample.MemoryRatio == nil || *sample.MemoryRatio != 0.4 {
		t.Fatalf("memory ratio = %#v", sample.MemoryRatio)
	}
}

func TestReadSmallFileEnforcesMetricSizeLimit(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "metric")
	if err := os.WriteFile(path, []byte("42\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	payload, err := readSmallFile(path)
	if err != nil || string(payload) != "42\n" {
		t.Fatalf("readSmallFile() = %q, %v", payload, err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), maxMetricFileBytes+1), 0o600); err != nil {
		t.Fatalf("WriteFile(oversized) error = %v", err)
	}
	if _, err := readSmallFile(path); err == nil {
		t.Fatal("readSmallFile() accepted an oversized metric")
	}
}

func TestNewSystemSamplerAndZeroDivisionAreSafe(t *testing.T) {
	t.Parallel()

	if sampler := NewSystemSampler(); sampler == nil {
		t.Fatal("NewSystemSampler() = nil")
	}
	if ratio := divide(1, 0); ratio != nil {
		t.Fatalf("divide(1, 0) = %v", *ratio)
	}
}
