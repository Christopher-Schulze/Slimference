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
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/slimference/slimference/internal/config"
)

var backoffWaitFn = func(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// systemPrompt is the mandatory instruction set for MiniMax summarization.
// It enforces a strict, deterministic output format with zero creative freedom.
// Every rule exists because violations were observed in testing.
// Multi-stack few-shot examples (T87). The base prompt body is identical
// across stacks; only the trailing EXAMPLE INPUT / CORRECT OUTPUT block
// rotates so the model is primed with stack-appropriate idioms. Picked
// per request from buildSystemPrompt + pickExampleLang.
const exampleGo = "EXAMPLE INPUT:\n" +
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
	"- User requested auth addition to API, checked src/auth/handler.go [msg:0,1]\n" +
	"- src/auth/handler.go contains HandleLogin() - needs token validation [msg:2,3]\n" +
	"- edit_file applied to src/auth/handler.go (added token validation) [msg:5,6]\n" +
	"- Decision: add token validation to HandleLogin() -> approved and implemented [msg:3,4]\n" +
	"- go test ./src/auth/... passed (0.012s) [msg:7,8]\n\n"

const examplePython = "EXAMPLE INPUT:\n" +
	"[USER msg 0]\nNeed to add auth to the FastAPI service. Look at the current login handler.\n---\n" +
	"[ASSISTANT msg 1]\n<tool_use name=\"read_file\" input={\"path\":\"app/auth/handler.py\"}>\n---\n" +
	"[USER msg 2]\n<tool_result id=\"r1\">def handle_login(request):\n    # TODO: validate token\n    return Response(200)</tool_result>\n---\n" +
	"[ASSISTANT msg 3]\napp/auth/handler.py:handle_login() needs token validation.\n---\n" +
	"[USER msg 4]\nGo. Then run `pytest app/auth/`.\n---\n" +
	"[ASSISTANT msg 5]\n<tool_use name=\"edit_file\" input={\"path\":\"app/auth/handler.py\"}>\n---\n" +
	"[USER msg 6]\n<tool_result id=\"r2\">OK</tool_result>\n---\n" +
	"[ASSISTANT msg 7]\n<tool_use name=\"bash\" input={\"command\":\"pytest app/auth/\"}>\n---\n" +
	"[USER msg 8]\n<tool_result id=\"r3\">==== 12 passed in 0.34s ====</tool_result>\n---\n\n" +
	"CORRECT OUTPUT FOR ABOVE INPUT:\n" +
	"- User requested auth addition to FastAPI, checked app/auth/handler.py [msg:0,1]\n" +
	"- app/auth/handler.py contains handle_login() - needs token validation [msg:2,3]\n" +
	"- edit_file applied to app/auth/handler.py (added token validation) [msg:5,6]\n" +
	"- Decision: add token validation to handle_login() -> approved and implemented [msg:3,4]\n" +
	"- pytest app/auth/ passed (12 tests, 0.34s) [msg:7,8]\n\n"

const exampleTS = "EXAMPLE INPUT:\n" +
	"[USER msg 0]\nAdd auth to the Express API. Open the current login handler.\n---\n" +
	"[ASSISTANT msg 1]\n<tool_use name=\"read_file\" input={\"path\":\"src/auth/handler.ts\"}>\n---\n" +
	"[USER msg 2]\n<tool_result id=\"r1\">export function handleLogin(req: Request, res: Response) {\n  // TODO: validate token\n  return res.status(200).send();\n}</tool_result>\n---\n" +
	"[ASSISTANT msg 3]\nsrc/auth/handler.ts:handleLogin() needs token validation.\n---\n" +
	"[USER msg 4]\nGo. Then run `npm test --prefix src/auth`.\n---\n" +
	"[ASSISTANT msg 5]\n<tool_use name=\"edit_file\" input={\"path\":\"src/auth/handler.ts\"}>\n---\n" +
	"[USER msg 6]\n<tool_result id=\"r2\">OK</tool_result>\n---\n" +
	"[ASSISTANT msg 7]\n<tool_use name=\"bash\" input={\"command\":\"npm test --prefix src/auth\"}>\n---\n" +
	"[USER msg 8]\n<tool_result id=\"r3\">Tests:       8 passed, 8 total\nTime:        0.842 s</tool_result>\n---\n\n" +
	"CORRECT OUTPUT FOR ABOVE INPUT:\n" +
	"- User requested auth addition to Express API, checked src/auth/handler.ts [msg:0,1]\n" +
	"- src/auth/handler.ts contains handleLogin() - needs token validation [msg:2,3]\n" +
	"- edit_file applied to src/auth/handler.ts (added token validation) [msg:5,6]\n" +
	"- Decision: add token validation to handleLogin() -> approved and implemented [msg:3,4]\n" +
	"- npm test --prefix src/auth passed (8 tests, 0.842s) [msg:7,8]\n\n"

// systemPromptHeader is the stack-agnostic body of the prompt. T87 stack
// examples are appended at request time via buildSystemPrompt.
const systemPromptHeader = "You are a deterministic information extractor. You compress AI coding session transcripts into structured reference summaries. You never think, reason, or explain. You only extract and condense facts.\n\n" +
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
	"LINEAGE MARKERS (T92, mandatory):\n" +
	"- End every bullet with a marker [msg:N] or [msg:N,M,...] where N/M are\n" +
	"  the message indices (from the [USER msg N] / [ASSISTANT msg N]\n" +
	"  headers) the bullet was extracted from. The marker is REQUIRED so\n" +
	"  the proxy can reverse-trace facts back to original messages.\n\n"

// systemPromptFooter is the trailing instruction appended after the
// stack-specific example block. T87.
const systemPromptFooter = "START your output with \"- \" immediately. First character must be dash-space."

// systemPrompt is the legacy default-stack prompt kept as a compile-time
// fallback when buildSystemPrompt is not invoked (e.g. legacy tests). T87.
var systemPrompt = systemPromptHeader + exampleGo + systemPromptFooter

// promptOverrideBody, when non-empty, replaces systemPromptHeader at
// request time. T86 lets operators iterate on the prompt without
// recompiling: load a file once at startup, set this var, and every
// subsequent buildSystemPrompt call uses the file content.
var promptOverrideBody string

// promptOverrideVersion is the version tag parsed from the first
// `# version: ...` line of the prompt file. Recorded in RequestSummary
// so analytics can be sliced by prompt revision.
var promptOverrideVersion string

// SetPromptOverride sets a custom prompt body + version. Empty body
// reverts to the compile-time default. T86.
func SetPromptOverride(body, version string) {
	promptOverrideBody = body
	promptOverrideVersion = version
}

// PromptVersion returns the active prompt version tag, or "default"
// when no override is configured. T86.
func PromptVersion() string {
	if promptOverrideVersion == "" {
		return "default"
	}
	return promptOverrideVersion
}

// LoadPromptOverrideFromPath reads path and configures the active
// prompt. The file's first non-empty line may carry a
// `# version: <tag>` annotation; the rest is treated as the prompt
// body. Returns the parsed version tag (or "" when none was set) and
// any IO error. Caller can pass the returned version through to
// telemetry. T86.
func LoadPromptOverrideFromPath(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	body, version := parsePromptDocument(string(data))
	SetPromptOverride(body, version)
	return version, nil
}

func parsePromptDocument(s string) (body, version string) {
	lines := strings.Split(s, "\n")
	skip := 0
	for i, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			skip = i + 1
			continue
		}
		if strings.HasPrefix(trim, "# version:") {
			version = strings.TrimSpace(strings.TrimPrefix(trim, "# version:"))
			skip = i + 1
			continue
		}
		break
	}
	return strings.Join(lines[skip:], "\n"), version
}

