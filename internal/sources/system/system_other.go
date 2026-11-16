//go:build !linux && !(darwin && cgo)

package system

func (s *Source) readCPU() (cpuSample, error) {
	return cpuSample{}, nil
}

func (s *Source) readHostMetrics() (hostMetrics, error) {
	return hostMetrics{}, nil
}
