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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
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
// and mediates two narrow JSON-RPC seams. On stdin, the conversation
// `thread/start` request from Codex Desktop carries `modelProvider: null`, which
// resolves to the account default (chatgpt.com direct). The shim rewrites that
// single field to the scoped Slimference provider so the Desktop conversation
// rides the same no-CA WSS Phase-F path the CLI uses. On stdout, config-shaped
// responses are augmented so the Desktop start screen can render the same
// Slimference provider chip the routed thread will use even if Codex changes
// config/read request details. All unrelated frames are byte-identical, and any
// setup failure falls back to a plain exec passthrough (no rewrite) so the
// app-server never breaks.
func runCodexDesktopAppServerMediated(argv0 string, argv []string, env []string, stdinSrc io.Reader, p installPrinter) int {
	cmd := exec.Command(argv0, argv[1:]...)
	cmd.Env = env
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		if execErr := codexDesktopAppServerExecFn(argv0, argv, env); execErr != nil {
			fmt.Fprintf(p.Err, "slimference app-server shim: exec %s: %v\n", argv0, execErr)
			return 1
		}
		return 0
	}
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
	logger := newCodexDesktopShimFileLogger()
	provider := codexDesktopProviderConfigFromArgv(argv)
	logger.Log(codexDesktopShimLogRecord{
		Event:          "shim_start",
		Provider:       codexSlimferenceProviderID,
		BaseURLPresent: provider.baseURL != "",
	})
	mediator := newCodexDesktopAppServerMediatorWithLogger(provider, logger)
	stdoutDone := make(chan struct{})
	go func() {
		mediator.mediateStdout(stdout, os.Stdout)
		close(stdoutDone)
	}()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	go func() {
		for s := range sigs {
			_ = cmd.Process.Signal(s)
		}
	}()
	go func() {
		mediator.mediateStdin(stdinSrc, stdin)
		_ = stdin.Close()
	}()
	waitErr := cmd.Wait()
	<-stdoutDone
	signal.Stop(sigs)
	close(sigs)
	if waitErr == nil {
		logger.Log(codexDesktopShimLogRecord{
			Event:    "shim_exit",
			Provider: codexSlimferenceProviderID,
			ExitCode: 0,
		})
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			logger.Log(codexDesktopShimLogRecord{
				Event:    "shim_exit",
				Provider: codexSlimferenceProviderID,
				ExitCode: code,
			})
			return code
		}
	}
	logger.Log(codexDesktopShimLogRecord{
		Event:    "shim_exit",
		Provider: codexSlimferenceProviderID,
		ExitCode: 1,
	})
	fmt.Fprintf(p.Err, "slimference app-server shim: %v\n", waitErr)
	return 1
}

// mediateCodexDesktopAppServerStdin copies newline-delimited JSON-RPC from the
// client (Codex Desktop) to the real app-server, rewriting only `thread/start`
// requests that use the default provider. Codex Desktop frames stdio as
// newline-delimited JSON (one message per line), verified live against 0.133.0.
func mediateCodexDesktopAppServerStdin(in io.Reader, out io.Writer) {
	newCodexDesktopAppServerMediator(codexDesktopProviderConfig{}).mediateStdin(in, out)
}

func mediateCodexDesktopAppServerStdout(in io.Reader, out io.Writer) {
	newCodexDesktopAppServerMediator(codexDesktopProviderConfig{}).mediateStdout(in, out)
}

type codexDesktopProviderConfig struct {
	baseURL string
}

type codexDesktopAppServerMediator struct {
	provider       codexDesktopProviderConfig
	logger         codexDesktopShimLogger
	requestMethods map[string]string
	requestMu      sync.Mutex
}

func newCodexDesktopAppServerMediator(provider codexDesktopProviderConfig) *codexDesktopAppServerMediator {
	return newCodexDesktopAppServerMediatorWithLogger(provider, nil)
}

