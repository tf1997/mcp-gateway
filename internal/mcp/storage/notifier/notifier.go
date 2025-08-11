package notifier

import (
	"context"

	"mcp-gateway/internal/common/config"
)

// Notifier defines the interface for configuration update notification
type Notifier interface {
	// Watch returns a channel that receives notifications when servers are updated
	Watch(ctx context.Context) (<-chan *config.MCPConfig, error)

	// NotifyUpdate triggers an update notification
	NotifyUpdate(ctx context.Context, updated *config.MCPConfig) error

	// CanReceive returns true if the notifier can receive updates
	CanReceive() bool

	// CanSend returns true if the notifier can send updates
	CanSend() bool
}
