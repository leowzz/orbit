package system

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

type hostMetrics struct {
	memUsed       uint64
	memTotal      uint64
	swapUsed      uint64
	swapTotal     uint64
	load1mMilli   uint32
	hasLoad1m     bool
	uptimeSeconds uint64
}

func parseProcStatFile(path string) (cpuSample, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cpuSample{}, fmt.Errorf("read %s: %w", path, err)
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuSample{}, errors.New("unexpected /proc/stat cpu line")
		}
		var total, idle uint64
		for i, field := range fields[1:] {
			val, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSample{}, fmt.Errorf("parse /proc/stat value: %w", err)
			}
			total += val
			// Field 3 is idle, field 4 is iowait.
			if i == 3 || i == 4 {
				idle += val
			}
		}
		return cpuSample{idle: idle, total: total}, nil
	}
	return cpuSample{}, errors.New("missing aggregate cpu line in /proc/stat")
}

func parseProcMeminfoFile(path string) (memUsed, memTotal, swapUsed, swapTotal uint64, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("read %s: %w", path, err)
	}
	values := make(map[string]uint64, 8)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		key := line[:colon]
		fields := strings.Fields(line[colon+1:])
		if len(fields) == 0 {
			continue
		}
		val, parseErr := strconv.ParseUint(fields[0], 10, 64)
		if parseErr != nil {
			continue
		}
		if len(fields) >= 2 && strings.EqualFold(fields[1], "kB") {
			val *= 1024
		}
		values[key] = val
	}
	memTotal, ok := values["MemTotal"]
	if !ok {
		return 0, 0, 0, 0, errors.New("missing MemTotal in /proc/meminfo")
	}
	var memAvailable uint64
	if avail, hasAvail := values["MemAvailable"]; hasAvail {
		memAvailable = avail
	} else {
		memAvailable = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	if memAvailable < memTotal {
		memUsed = memTotal - memAvailable
	}
	swapTotal = values["SwapTotal"]
	swapFree := values["SwapFree"]
	if swapFree < swapTotal {
		swapUsed = swapTotal - swapFree
	}
	return memUsed, memTotal, swapUsed, swapTotal, nil
}

func parseProcLoadavgFile(path string) (uint32, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, false
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val < 0 {
		return 0, false
	}
	scaled := math.Round(val * 1000)
	if scaled > math.MaxUint32 {
		return math.MaxUint32, true
	}
	return uint32(scaled), true
}

func parseProcUptimeFile(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	val, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val < 0 {
		return 0
	}
	return uint64(val)
}
