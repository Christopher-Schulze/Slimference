package hostmetrics

import (
	"io/fs"
	"os"
	"runtime/metrics"
)

// ProcessSnapshot is a content-free resource sample for one local process.
type ProcessSnapshot struct {
	PID               int
	RSSBytes          int64
	RSSKnown          bool
	GoRetainedBytes   int64
	GoRetainedKnown   bool
	CPUUserSeconds    float64
	CPUSystemSeconds  float64
	CPUPercent        float64
	CPUWindowPercent  float64
	CPUWindowSeconds  float64
	CPUKnown          bool
	CPUWindowKnown    bool
	DiskReadOps       int64
	DiskWriteOps      int64
	DiskReadOpsDelta  int64
	DiskWriteOpsDelta int64
	DiskIOKnown       bool
	DiskWindowKnown   bool
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
		if retained, ok := goRetainedBytes(); ok {
			s.GoRetainedBytes = retained
			s.GoRetainedKnown = true
		}
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

// goRetainedBytes reports the memory the Go runtime actually retains from
// the OS: everything it mapped minus pages already released back. ps-RSS on
// macOS keeps counting MADV_FREE pages until the kernel lazily reclaims
// them, so RSS alone overstates real memory pressure for a pure-Go daemon.
func goRetainedBytes() (int64, bool) {
	samples := []metrics.Sample{
		{Name: "/memory/classes/total:bytes"},
		{Name: "/memory/classes/heap/released:bytes"},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindUint64 || samples[1].Value.Kind() != metrics.KindUint64 {
		return 0, false
	}
	total := samples[0].Value.Uint64()
	released := samples[1].Value.Uint64()
	if released > total {
		return 0, false
	}
	return int64(total - released), true
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
	size, ok, _ := DirectorySizeBytesBounded(root, maxEntries)
	return size, ok
}

// DirectorySizeBytesBounded is DirectorySizeBytes plus a completeness flag.
// complete=false means the scan hit maxEntries before finishing. Callers that
// enforce resource budgets should treat that as pressure instead of accepting a
// partial undercount as healthy.
func DirectorySizeBytesBounded(root string, maxEntries int) (int64, bool, bool) {
	if root == "" {
		return 0, false, false
	}
	if maxEntries <= 0 {
		maxEntries = 20_000
	}
	var total int64
	seen := 0
	complete := true
	err := fs.WalkDir(os.DirFS(root), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		seen++
		if seen > maxEntries {
			complete = false
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
			return 0, true, true
		}
		return 0, false, false
	}
	return total, true, complete
}
