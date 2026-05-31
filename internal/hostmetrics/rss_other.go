//go:build !darwin && !linux

package hostmetrics

func currentRSSBytes(_ int) (int64, bool) {
	return 0, false
}
