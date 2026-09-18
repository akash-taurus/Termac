//go:build !windows

package verify

import "fmt"

// TCPConnection represents a TCP connection
type TCPConnection struct {
	LocalAddr  string
	RemoteAddr string
	State      uint32
	OwningPID  uint32
}

// GetProcessTCPSockets is not supported on non-Windows: transport uses Unix
// sockets here, so there is no TCP table to query. Return an explicit error
// instead of false assurance.
func GetProcessTCPSockets(pid int) ([]TCPConnection, error) {
	return nil, fmt.Errorf("GetProcessTCPSockets not supported on this platform")
}

// AssertNoTCPSockets always passes on non-Windows
func AssertNoTCPSockets(t interface{ Fatalf(string, ...interface{}) }, pid int, label string) {
	t.Fatalf("%s: TCP verification not supported on this platform (no TCP transport in use)", label)
}

// GetListeningPorts returns empty on non-Windows
func GetListeningPorts(pid int) ([]uint16, error) {
	return nil, fmt.Errorf("GetListeningPorts not supported on this platform")
}
