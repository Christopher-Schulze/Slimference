// Package codexroute manages the scoped Codex provider route.
//
// This is the product path for routing Codex CLI/Desktop through
// Slimference without machine-wide /etc/hosts or pf changes. It writes
// only a fenced, reversible block in ~/.codex/config.toml.
package codexroute

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/integrate"
)

const (
	markerStart = "# >>> slimference codex route >>>"
	markerEnd   = "# <<< slimference codex route <<<"
	providerID  = "slimference-codex"
)

// Transport selects how Codex should talk to the scoped provider.
type Transport string

const (
	// TransportHTTP disables Codex WebSockets so traffic goes through
	// the stable HTTP Responses path.
	TransportHTTP Transport = "http"
	// TransportWSS enables Codex WebSockets so traffic can use the
	// scoped WSS Phase-F adapter.
	TransportWSS Transport = "wss"
)

// Options controls the marker-owned provider block.
type Options struct {
	Transport Transport
}

// Event records a file operation outcome.
type Event struct {
	Path   string `json:"path"`
	Action string `json:"action"`
}

// Status describes the current Codex route in ~/.codex/config.toml.
type Status struct {
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Enabled    bool   `json:"enabled"`
	Complete   bool   `json:"complete"`
	Conflict   string `json:"conflict,omitempty"`
	LegacyKeys bool   `json:"legacy_keys"`
	BaseURL    string `json:"base_url"`
	Transport  string `json:"transport"`
}

// ConfigPath returns ~/.codex/config.toml inside home.
func ConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

// ProxyURL formats the local Slimference root endpoint.
func ProxyURL(host, port string) string {
	return "http://" + net.JoinHostPort(host, port)
}

// Enable upserts the scoped Codex provider route.
func Enable(home, proxyURL string) (Event, error) {
	return EnableWithOptions(home, proxyURL, Options{Transport: TransportHTTP})
}

// EnableWithOptions upserts the scoped Codex provider route.
func EnableWithOptions(home, proxyURL string, opts Options) (Event, error) {
	path := ConfigPath(home)
	content, exists, err := read(path)
	if err != nil {
		return Event{}, err
	}
	if !exists {
		return Event{Path: path, Action: "skipped_codex_config_absent"}, nil
	}
	opts = normalizeOptions(opts)
	body := blockBody(proxyURL, opts)
	if fenceComplete(content, body) && routeConflict(stripFence(content)) == "" {
		return Event{Path: path, Action: "skipped_idempotent"}, nil
	}
	if conflict := routeConflict(stripFence(content)); conflict != "" {
		return Event{}, fmt.Errorf("codex route conflict: %s", conflict)
	}
	if err := backup(path); err != nil {
		return Event{}, err
	}
	next := insertBeforeFirstTable(stripFence(content), renderBlock(body))
	if err := writeAtomic(path, []byte(next), 0o644); err != nil {
		return Event{}, err
	}
	return Event{Path: path, Action: "wrote_block"}, nil
}

// Disable removes the scoped Codex provider route.
func Disable(home string) (Event, error) {
	path := ConfigPath(home)
	content, exists, err := read(path)
	if err != nil {
		return Event{}, err
	}
	if !exists || !hasFence(content) {
		return Event{Path: path, Action: "skipped_idempotent"}, nil
	}
	if err := backup(path); err != nil {
		return Event{}, err
	}
	if err := writeAtomic(path, []byte(stripFence(content)), 0o644); err != nil {
		return Event{}, err
	}
	return Event{Path: path, Action: "removed_block"}, nil
}

// Inspect reads the scoped Codex provider route status.
func Inspect(home, proxyURL string) (Status, error) {
	return InspectWithOptions(home, proxyURL, Options{})
}

// InspectWithOptions reads the scoped Codex provider route status.
// An empty Transport accepts either HTTP or WSS and reports the detected
// route mode. A non-empty Transport requires that exact managed block.
func InspectWithOptions(home, proxyURL string, opts Options) (Status, error) {
	path := ConfigPath(home)
	content, exists, err := read(path)
	if err != nil {
		return Status{}, err
	}
	body := ""
	if opts.Transport != "" {
		body = blockBody(proxyURL, normalizeOptions(opts))
	}
	stripped := stripFence(content)
	transport := detectTransport(content)
	return Status{
		Path:       path,
		Exists:     exists,
		Enabled:    hasFence(content),
		Complete:   exists && routeComplete(content, proxyURL, body) && routeConflict(stripped) == "",
		Conflict:   routeConflict(stripped),
		LegacyKeys: hasLegacyKeys(stripped),
		BaseURL:    integrate.CodexOpenAIBaseURL(proxyURL),
		Transport:  transport,
	}, nil
}

