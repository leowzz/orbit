//go:build darwin && cgo

package system

/*
#include <mach/mach.h>
#include <mach/mach_host.h>
#include <sys/sysctl.h>
#include <sys/time.h>
#include <sys/types.h>
#include <sys/resource.h>

static int orbit_read_cpu(uint64_t *idle, uint64_t *total) {
	host_cpu_load_info_data_t cpu;
	mach_msg_type_number_t count = HOST_CPU_LOAD_INFO_COUNT;
	if (host_statistics(mach_host_self(), HOST_CPU_LOAD_INFO, (host_info_t)&cpu, &count) != KERN_SUCCESS) {
		return -1;
	}
	uint64_t u = cpu.cpu_ticks[CPU_STATE_USER];
	uint64_t s = cpu.cpu_ticks[CPU_STATE_SYSTEM];
	uint64_t n = cpu.cpu_ticks[CPU_STATE_NICE];
	uint64_t i = cpu.cpu_ticks[CPU_STATE_IDLE];
	*idle = i;
	*total = u + s + n + i;
	return 0;
}

static int orbit_read_mem(uint64_t *used, uint64_t *total) {
	uint64_t memsize = 0;
	size_t len = sizeof(memsize);
	if (sysctlbyname("hw.memsize", &memsize, &len, NULL, 0) != 0) {
		return -1;
	}
	vm_size_t page_size = 0;
	if (host_page_size(mach_host_self(), &page_size) != KERN_SUCCESS) {
		return -1;
	}
	vm_statistics64_data_t vm;
	mach_msg_type_number_t count = HOST_VM_INFO64_COUNT;
	if (host_statistics64(mach_host_self(), HOST_VM_INFO64, (host_info64_t)&vm, &count) != KERN_SUCCESS) {
		return -1;
	}
	uint64_t u = ((uint64_t)vm.active_count + (uint64_t)vm.wire_count + (uint64_t)vm.compressor_page_count) * (uint64_t)page_size;
	if (u > memsize) {
		u = memsize;
	}
	*used = u;
	*total = memsize;
	return 0;
}

static int orbit_read_swap(uint64_t *used, uint64_t *total) {
	struct xsw_usage swap;
	size_t len = sizeof(swap);
	if (sysctlbyname("vm.swapusage", &swap, &len, NULL, 0) != 0) {
		return -1;
	}
	*used = swap.xsu_used;
	*total = swap.xsu_total;
	return 0;
}

static int orbit_read_loadavg(double *one) {
	struct loadavg load;
	size_t len = sizeof(load);
	if (sysctlbyname("vm.loadavg", &load, &len, NULL, 0) != 0 || load.fscale == 0) {
		return -1;
	}
	*one = (double)load.ldavg[0] / (double)load.fscale;
	return 0;
}

static int orbit_read_boottime(int64_t *sec) {
	struct timeval tv;
	size_t len = sizeof(tv);
	if (sysctlbyname("kern.boottime", &tv, &len, NULL, 0) != 0) {
		return -1;
	}
	*sec = (int64_t)tv.tv_sec;
	return 0;
}
*/
import "C"
import (
	"errors"
	"math"
)

func (s *Source) readCPU() (cpuSample, error) {
	var idle, total C.uint64_t
	if C.orbit_read_cpu(&idle, &total) != 0 {
		return cpuSample{}, errors.New("read mach cpu load info failed")
	}
	return cpuSample{idle: uint64(idle), total: uint64(total)}, nil
}

func (s *Source) readHostMetrics() (hostMetrics, error) {
	var memUsed, memTotal C.uint64_t
	if C.orbit_read_mem(&memUsed, &memTotal) != 0 {
		return hostMetrics{}, errors.New("read mach memory info failed")
	}
	var swapUsed, swapTotal C.uint64_t
	_ = C.orbit_read_swap(&swapUsed, &swapTotal)

	var loadOne C.double
	var loadMilli uint32
	hasLoad := false
	if C.orbit_read_loadavg(&loadOne) == 0 && !math.IsNaN(float64(loadOne)) && float64(loadOne) >= 0 {
		scaled := math.Round(float64(loadOne) * 1000)
		if scaled > math.MaxUint32 {
			loadMilli = math.MaxUint32
		} else {
			loadMilli = uint32(scaled)
		}
		hasLoad = true
	}

	var bootSec C.int64_t
	var uptime uint64
	if C.orbit_read_boottime(&bootSec) == 0 {
		nowSec := s.now().Unix()
		if nowSec > int64(bootSec) {
			uptime = uint64(nowSec - int64(bootSec))
		}
	}
	return hostMetrics{
		memUsed:       uint64(memUsed),
		memTotal:      uint64(memTotal),
		swapUsed:      uint64(swapUsed),
		swapTotal:     uint64(swapTotal),
		load1mMilli:   loadMilli,
		hasLoad1m:     hasLoad,
		uptimeSeconds: uptime,
	}, nil
}
