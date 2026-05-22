package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
)

const (
	codexDesktopShimActiveEnv      = "SLIMFERENCE_CODEX_DESKTOP_ACTIVE"
	codexDesktopShimUpstreamBinEnv = "SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN"
	codexDesktopShimBaseURLEnv     = "SLIMFERENCE_CODEX_DESKTOP_BASE_URL"
	// codexSlimferenceProviderID is the model provider the shim defines and
	// the one it forces onto Codex Desktop's default (null) thread/start.
	codexSlimferenceProviderID = "slimference-codex"
)

var (
	codexDesktopAppServerExecFn = syscall.Exec
	codexDesktopAppServerStatFn = os.Stat
)

func handleCodexDesktopAppServerShim(args []string) {
	exitFn(runCodexDesktopAppServerShim(args, defaultInstallPrinter()))
}

func runCodexDesktopAppServerShim(args []string, p installPrinter) int {
	argv0, argv, env, err := buildCodexDesktopAppServerShimExec(args, os.Environ())
	if err != nil {
		fmt.Fprintf(p.Err, "slimference app-server shim: %v\n", err)
		return 1
	}
	return runCodexDesktopAppServerMediated(argv0, argv, env, os.Stdin, p)
}

// runCodexDesktopAppServerMediated launches the real Codex app-server as a child
// and mediates only its stdin: the conversation `thread/start` request from
// Codex Desktop carries `modelProvider: null`, which resolves to the account
// default (chatgpt.com direct). The shim rewrites that single field to the
// scoped Slimference provider so the Desktop conversation rides the same no-CA
// WSS Phase-F path the CLI uses. stdout/stderr are passed through untouched so
// streaming responses see no added latency. Any setup failure falls back to a
// plain exec passthrough (no rewrite) so the app-server never breaks.
func runCodexDesktopAppServerMediated(argv0 string, argv []string, env []string, stdinSrc io.Reader, p installPrinter) int {
	cmd := exec.Command(argv0, argv[1:]...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		if execErr := codexDesktopAppServerExecFn(argv0, argv, env); execErr != nil {
			fmt.Fprintf(p.Err, "slimference app-server shim: exec %s: %v\n", argv0, execErr)
			return 1
		}
		return 0
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(p.Err, "slimference app-server shim: start %s: %v\n", argv0, err)
		return 1
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()
	go func() {
		mediateCodexDesktopAppServerStdin(stdinSrc, stdin)
		_ = stdin.Close()
	}()
	waitErr := cmd.Wait()
	signal.Stop(sigs)
	close(sigs)
	if waitErr == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code
		}
	}
	fmt.Fprintf(p.Err, "slimference app-server shim: %v\n", waitErr)
	return 1
}

