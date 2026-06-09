package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/proxy"
)

// bypassProxyURL is overridable in tests.
var bypassProxyURL = "http://127.0.0.1:8990"

// bypassHTTPClient is swappable in tests that do not want to hit the
// network.
var bypassHTTPClient = &http.Client{Timeout: 2 * time.Second}

// handleBypassCmd implements `slimference bypass <on|off|status>` plus
// the T81 scoped flags --duration / --next-request[=N]. Talks to the
// running daemon via the admin API.
func handleBypassCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference bypass <on|off|status> [--duration=Ns|--next-request[=N]]")
		exitFn(1)
		return
	}
	verb := args[0]
	durationSec, nextReqs, err := parseBypassFlags(args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "bypass: %v\n", err)
		exitFn(1)
		return
	}
	switch verb {
	case "on":
		ok := postBypass(AdminBypassPayload{Enabled: true, DurationSeconds: durationSec, NextRequests: nextReqs})
		if !ok {
			exitFn(1)
			return
		}
		switch {
		case nextReqs > 0:
			fmt.Printf("bypass: on - reverts after %d request(s)\n", nextReqs)
		case durationSec > 0:
			fmt.Printf("bypass: on - reverts after %ds\n", durationSec)
		default:
			fmt.Println("bypass: on - proxy forwards traffic unmodified")
		}
	case "off":
		ok := postBypass(AdminBypassPayload{Enabled: false})
		if !ok {
			exitFn(1)
			return
		}
		fmt.Println("bypass: off - compression layers active")
	case "status":
		enabled, ok := getBypass()
		if !ok {
			fmt.Fprintln(os.Stderr, "could not reach daemon (is it running?)")
			exitFn(1)
			return
		}
		if enabled {
			fmt.Println("bypass: on")
		} else {
			fmt.Println("bypass: off")
		}
	default:
		fmt.Fprintf(os.Stderr, "bypass: unknown verb %q\n", verb)
		exitFn(1)
	}
}

// AdminBypassPayload is the wire shape used by handleBypassCmd. Mirrors
// proxy.AdminBypassRequest but kept in cmd/slimference to avoid pulling
// the proxy package's full surface.
type AdminBypassPayload struct {
	Enabled         bool `json:"enabled"`
	DurationSeconds int  `json:"duration_seconds,omitempty"`
	NextRequests    int  `json:"next_requests,omitempty"`
}

// parseBypassFlags extracts --duration=Ns / --next-request[=N] from the
// trailing arg slice. Both flags are mutually exclusive.
func parseBypassFlags(args []string) (durationSec, nextReqs int, err error) {
	for _, a := range args {
		switch {
		case a == "":
		case len(a) > len("--duration=") && a[:len("--duration=")] == "--duration=":
			value := a[len("--duration="):]
			d, perr := parseDurationSeconds(value)
			if perr != nil {
				return 0, 0, perr
			}
			durationSec = d
		case a == "--next-request":
			nextReqs = 1
		case len(a) > len("--next-request=") && a[:len("--next-request=")] == "--next-request=":
			n, perr := parsePositiveIntFlag(a[len("--next-request="):])
			if perr != nil {
				return 0, 0, perr
			}
			nextReqs = n
		default:
			return 0, 0, fmt.Errorf("unknown flag: %s", a)
		}
	}
	if durationSec > 0 && nextReqs > 0 {
		return 0, 0, fmt.Errorf("--duration and --next-request are mutually exclusive")
	}
	return durationSec, nextReqs, nil
}

func parsePositiveIntFlag(s string) (int, error) {
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid integer: %q", s)
	}
	return n, nil
}

// parseDurationSeconds accepts plain integers (seconds) or `Ns`, `Nm`,
// `Nh` suffixes for terse CLI input. Negative or zero values error.
func parseDurationSeconds(s string) (int, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}
	last := s[len(s)-1]
	multiplier := 1
	tail := s
	switch last {
	case 's':
		tail = s[:len(s)-1]
	case 'm':
		multiplier = 60
		tail = s[:len(s)-1]
	case 'h':
		multiplier = 3600
		tail = s[:len(s)-1]
	}
	var n int
	if _, err := fmt.Sscanf(tail, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid duration: %q", s)
	}
	return n * multiplier, nil
}

func postBypass(payload AdminBypassPayload) bool {
	body, _ := json.Marshal(proxy.AdminBypassRequest{
		Enabled:         payload.Enabled,
		DurationSeconds: payload.DurationSeconds,
		NextRequests:    payload.NextRequests,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		bypassProxyURL+proxy.AdminBypassPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := bypassHTTPClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bypass: %v\n", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "bypass: daemon returned %d\n", resp.StatusCode)
		return false
	}
	var out proxy.AdminBypassResponse
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return true
}

func getBypass() (enabled, ok bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		bypassProxyURL+proxy.AdminBypassPath, nil)
	resp, err := bypassHTTPClient.Do(req)
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, false
	}
	var out proxy.AdminBypassResponse
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return out.Enabled, true
}
