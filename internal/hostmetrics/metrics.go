package hostmetrics

// ProcessSnapshot is a content-free resource sample for one local process.
type ProcessSnapshot struct {
	PID      int
	RSSBytes int64
	RSSKnown bool
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
	return s
}
