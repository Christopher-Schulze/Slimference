//go:build !darwin && !linux

package hostmetrics

func currentCPUTime() (CPUTime, bool) {
	return CPUTime{}, false
}

func currentDiskIO() (DiskIO, bool) {
	return DiskIO{}, false
}
