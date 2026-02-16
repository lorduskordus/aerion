//go:build windows

package platform

import (
	"context"
	"sync"

	"github.com/hkdb/aerion/internal/logging"
)

// WindowsNetworkMonitor monitors network connectivity on Windows
// TODO: Implement using NetworkListManager COM interface
type WindowsNetworkMonitor struct {
	events    chan NetworkEvent
	stopChan  chan struct{}
	running   bool
	connected bool
	mu        sync.RWMutex
}

// NewNetworkMonitor creates a new network connectivity monitor for Windows
func NewNetworkMonitor() NetworkMonitor {
	return &WindowsNetworkMonitor{
		events:    make(chan NetworkEvent, 10),
		stopChan:  make(chan struct{}),
		connected: true, // assume connected
	}
}

// Start begins monitoring for network connectivity changes
// TODO: Implement using NetworkListManager COM interface
func (m *WindowsNetworkMonitor) Start(ctx context.Context) error {
	log := logging.WithComponent("network-monitor")

	if m.running {
		return nil
	}

	m.running = true
	log.Info().Msg("Network monitor started (Windows stub — not implemented)")
	return nil
}

// Events returns the channel for receiving network connectivity events
func (m *WindowsNetworkMonitor) Events() <-chan NetworkEvent {
	return m.events
}

// IsConnected returns the current connectivity state
func (m *WindowsNetworkMonitor) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// WaitForConnection blocks until network is available or context is cancelled
func (m *WindowsNetworkMonitor) WaitForConnection(ctx context.Context) bool {
	return true // stub: always connected
}

// Invalidate resets the cached connectivity state to disconnected
func (m *WindowsNetworkMonitor) Invalidate() {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

// Stop stops the monitor and cleans up resources
func (m *WindowsNetworkMonitor) Stop() error {
	log := logging.WithComponent("network-monitor")

	if !m.running {
		return nil
	}

	m.running = false
	close(m.stopChan)

	log.Info().Msg("Network monitor stopped (Windows)")
	return nil
}
