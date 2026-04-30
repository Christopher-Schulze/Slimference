package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/slimference/slimference/internal/config"
)

// watchFlags captures `slimference watch` flag input. T79.
type watchFlags struct {
	intervalSeconds int
	once            bool
	endpoint        string
}

func parseWatchArgs(args []string) (watchFlags, error) {
	f := watchFlags{intervalSeconds: 2}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--once":
			f.once = true
		case "--interval":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("--interval requires a value in seconds")
			}
			n, err := parsePositiveSeconds(args[i])
			if err != nil {
				return f, err
			}
			f.intervalSeconds = n
		case "--endpoint":
			i++
			if i >= len(args) {
				return f, fmt.Errorf("--endpoint requires a URL")
			}
			f.endpoint = args[i]
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "--") {
				return f, fmt.Errorf("unknown flag: %s", a)
			}
			return f, fmt.Errorf("unexpected positional argument: %s", a)
		}
	}
	return f, nil
}

func parsePositiveSeconds(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid interval: %q", s)
	}
	return n, nil
}

// watchAdminEndpoint resolves the URL `slimference watch` should poll.
// Defaults to the configured listen port on 127.0.0.1.
func watchAdminEndpoint(cfg *config.Config, override string) string {
	if override != "" {
		return strings.TrimRight(override, "/") + "/_slimference/admin/status"
	}
	port := 8990
	if cfg != nil && cfg.Proxy.ListenPort > 0 {
		port = cfg.Proxy.ListenPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/_slimference/admin/status", port)
}

// fetchAdminStatus issues a GET against the admin URL and returns the
// raw JSON body. Failures (network, non-2xx, body-read) bubble up so
// the caller can decide whether to keep ticking.
func fetchAdminStatus(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("admin status: http %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// watchView is the subset of /admin/status the live watcher renders.
// Stays narrow on purpose so the JSON contract changes are unlikely to
// break the operator's display surface.
type watchView struct {
	Bypass            bool                       `json:"bypass"`
	AnyDegraded       bool                       `json:"any_provider_degraded"`
	Layers            map[string]bool            `json:"layers"`
	CacheEntries      int                        `json:"cache_entries"`
	AnalyticsQueue    map[string]any             `json:"analytics_queue"`
	Quality           map[string]any             `json:"quality"`
	ProviderHealth    map[string]map[string]any  `json:"provider_health"`
	RecentRequests    []map[string]any           `json:"recent_requests"`
	Pipeline          []map[string]any           `json:"pipeline"`
}

// renderWatchTick formats a single sample for the operator. Shows
// up/down state, layer toggles, queue depth, and the last few
// request latencies if present.
func renderWatchTick(now time.Time, body []byte) string {
	var view watchView
	if err := json.Unmarshal(body, &view); err != nil {
		return fmt.Sprintf("[%s] parse error: %v\n", now.Format("15:04:05"), err)
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%s] ", now.Format("15:04:05")))
	if view.Bypass {
		sb.WriteString("BYPASS ON | ")
	}
	if view.AnyDegraded {
		sb.WriteString("PROVIDER DEGRADED | ")
	}
	enabled := []string{}
	for _, layer := range []string{"1", "2", "3"} {
		if view.Layers[layer] {
			enabled = append(enabled, "L"+layer)
		}
	}
	sb.WriteString("layers=" + strings.Join(enabled, ","))
	sb.WriteString(fmt.Sprintf(" cache=%d", view.CacheEntries))
	if depth, ok := view.AnalyticsQueue["depth"]; ok {
		sb.WriteString(fmt.Sprintf(" queue=%v", depth))
	}
	sb.WriteString("\n")
	if len(view.RecentRequests) > 0 {
		last := view.RecentRequests[len(view.RecentRequests)-1]
		sb.WriteString(fmt.Sprintf("  last: provider=%v saved=%v ratio=%v\n",
			last["provider"], last["tokens_saved"], last["compression_ratio"]))
	}
	return sb.String()
}

// runWatchLoop is the testable inner loop for `slimference watch`. T79.
func runWatchLoop(ctx context.Context, client *http.Client, url string, intervalSeconds int, once bool, stdout, stderr io.Writer) {
	tick := func() {
		body, err := fetchAdminStatus(ctx, client, url)
		if err != nil {
			fmt.Fprintf(stderr, "[%s] fetch: %v\n", time.Now().Format("15:04:05"), err)
			return
		}
		fmt.Fprint(stdout, renderWatchTick(time.Now(), body))
	}
	tick()
	if once {
		return
	}
	t := time.NewTicker(time.Duration(intervalSeconds) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			tick()
		}
	}
}

// handleWatchCmd implements `slimference watch`. T79.
func handleWatchCmd(args []string) {
	flags, err := parseWatchArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
		return
	}
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	url := watchAdminEndpoint(cfg, flags.endpoint)
	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	runWatchLoop(ctx, client, url, flags.intervalSeconds, flags.once, os.Stdout, os.Stderr)
}