// examplePromptCounters records which stack examples were chosen so the
// operator can monitor per-language distribution. T87.
var examplePromptCounters = map[string]int64{}

// ExamplePromptCount returns the cumulative pick count for a stack tag.
// Unknown tags return zero. T87.
func ExamplePromptCount(lang string) int64 { return examplePromptCounters[lang] }

// ExamplePromptCounts returns a copy of the per-stack pick counters.
func ExamplePromptCounts() map[string]int64 {
	out := make(map[string]int64, len(examplePromptCounters))
	for k, v := range examplePromptCounters {
		out[k] = v
	}
	return out
}

// ResetExamplePromptCounts clears the picker counters. Test helper.
func ResetExamplePromptCounts() {
	for k := range examplePromptCounters {
		delete(examplePromptCounters, k)
	}
}

// pickExampleLang scans the input transcript for cheap signals (file
// extensions, language-specific tokens) and returns one of "go",
// "python", or "ts". Defaults to "go" on tie or empty signals to
// preserve the previous prompt behaviour. T87.
func pickExampleLang(input string) string {
	scores := map[string]int{"go": 0, "python": 0, "ts": 0}
	low := strings.ToLower(input)

	// File-extension signals carry the most weight.
	for _, sig := range []struct {
		needle string
		lang   string
		weight int
	}{
		{".go", "go", 3},
		{".py", "python", 3},
		{".ts", "ts", 3},
		{".tsx", "ts", 3},
		{".jsx", "ts", 2},
		{".js", "ts", 2},
		{".rs", "go", 0}, // rust falls back to Go style for now
	} {
		scores[sig.lang] += strings.Count(low, sig.needle) * sig.weight
	}

	// Tool-name signals.
	for _, sig := range []struct {
		needle string
		lang   string
		weight int
	}{
		{"go test", "go", 2},
		{"go build", "go", 2},
		{"package main", "go", 2},
		{"pytest", "python", 2},
		{"def ", "python", 2},
		{"import os", "python", 1},
		{"npm test", "ts", 2},
		{"npm run", "ts", 1},
		{"yarn ", "ts", 1},
		{"pnpm ", "ts", 1},
		{"tsc ", "ts", 1},
		{"function ", "ts", 1},
	} {
		scores[sig.lang] += strings.Count(low, sig.needle) * sig.weight
	}

	best := "go"
	bestScore := scores["go"]
	for _, lang := range []string{"python", "ts"} {
		if scores[lang] > bestScore {
			best = lang
			bestScore = scores[lang]
		}
	}
	return best
}

