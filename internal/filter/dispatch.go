package filter

import (
	"path/filepath"
	"strings"
)

// ClassifyCommand returns a coarse bucket for the first token of argv (Layer-0 routing).
// Used for metrics and future built-in filters (git, docker, …).
func ClassifyCommand(argv []string) string {
	if len(argv) == 0 {
		return "empty"
	}
	base := filepath.Base(strings.TrimPrefix(argv[0], "./"))
	switch base {
	case "git", "docker", "kubectl", "npm", "pnpm", "cargo", "go", "pytest", "rg", "grep":
		return base
	default:
		return "generic"
	}
}
