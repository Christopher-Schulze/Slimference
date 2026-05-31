package proxy

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
)

func TestDaemonProbeReportsLocalProcessState(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.startedAt = time.Now().Add(-2 * time.Second)

	got := DaemonProbe{Proxy: p}.ProbeDaemon(context.Background())
	if !got.Running || !got.HealthOK {
		t.Fatalf("daemon health=%+v", got)
	}
	if got.PID != os.Getpid() {
		t.Fatalf("pid=%d want %d", got.PID, os.Getpid())
	}
	if got.Version != Version {
		t.Fatalf("version=%q want %q", got.Version, Version)
	}
	if got.UptimeSec < 1 {
		t.Fatalf("uptime=%d want >=1", got.UptimeSec)
	}
	if got.RSSBytes < 0 {
		t.Fatalf("rss=%d", got.RSSBytes)
	}
}

func TestDaemonProbeNilProxy(t *testing.T) {
	t.Parallel()
	if got := (DaemonProbe{}).ProbeDaemon(context.Background()); got.Running {
		t.Fatalf("nil proxy should not report running: %+v", got)
	}
}
