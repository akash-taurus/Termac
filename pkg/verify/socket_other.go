//go:build !windows

package verify

// TCPConnection represents a TCP connection
type TCPConnection struct {
	LocalAddr  string
	RemoteAddr string
	State      uint32
	OwningPID  uint32
}

// GetProcessTCPSockets returns empty on non-Windows platforms
func GetProcessTCPSockets(pid int) ([]TCPConnection, error) {
	return nil, nil
}

// AssertNoTCPSockets always passes on non-Windows
func AssertNoTCPSockets(t interface{ Fatalf(string, ...interface{}) }, pid int, label string) {
}

// GetListeningPorts returns empty on non-Windows
func GetListeningPorts(pid int) ([]uint16, error) {
	return nil, nil
}