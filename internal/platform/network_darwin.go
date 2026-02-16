//go:build darwin

package platform

import (
	"context"
	"sync"

	"github.com/hkdb/aerion/internal/logging"
)

// DarwinNetworkMonitor monitors network connectivity on macOS
// TODO: Implement using NWPathMonitor
type DarwinNetworkMonitor struct {
	events    chan NetworkEvent
	stopChan  chan struct{}
	running   bool
	connected bool
	mu        sync.RWMutex
}

// NewNetworkMonitor creates a new network connectivity monitor for macOS
func NewNetworkMonitor() NetworkMonitor {
	return &DarwinNetworkMonitor{
		events:    make(chan NetworkEvent, 10),
		stopChan:  make(chan struct{}),
		connected: true, // assume connected
	}
}

// Start begins monitoring for network connectivity changes
// TODO: Implement using NWPathMonitor
func (m *DarwinNetworkMonitor) Start(ctx context.Context) error {
	log := logging.WithComponent("network-monitor")

	if m.running {
		return nil
	}

	m.running = true
	log.Info().Msg("Network monitor started (macOS stub — not implemented)")
	return nil
}

// Events returns the channel for receiving network connectivity events
func (m *DarwinNetworkMonitor) Events() <-chan NetworkEvent {
	return m.events
}

// IsConnected returns the current connectivity state
func (m *DarwinNetworkMonitor) IsConnected() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.connected
}

// WaitForConnection blocks until network is available or context is cancelled
func (m *DarwinNetworkMonitor) WaitForConnection(ctx context.Context) bool {
	return true // stub: always connected
}

// Invalidate resets the cached connectivity state to disconnected
func (m *DarwinNetworkMonitor) Invalidate() {
	m.mu.Lock()
	m.connected = false
	m.mu.Unlock()
}

// Stop stops the monitor and cleans up resources
func (m *DarwinNetworkMonitor) Stop() error {
	log := logging.WithComponent("network-monitor")

	if !m.running {
		return nil
	}

	m.running = false
	close(m.stopChan)

	log.Info().Msg("Network monitor stopped (macOS)")
	return nil
}
