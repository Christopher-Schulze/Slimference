package proxy

import (
	"path/filepath"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/control/apps"
)

func TestProxyAppsManagerNilByDefault(t *testing.T) {
	p := New(config.Defaults())
	if p.AppsManager() != nil {
		t.Fatalf("expected nil manager by default")
	}
}

func TestProxySetAppsManagerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := New(config.Defaults())

	p.SetAppsManager(m)
	if got := p.AppsManager(); got != m {
		t.Fatalf("AppsManager mismatch: got %p want %p", got, m)
	}
}

func TestProxySetAppsManagerNilClears(t *testing.T) {
	dir := t.TempDir()
	m, err := apps.NewManager(filepath.Join(dir, "apps.toml"))
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	p := New(config.Defaults())

	p.SetAppsManager(m)
	p.SetAppsManager(nil)
	if p.AppsManager() != nil {
		t.Fatalf("clear failed")
	}
}

func TestProxyOutputReduceCountersSnapshotEmpty(t *testing.T) {
	p := New(config.Defaults())
	snap := p.OutputReduceCountersSnapshot()
	if snap.StreamcutFired != 0 || snap.RepdetResponsesRewritten != 0 {
		t.Fatalf("expected zero snapshot, got %+v", snap)
	}
}

func TestProxyOutputReduceCountersSnapshotAfterBumps(t *testing.T) {
	p := New(config.Defaults())

	p.outputReduceCounters.RecordStreamcutFire(1024)
	p.outputReduceCounters.RecordRepdetRewrite(3, 512)
	p.outputReduceCounters.RecordBeTerseInjection(48)

	snap := p.OutputReduceCountersSnapshot()
	if snap.StreamcutFired != 1 {
		t.Errorf("streamcut fired=%d", snap.StreamcutFired)
	}
	if snap.RepdetResponsesRewritten != 1 || snap.RepdetMatchesRewritten != 3 || snap.RepdetBytesSaved != 512 {
		t.Errorf("repdet snapshot wrong: %+v", snap)
	}
	if snap.BeterseInjections != 1 || snap.BeterseHintBytes != 48 {
		t.Errorf("beterse snapshot wrong: %+v", snap)
	}
}
