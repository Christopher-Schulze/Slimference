package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleCaptureSessionCmd implements `slimference capture-session [--name=<n>] [--start|--stop|--status]`.
//
// The subcommand is intentionally an instructive wrapper rather than an
// in-process recorder: actual capture happens by routing the proxy's
// existing redacted decision log to a maintainer-named file via the
// SLIMFERENCE_DEBUG_DECISIONS_LOG environment variable. This keeps the
// privacy guarantee surface tiny and aligned with T109 (the redactor
// sits in the existing log path) and T118 (the maintainer-driven
// capture process documented in docs/live-corpus-policy.md).
func handleCaptureSessionCmd(args []string) {
	exitCode := captureSessionRun(args, captureSessionEnv{
		Now:    time.Now,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Home:   os.Getenv("HOME"),
	})
	if exitCode != 0 {
		exitFn(exitCode)
	}
}

type captureSessionEnv struct {
	Now    func() time.Time
	Stdout io.Writer
	Stderr io.Writer
	Home   string
}

func captureSessionRun(args []string, env captureSessionEnv) int {
	name := ""
	mode := "start"
	for _, a := range args {
		switch {
		case a == "--start":
			mode = "start"
		case a == "--stop":
			mode = "stop"
		case a == "--status":
			mode = "status"
		case strings.HasPrefix(a, "--name="):
			name = strings.TrimPrefix(a, "--name=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(env.Stderr, "capture-session: unknown flag %q\n", a)
			return 2
		default:
			fmt.Fprintf(env.Stderr, "capture-session: unexpected argument %q\n", a)
			return 2
		}
	}

	if env.Home == "" {
		fmt.Fprintln(env.Stderr, "capture-session: HOME not set; cannot resolve capture directory")
		return 1
	}
	captureDir := filepath.Join(env.Home, ".slimference", "captures")

	switch mode {
	case "start":
		if name == "" {
			name = fmt.Sprintf("session_%s", env.Now().UTC().Format("20060102_150405"))
		}
		path := filepath.Join(captureDir, name+".jsonl")
		fmt.Fprintf(env.Stdout, "Capture session: %s\n", name)
		fmt.Fprintf(env.Stdout, "Output:          %s\n", path)
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintln(env.Stdout, "To capture, run the proxy with:")
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintf(env.Stdout, "  SLIMFERENCE_DEBUG_DECISIONS_LOG=%s slimference start\n", path)
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintln(env.Stdout, "Each request lands as a redacted RequestSummary line. T109's outbound")
		fmt.Fprintln(env.Stdout, "redactor (default-on) strips secret patterns, normalises absolute")
		fmt.Fprintln(env.Stdout, "paths and drops auth headers before anything reaches disk.")
		fmt.Fprintln(env.Stdout, "")
		fmt.Fprintln(env.Stdout, "Privacy review BEFORE check-in is mandatory. See:")
		fmt.Fprintln(env.Stdout, "  docs/live-corpus-policy.md")
	case "stop":
		fmt.Fprintln(env.Stdout, "Stop the proxy in the shell where it runs (Ctrl+C or `slimference stop`).")
		fmt.Fprintf(env.Stdout, "Captured files live under %s\n", captureDir)
	case "status":
		entries, err := os.ReadDir(captureDir)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(env.Stdout, "No captures yet (%s does not exist).\n", captureDir)
				return 0
			}
			fmt.Fprintf(env.Stderr, "capture-session: read %s: %v\n", captureDir, err)
			return 1
		}
		fmt.Fprintf(env.Stdout, "Captures under %s:\n", captureDir)
		shown := 0
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".jsonl" {
				continue
			}
			fmt.Fprintf(env.Stdout, "  %s\n", e.Name())
			shown++
		}
		if shown == 0 {
			fmt.Fprintln(env.Stdout, "  (none)")
		}
	}
	return 0
}
