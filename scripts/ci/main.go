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
	}
}

func main() {
	root, err := findModuleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ci: %v\n", err)
		os.Exit(2)
	}

	steps := defaultSteps()

	total := len(steps)
	for i, s := range steps {
		fmt.Printf("[%d/%d] %s\n", i+1, total, s.label)
		c := exec.Command(s.cmd, s.args...)
		c.Dir = root
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			fmt.Printf("\nFAIL: step %d/%d (%s)\n", i+1, total, s.label)
			os.Exit(1)
		}
	}

	fmt.Printf("\nPASS: all %d steps completed\n", total)
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
