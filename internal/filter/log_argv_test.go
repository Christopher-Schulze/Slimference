package filter

import (
	"testing"
)

func TestIsLogReadingArgv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		argv []string
		want bool
	}{
		{[]string{"tail", "-f", "app.log"}, true},
		{[]string{"tail", "-F", "app.log"}, true},
		{[]string{"head", "-20", "debug.log"}, true},
		{[]string{"less", "server.out"}, true},
		{[]string{"more", "server.err"}, true},
		{[]string{"cat", "app.log"}, true},
		{[]string{"journalctl", "-u", "nginx"}, true},
		{[]string{"lnav", "syslog"}, true},
		{[]string{"docker", "logs", "ctr"}, true},
		{[]string{"kubectl", "logs", "pod"}, true},
		{[]string{"podman", "logs", "ctr"}, true},
		{[]string{"grep", "ERROR", "app.log"}, true},
		{[]string{"awk", "{print $1}", "server.log"}, true},
		{[]string{"sed", "s/foo/bar/", "app.txt"}, true},
		{[]string{"rg", "WARN", "app.err"}, true},
		{[]string{"grep", "ERROR", "app.go"}, false},
		{[]string{"grep", "pattern"}, false},
		{[]string{"python", "script.py"}, false},
		{[]string{"go", "test"}, false},
		{[]string{}, false},
		{[]string{"cat"}, false},
		{[]string{"tail"}, false},
		{[]string{"tail", "-f", "Makefile"}, true},
	}
	for _, tt := range tests {
		got := isLogReadingArgv(tt.argv)
		if got != tt.want {
			t.Errorf("isLogReadingArgv(%v) = %v, want %v", tt.argv, got, tt.want)
		}
	}
}
