package main

import "testing"

func TestParseCPUTimes(t *testing.T) {
	stat := "cpu  100 20 30 800 50 0 0 0 0 0\ncpu0 1 2 3 4 5 0 0 0 0 0\nbtime 1792502664\n"
	got, err := parseCPUTimes(stat)
	if err != nil {
		t.Fatal(err)
	}
	// idle = idle + iowait
	want := cpuTimes{Idle: 850, Total: 1000}
	if got != want {
		t.Errorf("parseCPUTimes = %+v, want %+v", got, want)
	}
}

func TestCPUPercent(t *testing.T) {
	a := cpuTimes{Idle: 850, Total: 1000}
	b := cpuTimes{Idle: 925, Total: 1100}
	if got := cpuPercent(a, b); got != 25 {
		t.Errorf("cpuPercent = %v, want 25", got)
	}
	if got := cpuPercent(a, a); got != 0 {
		t.Errorf("cpuPercent with no elapsed ticks = %v, want 0", got)
	}
}

func TestParseMeminfo(t *testing.T) {
	meminfo := "MemTotal:       32598780 kB\nMemFree:         1000000 kB\nMemAvailable:   17122620 kB\n"
	total, avail, err := parseMeminfo(meminfo)
	if err != nil {
		t.Fatal(err)
	}
	if total != 32598780*1024 || avail != 17122620*1024 {
		t.Errorf("parseMeminfo = %d, %d", total, avail)
	}
}
