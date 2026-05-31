package hostmetrics

import (
	"os"
	"testing"
)

func TestCurrentProcessInvalidPID(t *testing.T) {
	t.Parallel()
	got := CurrentProcess(0)
	if got.PID != 0 || got.RSSKnown || got.RSSBytes != 0 {
		t.Fatalf("invalid pid snapshot=%+v", got)
	}
}

func TestCurrentProcessSelf(t *testing.T) {
	t.Parallel()
	got := CurrentProcess(os.Getpid())
	if got.PID != os.Getpid() {
		t.Fatalf("pid=%d want %d", got.PID, os.Getpid())
	}
	if got.RSSKnown && got.RSSBytes <= 0 {
		t.Fatalf("known RSS must be positive: %+v", got)
	}
}
