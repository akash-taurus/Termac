//go:build windows

package process

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

var (
	modkernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = modkernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = modkernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = modkernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = modkernel32.NewProc("TerminateJobObject")
)

const (
	// JobObjectExtendedLimitInformation class for SetInformationJobObject.
	jobObjectExtendedLimitInformationClass = 9

	// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE ensures all processes in the job terminate when the job handle closes.
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x2000

	// Process access rights needed to assign a process to a job object.
	processSetQuota = 0x0100
	processTerminate = 0x0001
)

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type jobObjectBasicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type jobObjectExtendedLimitInformation struct {
	BasicLimitInformation jobObjectBasicLimitInformation
	IoInfo                 ioCounters
	ProcessMemoryLimit     uintptr
	JobMemoryLimit         uintptr
	PeakProcessMemoryLimit uintptr
	PeakJobMemoryLimit     uintptr
}

// Job encapsulates a Windows Job Object handle configured to terminate all descendant
// member processes upon closing (JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE).
type Job struct {
	mu     sync.Mutex
	handle syscall.Handle
	closed bool
}

// NewJob creates a new anonymous Windows Job Object configured with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. When the Job handle is closed (or when
// the host process exits/crashes), all processes assigned to this job are
// terminated atomically by the Windows kernel.
func NewJob() (*Job, error) {
	r1, _, err := procCreateJobObjectW.Call(0, 0)
	if r1 == 0 {
		return nil, fmt.Errorf("CreateJobObjectW failed: %w", err)
	}
	hJob := syscall.Handle(r1)

	info := jobObjectExtendedLimitInformation{
		BasicLimitInformation: jobObjectBasicLimitInformation{
			LimitFlags: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}

	r1, _, err = procSetInformationJobObject.Call(
		uintptr(hJob),
		uintptr(jobObjectExtendedLimitInformationClass),
		uintptr(unsafe.Pointer(&info)),
		uintptr(unsafe.Sizeof(info)),
	)
	if r1 == 0 {
		_ = syscall.CloseHandle(hJob)
		return nil, fmt.Errorf("SetInformationJobObject failed: %w", err)
	}

	return &Job{
		handle: hJob,
	}, nil
}

// Handle returns the underlying Windows Job Object handle.
func (j *Job) Handle() syscall.Handle {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.handle
}

// AssignHandle assigns a process to this Job Object given an open process handle.
func (j *Job) AssignHandle(hProcess syscall.Handle) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return errors.New("cannot assign process to closed Job")
	}

	r1, _, err := procAssignProcessToJobObject.Call(uintptr(j.handle), uintptr(hProcess))
	if r1 == 0 {
		return fmt.Errorf("AssignProcessToJobObject failed: %w", err)
	}
	return nil
}

// AssignPID opens the process with required access rights and assigns it to this Job Object.
func (j *Job) AssignPID(pid int) error {
	if pid <= 0 {
		return ErrInvalidPID
	}

	hProc, err := syscall.OpenProcess(processSetQuota|processTerminate, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("OpenProcess failed for PID %d: %w", pid, err)
	}
	defer syscall.CloseHandle(hProc)

	return j.AssignHandle(hProc)
}

// AssignProcess assigns the given *os.Process to this Job Object.
func (j *Job) AssignProcess(proc *os.Process) error {
	if proc == nil {
		return ErrNilCmd
	}
	return j.AssignPID(proc.Pid)
}

// Terminate forcefully terminates all processes associated with this Job Object.
func (j *Job) Terminate(exitCode uint32) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil
	}

	r1, _, err := procTerminateJobObject.Call(uintptr(j.handle), uintptr(exitCode))
	if r1 == 0 {
		return fmt.Errorf("TerminateJobObject failed: %w", err)
	}
	return nil
}

// Close closes the Job Object handle. Because JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// is set, closing the handle triggers the Windows kernel to terminate all member processes.
func (j *Job) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()

	if j.closed {
		return nil
	}
	j.closed = true

	if err := syscall.CloseHandle(j.handle); err != nil {
		return fmt.Errorf("CloseHandle failed for Job: %w", err)
	}
	return nil
}
