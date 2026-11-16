package system

import (
	"context"
	"runtime"
	"sync"
	"time"
)

const defaultPrimeWait = 200 * time.Millisecond

// Snapshot captures point-in-time host resource telemetry without process or path details.
type Snapshot struct {
	CPUUsagePermille uint32
	CPUCores         uint32
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	SwapUsedBytes    uint64
	SwapTotalBytes   uint64
	Load1mMilli      uint32
	HasLoad1m        bool
	UptimeSeconds    uint64
	ObservedAt       time.Time
}

type cpuSample struct {
	idle  uint64
	total uint64
}

// Source collects lightweight host CPU, memory, swap, load, and uptime metrics.
type Source struct {
	mu        sync.Mutex
	hasPrev   bool
	prev      cpuSample
	procDir   string
	now       func() time.Time
	primeWait time.Duration
}

// New constructs a host system metrics source using default OS interfaces.
func New() *Source {
	return &Source{
		procDir:   "/proc",
		now:       time.Now,
		primeWait: defaultPrimeWait,
	}
}

// Fetch reads current host resource usage. On the first call, it waits briefly
// between two CPU counter reads so the returned CPU percentage is already meaningful.
func (s *Source) Fetch(ctx context.Context) (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.hasPrev {
		initial, err := s.readCPU()
		if err != nil {
			return Snapshot{}, err
		}
		s.prev = initial
		s.hasPrev = true
		if s.primeWait > 0 {
			timer := time.NewTimer(s.primeWait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return Snapshot{}, ctx.Err()
			case <-timer.C:
			}
		}
	}

	current, err := s.readCPU()
	if err != nil {
		return Snapshot{}, err
	}
	cpuPermille := computeCPUPermille(s.prev, current)
	s.prev = current

	metrics, err := s.readHostMetrics()
	if err != nil {
		return Snapshot{}, err
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	return Snapshot{
		CPUUsagePermille: cpuPermille,
		CPUCores:         uint32(cores),
		MemoryUsedBytes:  metrics.memUsed,
		MemoryTotalBytes: metrics.memTotal,
		SwapUsedBytes:    metrics.swapUsed,
		SwapTotalBytes:   metrics.swapTotal,
		Load1mMilli:      metrics.load1mMilli,
		HasLoad1m:        metrics.hasLoad1m,
		UptimeSeconds:    metrics.uptimeSeconds,
		ObservedAt:       s.now().UTC(),
	}, nil
}

func computeCPUPermille(prev, curr cpuSample) uint32 {
	if curr.total <= prev.total {
		return 0
	}
	totalDelta := curr.total - prev.total
	var idleDelta uint64
	if curr.idle > prev.idle {
		idleDelta = curr.idle - prev.idle
	}
	if idleDelta >= totalDelta {
		return 0
	}
	busy := totalDelta - idleDelta
	permille := (busy*1000 + totalDelta/2) / totalDelta
	if permille > 1000 {
		return 1000
	}
	return uint32(permille)
}
