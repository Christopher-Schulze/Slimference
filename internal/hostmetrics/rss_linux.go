//go:build linux

package hostmetrics

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func currentRSSBytes(pid int) (int64, bool) {
	if pid <= 0 {
		return 0, false
	}
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "statm"))
	if err != nil {
		return 0, false
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return 0, false
	}
	pages, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pages <= 0 {
		return 0, false
	}
	return pages * int64(os.Getpagesize()), true
}
