package staleread

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// SupersededCommandStats summarises what PruneSupersededCommandOutputs did.
type SupersededCommandStats struct {
	BlocksReplaced int
	BytesReplaced  int
	CommandsPruned int
}

// PruneSupersededCommandOutputs walks the message slice and identifies
// tool_result blocks whose corresponding tool_use ran a command that was
// re-run later in the conversation. The earlier (superseded) command output
// is replaced with a compact marker, keeping only the most recent output
// for each unique command line.
//
// Safety guarantees:
//   - Only tool_result blocks with a matching tool_use are considered
//   - Only commands that are deterministic and repeatable are eligible
//     (git status, git diff, git log, go test, etc.)
//   - The MOST RECENT output for each command is always preserved
//   - User messages and model reasoning are NEVER touched
//   - Fail-open: if no tool_use metadata is available, no pruning happens
//
// This is complementary to PruneObsoleteReads (which handles file reads
// superseded by edits) and AgeMessages (which handles stale repeated reads).
func PruneSupersededCommandOutputs(messages []types.Message, opts ObsoleteOptions) ([]types.Message, SupersededCommandStats) {
	if len(messages) == 0 {
		return messages, SupersededCommandStats{}
	}

	// First pass: build tool_use_id -> command line map, and
	// command line -> list of message indices where it appears.
	idToCommand := map[string]string{}
	commandTurns := map[string][]int{}
	for i, msg := range messages {
		for _, blk := range msg.Content {
			if blk.Type != "tool_use" {
				continue
			}
			if blk.ToolUseID == "" {
				continue
			}
			cmd := extractCommandLine(blk)
			if cmd == "" {
				continue
			}
			if !isPrunableCommand(cmd) {
				continue
			}
			idToCommand[blk.ToolUseID] = cmd
			commandTurns[cmd] = append(commandTurns[cmd], i)
		}
	}

	if len(idToCommand) == 0 {
		return messages, SupersededCommandStats{}
	}

	// Second pass: for each command, find the LAST occurrence.
	// All earlier occurrences are superseded and can be pruned.
	lastOccurrence := map[string]int{}
	for cmd, turns := range commandTurns {
		if len(turns) == 0 {
			continue
		}
		lastOccurrence[cmd] = turns[len(turns)-1]
	}

	// Third pass: replace superseded command outputs with markers.
	stats := SupersededCommandStats{}
	prunedCommands := map[string]struct{}{}
	out := make([]types.Message, len(messages))
	copy(out, messages)
	for i, msg := range out {
		var newContent []types.ContentBlock
		mutated := false
		for j, blk := range msg.Content {
			if blk.Type != "tool_result" {
				continue
			}
			refID := toolResultRefID(blk)
			cmd, ok := idToCommand[refID]
			if !ok {
				continue
			}
			lastTurn, hasLast := lastOccurrence[cmd]
			if !hasLast || lastTurn <= i {
				continue // this IS the last occurrence, keep it
			}
			if !mutated {
				newContent = make([]types.ContentBlock, len(msg.Content))
				copy(newContent, msg.Content)
				mutated = true
			}
			origLen := len(blk.Text)
			marker := fmt.Sprintf("[context-elided kind=superseded-command cmd=%q superseded_turn=%d]", cmd, lastTurn)
			rewritten := blk
			rewritten.Text = marker
			rewritten.RawBlock = nil
			newContent[j] = rewritten
			stats.BlocksReplaced++
			stats.BytesReplaced += origLen - len(marker)
			prunedCommands[cmd] = struct{}{}
		}
		if mutated {
			out[i].Content = newContent
		}
	}
	stats.CommandsPruned = len(prunedCommands)
	return out, stats
}

// extractCommandLine extracts the command line from a tool_use block.
// Supports both Codex exec_command (arguments.cmd) and shell tools.
func extractCommandLine(blk types.ContentBlock) string {
	if blk.ToolInput == "" {
		return ""
	}
	// Try JSON parse for structured tool input
	var input map[string]any
	if err := jsonUnmarshal(blk.ToolInput, &input); err == nil {
		if cmd, ok := input["cmd"].(string); ok && cmd != "" {
			return strings.TrimSpace(cmd)
		}
		if command, ok := input["command"].(string); ok && command != "" {
			return strings.TrimSpace(command)
		}
	}
	// Fall back to raw input as command
	return strings.TrimSpace(blk.ToolInput)
}

// isPrunableCommand returns true for commands that are deterministic and
// repeatable — running them again produces a fresh result that supersedes
// the earlier one. Non-deterministic commands (e.g., `tail -f`, `watch`)
// are NOT prunable because the earlier output may contain unique information.
func isPrunableCommand(cmd string) bool {
	if cmd == "" {
		return false
	}
	lower := strings.ToLower(cmd)

	// Non-deterministic commands — NEVER prune
	nonDeterministic := []string{
		"tail -f", "follow", "watch ", "top ", "htop",
		"less ", "more ", "man ", "vi ", "vim ", "nano ",
		"ssh ", "telnet ", "nc ", "netcat ",
	}
	for _, nd := range nonDeterministic {
		if strings.Contains(lower, nd) {
			return false
		}
	}

	// Deterministic, repeatable commands — safe to prune when superseded
	prunablePrefixes := []string{
		"git status", "git diff", "git log", "git show", "git branch",
		"git remote", "git tag", "git stash list",
		"go test", "go build", "go vet", "go fmt", "go mod",
		"cargo test", "cargo build", "cargo check", "cargo clippy",
		"npm test", "npm run", "npx tsc", "npx eslint",
		"pytest", "python -m pytest",
		"make ", "cmake ",
		"rg ", "grep ", "find ",
		"ls ", "ls -", "ll ", "file ",
		"wc ", "cat ", "head ", "tail -",
		"docker ps", "docker images", "docker logs",
		"kubectl get", "kubectl describe", "kubectl logs",
		"helm list", "helm status",
		"systemctl status", "journalctl",
		"df ", "du ", "free ", "uname ",
		"ps ", "lsof ", "netstat ", "ss ",
	}
	for _, prefix := range prunablePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// jsonUnmarshal is a helper to avoid importing encoding/json in the
// function signature. Uses the same approach as the rest of the package.
func jsonUnmarshal(data string, v any) error {
	return json.Unmarshal([]byte(data), v)
}
