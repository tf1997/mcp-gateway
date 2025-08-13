package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"mcp-gateway/internal/common/config"
	"mcp-gateway/internal/core/state"
	"go.uber.org/zap"
)

type DiskStore struct {
	logger  *zap.Logger
	baseDir string
	cfg     *config.StorageConfig
}

var _ Store = (*DiskStore)(nil)

func NewDiskStore(logger *zap.Logger, cfg *config.StorageConfig) (*DiskStore, error) {
	logger = logger.Named("mcp.store.disk")

	baseDir := cfg.Disk.Path
	if baseDir == "" {
		baseDir = "./data"
	}

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, err
	}

	return &DiskStore{
		baseDir: baseDir,
		logger:  logger,
		cfg:     cfg,
	}, nil
}

// SaveState saves the State to a file on disk
func (s *DiskStore) SaveState(ctx context.Context, state *state.State) error {
	filePath := fmt.Sprintf("%s/state.json", s.baseDir)
	// Open the file for writing
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Serialize the State to JSON
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Pretty-print JSON
	if err := encoder.Encode(state); err != nil {
		return fmt.Errorf("failed to encode state to JSON: %w", err)
	}

	return nil
}

// LoadState loads the State from a file on disk
func (s *DiskStore) LoadState(ctx context.Context) (*state.State, error) {
	filePath := fmt.Sprintf("%s/state.json", s.baseDir)
	// Open the file for reading
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Deserialize the JSON into a State object
	var loadedState state.State
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&loadedState); err != nil {
		return nil, fmt.Errorf("failed to decode state from JSON: %w", err)
	}

	return &loadedState, nil
}
