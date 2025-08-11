package helper

import (
	"os"
	"path/filepath"
)

// GetPIDPath returns the path to the PID file.
//
// Priority:
// 1. If filename is an absolute path, return it directly.
// 2. Check ./{filename} and ./configs/{filename}
// 3. Otherwise, fallback to /var/run/mcp-gateway.pid
func GetPIDPath(filename string) string {
	if filepath.IsAbs(filename) {
		return filename
	}

	currentDir := getPIDCurrentDir(filename)
	if currentDir != "" {
		return currentDir
	}

	// fallback
	return filepath.Join("/var/run/mcp-gateway.pid")
}

func getPIDCurrentDir(filename string) string {
	if filename == "" {
		return ""
	}

	currentDir, err := os.Getwd()
	if err != nil || currentDir == "" {
		return ""
	}

	candidatePath := filepath.Join(currentDir, filename)
	absPath, err := filepath.Abs(candidatePath)
	if err != nil {
		return ""
	}

	// Check if parent directory exists
	parentDir := filepath.Dir(absPath)
	if _, err := os.Stat(parentDir); err == nil {
		return absPath
	}

	return ""
}
