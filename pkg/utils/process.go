package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// SendSignalToPIDFile sends a signal to the process identified by the PID file
func SendSignalToPIDFile(pidFile string, sig syscall.Signal) error {
	if pidFile == "" {
		return fmt.Errorf("PID file path is empty")
	}

	pid, err := readPIDFile(pidFile)
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	if err := signalProcess(pid, sig); err != nil {
		return fmt.Errorf("failed to signal process: %w", err)
	}

	return nil
}

// readPIDFile reads and parses the PID file
func readPIDFile(pidFile string) (int, error) {
	pidBytes, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID format in file: %w", err)
	}

	if pid <= 0 {
		return 0, fmt.Errorf("invalid PID value: %d", pid)
	}

	return pid, nil
}

// signalProcess finds the process and sends the specified signal
func signalProcess(pid int, sig syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("process not found: %w", err)
	}

	if err := process.Signal(sig); err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	return nil
}
