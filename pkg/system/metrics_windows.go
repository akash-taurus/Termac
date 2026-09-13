//go:build windows

package system

import (
	"fmt"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modkernel32               = windows.NewLazySystemDLL("kernel32.dll")
	procGetSystemTimes        = modkernel32.NewProc("GetSystemTimes")
	procGetLogicalDrives      = modkernel32.NewProc("GetLogicalDrives")
	procGlobalMemoryStatusEx  = modkernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// filetimeToUint64 converts windows.Filetime to uint64 timestamp
func filetimeToUint64(ft windows.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
}

type Collector struct {
	mu           sync.Mutex
	prevIdle     uint64
	prevKernel   uint64
	prevUser     uint64
	hasPrevTimes bool
	cpuHistory   []float64
	maxHistory   int
}

func NewCollector(historyCapacity int) *Collector {
	if historyCapacity <= 0 {
		historyCapacity = 40
	}
	c := &Collector{
		maxHistory: historyCapacity,
		cpuHistory: make([]float64, 0, historyCapacity),
	}
	// Prime initial CPU sampling
	_, _ = c.SampleCPU()
	return c
}

// SampleCPU calculates current CPU load % using GetSystemTimes
func (c *Collector) SampleCPU() (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var idle, kernel, user windows.Filetime
	r1, _, err := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idle)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r1 == 0 {
		return 0, fmt.Errorf("GetSystemTimes failed: %w", err)
	}

	curIdle := filetimeToUint64(idle)
	curKernel := filetimeToUint64(kernel)
	curUser := filetimeToUint64(user)

	if !c.hasPrevTimes {
		c.prevIdle = curIdle
		c.prevKernel = curKernel
		c.prevUser = curUser
		c.hasPrevTimes = true
		return 0, nil
	}

	idleDelta := curIdle - c.prevIdle
	kernelDelta := curKernel - c.prevKernel
	userDelta := curUser - c.prevUser

	c.prevIdle = curIdle
	c.prevKernel = curKernel
	c.prevUser = curUser

	totalDelta := kernelDelta + userDelta
	if totalDelta == 0 {
		return 0, nil
	}

	// On Windows, kernelDelta already includes idle time
	var busyDelta uint64
	if totalDelta > idleDelta {
		busyDelta = totalDelta - idleDelta
	}
	percent := (float64(busyDelta) / float64(totalDelta)) * 100.0
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	c.cpuHistory = append(c.cpuHistory, percent)
	if len(c.cpuHistory) > c.maxHistory {
		c.cpuHistory = c.cpuHistory[1:]
	}

	return percent, nil
}

// SampleMemory queries RAM status via GlobalMemoryStatusEx
func (c *Collector) SampleMemory() (MemoryMetrics, error) {
	var status memoryStatusEx
	status.Length = uint32(unsafe.Sizeof(status))
	r1, _, err := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&status)))
	if r1 == 0 {
		return MemoryMetrics{}, fmt.Errorf("GlobalMemoryStatusEx failed: %w", err)
	}

	total := status.TotalPhys
	free := status.AvailPhys
	used := total - free
	var pct float64
	if total > 0 {
		pct = (float64(used) / float64(total)) * 100.0
	}

	return MemoryMetrics{
		TotalBytes:     total,
		AvailableBytes: free,
		UsedBytes:      used,
		UsedPercent:    pct,
	}, nil
}

// SampleDisks enumerates active drives and retrieves storage stats
func (c *Collector) SampleDisks() ([]DiskMetrics, error) {
	r1, _, _ := procGetLogicalDrives.Call()
	driveMask := uint32(r1)

	var disks []DiskMetrics
	for i := 0; i < 26; i++ {
		if (driveMask & (1 << i)) != 0 {
			letter := fmt.Sprintf("%c:\\", 'A'+i)
			rootPtr, err := windows.UTF16PtrFromString(letter)
			if err != nil {
				continue
			}

			// Check drive type (ignore CD-ROM, network, unknown)
			driveType := windows.GetDriveType(rootPtr)
			if driveType != windows.DRIVE_FIXED && driveType != windows.DRIVE_REMOVABLE {
				continue
			}

			var freeBytes, totalBytes, totalFreeBytes uint64
			err = windows.GetDiskFreeSpaceEx(rootPtr, &freeBytes, &totalBytes, &totalFreeBytes)
			if err != nil || totalBytes == 0 {
				continue
			}

			used := totalBytes - totalFreeBytes
			pct := (float64(used) / float64(totalBytes)) * 100.0
			disks = append(disks, DiskMetrics{
				DriveLetter: letter,
				TotalBytes:  totalBytes,
				FreeBytes:   totalFreeBytes,
				UsedBytes:   used,
				UsedPercent: pct,
			})
		}
	}
	return disks, nil
}

// SampleProcesses captures snapshot of running processes
func (c *Collector) SampleProcesses(topN int) (int, int, []ProcessInfo, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0, 0, nil, err
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snapshot, &entry); err != nil {
		return 0, 0, nil, err
	}

	totalProcs := 0
	totalThreads := 0
	var list []ProcessInfo

	for {
		totalProcs++
		totalThreads += int(entry.Threads)

		name := syscall.UTF16ToString(entry.ExeFile[:])
		list = append(list, ProcessInfo{
			PID:         entry.ProcessID,
			PPID:        entry.ParentProcessID,
			Name:        name,
			ThreadCount: entry.Threads,
		})

		entry.Size = uint32(unsafe.Sizeof(entry))
		err := windows.Process32Next(snapshot, &entry)
		if err != nil {
			break
		}
	}

	// Sort by thread count descending as proxy for activity
	sort.Slice(list, func(i, j int) bool {
		return list[i].ThreadCount > list[j].ThreadCount
	})

	if topN > 0 && len(list) > topN {
		list = list[:topN]
	}

	return totalProcs, totalThreads, list, nil
}

// TakeSnapshot aggregates CPU, memory, disk, and process statistics
func (c *Collector) TakeSnapshot() SystemSnapshot {
	cpuPct, _ := c.SampleCPU()
	mem, _ := c.SampleMemory()
	disks, _ := c.SampleDisks()
	procCount, threadCount, topProcs, _ := c.SampleProcesses(10)

	c.mu.Lock()
	histCopy := make([]float64, len(c.cpuHistory))
	copy(histCopy, c.cpuHistory)
	c.mu.Unlock()

	return SystemSnapshot{
		Timestamp:    time.Now(),
		CPUPercent:   cpuPct,
		CPUHistory:   histCopy,
		Memory:       mem,
		Disks:        disks,
		ProcessCount: procCount,
		ThreadCount:  threadCount,
		TopProcesses: topProcs,
	}
}
