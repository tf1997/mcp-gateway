package session

import (
	"fmt"

	"go.uber.org/zap"

	"mcp-gateway/internal/common/config"
	"mcp-gateway/pkg/kafka"
)

// Type represents the type of session store
type Type string

const (
	// TypeMemory represents in-memory session store
	TypeMemory Type = "memory"
	// TypeRedis represents Redis-based session store
	TypeRedis Type = "redis"
)

// NewStore creates a new session store based on configuration
func NewStore(logger *zap.Logger, kafkaProducer *kafka.KafkaProducer, cfg *config.SessionConfig, nodeIP string) (Store, error) {
	logger.Info("Initializing session store", zap.String("type", cfg.Type))
	switch Type(cfg.Type) {
	case TypeMemory:
		return NewMemoryStore(logger, kafkaProducer, nodeIP), nil
	case TypeRedis:
		return NewRedisStore(logger, kafkaProducer, cfg.Redis, nodeIP)
	default:
		return nil, fmt.Errorf("unsupported session store type: %s", cfg.Type)
	}
}
