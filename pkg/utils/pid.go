package utils

import (
	"fmt"
	"os"
	"path/filepath"
)

// PIDManager handles PID file operations
type PIDManager struct {
	pidFile string
}

// NewPIDManager creates a new PIDManager instance
func NewPIDManager(pidFile string) *PIDManager {
	return &PIDManager{
		pidFile: pidFile,
	}
}

// NewPIDManagerFromConfig creates a new PIDManager instance from config
func NewPIDManagerFromConfig(pidFile string) *PIDManager {
	return NewPIDManager(pidFile)
}

// WritePID writes the current process ID to the PID file
func (p *PIDManager) WritePID() error {
	dir := filepath.Dir(p.pidFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}

	pid := os.Getpid()
	return os.WriteFile(p.pidFile, []byte(fmt.Sprintf("%d\n", pid)), 0644)
}

// RemovePID removes the PID file
func (p *PIDManager) RemovePID() error {
	return os.Remove(p.pidFile)
}

// GetPIDFile returns the PID file path
func (p *PIDManager) GetPIDFile() string {
	return p.pidFile
}
