package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
)

const (
	codexDesktopShimActiveEnv      = "SLIMFERENCE_CODEX_DESKTOP_ACTIVE"
	codexDesktopShimUpstreamBinEnv = "SLIMFERENCE_CODEX_DESKTOP_UPSTREAM_BIN"
	codexDesktopShimBaseURLEnv     = "SLIMFERENCE_CODEX_DESKTOP_BASE_URL"
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
	if err := codexDesktopAppServerExecFn(argv0, argv, env); err != nil {
		fmt.Fprintf(p.Err, "slimference app-server shim: exec %s: %v\n", argv0, err)
		return 1
	}
	return 0
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