// mediateCodexDesktopAppServerStdin copies newline-delimited JSON-RPC from the
// client (Codex Desktop) to the real app-server, rewriting only `thread/start`
// requests that use the default provider. Codex Desktop frames stdio as
// newline-delimited JSON (one message per line), verified live against 0.133.0.
func mediateCodexDesktopAppServerStdin(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := out.Write(maybeRewriteCodexDesktopThreadStart(line)); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// maybeRewriteCodexDesktopThreadStart rewrites one newline-terminated line,
// preserving the trailing newline. Non-matching lines are returned byte-identical.
func maybeRewriteCodexDesktopThreadStart(line []byte) []byte {
	hasNL := len(line) > 0 && line[len(line)-1] == '\n'
	content := line
	if hasNL {
		content = line[:len(line)-1]
	}
	rewritten, changed := rewriteCodexDesktopThreadStart(content)
	if !changed {
		return line
	}
	if hasNL {
		return append(rewritten, '\n')
	}
	return rewritten
}

// rewriteCodexDesktopThreadStart forces modelProvider on a default-provider
// `thread/start` request. It fails open: any parse ambiguity, an explicit
// non-null provider, or a realtime/voice thread returns the original bytes
// unchanged so the shim never corrupts the stream or touches voice traffic.
func rewriteCodexDesktopThreadStart(content []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return content, false
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(trimmed, &msg) != nil {
		return content, false
	}
	if codexJSONRPCMethod(msg) != "thread/start" {
		return content, false
	}
	paramsRaw, ok := msg["params"]
	if !ok {
		return content, false
	}
	var params map[string]json.RawMessage
	if json.Unmarshal(paramsRaw, &params) != nil {
		return content, false
	}
	if mp, ok := params["modelProvider"]; ok && !codexJSONIsNull(mp) {
		return content, false
	}
	if codexThreadStartIsRealtime(params) {
		return content, false
	}
	params["modelProvider"] = json.RawMessage(strconv.Quote(codexSlimferenceProviderID))
	newParams, err := json.Marshal(params)
	if err != nil {
		return content, false
	}
	msg["params"] = newParams
	out, err := json.Marshal(msg)
	if err != nil {
		return content, false
	}
	return out, true
}

func codexJSONRPCMethod(msg map[string]json.RawMessage) string {
	raw, ok := msg["method"]
	if !ok {
		return ""
	}
	var m string
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	return m
}

func codexJSONIsNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

// codexThreadStartIsRealtime reports whether a thread/start opts into Codex's
// realtime (voice) conversation feature. Unparseable config is treated as
// realtime so the shim conservatively leaves such threads untouched.
func codexThreadStartIsRealtime(params map[string]json.RawMessage) bool {
	cfgRaw, ok := params["config"]
	if !ok || codexJSONIsNull(cfgRaw) {
		return false
	}
	var cfg map[string]json.RawMessage
	if json.Unmarshal(cfgRaw, &cfg) != nil {
		return true
	}
	raw, ok := cfg["features.realtime_conversation"]
	if !ok {
		return false
	}
	var enabled bool
	if json.Unmarshal(raw, &enabled) != nil {
		return true
	}
	return enabled
}

func buildCodexDesktopAppServerShimExec(args []string, env []string) (string, []string, []string, error) {
	if envValue(env, codexDesktopShimActiveEnv) != "1" {
		return "", nil, nil, fmt.Errorf("not a scoped Codex Desktop launch")
	}
	upstreamBin := strings.TrimSpace(envValue(env, codexDesktopShimUpstreamBinEnv))
	if upstreamBin == "" {
		return "", nil, nil, fmt.Errorf("%s missing", codexDesktopShimUpstreamBinEnv)
	}
	if err := validateCodexDesktopAppServerUpstream(upstreamBin); err != nil {
		return "", nil, nil, err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(envValue(env, codexDesktopShimBaseURLEnv)), "/")
	if baseURL == "" {
		return "", nil, nil, fmt.Errorf("%s missing", codexDesktopShimBaseURLEnv)
	}
	if err := validateCodexDesktopAppServerBaseURL(baseURL); err != nil {
		return "", nil, nil, err
	}
	argv := []string{
		upstreamBin,
		"app-server",
		"-c", "model_provider=" + strconv.Quote("slimference-codex"),
		"-c", "model_providers.slimference-codex.name=" + strconv.Quote("Slimference"),
		"-c", "model_providers.slimference-codex.base_url=" + strconv.Quote(baseURL),
		"-c", "model_providers.slimference-codex.requires_openai_auth=true",
		"-c", "model_providers.slimference-codex.supports_websockets=true",
		"-c", "model_providers.slimference-codex.wire_api=" + strconv.Quote("responses"),
	}
	argv = append(argv, args...)
	return upstreamBin, argv, sanitizeCodexDesktopAppServerShimEnv(env), nil
}

func validateCodexDesktopAppServerUpstream(path string) error {
	info, err := codexDesktopAppServerStatFn(path)
	if err != nil {
		return fmt.Errorf("upstream codex binary %q is not accessible: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("upstream codex binary %q is a directory", path)
	}
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("upstream codex binary %q is not executable", path)
	}
	return nil
}

func validateCodexDesktopAppServerBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", codexDesktopShimBaseURLEnv, err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("%s must use local http, got %q", codexDesktopShimBaseURLEnv, u.Scheme)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("%s must not include query or fragment", codexDesktopShimBaseURLEnv)
	}
	if strings.TrimRight(u.Path, "/") != "/backend-api/codex" {
		return fmt.Errorf("%s must point at /backend-api/codex, got %q", codexDesktopShimBaseURLEnv, u.Path)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s must point at loopback, got %q", codexDesktopShimBaseURLEnv, host)
	}
	return nil
}

func sanitizeCodexDesktopAppServerShimEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		key := kv[:eq]
		if key == "CODEX_CLI_PATH" || strings.HasPrefix(key, "SLIMFERENCE_CODEX_DESKTOP_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}