// buildSystemPrompt returns the system prompt with the stack-appropriate
// few-shot example. The picker output is recorded for telemetry. T87.
// When a T86 prompt override is configured, the override body replaces
// the header so operators can iterate without recompiling; the example
// + footer stay so telemetry-gated counters still fire.
func buildSystemPrompt(input string) string {
	lang := pickExampleLang(input)
	examplePromptCounters[lang]++
	example := exampleGo
	switch lang {
	case "python":
		example = examplePython
	case "ts":
		example = exampleTS
	}
	header := systemPromptHeader
	if promptOverrideBody != "" {
		header = promptOverrideBody
	}
	return header + example + systemPromptFooter
}

// coTRegex matches chain-of-thought thinking blocks that some models emit.
// Kept for back-compat; the active stripper is StripCoTTags below which
// covers a wider set of reasoner tag families. T89.
var coTRegex = regexp.MustCompile(`(?s)<think[^>]*>.*?</think\s*>`)

// defaultCoTTags is the canonical list of reasoner-tag families that
// should never reach the validator. Order does not matter; the stripper
// loops to a fixed point so nested tags collapse cleanly. New families
// observed in the wild can be appended; previous entries stay so existing
// fixtures keep working.
var defaultCoTTags = []string{
	"think",
	"thinking",
	"reasoning",
	"reason",
	"analysis",
	"scratchpad",
	"reflection",
	"plan",
	"chain_of_thought",
	"chain-of-thought",
	"inner_thought",
	"inner_monologue",
}

