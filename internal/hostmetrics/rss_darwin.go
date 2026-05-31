//go:build darwin

package hostmetrics

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func currentRSSBytes(pid int) (int64, bool) {
	if pid <= 0 {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, false
	}
	return parsePSRSSKilobytes(string(out))
}

func parsePSRSSKilobytes(raw string) (int64, bool) {
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return 0, false
	}
	kb, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil || kb <= 0 {
		return 0, false
	}
	return kb * 1024, true
}
