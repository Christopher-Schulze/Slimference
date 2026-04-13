// Command coverage runs go test with coverage and optionally enforces a minimum total %.
// Usage (from module root): go run ./scripts/coverage -- -min=100
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
	minPct := flag.Float64("min", 0, "if >0, exit 1 when total coverage is below this percent (e.g. 100)")
	keep := flag.Bool("keep", false, "do not delete the generated coverage profile")
	flag.Parse()

	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage: %v\n", err)
		os.Exit(2)
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
	testCmd.Stdout = os.Stdout
	testCmd.Stderr = os.Stderr
	if err := testCmd.Run(); err != nil {
		os.Exit(1)
	}

	coverCmd := exec.Command("go", "tool", "cover", "-func="+prof)
	coverCmd.Dir = root
	out, err := coverCmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage: go tool cover: %v\n", err)
		os.Exit(1)
	}

	total, ok := parseTotalPercent(string(out))
	if !ok {
		fmt.Fprintf(os.Stderr, "coverage: could not parse total from cover -func output\n")
		os.Exit(1)
	}
	fmt.Printf("total coverage (statements): %.1f%%\n", total)

	if *minPct > 0 && total+1e-9 < *minPct {
		fmt.Fprintf(os.Stderr, "coverage: %.1f%% < required %.1f%%\n", total, *minPct)
		os.Exit(1)
	}
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
