//go:build darwin || linux

package hostmetrics

import "syscall"

func currentCPUTime() (CPUTime, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return CPUTime{}, false
	}
	return CPUTime{
		UserSeconds:   timevalSeconds(usage.Utime),
		SystemSeconds: timevalSeconds(usage.Stime),
	}, true
}

func currentDiskIO() (DiskIO, bool) {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return DiskIO{}, false
	}
	return DiskIO{
		ReadOps:  usage.Inblock,
		WriteOps: usage.Oublock,
	}, true
}

func timevalSeconds(t syscall.Timeval) float64 {
	return float64(t.Sec) + float64(t.Usec)/1_000_000
}
