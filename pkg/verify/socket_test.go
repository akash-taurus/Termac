package verify

import (
	"os"
	"testing"
)

func TestGetProcessTCPSockets_CurrentProcess(t *testing.T) {
	conns, err := GetProcessTCPSockets(os.Getpid())
	if err != nil {
		t.Fatalf("GetProcessTCPSockets failed: %v", err)
	}
	t.Logf("Current process %d has %d TCP sockets", os.Getpid(), len(conns))
}

func TestAssertNoTCPSockets_NoSockets(t *testing.T) {
	// Use a non-existent PID which should have 0 sockets
	AssertNoTCPSockets(t, 999999, "test_nonexistent_pid")
}