// cotTagCounts records how often each tag family was stripped. Read via
// CoTTagCount for /admin/status.summarization.cot. Atomic-free because
// the summarizer call path is already serialized per request.
var cotTagCounts = map[string]int64{}

// CoTTagCount returns the cumulative strip count for a given tag family.
// Unknown families return zero. T89 telemetry surface.
func CoTTagCount(tag string) int64 { return cotTagCounts[tag] }

// CoTTagCounts returns a copy of the per-tag strip counts.
func CoTTagCounts() map[string]int64 {
	out := make(map[string]int64, len(cotTagCounts))
	for k, v := range cotTagCounts {
		out[k] = v
	}
	return out
}

// ResetCoTTagCounts clears the counters. Test helper.
func ResetCoTTagCounts() {
	for k := range cotTagCounts {
		delete(cotTagCounts, k)
	}
}

// lineageMarkerRegex matches [msg:N] or [msg:N,M,...] markers appended to
// a bullet line. T92.
var lineageMarkerRegex = regexp.MustCompile(`\s*\[msg:\d+(?:,\d+)*\]\s*$`)

// hasLineageMarker reports whether a bullet line carries a [msg:...]
// suffix marker. T92.
func hasLineageMarker(line string) bool {
	return lineageMarkerRegex.MatchString(line)
}

// StripLineageMarker returns the bullet text with any trailing [msg:...]
// marker removed. Used by human-facing display layers; the on-the-wire
// summary keeps the marker so the model + future T76 WP3 re-injection
// can use it. T92.
func StripLineageMarker(line string) string {
	return lineageMarkerRegex.ReplaceAllString(line, "")
}

// lineageBulletStats accumulates per-summary bullet-marker presence
// rates so the operator can monitor prompt compliance. T92.
type lineageBulletStats struct {
	totalBullets   int64
	markedBullets  int64
}

var lineageStats lineageBulletStats

// RecordLineageStats inspects a cleaned summary and updates the rolling
// marker-presence counters. Called from cleanSummaryOutput so every
// successful summary pass is measured.
func RecordLineageStats(summary string) {
	for _, line := range strings.Split(summary, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- ") {
			continue
		}
		lineageStats.totalBullets++
		if hasLineageMarker(trimmed) {
			lineageStats.markedBullets++
		}
	}
}

// LineageMarkerRate returns the cumulative ratio of bullets that carried
// a [msg:...] marker, or 0 when no bullets have been observed yet. T92.
func LineageMarkerRate() float64 {
	if lineageStats.totalBullets == 0 {
		return 0
	}
	return float64(lineageStats.markedBullets) / float64(lineageStats.totalBullets)
}

// LineageMarkerCounts returns the raw (marked, total) bullet counters.
func LineageMarkerCounts() (marked, total int64) {
	return lineageStats.markedBullets, lineageStats.totalBullets
}

// ResetLineageMarkerStats clears the counters. Test helper.
func ResetLineageMarkerStats() {
	lineageStats = lineageBulletStats{}
}

// StripCoTTags removes paired XML-style tag blocks for any of the
// declared reasoner-tag families. The stripper iterates until no more
// matches are found so nested tags collapse cleanly. Each strip
// increments the family counter for telemetry. T89.
func StripCoTTags(s string, tags []string) string {
	if len(tags) == 0 {
		return s
	}
	out := s
	for {
		before := out
		for _, tag := range tags {
			pattern := `(?s)<` + regexp.QuoteMeta(tag) + `\b[^>]*>.*?</` + regexp.QuoteMeta(tag) + `\s*>`
			re := regexp.MustCompile(pattern)
			matches := re.FindAllStringIndex(out, -1)
			if len(matches) == 0 {
				continue
			}
			cotTagCounts[tag] += int64(len(matches))
			out = re.ReplaceAllString(out, "")
		}
		if out == before {
			break
		}
	}
	return out
}

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
	// MinTokens caps the lower bound on completion length. Only sent
	// when the active provider's capability map advertises support
	// (T91 + T88) so non-supporting providers never see the field.
	MinTokens        int         `json:"min_tokens,omitempty"`
	// Seed pins the RNG for greedy reproducibility on providers that
	// support it (T88). Omitted when the capability flag is off.
	Seed             int         `json:"seed,omitempty"`
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

