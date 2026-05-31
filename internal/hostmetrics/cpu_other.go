//go:build !darwin && !linux

package hostmetrics

func currentCPUTime() (CPUTime, bool) {
	return CPUTime{}, false
}
