package auth

type StorageType string

const (
	// StorageTypeMemory represents an in-memory store
	StorageTypeMemory StorageType = "memory"
	// StorageTypeRedis represents a Redis-based store
	StorageTypeRedis StorageType = "redis"
)
