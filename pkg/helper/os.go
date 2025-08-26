package helper

import "runtime"

// IsLinux checks if the current operating system is Linux.
func IsLinux() bool {
	return runtime.GOOS == "linux"
}
