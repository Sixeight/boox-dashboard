package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

type System struct {
	Hostname   string   `json:"hostname"`
	CPUPercent float64  `json:"cpuPercent"`
	MemUsed    uint64   `json:"memUsed"`
	MemTotal   uint64   `json:"memTotal"`
	DiskUsed   uint64   `json:"diskUsed"`
	DiskTotal  uint64   `json:"diskTotal"`
	CPUTempC   *float64 `json:"cpuTempC"`
	UptimeSec  int64    `json:"uptimeSec"`
}

type cpuTimes struct {
	Idle  uint64
	Total uint64
}

// lastCPU is the previous /proc/stat sample, so each request reports usage since the one before it
// (the page's poll interval) without sleeping; the first request reports usage since boot.
var lastCPU struct {
	sync.Mutex
	times cpuTimes
}

func readSystem() (System, error) {
	var s System

	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return s, err
	}
	now, err := parseCPUTimes(string(stat))
	if err != nil {
		return s, err
	}
	lastCPU.Lock()
	s.CPUPercent = cpuPercent(lastCPU.times, now)
	lastCPU.times = now
	lastCPU.Unlock()

	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return s, err
	}
	s.UptimeSec = info.Uptime

	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return s, err
	}
	total, avail, err := parseMeminfo(string(meminfo))
	if err != nil {
		return s, err
	}
	s.MemTotal, s.MemUsed = total, total-avail

	var fs syscall.Statfs_t
	if err := syscall.Statfs("/", &fs); err != nil {
		return s, err
	}
	s.DiskTotal = fs.Blocks * uint64(fs.Bsize)
	s.DiskUsed = (fs.Blocks - fs.Bfree) * uint64(fs.Bsize)

	s.Hostname, _ = os.Hostname()
	s.CPUTempC = readCPUTemp()
	return s, nil
}

// parseCPUTimes reads the aggregate "cpu" line of /proc/stat. Idle includes iowait.
func parseCPUTimes(stat string) (cpuTimes, error) {
	for _, line := range strings.Split(stat, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, f := range fields[1:] {
			v, err := strconv.ParseUint(f, 10, 64)
			if err != nil {
				return cpuTimes{}, fmt.Errorf("/proc/stat: %w", err)
			}
			t.Total += v
			if i == 3 || i == 4 {
				t.Idle += v
			}
		}
		return t, nil
	}
	return cpuTimes{}, fmt.Errorf("/proc/stat: no cpu line")
}

func cpuPercent(before, after cpuTimes) float64 {
	total := after.Total - before.Total
	if total == 0 {
		return 0
	}
	return float64(total-(after.Idle-before.Idle)) / float64(total) * 100
}

// parseMeminfo returns MemTotal and MemAvailable in bytes.
func parseMeminfo(meminfo string) (total, avail uint64, err error) {
	var gotTotal, gotAvail bool
	for _, line := range strings.Split(meminfo, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total, gotTotal = v*1024, true
		case "MemAvailable:":
			avail, gotAvail = v*1024, true
		}
	}
	if !gotTotal || !gotAvail {
		return 0, 0, fmt.Errorf("/proc/meminfo: MemTotal or MemAvailable missing")
	}
	return total, avail, nil
}

// readCPUTemp returns the CPU package temperature from the coretemp or k10temp hwmon, if present.
func readCPUTemp() *float64 {
	dirs, _ := filepath.Glob("/sys/class/hwmon/hwmon*")
	for _, dir := range dirs {
		name, err := os.ReadFile(filepath.Join(dir, "name"))
		if err != nil {
			continue
		}
		if n := strings.TrimSpace(string(name)); n != "coretemp" && n != "k10temp" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "temp1_input"))
		if err != nil {
			continue
		}
		milli, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			continue
		}
		c := milli / 1000
		return &c
	}
	return nil
}
