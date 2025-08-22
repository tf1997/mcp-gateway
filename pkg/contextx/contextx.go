package contextx

import (
	"context"
	"mcp-gateway/internal/common/config"

	"go.uber.org/zap"
)

// Define custom types for context keys to avoid collisions.
type (
	contextKeyLogger struct{}
	contextKeyConfig struct{}
)

// NewContextWithLogger creates a new context with the given zap logger.
func NewContextWithLogger(ctx context.Context, logger *zap.Logger) context.Context {
	return context.WithValue(ctx, contextKeyLogger{}, logger)
}

// LoggerFromContext retrieves the zap logger from the context.
// It returns a no-op logger if not found.
func LoggerFromContext(ctx context.Context) *zap.Logger {
	if logger, ok := ctx.Value(contextKeyLogger{}).(*zap.Logger); ok {
		return logger
	}
	// Return a no-op logger if none is found in the context
	return zap.NewNop()
}

// NewContextWithConfig creates a new context with the given MCPGatewayConfig.
func NewContextWithConfig(ctx context.Context, cfg *config.MCPGatewayConfig) context.Context {
	return context.WithValue(ctx, contextKeyConfig{}, cfg)
}

// ConfigFromContext retrieves the MCPGatewayConfig from the context.
// It returns a default empty config if not found.
func ConfigFromContext(ctx context.Context) *config.MCPGatewayConfig {
	if cfg, ok := ctx.Value(contextKeyConfig{}).(*config.MCPGatewayConfig); ok {
		return cfg
	}
	// Return a default empty config if none is found
	return &config.MCPGatewayConfig{}
}
