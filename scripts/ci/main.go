// Command ci runs the full CI pipeline locally.
// Usage (from module root): go run ./scripts/ci
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type step struct {
	label string
	cmd   string
	args  []string
}

func defaultSteps() []step {
	return []step{
		{
			label: "go vet",
			cmd:   "go",
			args:  []string{"vet", "./cmd/...", "./internal/..."},
		},
		{
			label: "go build",
			cmd:   "go",
			args:  []string{"build", "./cmd/..."},
		},
		{
			label: "go test",
			cmd:   "go",
			args:  []string{"test", "./cmd/...", "./internal/...", "-timeout", "120s"},
		},
		{
			label: "coverage gate",
			cmd:   "go",
			args:  []string{"run", "./scripts/coverage", "-min=100"},
		},
		{
			label: "codex smoke gate",
			cmd:   "go",
			args:  []string{"run", "./scripts/benchmarks", "codex-smoke-gate", "tests/fixtures/codex"},
		},
	}
}

func main() {
	os.Exit(run(defaultSteps(), os.Stdout, os.Stderr))
}

// run executes the given steps with the supplied IO streams and returns the
// process exit code. Split out of main so unit tests can drive the runner
// without actually calling os.Exit. The caller provides the steps so tests
// can run with mocked cheap commands.
func run(steps []step, stdout, stderr *os.File) int {
	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(stderr, "ci: %v\n", err)
		return 2
	}

	total := len(steps)
	for i, s := range steps {
		fmt.Fprintf(stdout, "[%d/%d] %s\n", i+1, total, s.label)
		c := exec.Command(s.cmd, s.args...)
		c.Dir = root
		c.Stdout = stdout
		c.Stderr = stderr
		if err := c.Run(); err != nil {
			fmt.Fprintf(stdout, "\nFAIL: step %d/%d (%s)\n", i+1, total, s.label)
			return 1
		}
	}

	fmt.Fprintf(stdout, "\nPASS: all %d steps completed\n", total)
	return 0
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
