package hostmetrics

import (
	"io/fs"
	"os"
)

// ProcessSnapshot is a content-free resource sample for one local process.
type ProcessSnapshot struct {
	PID              int
	RSSBytes         int64
	RSSKnown         bool
	CPUUserSeconds   float64
	CPUSystemSeconds float64
	CPUPercent       float64
	CPUKnown         bool
	DiskReadOps      int64
	DiskWriteOps     int64
	DiskIOKnown      bool
}

// CurrentProcess returns the best available local resource sample for pid.
// Unsupported platforms or probe failures leave fields unknown instead of
// guessing.
func CurrentProcess(pid int) ProcessSnapshot {
	s := ProcessSnapshot{PID: pid}
	if rss, ok := currentRSSBytes(pid); ok {
		s.RSSBytes = rss
		s.RSSKnown = true
	}
	if pid == os.Getpid() {
		if cpu, ok := currentCPUTime(); ok {
			s.CPUUserSeconds = cpu.UserSeconds
			s.CPUSystemSeconds = cpu.SystemSeconds
			s.CPUKnown = true
		}
		if disk, ok := currentDiskIO(); ok {
			s.DiskReadOps = disk.ReadOps
			s.DiskWriteOps = disk.WriteOps
			s.DiskIOKnown = true
		}
	}
	return s
}

// CPUTime is the process CPU time consumed so far.
type CPUTime struct {
	UserSeconds   float64
	SystemSeconds float64
}

// DiskIO is the process lifetime block I/O operation count reported by the OS.
type DiskIO struct {
	ReadOps  int64
	WriteOps int64
}

// DirectorySizeBytes returns the recursive size of root. It is best-effort and
// bounded by maxEntries so admin polling cannot spend unbounded time in a large
// state tree. Missing directories are known-empty.
func DirectorySizeBytes(root string, maxEntries int) (int64, bool) {
	if root == "" {
		return 0, false
	}
	if maxEntries <= 0 {
		maxEntries = 20_000
	}
	var total int64
	seen := 0
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		seen++
		if seen > maxEntries {
			return fs.SkipAll
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err == nil {
			total += info.Size()
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return 0, true
		}
		return 0, false
	}
	return total, true
}
