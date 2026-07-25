package resourceguard

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
)

const maxMetricFileBytes = 64 * 1024

type systemSampler struct {
	readFile      func(string) ([]byte, error)
	cpuCount      func() int
	processMemory func() (uint64, uint64, bool)
}

func NewSystemSampler() Sampler {
	return systemSampler{
		readFile:      readSmallFile,
		cpuCount:      func() int { return runtime.GOMAXPROCS(0) },
		processMemory: processMemoryUsage,
	}
}

func (s systemSampler) Sample() Sample {
	memoryRatio := s.cgroupMemoryRatio()
	if memoryRatio == nil {
		memoryRatio = s.procMemoryRatio()
	}
	if memoryRatio == nil && s.processMemory != nil {
		if used, limit, ok := s.processMemory(); ok {
			memoryRatio = divide(used, limit)
		}
	}
	return Sample{MemoryRatio: memoryRatio, LoadRatio: s.procLoadRatio()}
}

func (s systemSampler) cgroupMemoryRatio() *float64 {
	paths := [][2]string{
		{"/sys/fs/cgroup/memory.current", "/sys/fs/cgroup/memory.max"},
		{"/sys/fs/cgroup/memory/memory.usage_in_bytes", "/sys/fs/cgroup/memory/memory.limit_in_bytes"},
	}
	for _, pair := range paths {
		used, usedErr := s.readUint(pair[0])
		limitPayload, limitErr := s.readFile(pair[1])
		if usedErr != nil || limitErr != nil || string(bytes.TrimSpace(limitPayload)) == "max" {
			continue
		}
		limit, err := strconv.ParseUint(string(bytes.TrimSpace(limitPayload)), 10, 64)
		if err != nil || limit == 0 || limit > 1<<60 {
			continue
		}
		return divide(used, limit)
	}
	return nil
}

func (s systemSampler) procMemoryRatio() *float64 {
	payload, err := s.readFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	values := make(map[string]uint64, 2)
	for _, line := range strings.Split(string(payload), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		if key != "MemTotal" && key != "MemAvailable" {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr == nil {
			values[key] = value
		}
	}
	total, totalFound := values["MemTotal"]
	available, availableFound := values["MemAvailable"]
	if !totalFound || !availableFound || total == 0 || available > total {
		return nil
	}
	return divide(total-available, total)
}

func (s systemSampler) procLoadRatio() *float64 {
	payload, err := s.readFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(payload))
	count := s.cpuCount()
	if len(fields) == 0 || count < 1 {
		return nil
	}
	load, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(load) || math.IsInf(load, 0) || load < 0 {
		return nil
	}
	ratio := load / float64(count)
	return &ratio
}

func (s systemSampler) readUint(path string) (uint64, error) {
	payload, err := s.readFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(string(bytes.TrimSpace(payload)), 10, 64)
}

func divide(numerator, denominator uint64) *float64 {
	if denominator == 0 {
		return nil
	}
	ratio := float64(numerator) / float64(denominator)
	return &ratio
}

func readSmallFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maxMetricFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > maxMetricFileBytes {
		return nil, errors.New("metric file exceeds size limit")
	}
	return payload, nil
}

func processMemoryUsage() (uint64, uint64, bool) {
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
		{Name: "/gc/gomemlimit:bytes"},
	}
	metrics.Read(samples)
	for _, sample := range samples {
		if sample.Value.Kind() != metrics.KindUint64 {
			return 0, 0, false
		}
	}
	total := samples[0].Value.Uint64()
	released := samples[1].Value.Uint64()
	limit := samples[2].Value.Uint64()
	if released > total || limit == 0 || limit > 1<<60 {
		return 0, 0, false
	}
	return total - released, limit, true
}
