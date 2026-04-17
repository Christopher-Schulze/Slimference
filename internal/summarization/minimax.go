package summarization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/slimference/slimference/internal/config"
)

var sleepFn = time.Sleep

// systemPrompt is the mandatory instruction set for MiniMax summarization.
// It enforces a strict, deterministic output format with zero creative freedom.
// Every rule exists because violations were observed in testing.
const systemPrompt = "You are a deterministic information extractor. You compress AI coding session transcripts into structured reference summaries. You never think, reason, or explain. You only extract and condense facts.\n\n" +
	"MANDATORY OUTPUT FORMAT:\n" +
	"- One fact per line, prefixed with a dash: \"- \"\n" +
	"- No other format is acceptable. No paragraphs. No prose. No sections.\n\n" +
	"CONTENT RULES (violating ANY rule = failed output):\n" +
	"1. Copy ALL file paths verbatim. Include extension. Example: src/auth/handler.go\n" +
	"2. Copy ALL function/method names verbatim. Example: handleLogin()\n" +
	"3. Copy ALL error messages verbatim, including stack traces.\n" +
	"4. Copy ALL decisions and their outcomes. Example: Decision: use SQLite -> approved\n" +
	"5. Include ALL tool names and their results: commands run, exit codes, test counts.\n" +
	"6. Preserve ALL variable names, type names, interface names from code.\n" +
	"7. Preserve ALL URLs, API endpoints, config keys, env var names.\n\n" +
	"OMIT ONLY:\n" +
	"- Greetings, acknowledgments, pleasantries, repeated context.\n" +
	"- Verbose build/test output -> keep only errors and summary counts.\n" +
	"- Full file contents -> keep only the fact that a file was read/edited.\n\n" +
	"STRICTLY FORBIDDEN (output is REJECTED if any are present):\n" +
	"- No thinking tags or chain-of-thought reasoning.\n" +
	"- No preamble: do not start with \"Here is\" or \"Summary:\" or \"The conversation\".\n" +
	"- No markdown headers.\n" +
	"- No code fences around the summary itself.\n" +
	"- No explanations of what you did or why.\n" +
	"- No meta-commentary about the compression process.\n" +
	"- No \"...\" ellipsis to skip content - extract the actual facts.\n" +
	"- No redundant rephrasing - use original terms exactly.\n\n" +
	"EXAMPLE INPUT:\n" +
	"[USER msg 0]\nI need to add auth to the API. Let me check the current handler.\n---\n" +
	"[ASSISTANT msg 1]\n<tool_use name=\"read_file\" input={\"path\":\"src/auth/handler.go\"}>\n---\n" +
	"[USER msg 2]\n<tool_result id=\"r1\">package auth\n\nfunc HandleLogin(w http.ResponseWriter, r *http.Request) {\n    // TODO: validate token\n    w.WriteHeader(200)\n}</tool_result>\n---\n" +
	"[ASSISTANT msg 3]\nThe HandleLogin function in src/auth/handler.go needs token validation added.\n---\n" +
	"[USER msg 4]\nGo ahead. Also run `go test ./src/auth/...` after.\n---\n" +
	"[ASSISTANT msg 5]\n<tool_use name=\"edit_file\" input={\"path\":\"src/auth/handler.go\"}>\n---\n" +
	"[USER msg 6]\n<tool_result id=\"r2\">OK</tool_result>\n---\n" +
	"[ASSISTANT msg 7]\n<tool_use name=\"bash\" input={\"command\":\"go test ./src/auth/...\"}>\n---\n" +
	"[USER msg 8]\n<tool_result id=\"r3\">ok  github.com/app/auth   0.012s\nPASS</tool_result>\n---\n\n" +
	"CORRECT OUTPUT FOR ABOVE INPUT:\n" +
	"- User requested auth addition to API, checked src/auth/handler.go\n" +
	"- src/auth/handler.go contains HandleLogin() - needs token validation\n" +
	"- edit_file applied to src/auth/handler.go (added token validation)\n" +
	"- Decision: add token validation to HandleLogin() -> approved and implemented\n" +
	"- go test ./src/auth/... passed (0.012s)\n\n" +
	"START your output with \"- \" immediately. First character must be dash-space."

// coTRegex matches chain-of-thought thinking blocks that some models emit.
var coTRegex = regexp.MustCompile(`(?s)<think[^>]*>.*?</think\s*>`)

// preambleRegex matches common preamble patterns that violate the format requirement.
var preambleRegex = regexp.MustCompile(`(?m)^(?:Here is|Summary:|The conversation|Below is|I have|This is|Compressed|Result:|Output:)[^\n]*\n?`)

