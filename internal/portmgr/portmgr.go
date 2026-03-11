package portmgr

import (
	"fmt"
	"net"
)

// IsPortFree attempts a TCP bind on localhost:<port>. Returns true if available.
func IsPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// NextFreePort starting from desired, returns first available port.
func NextFreePort(desired int) (int, error) {
	for port := desired; port < 65535; port++ {
		if IsPortFree(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free port found starting from %d", desired)
}
