//go:build !darwin || !cgo

package main

import "fmt"

func codexDesktopIndicatorSupported() bool {
	return false
}

func runCodexDesktopIndicatorWindow(label string, watchPID int) error {
	return fmt.Errorf("macOS cgo indicator unavailable (label=%q watch_pid=%d)", label, watchPID)
}
