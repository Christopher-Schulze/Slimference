package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/config"
)

// qualityFlags captures the CLI flags for `slimference quality`. T77.
type qualityFlags struct {
	json    bool
	url     string
	timeout time.Duration
}

func parseQualityArgs(args []string) (qualityFlags, error) {
	f := qualityFlags{timeout: 3 * time.Second}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--json", "-json":
			f.json = true
		case "--url":
			i++
			if i >= len(args) || args[i] == "" {
				return f, fmt.Errorf("--url requires a value")
			}
			f.url = args[i]
		default:
			if strings.HasPrefix(a, "-") {
				return f, fmt.Errorf("unknown flag: %s", a)
			}
			return f, fmt.Errorf("unexpected argument: %s", a)
		}
	}
	return f, nil
}

// qualityView is the subset of /admin/status.quality that
// `slimference quality` renders. T77.
type qualityView struct {
	Quality map[string]any `json:"quality"`
}

// fetchQualityBlock issues a GET against /admin/status and returns the
// quality sub-block as a generic map. Failures bubble up so the caller
// can produce a useful error.
func fetchQualityBlock(ctx context.Context, client *http.Client, url string) (map[string]any, error) {
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var view qualityView
	if err := json.Unmarshal(body, &view); err != nil {
		return nil, fmt.Errorf("parse admin status: %w", err)
	}
	return view.Quality, nil
}

// renderQualityText formats the quality block for human reading. The
// shape mirrors `quality.QualitySnapshot` (reread / cache_miss_spike /
// net_savings) but stays tolerant of missing keys so a future schema
// extension does not break the CLI.
func renderQualityText(q map[string]any) string {
	var sb strings.Builder
	sb.WriteString("Slimference quality signals\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	if q == nil {
		sb.WriteString("(no quality data; daemon may not be running)\n")
		return sb.String()
	}
	if rr, ok := q["reread"].(map[string]any); ok {
		sb.WriteString(fmt.Sprintf("Re-read sessions:           %v\n", rr["sessions"]))
		sb.WriteString(fmt.Sprintf("Re-read events:             %v\n", rr["reread_count"]))
		sb.WriteString(fmt.Sprintf("Re-read rate:               %s\n", formatFloat(rr["reread_rate"])))
	}
	if cm, ok := q["cache_miss_spike"].(map[string]any); ok {
		sb.WriteString(fmt.Sprintf("Cache hit rate (window):    %s\n", formatFloat(cm["window_hit_rate"])))
		sb.WriteString(fmt.Sprintf("Cache miss spike active:    %v\n", cm["spike_active"]))
		sb.WriteString(fmt.Sprintf("Cache spike last triggered: %v\n", cm["last_spike_unix"]))
	}
	if ns, ok := q["net_savings"].(map[string]any); ok {
		sb.WriteString(fmt.Sprintf("Net savings (tokens):       %v\n", ns["net_saved_tokens"]))
		sb.WriteString(fmt.Sprintf("Net savings ratio:          %s\n", formatFloat(ns["net_savings_ratio"])))
	}
	return sb.String()
}

func formatFloat(v any) string {
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.4f", f)
	}
	return fmt.Sprintf("%v", v)
}

// resolveQualityURL builds the admin URL the CLI should poll. Mirrors
// watch_cmd.go::watchAdminEndpoint so the two commands share the same
// resolution rules.
func resolveQualityURL(cfg *config.Config, override string) string {
	if override != "" {
		return strings.TrimRight(override, "/") + "/_slimference/admin/status"
	}
	port := 8990
	if cfg != nil && cfg.Proxy.ListenPort > 0 {
		port = cfg.Proxy.ListenPort
	}
	return fmt.Sprintf("http://127.0.0.1:%d/_slimference/admin/status", port)
}

// handleQualityCmd implements `slimference quality [--json] [--url
// <base>]`. T77 surface for the three quality signals exposed via
// /admin/status.quality.
func handleQualityCmd(args []string) {
	flags, err := parseQualityArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		fmt.Fprintln(os.Stderr, "usage: slimference quality [--json] [--url <base>]")
		exitFn(1)
		return
	}
	cfg, _ := configLoadFn()
	url := resolveQualityURL(cfg, flags.url)
	ctx, cancel := context.WithTimeout(context.Background(), flags.timeout)
	defer cancel()
	client := &http.Client{Timeout: flags.timeout}
	q, err := fetchQualityBlock(ctx, client, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch quality: %v\n", err)
		exitFn(1)
		return
	}
	if flags.json {
		out, _ := json.MarshalIndent(q, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(renderQualityText(q))
}
