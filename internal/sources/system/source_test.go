package system

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestComputeCPUPermille(t *testing.T) {
	t.Parallel()
	prev := cpuSample{idle: 1000, total: 2000}
	curr := cpuSample{idle: 1300, total: 2500}
	// total delta = 500, idle delta = 300, busy = 200 -> 400 permille (40.0%)
	if got := computeCPUPermille(prev, curr); got != 400 {
		t.Fatalf("computeCPUPermille = %d, want 400", got)
	}
	if got := computeCPUPermille(curr, curr); got != 0 {
		t.Fatalf("equal samples = %d, want 0", got)
	}
	if got := computeCPUPermille(prev, cpuSample{idle: 2000, total: 2500}); got != 0 {
		t.Fatalf("idle > total delta = %d, want 0", got)
	}
}

func TestSourceFetchCurrentHost(t *testing.T) {
	src := New()
	src.primeWait = 10 * time.Millisecond
	snap, err := src.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.CPUCores < 1 || snap.CPUUsagePermille > 1000 || snap.ObservedAt.IsZero() {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		if snap.MemoryTotalBytes == 0 || snap.MemoryUsedBytes > snap.MemoryTotalBytes {
			t.Fatalf("unexpected memory stats: %+v", snap)
		}
	}
}

func TestLinuxProcParsingFixtures(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", "cpu  100 0 50 850 0 0 0 0 0 0\ncpu0 50 0 25 425 0 0 0 0 0 0\n")
	write("meminfo", "MemTotal:       16384000 kB\nMemFree:         2000000 kB\nMemAvailable:   10384000 kB\nSwapTotal:       2097152 kB\nSwapFree:        1048576 kB\n")
	write("loadavg", "1.42 1.15 0.98 2/412 12345\n")
	write("uptime", "3600.75 14000.00\n")

	prev, err := parseProcStatFile(filepath.Join(dir, "stat"))
	if err != nil {
		t.Fatalf("parseProcStatFile: %v", err)
	}
	memUsed, memTotal, swapUsed, swapTotal, err := parseProcMeminfoFile(filepath.Join(dir, "meminfo"))
	if err != nil {
		t.Fatalf("parseProcMeminfoFile: %v", err)
	}
	if memTotal != 16384000*1024 || memUsed != 6000000*1024 {
		t.Fatalf("unexpected memory: used=%d total=%d", memUsed, memTotal)
	}
	if swapTotal != 2097152*1024 || swapUsed != 1048576*1024 {
		t.Fatalf("unexpected swap: used=%d total=%d", swapUsed, swapTotal)
	}
	load1m, hasLoad := parseProcLoadavgFile(filepath.Join(dir, "loadavg"))
	uptime := parseProcUptimeFile(filepath.Join(dir, "uptime"))
	if !hasLoad || load1m != 1420 || uptime != 3600 {
		t.Fatalf("unexpected load/uptime: has=%v load=%d uptime=%d", hasLoad, load1m, uptime)
	}
	write("stat", "cpu  200 0 150 1650 0 0 0 0 0 0\n")
	curr, err := parseProcStatFile(filepath.Join(dir, "stat"))
	if err != nil {
		t.Fatalf("parseProcStatFile2: %v", err)
	}
	if got := computeCPUPermille(prev, curr); got != 200 {
		t.Fatalf("CPUUsagePermille = %d, want 200", got)
	}
}