func newCodexDesktopAppServerMediatorWithLogger(provider codexDesktopProviderConfig, logger codexDesktopShimLogger) *codexDesktopAppServerMediator {
	return &codexDesktopAppServerMediator{
		provider:       provider,
		logger:         logger,
		requestMethods: make(map[string]string),
	}
}

func (m *codexDesktopAppServerMediator) mediateStdin(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			rewritten, changed := m.maybeRewriteStdinLine(line)
			if changed {
				m.log(codexDesktopShimLogRecord{
					Event:    "thread_start_rewrite",
					Provider: codexSlimferenceProviderID,
				})
			}
			if _, werr := out.Write(rewritten); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// mediateStdout copies newline-delimited JSON-RPC from the real app-server to
// Codex Desktop. It rewrites only app-server responses shaped as config/read or
// tracked model/list results so the blank Desktop start screen exposes the
// scoped Slimference route. All notifications, unrelated responses, non-JSON
// payloads, and malformed frames pass through byte-identically.
func (m *codexDesktopAppServerMediator) mediateStdout(in io.Reader, out io.Writer) {
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			rewritten, method, rewriteKind := m.maybeRewriteResponseLine(line)
			switch {
			case rewriteKind != "":
				m.log(codexDesktopShimLogRecord{
					Event:          rewriteKind,
					Method:         method,
					Provider:       codexSlimferenceProviderID,
					BaseURLPresent: m.provider.baseURL != "",
				})
			case method == "model/list":
				m.log(codexDesktopShimLogRecord{
					Event:    "model_list_seen",
					Method:   method,
					Provider: codexSlimferenceProviderID,
				})
			case method == "config/read":
				m.log(codexDesktopShimLogRecord{
					Event:    "config_read_seen_unmatched",
					Method:   method,
					Provider: codexSlimferenceProviderID,
				})
			}
			if _, werr := out.Write(rewritten); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (m *codexDesktopAppServerMediator) maybeRewriteStdinLine(line []byte) ([]byte, bool) {
	hasNL := len(line) > 0 && line[len(line)-1] == '\n'
	content := line
	if hasNL {
		content = line[:len(line)-1]
	}
	m.rememberRequestMethod(content)
	rewritten, changed := rewriteCodexDesktopThreadStart(content)
	if !changed {
		return line, false
	}
	if hasNL {
		return append(rewritten, '\n'), true
	}
	return rewritten, true
}

func (m *codexDesktopAppServerMediator) maybeRewriteResponseLine(line []byte) ([]byte, string, string) {
	hasNL := len(line) > 0 && line[len(line)-1] == '\n'
	content := line
	if hasNL {
		content = line[:len(line)-1]
	}
	method := m.takeResponseMethod(content)
	rewritten, kind := m.rewriteResponseContent(content, method)
	if kind == "" {
		return line, method, ""
	}
	if hasNL {
		return append(rewritten, '\n'), method, kind
	}
	return rewritten, method, kind
}

func (m *codexDesktopAppServerMediator) rewriteResponseContent(content []byte, method string) ([]byte, string) {
	rewritten, changed := rewriteCodexDesktopConfigReadResponse(content, m.provider)
	if changed {
		return rewritten, "config_read_rewrite"
	}
	if method != "model/list" {
		return content, ""
	}
	rewritten, changed = rewriteCodexDesktopModelListResponse(content)
	if !changed {
		return content, ""
	}
	return rewritten, "model_list_rewrite"
}

func (m *codexDesktopAppServerMediator) rememberRequestMethod(content []byte) {
	id, method := codexJSONRPCIDAndMethod(content)
	if id == "" || method == "" {
		return
	}
	m.requestMu.Lock()
	m.requestMethods[id] = method
	m.requestMu.Unlock()
}

func (m *codexDesktopAppServerMediator) takeResponseMethod(content []byte) string {
	id := codexJSONRPCID(content)
	if id == "" {
		return ""
	}
	m.requestMu.Lock()
	method := m.requestMethods[id]
	delete(m.requestMethods, id)
	m.requestMu.Unlock()
	return method
}

func (m *codexDesktopAppServerMediator) log(record codexDesktopShimLogRecord) {
	if m.logger == nil {
		return
	}
	m.logger.Log(record)
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

func rewriteCodexDesktopConfigReadResponse(content []byte, provider codexDesktopProviderConfig) ([]byte, bool) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return content, false
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(trimmed, &msg) != nil {
		return content, false
	}
	resultRaw, ok := msg["result"]
	if !ok {
		return content, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(resultRaw, &result) != nil {
		return content, false
	}
	configRaw, ok := result["config"]
	if !ok {
		return content, false
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(configRaw, &config) != nil {
		return content, false
	}
	config["model_provider"] = json.RawMessage(strconv.Quote(codexSlimferenceProviderID))
	config["modelProvider"] = json.RawMessage(strconv.Quote(codexSlimferenceProviderID))
	if provider.baseURL != "" {
		providers := codexDesktopModelProvidersConfig(provider.baseURL)
		config["model_providers"] = providers
		config["modelProviders"] = providers
	}
	newConfig, err := json.Marshal(config)
	if err != nil {
		return content, false
	}
	result["config"] = newConfig
	newResult, err := json.Marshal(result)
	if err != nil {
		return content, false
	}
	msg["result"] = newResult
	out, err := json.Marshal(msg)
	if err != nil {
		return content, false
	}
	return out, true
}

func rewriteCodexDesktopModelListResponse(content []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return content, false
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(trimmed, &msg) != nil {
		return content, false
	}
	resultRaw, ok := msg["result"]
	if !ok {
		return content, false
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(resultRaw, &result) != nil {
		return content, false
	}
	dataRaw, ok := result["data"]
	if !ok {
		return content, false
	}
	var models []map[string]json.RawMessage
	if json.Unmarshal(dataRaw, &models) != nil {
		return content, false
	}
	changed := false
	for i := range models {
		if codexDesktopMarkModelDisplayName(models[i]) {
			changed = true
		}
	}
	if !changed {
		return content, false
	}
	newData, err := json.Marshal(models)
	if err != nil {
		return content, false
	}
	result["data"] = newData
	newResult, err := json.Marshal(result)
	if err != nil {
		return content, false
	}
	msg["result"] = newResult
	out, err := json.Marshal(msg)
	if err != nil {
		return content, false
	}
	return out, true
}

func codexDesktopMarkModelDisplayName(model map[string]json.RawMessage) bool {
	rawModel, ok := model["model"]
	if !ok {
		return false
	}
	var modelID string
	if json.Unmarshal(rawModel, &modelID) != nil || strings.TrimSpace(modelID) == "" {
		return false
	}
	display := modelID
	if rawDisplay, ok := model["displayName"]; ok {
		var parsed string
		if json.Unmarshal(rawDisplay, &parsed) == nil && strings.TrimSpace(parsed) != "" {
			display = parsed
		}
	}
	if strings.Contains(strings.ToLower(display), "slimference") {
		return false
	}
	model["displayName"] = json.RawMessage(strconv.Quote("Slimference " + display))
	return true
}

func codexDesktopModelProvidersConfig(baseURL string) json.RawMessage {
	type providerEntry struct {
		ID                      string `json:"id"`
		Name                    string `json:"name"`
		DisplayName             string `json:"displayName"`
		Label                   string `json:"label"`
		BaseURL                 string `json:"base_url"`
		BaseURLCamel            string `json:"baseUrl"`
		BaseURLInitialism       string `json:"baseURL"`
		RequiresOpenAIAuth      bool   `json:"requires_openai_auth"`
		RequiresOpenAIAuthCamel bool   `json:"requiresOpenAIAuth"`
		RequiresOpenAIAuthAlt   bool   `json:"requiresOpenAiAuth"`
		SupportsWebSockets      bool   `json:"supports_websockets"`
		SupportsWebSocketsCamel bool   `json:"supportsWebSockets"`
		SupportsWebsocketsAlt   bool   `json:"supportsWebsockets"`
		WireAPI                 string `json:"wire_api"`
		WireAPICamel            string `json:"wireApi"`
		WireAPIInitialism       string `json:"wireAPI"`
	}
	body := map[string]providerEntry{
		codexSlimferenceProviderID: {
			ID:                      codexSlimferenceProviderID,
			Name:                    "Slimference",
			DisplayName:             "Slimference",
			Label:                   "Slimference",
			BaseURL:                 baseURL,
			BaseURLCamel:            baseURL,
			BaseURLInitialism:       baseURL,
			RequiresOpenAIAuth:      true,
			RequiresOpenAIAuthCamel: true,
			RequiresOpenAIAuthAlt:   true,
			SupportsWebSockets:      true,
			SupportsWebSocketsCamel: true,
			SupportsWebsocketsAlt:   true,
			WireAPI:                 "responses",
			WireAPICamel:            "responses",
			WireAPIInitialism:       "responses",
		},
	}
	out, err := json.Marshal(body)
	if err != nil {
		return nil
	}
	return out
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

func codexJSONRPCIDAndMethod(content []byte) (string, string) {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", ""
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(trimmed, &msg) != nil {
		return "", ""
	}
	id := codexJSONRPCIDFromMap(msg)
	if id == "" {
		return "", ""
	}
	return id, codexJSONRPCMethod(msg)
}

func codexJSONRPCID(content []byte) string {
	trimmed := bytes.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ""
	}
	var msg map[string]json.RawMessage
	if json.Unmarshal(trimmed, &msg) != nil {
		return ""
	}
	return codexJSONRPCIDFromMap(msg)
}

func codexJSONRPCIDFromMap(msg map[string]json.RawMessage) string {
	raw, ok := msg["id"]
	if !ok {
		return ""
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		return ""
	}
	return string(trimmed)
}

func codexJSONIsNull(raw json.RawMessage) bool {
	return string(bytes.TrimSpace(raw)) == "null"
}

type codexDesktopShimLogger interface {
	Log(codexDesktopShimLogRecord)
}

type codexDesktopShimLogRecord struct {
	Time           string `json:"time"`
	Event          string `json:"event"`
	Method         string `json:"method,omitempty"`
	Provider       string `json:"provider,omitempty"`
	BaseURLPresent bool   `json:"base_url_present,omitempty"`
	ExitCode       int    `json:"exit_code,omitempty"`
}

type codexDesktopShimFileLogger struct {
	path string
	mu   sync.Mutex
}

func newCodexDesktopShimFileLogger() *codexDesktopShimFileLogger {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return &codexDesktopShimFileLogger{}
	}
	return &codexDesktopShimFileLogger{
		path: filepath.Join(home, ".slimference", "logs", "desktop-shim.jsonl"),
	}
}

func (l *codexDesktopShimFileLogger) Log(record codexDesktopShimLogRecord) {
	if l == nil || l.path == "" || record.Event == "" {
		return
	}
	record.Time = time.Now().UTC().Format(time.RFC3339Nano)
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(record)
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

func codexDesktopProviderConfigFromArgv(argv []string) codexDesktopProviderConfig {
	const prefix = "model_providers." + codexSlimferenceProviderID + ".base_url="
	for _, arg := range argv {
		if !strings.HasPrefix(arg, prefix) {
			continue
		}
		raw := strings.TrimPrefix(arg, prefix)
		var baseURL string
		if json.Unmarshal([]byte(raw), &baseURL) == nil && baseURL != "" {
			return codexDesktopProviderConfig{baseURL: baseURL}
		}
		return codexDesktopProviderConfig{baseURL: strings.Trim(raw, `"`)}
	}
	return codexDesktopProviderConfig{}
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
