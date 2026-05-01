package transparent

import (
	"context"
	"os/exec"
)

// runCommand is the production exec helper used by networksetup,
// security and launchctl wrappers. Tests override the per-Manager
// `exec` field instead so this never runs under go test.
func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}
