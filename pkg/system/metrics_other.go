//go:build !windows

package system

import (
	"sync"
	"time"
)

type Collector struct {
	mu         sync.Mutex
	cpuHistory []float64
	maxHistory int
}

func NewCollector(historyCapacity int) *Collector {
	if historyCapacity <= 0 {
		historyCapacity = 40
	}
	return &Collector{
		maxHistory: historyCapacity,
		cpuHistory: make([]float64, 0, historyCapacity),
	}
}

func (c *Collector) SampleCPU() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	simulated := 15.5
	c.cpuHistory = append(c.cpuHistory, simulated)
	if len(c.cpuHistory) > c.maxHistory {
		c.cpuHistory = c.cpuHistory[1:]
	}
	return simulated, nil
}

func (c *Collector) SampleMemory() (MemoryMetrics, error) {
	total := uint64(16 * 1024 * 1024 * 1024)
	used := uint64(8 * 1024 * 1024 * 1024)
	return MemoryMetrics{
		TotalBytes:     total,
		AvailableBytes: total - used,
		UsedBytes:      used,
		UsedPercent:    50.0,
	}, nil
}

func (c *Collector) SampleDisks() ([]DiskMetrics, error) {
	total := uint64(512 * 1024 * 1024 * 1024)
	used := uint64(200 * 1024 * 1024 * 1024)
	return []DiskMetrics{
		{
			DriveLetter: "/",
			TotalBytes:  total,
			FreeBytes:   total - used,
			UsedBytes:   used,
			UsedPercent: (float64(used) / float64(total)) * 100.0,
		},
	}, nil
}

func (c *Collector) SampleProcesses(topN int) (int, int, []ProcessInfo, error) {
	procs := []ProcessInfo{
		{PID: 1, PPID: 0, Name: "init", ThreadCount: 1},
		{PID: 100, PPID: 1, Name: "dashboard", ThreadCount: 8},
	}
	return len(procs), 9, procs, nil
}

func (c *Collector) TakeSnapshot() SystemSnapshot {
	cpu, _ := c.SampleCPU()
	mem, _ := c.SampleMemory()
	disks, _ := c.SampleDisks()
	pCount, tCount, procs, _ := c.SampleProcesses(10)

	c.mu.Lock()
	hist := make([]float64, len(c.cpuHistory))
	copy(hist, c.cpuHistory)
	c.mu.Unlock()

	return SystemSnapshot{
		Timestamp:    time.Now(),
		CPUPercent:   cpu,
		CPUHistory:   hist,
		Memory:       mem,
		Disks:        disks,
		ProcessCount: pCount,
		ThreadCount:  tCount,
		TopProcesses: procs,
		// These values are placeholders on non-Windows; flag them so the UI
		// shows a "simulated" banner instead of implying live host metrics.
		Simulated: true,
	}
}
