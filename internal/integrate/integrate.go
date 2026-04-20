// Package integrate wires Slimference into Claude Code and Codex with one
// idempotent command. It manages shell-rc exports, Codex config.toml blocks,
// hook installation, and the launchd service so a new user runs a single
// `slimference integrate install` and has the full pipeline live.
//
// All edits use fenced marker blocks so re-running is a no-op and remove
// is exact. Every first-touch of a user-owned file leaves a timestamped
// backup at `<path>.slim-backup-<ts>` so anxious operators can revert.
package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ProxyURL is the localhost endpoint the proxy binds. Hard-coded here so
// integrate does not depend on a full config load to know where to point
// clients. Config overrides via proxy.listen_port / listen_address are
// supported through Options.ProxyURL.
const ProxyURL = "http://127.0.0.1:8990"

// markerStart and markerEnd fence every block we write. Keep them identical
// across every file type so detection is a simple substring search.
const (
	markerStart = "# >>> slimference integration >>>"
	markerEnd   = "# <<< slimference integration <<<"
)

// ClientState enumerates the integration state of a single client.
type ClientState int

const (
	// ClientNotInstalled: the client binary is not on PATH.
	ClientNotInstalled ClientState = iota
	// ClientInstalled: binary present but no Slimference wiring.
	ClientInstalled
	// ClientPartiallyWired: some components wired, some missing.
	ClientPartiallyWired
	// ClientFullyWired: every wire point is Slimference-owned.
	ClientFullyWired
)

func (s ClientState) String() string {
	switch s {
	case ClientNotInstalled:
		return "not_installed"
	case ClientInstalled:
		return "installed"
	case ClientPartiallyWired:
		return "partially_wired"
	case ClientFullyWired:
		return "fully_wired"
	}
	return "unknown"
}

// ClientStatus describes the wiring of one client (Claude Code or Codex).
type ClientStatus struct {
	Name       string      `json:"name"`
	State      ClientState `json:"state"`
	BinaryPath string      `json:"binary_path,omitempty"`
	ConfigPath string      `json:"config_path,omitempty"`
	Details    []string    `json:"details"`
}

// DaemonStatus describes the state of the launchd service on macOS.
type DaemonStatus struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"`
	PID       int    `json:"pid,omitempty"`
	Health    string `json:"health,omitempty"`
	Details   []string `json:"details"`
}

// Report is the aggregate outcome of a detect/install/remove pass.
type Report struct {
	Claude ClientStatus `json:"claude"`
	Codex  ClientStatus `json:"codex"`
	Daemon DaemonStatus `json:"daemon"`
	Writes []WriteEvent `json:"writes,omitempty"`
	Errors []string     `json:"errors,omitempty"`
}

// WriteEvent records a single side-effecting operation.
type WriteEvent struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "wrote_block" | "removed_block" | "backed_up" | "skipped_idempotent"
}

// Options controls an Install or Remove run.
type Options struct {
	// DryRun prints the intended actions without touching any file.
	DryRun bool
	// Client filters the run to one of {"all", "claude", "codex", "daemon"}.
	// Empty defaults to "all".
	Client string
	// Force re-applies blocks even if the marker is already present (used to
	// self-heal corrupted content inside an existing block).
	Force bool
	// ProxyURL overrides the default `http://127.0.0.1:8990` endpoint.
	// Useful when the user runs on a non-default port.
	ProxyURL string
	// HomeDir overrides os.UserHomeDir for tests.
	HomeDir string
}

// resolveProxyURL returns the configured URL or the default constant.
func (o Options) resolveProxyURL() string {
	if strings.TrimSpace(o.ProxyURL) != "" {
		return strings.TrimSpace(o.ProxyURL)
	}
	return ProxyURL
}

// resolveHome returns the HOME directory, honouring Options override first.
func (o Options) resolveHome() (string, error) {
	if o.HomeDir != "" {
		return o.HomeDir, nil
	}
	return os.UserHomeDir()
}

// backupOnce copies src to `<src>.slim-backup-<ts>` if no backup exists yet
// for this integration run. It is a best-effort helper; if the source does
// not exist yet (first-time install) it returns nil.
func backupOnce(src string) (string, error) {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	dst := fmt.Sprintf("%s.slim-backup-%s", src,
		time.Now().UTC().Format("20060102T150405"))
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return "", err
	}
	return dst, nil
}

// writeAtomic writes data to path through a temp file + rename, preserving
// the destination's mode when it already exists.
func writeAtomic(path string, data []byte, defaultMode os.FileMode) error {
	mode := defaultMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode() & os.ModePerm
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".slim-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

// splitBlock returns (before, blockLines, after, exists). blockLines includes
// the marker lines themselves so removeBlock writes `before + after`.
func splitBlock(content string) (before, block, after string, exists bool) {
	sIdx := strings.Index(content, markerStart)
	if sIdx < 0 {
		return content, "", "", false
	}
	eIdx := strings.Index(content[sIdx:], markerEnd)
	if eIdx < 0 {
		// Unterminated block - treat as corruption; return entire tail as
		// block so the caller can decide to overwrite.
		return content[:sIdx], content[sIdx:], "", true
	}
	eIdx += sIdx + len(markerEnd)
	return content[:sIdx], content[sIdx:eIdx], content[eIdx:], true
}

// replaceOrAppendBlock returns the content with the marker block set to the
// provided body (without markers). The function handles:
//   - content with no existing block: appends a separator + block at end.
//   - content with existing block: replaces in place, preserving before/after.
//   - empty body: removes the block.
// Output always ends in exactly one newline when non-empty so successive
// calls are bit-identical (idempotent round-trip).
func replaceOrAppendBlock(content, body string) string {
	before, _, after, exists := splitBlock(content)
	if strings.TrimSpace(body) == "" {
		// Remove: keep the non-block parts with a single separator.
		merged := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		if tail != "" {
			if merged != "" {
				merged += "\n"
			}
			merged += tail
		}
		merged = strings.TrimRight(merged, "\n")
		if merged != "" {
			merged += "\n"
		}
		return merged
	}
	// Normalise body: no leading/trailing blank lines inside the fence.
	body = strings.TrimSpace(body)
	newBlock := markerStart + "\n" + body + "\n" + markerEnd + "\n"

	var result string
	if !exists {
		// Append: ensure exactly one blank-line separator between old content
		// and the new block.
		prefix := strings.TrimRight(content, "\n")
		if prefix != "" {
			prefix += "\n\n"
		}
		result = prefix + newBlock
	} else {
		prefix := strings.TrimRight(before, "\n")
		tail := strings.TrimLeft(after, "\n")
		if prefix != "" {
			prefix += "\n\n"
		}
		result = prefix + newBlock
		if tail != "" {
			result += "\n" + tail
		}
	}
	// Single trailing newline for a clean POSIX file.
	result = strings.TrimRight(result, "\n") + "\n"
	return result
}

// renderBlock takes a trimmed body string and wraps it in the marker fences.
// Useful for tests and for dry-run previews.
func renderBlock(body string) string {
	return markerStart + "\n" + strings.TrimSpace(body) + "\n" + markerEnd + "\n"
}
