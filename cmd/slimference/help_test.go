package main

import (
	"strings"
	"testing"
)

func TestWantsHelp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", nil, false},
		{"help long", []string{"--help"}, true},
		{"help short", []string{"-h"}, true},
		{"help bare", []string{"help"}, true},
		{"help with topic", []string{"help", "filter"}, true},
		{"subcommand", []string{"doctor"}, false},
		{"unknown flag", []string{"--weird"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wantsHelp(tc.args); got != tc.want {
				t.Fatalf("wantsHelp(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestWantsVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want bool
	}{
		{"empty", nil, false},
		{"long", []string{"--version"}, true},
		{"short", []string{"-V"}, true},
		{"lower-v is not version", []string{"-v"}, false},
		{"subcommand", []string{"doctor"}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wantsVersion(tc.args); got != tc.want {
				t.Fatalf("wantsVersion(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestWantsHeadless(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		want bool
	}{
		{"empty", nil, "", false},
		{"no-tui", []string{"--no-tui"}, "", true},
		{"headless", []string{"--headless"}, "", true},
		{"env only", nil, "1", true},
		{"stopped by subcommand", []string{"doctor", "--no-tui"}, "", false},
		{"multiple flags before subcommand", []string{"--log-level", "debug", "--no-tui"}, "", true},
		{"port only, no headless", []string{"--port", "9000"}, "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("SLIMFERENCE_HEADLESS", tc.env)
			} else {
				t.Setenv("SLIMFERENCE_HEADLESS", "")
			}
			if got := wantsHeadless(tc.args); got != tc.want {
				t.Fatalf("wantsHeadless(%v) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}

func TestHelpTopLevelContainsKeywords(t *testing.T) {
	t.Parallel()
	out := helpTopLevel()
	for _, kw := range []string{"slimference", "SUBCOMMANDS", "doctor", "filter", "hook", "--no-tui", "FIRST STEPS"} {
		if !strings.Contains(out, kw) {
			t.Fatalf("help missing keyword %q", kw)
		}
	}
}

func TestHelpForSubcommandKnown(t *testing.T) {
	t.Parallel()
	topics := []string{"doctor", "filter", "hook", "rewrite", "posttool", "readhook",
		"expand", "checkpoint", "gain", "stats", "debug", "service", "daemon",
		"config", "test", "completion", "trust", "version"}
	for _, topic := range topics {
		topic := topic
		t.Run(topic, func(t *testing.T) {
			t.Parallel()
			out := helpForSubcommand(topic)
			if out == "" {
				t.Fatalf("empty help for %q", topic)
			}
			if !strings.Contains(out, topic) && topic != "version" {
				t.Fatalf("help for %q does not mention topic: %q", topic, out)
			}
		})
	}
}

func TestHelpForSubcommandUnknownFallsBack(t *testing.T) {
	t.Parallel()
	out := helpForSubcommand("nonexistent-topic-xyzzy")
	if !strings.Contains(out, "SUBCOMMANDS") {
		t.Fatalf("unknown topic should fall back to top-level help, got: %q", out)
	}
}

func TestPrintHelpDispatch(t *testing.T) {
	t.Parallel()
	// Ensure printHelp does not panic on the various forms and pulls from the
	// appropriate source.
	printHelp(nil)
	printHelp([]string{"--help"})
	printHelp([]string{"help", "doctor"})
	printHelp([]string{"-h", "filter"})
}