// fenceRegex matches markdown code fences wrapping the entire output.
var fenceRegex = regexp.MustCompile("(?m)^```[a-zA-Z]*\n|\n```$")

// mdHeaderRegex matches markdown headers.
var mdHeaderRegex = regexp.MustCompile(`(?m)^#{1,6}\s+[^\n]+$`)

// multiBlankLineRegex collapses multiple blank lines to one.
var multiBlankLineRegex = regexp.MustCompile(`\n{3,}`)

// mmRequest is the OpenAI-compatible chat completion request body.
type mmRequest struct {
	Model            string      `json:"model"`
	Messages         []mmMessage `json:"messages"`
	MaxTokens        int         `json:"max_tokens"`
	Temperature      float64     `json:"temperature"`
	TopP             float64     `json:"top_p"`
	FrequencyPenalty float64     `json:"frequency_penalty"`
	Stream           bool        `json:"stream"`
}

// mmMessage is a single role/content pair used in the chat completion API.
type mmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// mmResponse is the OpenAI-compatible chat completion response envelope.
type mmResponse struct {
	Choices []mmChoice `json:"choices"`
}

// mmChoice wraps a single completion candidate.
type mmChoice struct {
	Message mmMessage `json:"message"`
}

// MiniMaxClient calls the MiniMax M2.7 API for abstractive summarization.
type MiniMaxClient struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxRetries  int
	httpClient  *http.Client
	limiter     *rate.Limiter
}

// NewMiniMaxClient builds a MiniMaxClient from the supplied configuration.
func NewMiniMaxClient(cfg config.MiniMaxConfig) *MiniMaxClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: cfg.ConnectTimeout(),
		}).DialContext,
		ResponseHeaderTimeout: cfg.ResponseTimeout(),
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   cfg.ConnectTimeout() + cfg.ResponseTimeout(),
	}

	rpm := cfg.RateLimitRPM
	if rpm <= 0 {
		rpm = 10
	}
	// Convert RPM to per-second rate with a burst of 1.
	rps := rate.Limit(float64(rpm) / 60.0)
	limiter := rate.NewLimiter(rps, 1)

	return &MiniMaxClient{
		baseURL:     cfg.BaseURL,
		apiKey:      cfg.APIKey(),
		model:       cfg.Model,
		temperature: cfg.Temperature,
		maxRetries:  cfg.MaxRetries,
		httpClient:  httpClient,
		limiter:     limiter,
	}
}

// Name returns the provider identifier for the FallbackChain.
func (c *MiniMaxClient) Name() string {
	return "minimax-" + c.model
}

// IsConfigured reports whether the client has an API key configured.
func (c *MiniMaxClient) IsConfigured() bool {
	return c.apiKey != ""
}

// Summarize sends the inputText to MiniMax and returns the compressed summary.
// It retries up to MaxRetries times on 429 or 5xx responses with exponential backoff.
// Post-processing strips CoT artifacts, preambles, and format violations.
func (c *MiniMaxClient) Summarize(ctx context.Context, inputText string, startMsg, endMsg, targetTokens int) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter cancelled: %w", err)
	}

	userContent := fmt.Sprintf(
		"Compress messages %d-%d into bullet points (max ~%d tokens).\n"+
			"Rules: verbatim file paths, function names, errors, decisions. No prose. No preamble.\n"+
			"Start with \"- \" immediately.\n\n%s",
		startMsg, endMsg, targetTokens, inputText,
	)

	// Force temperature to 0 regardless of config for deterministic output.
	payload := mmRequest{
		Model: c.model,
		Messages: []mmMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userContent},
		},
		MaxTokens:        targetTokens,
		Temperature:      0,
		TopP:             1.0,
		FrequencyPenalty: 0,
		Stream:           false,
	}

	backoff := func(attempt int) time.Duration {
		base := 500 * time.Millisecond * time.Duration(1<<uint(attempt))
		if base > 10*time.Second {
			base = 10 * time.Second
		}
		jitter := time.Duration(rand.Int63n(int64(base) / 2))
		return base + jitter
	}
	maxAttempts := c.maxRetries + 1

	var lastErr error
	for attempt := range maxAttempts {
		raw, err := c.doRequest(payload)
		if err == nil {
			return cleanSummaryOutput(raw), nil
		}

		lastErr = err

		// Only retry on transient errors.
		if !isRetryable(err) {
			break
		}

		if attempt < maxAttempts-1 {
			sleepFn(backoff(attempt))
		}
	}

	return "", fmt.Errorf("minimax summarize failed after %d attempts: %w", maxAttempts, lastErr)
}

