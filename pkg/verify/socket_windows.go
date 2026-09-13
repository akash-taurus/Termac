//go:build windows

package verify

import (
	"fmt"
	"syscall"
	"unsafe"
)

// TCPConnection represents a TCP connection
type TCPConnection struct {
	LocalAddr  string
	RemoteAddr string
	State      uint32
	OwningPID  uint32
}

// GetProcessTCPSockets returns all TCP connections for a given PID
func GetProcessTCPSockets(pid int) ([]TCPConnection, error) {
	var connections []TCPConnection

	// Load iphlpapi.dll
	iphlpapi := syscall.NewLazyDLL("iphlpapi.dll")
	getExtendedTcpTable := iphlpapi.NewProc("GetExtendedTcpTable")
	if getExtendedTcpTable.Find() != nil {
		return nil, fmt.Errorf("GetExtendedTcpTable not found")
	}

	// First call to get required buffer size
	var size uint32
	ret, _, _ := getExtendedTcpTable.Call(
		0, // pTcpTable
		uintptr(unsafe.Pointer(&size)),
		1, // bOrder
		syscall.AF_INET,
		5, // TCP_TABLE_OWNER_PID_ALL
		0, // dwReserved
	)

	if ret != 122 { // ERROR_INSUFFICIENT_BUFFER
		if ret != 0 {
			return nil, fmt.Errorf("GetExtendedTcpTable failed: %d", ret)
		}
	}

	// Allocate buffer
	buf := make([]byte, size)
	ret, _, _ = getExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
		1, // bOrder
		syscall.AF_INET,
		5, // TCP_TABLE_OWNER_PID_ALL
		0, // dwReserved
	)

	if ret != 0 {
		return nil, fmt.Errorf("GetExtendedTcpTable failed: %d", ret)
	}

	// Parse the table
	table := (*MIB_TCPTABLE_OWNER_PID)(unsafe.Pointer(&buf[0]))
	numEntries := int(table.dwNumEntries)

	// MIB_TCPROW_OWNER_PID starts right after dwNumEntries
	rowPtr := unsafe.Pointer(uintptr(unsafe.Pointer(&table.table[0])))

	for i := 0; i < numEntries; i++ {
		row := (*MIB_TCPROW_OWNER_PID)(rowPtr)

		if int(row.dwOwningPid) == pid {
			connections = append(connections, TCPConnection{
				LocalAddr:  formatIPPort(row.dwLocalAddr, row.dwLocalPort),
				RemoteAddr: formatIPPort(row.dwRemoteAddr, row.dwRemotePort),
				State:      row.dwState,
				OwningPID:  row.dwOwningPid,
			})
		}

		// Move to next row
		rowPtr = unsafe.Pointer(uintptr(rowPtr) + unsafe.Sizeof(MIB_TCPROW_OWNER_PID{}))
	}

	return connections, nil
}

// AssertNoTCPSockets asserts that a process has no TCP sockets
func AssertNoTCPSockets(t interface{ Fatalf(string, ...interface{}) }, pid int, label string) {
	conns, err := GetProcessTCPSockets(pid)
	if err != nil {
		t.Fatalf("%s: failed to get TCP sockets: %v", label, err)
	}
	if len(conns) > 0 {
		t.Fatalf("%s: expected 0 TCP sockets, found %d: %+v", label, len(conns), conns)
	}
}

// GetListeningPorts returns all listening TCP ports for a process
func GetListeningPorts(pid int) ([]uint16, error) {
	conns, err := GetProcessTCPSockets(pid)
	if err != nil {
		return nil, err
	}

	var ports []uint16
	for _, conn := range conns {
		if conn.State == MIB_TCP_STATE_LISTEN {
			// Extract port from LocalAddr
			// LocalAddr format: "IP:PORT"
			ports = append(ports, extractPort(conn.LocalAddr))
		}
	}
	return ports, nil
}

// MIB_TCPROW_OWNER_PID represents a TCP row with PID
type MIB_TCPROW_OWNER_PID struct {
	dwState      uint32
	dwLocalAddr  uint32
	dwLocalPort  uint32
	dwRemoteAddr uint32
	dwRemotePort uint32
	dwOwningPid  uint32
}

// MIB_TCPTABLE_OWNER_PID represents a TCP table with PID
type MIB_TCPTABLE_OWNER_PID struct {
	dwNumEntries uint32
	table        [1]MIB_TCPROW_OWNER_PID // Variable length array
}

const (
	MIB_TCP_STATE_CLOSED      = 1
	MIB_TCP_STATE_LISTEN      = 2
	MIB_TCP_STATE_SYN_SENT    = 3
	MIB_TCP_STATE_SYN_RCVD    = 4
	MIB_TCP_STATE_ESTAB       = 5
	MIB_TCP_STATE_FIN_WAIT1   = 6
	MIB_TCP_STATE_FIN_WAIT2   = 7
	MIB_TCP_STATE_CLOSE_WAIT  = 8
	MIB_TCP_STATE_CLOSING     = 9
	MIB_TCP_STATE_LAST_ACK    = 10
	MIB_TCP_STATE_TIME_WAIT   = 11
	MIB_TCP_STATE_DELETE_TCB  = 12
)

func formatIPPort(addr uint32, port uint32) string {
	ip := fmt.Sprintf("%d.%d.%d.%d",
		byte(addr), byte(addr>>8), byte(addr>>16), byte(addr>>24))
	// Port is in network byte order
	p := (port>>8)&0xFF | (port<<8)&0xFF00
	return fmt.Sprintf("%s:%d", ip, p)
}

func extractPort(addr string) uint16 {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			var port uint16
			fmt.Sscanf(addr[i+1:], "%d", &port)
			return port
		}
	}
	return 0
}