package filter

import (
	"testing"
)

func TestDeniedShellCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf --no-preserve-root /foo", true},
		{"rm -rf /", true},
		{"mkfs.ext4 /dev/sda1", true},
		{"dd if=/dev/zero of=out", true},
		{"() { :|:& };:", true},
		{"echo hello", false},
		{"git status", false},
	}
	for _, tc := range cases {
		den, _ := DeniedShellCommand(tc.cmd)
		if den != tc.want {
			t.Errorf("DeniedShellCommand(%q) = %v, want %v", tc.cmd, den, tc.want)
		}
	}
}

func TestDeniedShellCommand_extraPatterns(t *testing.T) {
	SetExtraDenyPatterns([]string{`^echo\s+BLOCK`})
	t.Cleanup(func() { SetExtraDenyPatterns(nil) })
	if den, why := DeniedShellCommand("echo BLOCK"); !den || why == "" {
		t.Fatalf("want deny, got %v %q", den, why)
	}
	if den, _ := DeniedShellCommand("echo ok"); den {
		t.Fatal("should not match")
	}
}

func TestDeniedShellCommand_emptyString(t *testing.T) {
	t.Parallel()
	if den, _ := DeniedShellCommand(""); den {
		t.Fatal("empty command should not be denied")
	}
}

func TestSetExtraDenyPatterns_skipsEmptyAndInvalid(t *testing.T) {
	// Not parallel — modifies global extraDeny
	t.Cleanup(func() { SetExtraDenyPatterns(nil) })
	// Empty string and invalid pattern should be skipped; valid one should remain
	SetExtraDenyPatterns([]string{"", "[invalid-regex", `^safe`})
	if den, _ := DeniedShellCommand("safe cmd"); !den {
		t.Fatal("valid ^safe pattern should match 'safe cmd'")
	}
}

func TestAskRequired(t *testing.T) {
	t.Setenv("TOKENPROXY_CONFIRM_SUDO", "")
	if !AskRequired("sudo apt update") {
		t.Fatal("expected ask without TOKENPROXY_CONFIRM_SUDO")
	}
	t.Setenv("TOKENPROXY_CONFIRM_SUDO", "1")
	if AskRequired("sudo apt update") {
		t.Fatal("expected no ask when TOKENPROXY_CONFIRM_SUDO=1")
	}
	if AskRequired("apt update") {
		t.Fatal("sudo absent should not ask")
	}
}
