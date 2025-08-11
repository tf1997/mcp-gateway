package helper

import (
	"os"
	"path/filepath"
)

// GetCfgPath returns the path to the configuration file.
//
// Priority:
// 1. If filename is an absolute path, return it directly.
// 2. Check ./{filename} and ./configs/{filename}
// 3. Otherwise, fallback to /etc/unla/{filename}
func GetCfgPath(filename string) string {
	if filename == "" {
		panic("filename cannot be empty")
	}

	if filepath.IsAbs(filename) {
		return filename
	}

	currentDir := getCurrentDir(filename)
	if currentDir != "" {
		return currentDir
	}

	// fallback
	return filepath.Join("/etc/unla", filename)
}

func getCurrentDir(filename string) string {
	currentDir, err := os.Getwd()
	if err != nil || currentDir == "" {
		return ""
	}

	candidatePath := filepath.Join(currentDir, filename)
	_, err = os.Stat(candidatePath)
	if err == nil {
		absPath, err := filepath.Abs(candidatePath)
		if err == nil {
			return absPath
		}
	}

	candidatePath = filepath.Join(currentDir, "configs", filename)
	_, err = os.Stat(candidatePath)
	if err == nil {
		absPath, err := filepath.Abs(candidatePath)
		if err == nil {
			return absPath
		}
	}
	return ""
}
