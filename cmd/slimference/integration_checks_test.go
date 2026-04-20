package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// capturedCheck records (label, status, ok) from each check call.
type capturedCheck struct {
	label  string
	status string
	ok     bool
}

func TestRenderIntegrationChecks_HomeErrorFailsFast(t *testing.T) {
	orig := osUserHomeDir
	defer func() { osUserHomeDir = orig }()
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }

	var calls []capturedCheck
	renderIntegrationChecks(func(label string, fn func() (string, bool)) {
		s, ok := fn()
		calls = append(calls, capturedCheck{label, s, ok})
	})

	if len(calls) != 1 {
		t.Fatalf("expected 1 check, got %d", len(calls))
	}
	if calls[0].label != "integrate" || calls[0].ok {
		t.Fatalf("unexpected call: %+v", calls[0])
	}
}

func TestRenderIntegrationChecks_DaemonRunningPath(t *testing.T) {
	origStatus := integrateStatusFn
	defer func() { integrateStatusFn = origStatus }()
	integrateStatusFn = func(opts integrateOptions) integrateReport {
		return integrateReport{
			Claude: integrateReportClient{State: "fully_wired", BinaryPath: "/bin/claude"},
			Codex:  integrateReportClient{State: "installed", BinaryPath: "/bin/codex"},
			Daemon: integrateReportDaemon{Running: true, PID: 99},
		}
	}
	origPlist := daemonPlistPathFn
	defer func() { daemonPlistPathFn = origPlist }()
	tmp := t.TempDir()
	plistPath := filepath.Join(tmp, "com.slimference.daemon.plist")
	_ = os.WriteFile(plistPath, []byte("<plist/>"), 0o644)
	daemonPlistPathFn = func() string { return plistPath }

	var calls []capturedCheck
	renderIntegrationChecks(func(label string, fn func() (string, bool)) {
		s, ok := fn()
		calls = append(calls, capturedCheck{label, s, ok})
	})

	wantLabels := []string{"Claude Code integration", "Codex integration", "Daemon reachable", "launchd plist"}
	if len(calls) != len(wantLabels) {
		t.Fatalf("got %d calls, want %d", len(calls), len(wantLabels))
	}
	for i, w := range wantLabels {
		if calls[i].label != w {
			t.Errorf("call[%d] = %q, want %q", i, calls[i].label, w)
		}
	}
	if !strings.Contains(calls[2].status, "pid 99") {
		t.Fatalf("daemon status missing pid: %q", calls[2].status)
	}
	if !strings.Contains(calls[3].status, plistPath) {
		t.Fatalf("plist status missing path: %q", calls[3].status)
	}
}

func TestRenderIntegrationChecks_DaemonOfflineAndPlistAbsent(t *testing.T) {
	origStatus := integrateStatusFn
	defer func() { integrateStatusFn = origStatus }()
	integrateStatusFn = func(opts integrateOptions) integrateReport {
		return integrateReport{
			Claude: integrateReportClient{State: "not_installed"},
			Codex:  integrateReportClient{State: "partially_wired"},
			Daemon: integrateReportDaemon{Running: false},
		}
	}
	origPlist := daemonPlistPathFn
	defer func() { daemonPlistPathFn = origPlist }()
	daemonPlistPathFn = func() string { return "/nope/does-not-exist.plist" }

	var calls []capturedCheck
	renderIntegrationChecks(func(label string, fn func() (string, bool)) {
		s, ok := fn()
		calls = append(calls, capturedCheck{label, s, ok})
	})

	if !strings.Contains(calls[2].status, "not running") {
		t.Fatalf("daemon status: %q", calls[2].status)
	}
	if !strings.Contains(calls[3].status, "not installed") {
		t.Fatalf("plist status: %q", calls[3].status)
	}
}

func TestDefaultHealthProbe_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("pid 1 uptime 0s"))
	}))
	defer srv.Close()
	ok, status := defaultHealthProbe(srv.URL+"/admin/health", 2*time.Second)
	if !ok {
		t.Fatalf("expected ok, got false (status=%q)", status)
	}
	if !strings.Contains(status, "pid 1") {
		t.Fatalf("status = %q", status)
	}
}

func TestDefaultHealthProbe_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	ok, status := defaultHealthProbe(srv.URL+"/admin/health", 1*time.Second)
	if ok {
		t.Fatal("expected degraded on 500")
	}
	if !strings.Contains(status, "500") {
		t.Fatalf("status = %q", status)
	}
}

func TestDefaultHealthProbe_TimeoutPath(t *testing.T) {
	// Unroutable port forces the retry loop to hit the deadline.
	ok, status := defaultHealthProbe("http://127.0.0.1:1/admin/health", 100*time.Millisecond)
	if ok {
		t.Fatal("expected timeout")
	}
	if status == "" {
		t.Fatal("status empty on timeout")
	}
}
