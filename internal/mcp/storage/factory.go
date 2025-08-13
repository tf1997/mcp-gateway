package storage

import (
	"fmt"

	"go.uber.org/zap"

	"mcp-gateway/internal/common/config"
)

// NewStore creates a new store based on configuration
func NewStore(logger *zap.Logger, cfg *config.StorageConfig) (Store, error) {
	logger.Info("Initializing storage", zap.String("type", cfg.Type))
	switch cfg.Type {
	case "disk":
		return NewDiskStore(logger, cfg)
	default:
		return nil, fmt.Errorf("unsupported storage type: %s", cfg.Type)
	}
}
