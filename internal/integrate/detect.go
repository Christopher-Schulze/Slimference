package integrate

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/hooks"
)

// binaryOnPath returns the first PATH entry that contains `name`, or "".
// Does not run the binary - cheap `exec.LookPath` wrapper.
func binaryOnPath(name string) string {
	p, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return p
}

// DetectClaude returns the wiring state of Claude Code on this machine.
func DetectClaude(home, shellEnv string) ClientStatus {
	s := ClientStatus{Name: "claude"}

	binPath := binaryOnPath("claude")
	s.BinaryPath = binPath

	// Hook status: the existing internal/hooks package writes
	// ~/.claude/settings.json + ~/.slimference/hooks/*.sh. We check the
	// known artefact rather than parse JSON here to keep this package
	// zero-dependency on internal/hooks.
	hookScript := filepath.Join(home, ".slimference", "hooks", "claude-prompt.sh")
	hookPresent := fileExists(hookScript)

	// Shell env: rc file contains our block AND the env is exported in the
	// running shell.
	rc := DetectRCFile(home, shellEnv)
	rcContent, _, _ := ReadRC(rc.Path)
	_, _, _, rcHasBlock := splitBlock(rcContent)
	envExported := os.Getenv("ANTHROPIC_BASE_URL") == ProxyURL

	s.ConfigPath = rc.Path
	if binPath == "" {
		s.State = ClientNotInstalled
		s.Details = append(s.Details, "claude binary not found on PATH")
		return s
	}

	if hookPresent {
		s.Details = append(s.Details, "hooks: installed")
	} else {
		s.Details = append(s.Details, "hooks: missing")
	}
	if rcHasBlock {
		s.Details = append(s.Details, "shell-rc: wired ("+rc.Path+")")
	} else {
		s.Details = append(s.Details, "shell-rc: not wired ("+rc.Path+")")
	}
	if envExported {
		s.Details = append(s.Details, "ANTHROPIC_BASE_URL: active in current env")
	} else {
		s.Details = append(s.Details, "ANTHROPIC_BASE_URL: not exported (reload shell)")
	}

	wiredCount := 0
	if hookPresent {
		wiredCount++
	}
	if rcHasBlock {
		wiredCount++
	}
	switch wiredCount {
	case 0:
		s.State = ClientInstalled
	case 2:
		s.State = ClientFullyWired
	default:
		s.State = ClientPartiallyWired
	}
	return s
}

// DetectCodex returns the wiring state of Codex CLI on this machine.
func DetectCodex(home string) ClientStatus {
	s := ClientStatus{Name: "codex"}
	binPath := binaryOnPath("codex")
	s.BinaryPath = binPath

	configPath := CodexConfigPath(home)
	s.ConfigPath = configPath
	configExists := fileExists(configPath)

	configComplete := HasCompleteCodexBlock(home, ProxyURL)
	hookState := hooks.InspectCodexHooks(home)
	hookPresent := hookState.Complete()

	if binPath == "" {
		s.State = ClientNotInstalled
		s.Details = append(s.Details, "codex binary not found on PATH")
		return s
	}
	if !configExists {
		s.Details = append(s.Details, "config.toml not found (codex has not been run yet)")
	}
	if hookPresent {
		s.Details = append(s.Details, "hooks: installed")
	} else {
		s.Details = append(s.Details, "hooks: partial/missing")
		appendCodexHookDetails(&s, hookState)
	}
	if configComplete {
		s.Details = append(s.Details, "config-patch: installed (openai_base_url + chatgpt_base_url)")
	} else {
		s.Details = append(s.Details, "config-patch: off/incomplete")
		s.Details = append(s.Details, "transparent proxy mode does not require Codex config-patch")
	}

	wired := 0
	if hookPresent {
		wired++
	}
	if configComplete {
		wired++
	}
	switch wired {
	case 0:
		s.State = ClientInstalled
	case 2:
		s.State = ClientFullyWired
	default:
		s.State = ClientPartiallyWired
	}
	return s
}

func appendCodexHookDetails(s *ClientStatus, state hooks.CodexHookState) {
	if !state.HooksJSONExists {
		s.Details = append(s.Details, "hooks.json: missing")
	}
	if !state.PreEntry {
		s.Details = append(s.Details, "PreToolUse Bash hook: missing")
	}
	if !state.PostEntry {
		s.Details = append(s.Details, "PostToolUse Bash hook: missing")
	}
	if !state.ReadEntry {
		s.Details = append(s.Details, "PreToolUse Read hook: missing")
	}
	if !state.PreScript || !state.PreExecutable {
		s.Details = append(s.Details, "codex-pre-tool.sh: missing/not executable")
	}
	if !state.PostScript || !state.PostExecutable {
		s.Details = append(s.Details, "codex-post-tool.sh: missing/not executable")
	}
	if !state.ReadScript || !state.ReadExecutable {
		s.Details = append(s.Details, "codex-read-tool.sh: missing/not executable")
	}
}

// DetectDaemon probes the proxy health endpoint and returns the running
// status. The caller provides the base URL so the same detector works in
// tests against an httptest server.
func DetectDaemon(proxyURL string) DaemonStatus {
	s := DaemonStatus{}
	if proxyURL == "" {
		proxyURL = ProxyURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, proxyURL+"/admin/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.Running = false
		s.Health = "unreachable"
		s.Details = append(s.Details, "no response: "+err.Error())
		return s
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK {
		s.Running = true
		s.Health = "ok"
		// Health endpoint may return JSON with PID.
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		if pid, ok := payload["pid"].(float64); ok {
			s.PID = int(pid)
		}
	} else {
		s.Running = false
		s.Health = "http " + strings.ToLower(http.StatusText(resp.StatusCode))
	}
	return s
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Status runs every detector and returns the full report.
func Status(opts Options) Report {
	home, err := opts.resolveHome()
	if err != nil {
		return Report{Errors: []string{err.Error()}}
	}
	shell := os.Getenv("SHELL")
	r := Report{
		Claude: DetectClaude(home, shell),
		Codex:  DetectCodex(home),
		Daemon: DetectDaemon(opts.resolveProxyURL()),
	}
	return r
}