// computeStableSeed derives a deterministic seed from the request
// inputs so retries within the same call replay byte-for-byte. T88.
func computeStableSeed(model string, startMsg, endMsg int, input string) int {
	h := uint64(1469598103934665603) // FNV-64 offset
	mix := func(s string) {
		for i := 0; i < len(s); i++ {
			h ^= uint64(s[i])
			h *= 1099511628211
		}
	}
	mix(model)
	mix(fmt.Sprintf("%d-%d", startMsg, endMsg))
	mix(input)
	// Cap to int32 range so providers that store seed as int32 stay
	// happy.
	return int(h & 0x7fffffff)
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
	// caps gates optional request fields (seed, min_tokens, ...) per
	// provider so a non-supporting upstream never sees an unknown
	// parameter. T88 + T91. Defaults are conservative: zero-value
	// struct = no optional fields sent.
	caps capProvider
}

// capProvider is the narrow capability surface MiniMaxClient consumes.
// Decoupled from internal/types to avoid an import cycle and to let
// tests inject custom capability profiles.
type capProvider struct {
	SupportsSeed                bool
	SupportsMinCompletionTokens bool
}

// SetCapabilities overrides the capability profile for this client.
// Intended for test injection and for runtime upgrades when a provider
// rolls out a new field. T88 + T91.
func (c *MiniMaxClient) SetCapabilities(caps capProvider) { c.caps = caps }

// Capabilities returns the active capability profile.
func (c *MiniMaxClient) Capabilities() capProvider { return c.caps }

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
	// T87: system prompt is built per request with the stack-appropriate
	// few-shot example so non-Go sessions are not primed with Go idioms.
	payload := mmRequest{
		Model: c.model,
		Messages: []mmMessage{
			{Role: "system", Content: buildSystemPrompt(inputText)},
			{Role: "user", Content: userContent},
		},
		MaxTokens:        targetTokens,
		Temperature:      0,
		TopP:             1.0,
		FrequencyPenalty: 0,
		Stream:           false,
	}
	// T91: send min_tokens only when the active capability map says the
	// provider supports it. 70% of target floor avoids premature stops
	// without forcing the model to ramble.
	if c.caps.SupportsMinCompletionTokens {
		min := targetTokens * 7 / 10
		if min < 32 {
			min = 32
		}
		payload.MinTokens = min
	}
	// T88: seed-aware request building for greedy reproducibility on
	// providers that honour the field. Stable hash over the request
	// content so retries within the same call replay deterministically.
	if c.caps.SupportsSeed {
		payload.Seed = computeStableSeed(c.model, startMsg, endMsg, inputText)
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
		raw, err := c.doRequest(ctx, payload)
		if err == nil {
			return cleanSummaryOutput(raw), nil
		}

		lastErr = err
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		// Only retry on transient errors.
		if !isRetryable(err) {
			break
		}

		if attempt < maxAttempts-1 {
			if err := backoffWaitFn(ctx, backoff(attempt)); err != nil {
				return "", err
			}
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

	// 1. Strip chain-of-thought blocks for all known reasoner-tag families
	// (T89). Replaces the legacy single-family <think> regex with the
	// fixed-point multi-family stripper.
	s = StripCoTTags(s, defaultCoTTags)

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

	// T92 telemetry (RecordLineageStats) is called by Layer 2 only on
	// summaries that passed validation, so the per-bullet marker rate
	// reflects shipped output rather than every cleaned candidate.

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
func (c *MiniMaxClient) doRequest(ctx context.Context, payload mmRequest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", &retryableError{cause: err}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("read response body: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
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