// cleanSummaryOutput post-processes the raw MiniMax response to strip
// chain-of-thought artifacts, preambles, and format violations.
// This is the enforcement layer: even if the model disobeys the system prompt,
// the output is cleaned before it reaches the validator.
func cleanSummaryOutput(raw string) string {
	s := raw

	// 1. Strip chain-of-thought <think...</think > blocks (M2.7 CoT behavior).
	s = coTRegex.ReplaceAllString(s, "")

	// 2. Strip common preambles ("Here is the summary:", "Compressed output:", etc.).
	s = preambleRegex.ReplaceAllString(s, "")

	// 3. Strip markdown code fences wrapping the entire output.
	s = fenceRegex.ReplaceAllString(s, "")

	// 4. Strip markdown headers.
	s = mdHeaderRegex.ReplaceAllString(s, "")

	// 5. Collapse multiple blank lines.
	s = multiBlankLineRegex.ReplaceAllString(s, "\n\n")

	s = strings.TrimSpace(s)

	// 6. If output doesn't start with "- ", try to fix: find the first "- " line.
	if !strings.HasPrefix(s, "- ") {
		lines := strings.Split(s, "\n")
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "- ") {
				idx := strings.Index(s, trimmed)
				s = s[idx:]
				break
			}
		}
	}

	// 7. Deduplicate near-identical bullet points.
	s = deduplicateBullets(s)

	return s
}

// deduplicateBullets removes bullet lines that are substrings of other bullets
// or are exact duplicates. Keeps the longer/more specific version.
func deduplicateBullets(s string) string {
	lines := strings.Split(s, "\n")

	type bulletEntry struct {
		text    string
		content string
	}
	var allBullets []bulletEntry
	var isBullet []bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			content := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			allBullets = append(allBullets, bulletEntry{text: trimmed, content: content})
			isBullet = append(isBullet, true)
		} else {
			isBullet = append(isBullet, false)
		}
	}

	remove := make(map[int]bool, len(allBullets))
	for i := range allBullets {
		if remove[i] {
			continue
		}
		for j := range allBullets {
			if i == j || remove[j] {
				continue
			}
			a, b := allBullets[i].content, allBullets[j].content
			if a == b {
				remove[j] = true
				continue
			}
			if len(a) > len(b) && strings.Contains(a, b) {
				remove[j] = true
				continue
			}
			if similarEnough(a, b, 0.70) {
				if len(a) >= len(b) {
					remove[j] = true
				} else {
					remove[i] = true
				}
			}
		}
	}

	var result []string
	bi := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- ") {
			if bi < len(allBullets) && !remove[bi] {
				result = append(result, allBullets[bi].text)
			}
			bi++
		} else {
			result = append(result, line)
		}
	}
	return strings.Join(result, "\n")
}

// similarEnough reports whether two strings have a Jaccard word-level similarity
// above the given threshold (0.0-1.0). Used for fuzzy dedup of near-identical bullets.
func similarEnough(a, b string, threshold float64) bool {
	wordsA := toWordSet(a)
	wordsB := toWordSet(b)
	if len(wordsA) == 0 || len(wordsB) == 0 {
		return false
	}
	intersection := 0
	for w := range wordsA {
		if wordsB[w] {
			intersection++
		}
	}
	union := len(wordsA) + len(wordsB) - intersection
	return float64(intersection)/float64(union) >= threshold
}

// toWordSet splits a string into a set of lowercase words.
func toWordSet(s string) map[string]bool {
	words := strings.Fields(s)
	set := make(map[string]bool, len(words))
	for _, w := range words {
		if len(w) > 4 {
			set[strings.ToLower(w)] = true
		}
	}
	return set
}

// doRequest executes a single HTTP call and returns the summary text or a typed error.
func (c *MiniMaxClient) doRequest(payload mmRequest) (string, error) {
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", &retryableError{cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", &retryableError{
			cause: fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200)),
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var parsed mmResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if len(parsed.Choices) == 0 || parsed.Choices[0].Message.Content == "" {
		return "", fmt.Errorf("empty response from minimax")
	}

	return parsed.Choices[0].Message.Content, nil
}

// retryableError marks an error that should trigger a retry.
type retryableError struct {
	cause error
}

func (e *retryableError) Error() string { return e.cause.Error() }
func (e *retryableError) Unwrap() error { return e.cause }

// isRetryable returns true if the error is a *retryableError.
func isRetryable(err error) bool {
	_, ok := err.(*retryableError)
	return ok
}

// truncate clips a string to at most n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
