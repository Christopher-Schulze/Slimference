// Command verify runs prepared live-verification flows for the
// Slimference tasks that need real provider traffic to be closed:
//
//   - prompt-cache: T52 - send N identical Anthropic requests through a
//     running Slimference proxy and measure the prompt-cache hit rate
//     reported by the upstream.
//   - codex-smoke: T71 / T75 - send a single Codex-shaped request
//     through a running Slimference proxy with the operator's already-
//     installed Codex login (no CLI modification required).
//
// All flows are READ-ONLY against the operator's secrets: the tool
// never reads ANTHROPIC_API_KEY or chatgpt cookies itself; it only
// requires a running Slimference proxy and the standard env / login
// the CLIs already use.
//
// Usage:
//
//	go run ./scripts/verify -mode prompt-cache -url http://127.0.0.1:8990 -count 10
//	go run ./scripts/verify -mode codex-smoke -url http://127.0.0.1:8990
//
// Exit code 0 = verdict PASS; 1 = verdict FAIL; 2 = invocation error.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", "prompt-cache", "verification flow: prompt-cache | codex-smoke")
	url := flag.String("url", "http://127.0.0.1:8990", "Slimference proxy base URL")
	count := flag.Int("count", 10, "number of identical requests to send (prompt-cache mode)")
	model := flag.String("model", "claude-3-5-sonnet-20241022", "model id for prompt-cache mode")
	body := flag.String("body", "", "raw JSON body file (codex-smoke mode); reads stdin when empty")
	flag.Parse()

	switch *mode {
	case "prompt-cache":
		os.Exit(runPromptCache(*url, *model, *count))
	case "codex-smoke":
		os.Exit(runCodexSmoke(*url, *body))
	default:
		fmt.Fprintf(os.Stderr, "unknown -mode %q\n", *mode)
		os.Exit(2)
	}
}

// runPromptCache sends `count` identical Anthropic requests through
// the proxy and measures how many of them the upstream served from
// prompt cache. Verdict PASS when at least 80% of follow-up requests
// hit the cache.
func runPromptCache(base, model string, count int) int {
	if count < 2 {
		fmt.Fprintln(os.Stderr, "prompt-cache mode needs -count >= 2")
		return 2
	}
	body := buildPromptCacheBody(model)
	hits := 0
	misses := 0
	creates := 0

	for i := 0; i < count; i++ {
		usage, err := postAnthropic(base, body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "request %d failed: %v\n", i+1, err)
			return 2
		}
		if usage.CacheReadInputTokens > 0 {
			hits++
		}
		if usage.CacheCreationInputTokens > 0 {
			creates++
		}
		if usage.CacheReadInputTokens == 0 && usage.CacheCreationInputTokens == 0 {
			misses++
		}
		// Tiny pause so the upstream cache-write commits before the
		// next read.
		time.Sleep(150 * time.Millisecond)
	}

	hitRate := float64(hits) / float64(count-creates)
	if count-creates <= 0 {
		hitRate = 0
	}
	fmt.Printf("Slimference T52 prompt-cache verification\n")
	fmt.Printf("Requests:      %d\n", count)
	fmt.Printf("Cache hits:    %d\n", hits)
	fmt.Printf("Cache creates: %d\n", creates)
	fmt.Printf("Cache misses:  %d\n", misses)
	fmt.Printf("Hit rate:      %.1f %%\n", hitRate*100)
	if hitRate >= 0.8 {
		fmt.Println("Verdict: PASS (T52)")
		return 0
	}
	fmt.Println("Verdict: FAIL - prompt-cache hit rate below 80%")
	return 1
}

func buildPromptCacheBody(model string) []byte {
	prefix := strings.Repeat(
		"You are a precise assistant. Answer in one sentence. ", 200,
	)
	payload := map[string]any{
		"model":      model,
		"max_tokens": 64,
		"messages": []map[string]any{
			{"role": "user", "content": prefix + "What is 2+2?"},
		},
	}
	out, _ := json.Marshal(payload)
	return out
}

// upstreamUsage captures the cache fields Anthropic returns. Only the
// two cache_*_input_tokens fields are read here.
type upstreamUsage struct {
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// postAnthropic forwards `body` to the proxy's /v1/messages endpoint
// and returns the upstream usage block.
func postAnthropic(base string, body []byte) (upstreamUsage, error) {
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(base, "/")+"/v1/messages",
		bytes.NewReader(body))
	if err != nil {
		return upstreamUsage{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return upstreamUsage{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return upstreamUsage{}, err
	}
	if resp.StatusCode/100 != 2 {
		return upstreamUsage{}, fmt.Errorf("upstream %d: %s",
			resp.StatusCode, string(respBody))
	}
	var parsed struct {
		Usage upstreamUsage `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return upstreamUsage{}, fmt.Errorf("parse: %w", err)
	}
	return parsed.Usage, nil
}

// runCodexSmoke sends one Codex-shaped request body (read from -body
// or stdin) through the proxy at /backend-api/codex/ and returns
// PASS when the upstream returns 2xx with a non-empty body. Cookies
// / auth headers are expected to be present in the request body's
// `headers` field; the operator typically captures them from a real
// Codex CLI session.
func runCodexSmoke(base, bodyPath string) int {
	bodyBytes, err := readBodyOrStdin(bodyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read body: %v\n", err)
		return 2
	}
	if len(bodyBytes) == 0 {
		fmt.Fprintln(os.Stderr, "empty body; supply -body <path> or pipe via stdin")
		return 2
	}
	req, err := http.NewRequest(http.MethodPost,
		strings.TrimRight(base, "/")+"/backend-api/codex/conversation",
		bytes.NewReader(bodyBytes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "build: %v\n", err)
		return 2
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "do: %v\n", err)
		return 2
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read resp: %v\n", err)
		return 2
	}
	fmt.Printf("Slimference T71/T75 codex-smoke verification\n")
	fmt.Printf("Status:        %d\n", resp.StatusCode)
	fmt.Printf("Body bytes:    %d\n", len(respBody))
	if resp.StatusCode/100 == 2 && len(respBody) > 0 {
		fmt.Println("Verdict: PASS (T71/T75 smoke)")
		return 0
	}
	fmt.Println("Verdict: FAIL")
	return 1
}

func readBodyOrStdin(path string) ([]byte, error) {
	if path == "" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}
