package system

import (
	"fmt"
	"time"
)

// MemoryMetrics contains RAM and swap statistics
type MemoryMetrics struct {
	TotalBytes     uint64
	AvailableBytes uint64
	UsedBytes      uint64
	UsedPercent    float64
}

func (m MemoryMetrics) TotalGB() float64 {
	return float64(m.TotalBytes) / (1024 * 1024 * 1024)
}

func (m MemoryMetrics) UsedGB() float64 {
	return float64(m.UsedBytes) / (1024 * 1024 * 1024)
}

func (m MemoryMetrics) FreeGB() float64 {
	return float64(m.AvailableBytes) / (1024 * 1024 * 1024)
}

// DiskMetrics contains disk usage for a specific drive
type DiskMetrics struct {
	DriveLetter string
	TotalBytes  uint64
	FreeBytes   uint64
	UsedBytes   uint64
	UsedPercent float64
}

func (d DiskMetrics) TotalGB() float64 {
	return float64(d.TotalBytes) / (1024 * 1024 * 1024)
}

func (d DiskMetrics) UsedGB() float64 {
	return float64(d.UsedBytes) / (1024 * 1024 * 1024)
}

func (d DiskMetrics) FreeGB() float64 {
	return float64(d.FreeBytes) / (1024 * 1024 * 1024)
}

// ProcessInfo holds details of a running system process
type ProcessInfo struct {
	PID         uint32
	PPID        uint32
	Name        string
	ThreadCount uint32
}

// SystemSnapshot captures complete host resource utilization at a point in time
type SystemSnapshot struct {
	Timestamp    time.Time
	CPUPercent   float64
	CPUHistory   []float64
	Memory       MemoryMetrics
	Disks        []DiskMetrics
	ProcessCount int
	ThreadCount  int
	TopProcesses []ProcessInfo
	// Simulated is true when the platform has no real collector (non-Windows)
	// and the values are placeholders. The UI must not present them as real.
	Simulated bool
}

// FormatSparkline produces a Unicode sparkline graph for recent history values (0-100)
func FormatSparkline(values []float64, width int) string {
	if len(values) == 0 || width <= 0 {
		return ""
	}
	if width > 500 {
		width = 500
	}
	bars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	// Truncate or pad to width
	start := 0
	if len(values) > width {
		start = len(values) - width
	}
	slice := values[start:]

	result := make([]rune, len(slice))
	for i, v := range slice {
		// Clamp NaN/Inf/out-of-range.
		if v != v || v < 0 {
			v = 0
		}
		if v > 100 {
			v = 100
		}
		idx := int((v / 100.0) * float64(len(bars)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(bars) {
			idx = len(bars) - 1
		}
		result[i] = bars[idx]
	}
	return string(result)
}

// HumanSize converts bytes into a human-readable string
func HumanSize(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
		if exp >= 5 {
			break
		}
	}
	units := "KMGTPE"
	if exp >= len(units) {
		exp = len(units) - 1
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), units[exp])
}
