//go:build !windows

package process

import (
	"os"
)

// Handle represents an OS handle placeholder on non-Windows platforms.
type Handle uintptr

// Job is a no-op placeholder for non-Windows platforms.
type Job struct{}

// NewJob creates a no-op Job on non-Windows platforms.
func NewJob() (*Job, error) {
	return &Job{}, nil
}

// Handle returns an invalid handle on non-Windows platforms.
func (j *Job) Handle() Handle {
	return 0
}

// AssignHandle is a no-op on non-Windows platforms.
func (j *Job) AssignHandle(hProcess Handle) error {
	return nil
}

// AssignPID is a no-op on non-Windows platforms.
func (j *Job) AssignPID(pid int) error {
	if pid <= 0 {
		return ErrInvalidPID
	}
	return nil
}

// AssignProcess is a no-op on non-Windows platforms.
func (j *Job) AssignProcess(proc *os.Process) error {
	if proc == nil {
		return ErrNilCmd
	}
	return nil
}

// Terminate is a no-op on non-Windows platforms.
func (j *Job) Terminate(exitCode uint32) error {
	return nil
}

// Close is a no-op on non-Windows platforms.
func (j *Job) Close() error {
	return nil
}
