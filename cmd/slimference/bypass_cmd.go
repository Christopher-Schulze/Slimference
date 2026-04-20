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

	"github.com/slimference/slimference/internal/proxy"
)

// bypassProxyURL is overridable in tests.
var bypassProxyURL = "http://127.0.0.1:8990"

// bypassHTTPClient is swappable in tests that do not want to hit the
// network.
var bypassHTTPClient = &http.Client{Timeout: 2 * time.Second}

// handleBypassCmd implements `slimference bypass <on|off|status>` - the CLI
// mirror of the TUI bypass hotkey (T67). Talks to the running daemon via
// the admin API so a single invocation works from any shell.
func handleBypassCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference bypass <on|off|status>")
		exitFn(1)
		return
	}
	switch args[0] {
	case "on":
		ok := postBypass(true)
		if !ok {
			exitFn(1)
			return
		}
		fmt.Println("bypass: on - proxy forwards traffic unmodified")
	case "off":
		ok := postBypass(false)
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
		fmt.Fprintf(os.Stderr, "bypass: unknown verb %q\n", args[0])
		exitFn(1)
	}
}

func postBypass(enabled bool) bool {
	body, _ := json.Marshal(proxy.AdminBypassRequest{Enabled: enabled})
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
