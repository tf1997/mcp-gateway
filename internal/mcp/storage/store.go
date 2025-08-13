package storage

import (
	"context"

	"mcp-gateway/internal/core/state"
)

// Store defines the interface for MCP configuration storage
type Store interface {
	// SaveState saves the State 
	SaveState(ctx context.Context, state *state.State) error

	// LoadState loads the State
    LoadState(ctx context.Context) (*state.State, error)
}
