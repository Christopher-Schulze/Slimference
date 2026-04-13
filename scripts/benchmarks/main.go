// Command benchmarks runs go test -bench=. across TokenProxy hot-path packages
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

	fmt.Printf("TokenProxy benchmarks — benchtime=%s count=%d\n", *benchtime, *count)
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
