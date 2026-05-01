// Command benchmarks runs go test -bench=. across Slimference hot-path packages
// and formats the output. Useful for detecting performance regressions.
//
// Usage (from module root):
//
//	go run ./scripts/benchmarks                    # default: compression + filter, 3s
//	go run ./scripts/benchmarks -- -benchtime=1s   # shorter run
//	go run ./scripts/benchmarks -- -count=3        # 3 rounds for stability
//	go run ./scripts/benchmarks -- -pkg=compression # single package
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var benchPackages = []string{
	"./internal/compression/...",
	"./internal/filter/...",
}

func main() {
	// T34 session-report subcommand: aggregate a RequestSummary JSONL log
	// produced by the live proxy (or tests/fixtures/sample_session.jsonl)
	// into a human-readable report or a docs/benchmarks.md snippet.
	if len(os.Args) > 1 && os.Args[1] == "session-report" {
		format := "text"
		var path string
		for _, a := range os.Args[2:] {
			switch {
			case a == "--markdown":
				format = "markdown"
			case strings.HasPrefix(a, "--"):
				fmt.Fprintf(os.Stderr, "unknown flag %q\n", a)
				os.Exit(2)
			default:
				if path != "" {
					fmt.Fprintf(os.Stderr, "session-report takes a single path\n")
					os.Exit(2)
				}
				path = a
			}
		}
		if path == "" {
			fmt.Fprintln(os.Stderr, "Usage: session-report [--markdown] <path-to-jsonl>")
			os.Exit(2)
		}
		os.Exit(sessionReportFromPath(path, format))
	}

	// T118 benchmark-corpus subcommand: walk a per-category live-session
	// corpus directory, aggregate each category, and check it against the
	// declared metadata expectations. Used both standalone and as a CI
	// step so a regression in real-world savings ratio fails the build.
	if len(os.Args) > 1 && os.Args[1] == "benchmark-corpus" {
		os.Exit(runBenchmarkCorpus(os.Args[2:]))
	}

	// T75 codex-smoke-gate subcommand: aggregate a Codex evidence corpus
	// directory and assert it still meets the regression baseline declared
	// in `codex-metadata.json`. Exits non-zero on any miss so it can run as
	// a hard step inside `scripts/ci`.
	if len(os.Args) > 1 && os.Args[1] == "codex-smoke-gate" {
		var dir string
		for _, a := range os.Args[2:] {
			if strings.HasPrefix(a, "--") {
				fmt.Fprintf(os.Stderr, "unknown flag %q\n", a)
				os.Exit(2)
			}
			if dir != "" {
				fmt.Fprintln(os.Stderr, "codex-smoke-gate takes a single directory")
				os.Exit(2)
			}
			dir = a
		}
		if dir == "" {
			dir = filepath.Join("tests", "fixtures", "codex")
		}
		os.Exit(codexSmokeGate(dir, os.Stdout, os.Stderr))
	}

	benchtime := flag.String("benchtime", "3s", "benchmark duration per benchmark (go -benchtime)")
	count := flag.Int("count", 1, "number of benchmark rounds (go -count)")
	pkg := flag.String("pkg", "", "restrict to a single package name (e.g. compression, filter)")
	flag.Parse()

	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmarks: %v\n", err)
		os.Exit(2)
	}

	pkgs := benchPackages
	if *pkg != "" {
		pkgs = []string{"./internal/" + *pkg + "/..."}
	}

	fmt.Printf("Slimference benchmarks — benchtime=%s count=%d\n", *benchtime, *count)
	fmt.Println(strings.Repeat("=", 60))

	failed := false
	for _, p := range pkgs {
		fmt.Printf("\n--- %s ---\n", p)
		args := []string{
			"test",
			"-bench=.", "-benchmem",
			"-benchtime=" + *benchtime,
			fmt.Sprintf("-count=%d", *count),
			"-run=^$", // skip regular tests
			p,
		}
		cmd := exec.Command("go", args...)
		cmd.Dir = root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "FAILED: %s: %v\n", p, err)
			failed = true
		}
	}

	fmt.Println(strings.Repeat("=", 60))
	if failed {
		os.Exit(1)
	}
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found from cwd")
		}
		dir = parent
	}
}