// PreviewBlock returns the exact fenced block Enable would write.
func PreviewBlock(proxyURL string) string {
	return PreviewBlockWithOptions(proxyURL, Options{Transport: TransportHTTP})
}

// PreviewBlockWithOptions returns the exact fenced block
// EnableWithOptions would write.
func PreviewBlockWithOptions(proxyURL string, opts Options) string {
	return renderBlock(blockBody(proxyURL, normalizeOptions(opts)))
}

func blockBody(proxyURL string, opts Options) string {
	supportsWebSockets := "false"
	if opts.Transport == TransportWSS {
		supportsWebSockets = "true"
	}
	return fmt.Sprintf(`model_provider = %s

[model_providers.%s]
name = "Slimference"
base_url = %s
requires_openai_auth = true
supports_websockets = %s
wire_api = "responses"`,
		strconv.Quote(providerID),
		providerID,
		strconv.Quote(integrate.CodexOpenAIBaseURL(proxyURL)),
		supportsWebSockets,
	)
}

func normalizeOptions(opts Options) Options {
	if opts.Transport == "" {
		opts.Transport = TransportHTTP
	}
	return opts
}

func routeComplete(content, proxyURL, exactBody string) bool {
	if exactBody != "" {
		return fenceComplete(content, exactBody)
	}
	return fenceComplete(content, blockBody(proxyURL, Options{Transport: TransportHTTP})) ||
		fenceComplete(content, blockBody(proxyURL, Options{Transport: TransportWSS}))
}

func detectTransport(content string) string {
	start := strings.Index(content, markerStart)
	end := strings.Index(content, markerEnd)
	if start < 0 || end < start {
		return ""
	}
	block := content[start:end]
	if strings.Contains(block, "supports_websockets = true") {
		return string(TransportWSS)
	}
	if strings.Contains(block, "supports_websockets = false") {
		return string(TransportHTTP)
	}
	return ""
}

func read(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return string(data), true, nil
}

func backup(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dst := fmt.Sprintf("%s.slimference-codex-route-backup-%s",
		path, time.Now().UTC().Format("20060102T150405"))
	return os.WriteFile(dst, data, 0o600)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode() & os.ModePerm
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".slim-codex-route-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func renderBlock(body string) string {
	return markerStart + "\n" + strings.TrimSpace(body) + "\n" + markerEnd + "\n"
}

func hasFence(content string) bool {
	return strings.Contains(content, markerStart) && strings.Contains(content, markerEnd)
}

func fenceComplete(content, body string) bool {
	start := strings.Index(content, markerStart)
	end := strings.Index(content, markerEnd)
	if start < 0 || end < start {
		return false
	}
	block := content[start : end+len(markerEnd)]
	return strings.Contains(block, strings.TrimSpace(body))
}

func stripFence(content string) string {
	start := strings.Index(content, markerStart)
	if start < 0 {
		return content
	}
	end := strings.Index(content[start:], markerEnd)
	if end < 0 {
		return content[:start]
	}
	end += start + len(markerEnd)
	before := strings.TrimRight(content[:start], "\n")
	after := strings.TrimLeft(content[end:], "\n")
	switch {
	case before == "":
		return ensureTrailingNewline(after)
	case after == "":
		return ensureTrailingNewline(before)
	default:
		return ensureTrailingNewline(before + "\n\n" + after)
	}
}

func insertBeforeFirstTable(content, block string) string {
	lines := strings.Split(content, "\n")
	insertAt := len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			insertAt = i
			break
		}
	}
	before := strings.TrimRight(strings.Join(lines[:insertAt], "\n"), "\n")
	after := strings.TrimLeft(strings.Join(lines[insertAt:], "\n"), "\n")
	var out string
	if before != "" {
		out += before + "\n\n"
	}
	out += strings.TrimRight(block, "\n")
	if after != "" {
		out += "\n\n" + after
	}
	return ensureTrailingNewline(out)
}

func routeConflict(content string) string {
	inTable := false
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTable = true
			if trimmed == "[model_providers."+providerID+"]" {
				return "user-owned [model_providers." + providerID + "] already exists"
			}
			continue
		}
		if !inTable && (strings.HasPrefix(trimmed, "model_provider =") ||
			strings.HasPrefix(trimmed, "model_provider=")) {
			if !strings.Contains(trimmed, providerID) {
				return "top-level model_provider already set"
			}
		}
	}
	return ""
}

func hasLegacyKeys(content string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "openai_base_url") ||
			strings.HasPrefix(trimmed, "chatgpt_base_url") {
			return true
		}
	}
	return false
}

func ensureTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}
