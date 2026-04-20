package daemon

import (
	"strings"
	"testing"
	"time"
)

// TestT68_KeepAliveCrashedOnly pins the T68 contract: launchd restarts on
// crashes but NOT on clean exits. SuccessfulExit=false is the critical key.
func TestT68_KeepAliveCrashedOnly(t *testing.T) {
	plist := GenerateLaunchdPlist("/usr/local/bin/slimference")

	// Must be a dict, not the old "<true/>" bool that restarted on every exit.
	if strings.Contains(plist, "<key>KeepAlive</key>\n    <true/>") {
		t.Fatal("KeepAlive is still <true/> - clean stops will be restarted")
	}
	if !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatal("KeepAlive key missing")
	}
	if !strings.Contains(plist, "<key>SuccessfulExit</key>") {
		t.Fatal("KeepAlive dict missing SuccessfulExit")
	}
	if !strings.Contains(plist, "<key>Crashed</key>") {
		t.Fatal("KeepAlive dict missing Crashed")
	}
	// SuccessfulExit=false means "do not restart on exit 0".
	if !strings.Contains(plist,
		"<key>SuccessfulExit</key>\n        <false/>") {
		t.Fatal("SuccessfulExit should be <false/>")
	}
	// Crashed=true means "restart when the process crashes".
	if !strings.Contains(plist, "<key>Crashed</key>\n        <true/>") {
		t.Fatal("Crashed should be <true/>")
	}
}

// TestT68_ThrottleIntervalPresent ensures a minimum gap between restart
// attempts so a crash-loop does not peg the CPU.
func TestT68_ThrottleIntervalPresent(t *testing.T) {
	plist := GenerateLaunchdPlist("/usr/local/bin/slimference")
	if !strings.Contains(plist, "<key>ThrottleInterval</key>") {
		t.Fatal("ThrottleInterval missing")
	}
	// Our chosen value is 2 seconds.
	if !strings.Contains(plist, "<integer>2</integer>") {
		t.Fatal("ThrottleInterval value != 2")
	}
}

// TestT68_RunAtLoadStillTrue makes sure the KeepAlive reshape did not also
// lose the "start immediately on launchctl load" behaviour.
func TestT68_RunAtLoadStillTrue(t *testing.T) {
	plist := GenerateLaunchdPlist("/usr/local/bin/slimference")
	if !strings.Contains(plist,
		"<key>RunAtLoad</key>\n    <true/>") {
		t.Fatal("RunAtLoad must stay true")
	}
}

// TestT68_ParseLaunchctlList_ExtractsFields confirms the parser picks up
// PID + LastExitStatus from a realistic launchctl-list fragment.
func TestT68_ParseLaunchctlList_ExtractsFields(t *testing.T) {
	fixture := `{
	"LimitLoadToSessionType" = "Aqua";
	"Label" = "com.slimference.daemon";
	"OnDemand" = false;
	"LastExitStatus" = 256;
	"PID" = 54321;
	"TimeOut" = 30;
};
`
	s := parseLaunchctlList(fixture)
	if s.PID != 54321 {
		t.Fatalf("PID = %d", s.PID)
	}
	if s.LastExitStatus != 256 {
		t.Fatalf("LastExitStatus = %d", s.LastExitStatus)
	}
}

// TestT68_ParseLaunchctlList_MissingFieldsReturnZero covers the "daemon not
// loaded" edge case where the output is empty.
func TestT68_ParseLaunchctlList_MissingFieldsReturnZero(t *testing.T) {
	s := parseLaunchctlList("")
	if s.PID != 0 || s.LastExitStatus != 0 {
		t.Fatalf("expected zero snapshot, got %+v", s)
	}
}

// TestT68_FormatStatus_IncludesUptimeAndLaunchd confirms the JSON status
// surface grew the T68 fields when launchctl inspection succeeds.
func TestT68_FormatStatus_IncludesUptimeAndLaunchd(t *testing.T) {
	origIsRunning := isRunningFn
	defer func() { isRunningFn = origIsRunning }()
	isRunningFn = func() (bool, *PIDFile, error) {
		return true, &PIDFile{
			PID:       1234,
			Port:      8990,
			StartedAt: time.Now().Add(-5 * time.Minute),
		}, nil
	}

	origLaunchctl := launchctlInspectFn
	defer func() { launchctlInspectFn = origLaunchctl }()
	launchctlInspectFn = func(label string) (launchctlSnapshot, error) {
		return launchctlSnapshot{PID: 1234, LastExitStatus: 0}, nil
	}

	raw, err := FormatStatus()
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, substr := range []string{
		`"uptime_seconds":`,
		`"launchd_label": "com.slimference.daemon"`,
		`"launchd_keepalive": "Crashed=true, SuccessfulExit=false"`,
	} {
		if !strings.Contains(s, substr) {
			t.Errorf("status missing %q:\n%s", substr, s)
		}
	}
}
