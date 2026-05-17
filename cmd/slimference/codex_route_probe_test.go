package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/codexroute"
)

func TestCodexRouteProbeReportsRouteAndAutoDecision(t *testing.T) {
	home := t.TempDir()
	configPath := codexroute.ConfigPath(home)
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir codex config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"gpt-5\"\n"), 0o600); err != nil {
		t.Fatalf("write codex config: %v", err)
	}
	proxyURL := codexroute.ProxyURL("127.0.0.1", "8990")
	if _, err := codexroute.EnableWithOptions(home, proxyURL, codexroute.Options{Transport: codexroute.TransportWSS}); err != nil {
		t.Fatalf("EnableWithOptions: %v", err)
	}
	if err := codexroute.SaveCertification(home, codexroute.CertificationState{
		CodexVersion:       "codex-test",
		SlimferenceVersion: "slim-test",
		Passed:             true,
		FramesReencoded:    2,
	}); err != nil {
		t.Fatalf("SaveCertification: %v", err)
	}

	probe := &codexRouteProbe{
		home:               home,
		proxyURL:           proxyURL,
		codexVersionFn:     func() string { return "codex-test" },
		slimferenceVersion: "slim-test",
		healthFn:           func(string, string) error { return nil },
		port:               "8990",
	}
	got := probe.ProbeCodexRoute(context.Background())
	if !got.Exists || !got.Enabled || !got.Complete || got.Transport != "wss" {
		t.Fatalf("route state=%+v", got)
	}
	if !got.DaemonReachable || got.DaemonError != "" {
		t.Fatalf("daemon state=%+v", got)
	}
	if got.AutoTransport != "wss" || !got.WSSCertified || got.FallbackReason != "" {
		t.Fatalf("auto state=%+v", got)
	}
}

func TestCodexRouteProbeFallsBackHTTPWhenCertificationMissing(t *testing.T) {
	home := t.TempDir()
	probe := &codexRouteProbe{
		home:               home,
		proxyURL:           codexroute.ProxyURL("127.0.0.1", "8990"),
		codexVersionFn:     func() string { return "codex-test" },
		slimferenceVersion: "slim-test",
		healthFn: func(string, string) error {
			return os.ErrNotExist
		},
	}
	got := probe.ProbeCodexRoute(context.Background())
	if got.AutoTransport != "http" || got.WSSCertified {
		t.Fatalf("auto state=%+v", got)
	}
	if !strings.Contains(got.FallbackReason, "missing") {
		t.Fatalf("fallback reason=%q", got.FallbackReason)
	}
	if got.DaemonReachable || got.DaemonError == "" {
		t.Fatalf("daemon state=%+v", got)
	}
}
