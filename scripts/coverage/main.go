// Command coverage runs go test with coverage and optionally enforces a minimum total %.
// Usage (from module root): go run ./scripts/coverage -min=99.5
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: separated from main so unit tests drive
// it without os.Exit. Returns the process exit code.
func run(args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("coverage", flag.ContinueOnError)
	minPct := fs.Float64("min", 0, "if >0, exit 1 when total coverage is below this percent (e.g. 99.5)")
	keep := fs.Bool("keep", false, "do not delete the generated coverage profile")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(stderr, "coverage: %v\n", err)
		return 2
	}

	prof := filepath.Join(root, "coverage.out")
	_ = os.Remove(prof)
	if !*keep {
		defer func() { _ = os.Remove(prof) }()
	}

	testCmd := exec.Command("go", "test",
		"-coverprofile="+prof,
		"-covermode=atomic",
		"./cmd/...",
		"./internal/...",
	)
	testCmd.Dir = root
	testCmd.Stdout = stdout
	testCmd.Stderr = stderr
	if err := testCmd.Run(); err != nil {
		return 1
	}

	coverCmd := exec.Command("go", "tool", "cover", "-func="+prof)
	coverCmd.Dir = root
	out, err := coverCmd.Output()
	if err != nil {
		fmt.Fprintf(stderr, "coverage: go tool cover: %v\n", err)
		return 1
	}

	total, ok := parseTotalPercent(string(out))
	if !ok {
		fmt.Fprintf(stderr, "coverage: could not parse total from cover -func output\n")
		return 1
	}
	fmt.Fprintf(stdout, "total coverage (statements): %.1f%%\n", total)

	if *minPct > 0 && total+1e-9 < *minPct {
		fmt.Fprintf(stderr, "coverage: %.1f%% < required %.1f%%\n", total, *minPct)
		return 1
	}
	return 0
}

var totalLine = regexp.MustCompile(`total:\s+\(statements\)\s+([\d.]+)%`)

func parseTotalPercent(funcOutput string) (float64, bool) {
	lines := strings.Split(strings.TrimSpace(funcOutput), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if m := totalLine.FindStringSubmatch(strings.TrimSpace(lines[i])); len(m) == 2 {
			var v float64
			_, err := fmt.Sscanf(m[1], "%f", &v)
			if err != nil {
				return 0, false
			}
			return v, true
		}
	}
	return 0, false
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
