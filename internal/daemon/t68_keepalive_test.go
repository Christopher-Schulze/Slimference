package daemon

import (
	"strings"
	"testing"
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
