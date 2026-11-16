//go:build linux

package system

import "path/filepath"

func (s *Source) readCPU() (cpuSample, error) {
	return parseProcStatFile(filepath.Join(s.procDir, "stat"))
}

func (s *Source) readHostMetrics() (hostMetrics, error) {
	memUsed, memTotal, swapUsed, swapTotal, err := parseProcMeminfoFile(filepath.Join(s.procDir, "meminfo"))
	if err != nil {
		return hostMetrics{}, err
	}
	load1m, hasLoad := parseProcLoadavgFile(filepath.Join(s.procDir, "loadavg"))
	uptime := parseProcUptimeFile(filepath.Join(s.procDir, "uptime"))
	return hostMetrics{
		memUsed:       memUsed,
		memTotal:      memTotal,
		swapUsed:      swapUsed,
		swapTotal:     swapTotal,
		load1mMilli:   load1m,
		hasLoad1m:     hasLoad,
		uptimeSeconds: uptime,
	}, nil
}
