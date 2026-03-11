package tui

import (
	"fmt"
	"net"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// startHealthCheck fires an initial TCP probe after a short delay,
// giving the pod time to accept connections after the forward is established.
func startHealthCheck(id string, port int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(3 * time.Second)
		return probePort(id, port)
	}
}

// continueHealthCheck re-schedules a health probe at a longer interval.
func continueHealthCheck(id string, port int) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(15 * time.Second)
		return probePort(id, port)
	}
}

func probePort(id string, port int) MsgHealthResult {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err == nil {
		conn.Close()
		return MsgHealthResult{ID: id, Healthy: true}
	}
	return MsgHealthResult{ID: id, Healthy: false}
}
