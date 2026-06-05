// Command verify runs prepared live-verification flows for the
// Slimference tasks that need real provider traffic to be closed:
//
//   - prompt-cache: T52 - send N identical Anthropic requests through a
//     running Slimference proxy and measure the prompt-cache hit rate
//     reported by the upstream.
//   - codex-smoke: T71 / T75 - send a single Codex-shaped request
//     through a running Slimference proxy with the operator's already-
//     installed Codex login (no CLI modification required).
//   - live-corpus-plan: T146 - print the exact local capture, export,
//     metadata, benchmark, and policy-review steps for one corpus category.
//   - release-proof-plan: T271 - print the complete operator ceremony for
//     default-on promotion evidence across CLI, Desktop, workday, WSS proof,
//     and the live-corpus promotion gate.
//   - host-resource-plan: T272 - print the live RSS/CPU/disk/state/profile
//     evidence ceremony for one CLI or Desktop product workload.
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
//	go run ./scripts/verify -mode live-corpus-plan -category codex_cli_tool_heavy
//	go run ./scripts/verify -mode release-proof-plan
//	go run ./scripts/verify -mode host-resource-plan -client codex_desktop
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
	"path/filepath"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", "prompt-cache", "verification flow: prompt-cache | codex-smoke | live-corpus-plan | release-proof-plan | host-resource-plan")
	url := flag.String("url", "http://127.0.0.1:8990", "Slimference proxy base URL")
	count := flag.Int("count", 10, "number of identical requests to send (prompt-cache mode)")
	model := flag.String("model", "claude-3-5-sonnet-20241022", "model id for prompt-cache mode")
	body := flag.String("body", "", "raw JSON body file (codex-smoke mode); reads stdin when empty")
	category := flag.String("category", "codex_cli_tool_heavy", "live corpus category (live-corpus-plan mode)")
	client := flag.String("client", "codex_cli", "client label for corpus metadata (live-corpus-plan mode)")
	corpusRoot := flag.String("corpus-root", "tests/fixtures/live_corpus", "corpus root (live-corpus-plan mode)")
	flag.Parse()

	switch *mode {
	case "prompt-cache":
		os.Exit(runPromptCache(*url, *model, *count))
	case "codex-smoke":
		os.Exit(runCodexSmoke(*url, *body))
	case "live-corpus-plan":
		os.Exit(runLiveCorpusPlan(*corpusRoot, *category, *client, time.Now().UTC()))
	case "release-proof-plan":
		os.Exit(runReleaseProofPlan(*corpusRoot, time.Now().UTC()))
	case "host-resource-plan":
		os.Exit(runHostResourcePlan(*client, *corpusRoot, time.Now().UTC()))
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

func runLiveCorpusPlan(root, category, client string, now time.Time) int {
	category = safePlanName(category)
	client = normalizeLiveCorpusClient(category, client)
	root = strings.TrimSpace(root)
	if category == "" {
		fmt.Fprintln(os.Stderr, "live-corpus-plan: -category is required")
		return 2
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "live-corpus-plan: -corpus-root is required")
		return 2
	}
	sessionName := fmt.Sprintf("%s_%s", category, now.Format("20060102_150405"))
	capturePath := fmt.Sprintf("~/.slimference/captures/%s.jsonl", sessionName)
	categoryDir := filepath.Join(root, category)
	sessionFile := filepath.Join(categoryDir, sessionName+".jsonl")
	metadataFile := filepath.Join(categoryDir, "metadata.json")

	fmt.Println("Slimference T146 live corpus capture plan")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Category:     %s\n", category)
	fmt.Printf("Client:       %s\n", client)
	fmt.Printf("Capture file: %s\n", capturePath)
	fmt.Printf("Corpus file:  %s\n", sessionFile)
	fmt.Println("")
	if isOCRLFullHistoryCategory(category) {
		fmt.Println("OCRL full-history note: this category must prove model-facing OCRL on a full-history HTTP-style route.")
		fmt.Println("Codex WSS / Responses-delta sessions are intentionally shadow-only and do not satisfy this promotion proof.")
		fmt.Println("")
	}
	fmt.Println("1. Start capture against a running local Slimference path:")
	fmt.Printf("   SLIMFERENCE_DEBUG_DECISIONS_LOG=%s slimference start\n", capturePath)
	fmt.Println("")
	fmt.Println("2. Run the real operator task for this category, then stop Slimference.")
	fmt.Println("")
	fmt.Println("3. Review and export the captured flight log:")
	fmt.Printf("   slimference debug flight replay %s\n", capturePath)
	fmt.Printf("   mkdir -p %s\n", categoryDir)
	fmt.Printf("   slimference debug flight export %s\n", sessionFile)
	fmt.Println("")
	fmt.Println("4. Create metadata.json next to the session with this starting shape:")
	fmt.Println(renderLiveCorpusMetadataSkeleton(category, client))
	fmt.Println("")
	fmt.Println("5. Run gates before commit:")
	fmt.Println("   go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --check")
	fmt.Println("   go run ./scripts/benchmarks benchmark-corpus tests/fixtures/live_corpus --json")
	fmt.Printf("   go run ./scripts/benchmarks session-report %s\n", categoryDir)
	fmt.Println("")
	fmt.Println("6. Privacy rule: read the JSONL end-to-end before commit. Delete and recapture on any redaction doubt.")
	fmt.Printf("Metadata path: %s\n", metadataFile)
	return 0
}

func runReleaseProofPlan(root string, now time.Time) int {
	root = strings.TrimSpace(root)
	if root == "" {
		fmt.Fprintln(os.Stderr, "release-proof-plan: -corpus-root is required")
		return 2
	}
	matrixPath := fmt.Sprintf("~/.slimference/captures/release-proof-%s.jsonl", now.Format("20060102_150405"))

	fmt.Println("Slimference T271 release/default-on proof plan")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Purpose: prove a default-on change with real operator evidence, not synthetic smoke data.")
	fmt.Println("Privacy: this runbook is content-free. It prints commands only and never starts capture automatically.")
	fmt.Printf("Corpus root:  %s\n", root)
	fmt.Printf("Matrix file:  %s\n", matrixPath)
	fmt.Println("")
	fmt.Println("0. Clean local baseline:")
	fmt.Println("   go run ./scripts/ci")
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --check\n", root)
	fmt.Println("")
	fmt.Println("1. Open the real workday window before launching product clients:")
	fmt.Println("   go run ./scripts/utils workday-savings start")
	fmt.Println("")
	fmt.Println("2. Launch product paths only:")
	fmt.Println("   slimference codex run --transport=auto -- <real CLI task>")
	fmt.Println("   slimference codex launch-desktop --transport=app-server --replace-existing")
	fmt.Println("")
	fmt.Println("3. Capture the required live corpus categories for both clients:")
	for _, client := range releaseProofClients() {
		fmt.Printf("   # %s\n", client)
		for _, workload := range releaseProofWorkloads() {
			fmt.Printf("   go run ./scripts/verify -mode live-corpus-plan -corpus-root %s -client %s -category %s\n",
				root, client, workload)
		}
	}
	fmt.Println("")
	fmt.Println("3b. Capture the additional maxx mechanism categories:")
	for _, client := range releaseProofClients() {
		fmt.Printf("   # %s\n", client)
		for _, workload := range maxxProofWorkloads() {
			if isOCRLFullHistoryCategory(workload) {
				continue
			}
			fmt.Printf("   go run ./scripts/verify -mode live-corpus-plan -corpus-root %s -client %s -category %s\n",
				root, client, workload)
		}
	}
	fmt.Println("   # full_history_http")
	fmt.Printf("   go run ./scripts/verify -mode live-corpus-plan -corpus-root %s -client full_history_http -category ocrl_full_history\n", root)
	fmt.Println("")
	fmt.Println("4. Close all Codex sessions so WSS counters flush, then finish savings + host-resource measurement:")
	fmt.Println("   go run ./scripts/utils workday-savings finish")
	fmt.Println("")
	fmt.Println("5. Run WSS proof and release promotion gates:")
	fmt.Printf("   go run ./scripts/utils wss-proof-matrix %s --require-live-token-delta --json\n", matrixPath)
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --promotion-check\n", root)
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --promotion-check --json\n", root)
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --maxx-check\n", root)
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --maxx-check --json\n", root)
	fmt.Println("")
	fmt.Println("6. Promotion rule: default-on is allowed only if CI, WSS proof, workday savings, host-resource budget, promotion corpus, and maxx mechanism corpus all pass with zero error/canary/latency regressions.")
	return 0
}

func runHostResourcePlan(client, root string, now time.Time) int {
	client = safePlanName(client)
	root = strings.TrimSpace(root)
	if client == "" {
		fmt.Fprintln(os.Stderr, "host-resource-plan: -client is required")
		return 2
	}
	if root == "" {
		fmt.Fprintln(os.Stderr, "host-resource-plan: -corpus-root is required")
		return 2
	}
	stamp := now.Format("20060102_150405")
	bundleDir := fmt.Sprintf("~/.slimference/captures/host-resource-%s-%s", client, stamp)
	matrixPath := fmt.Sprintf("%s/matrix.jsonl", bundleDir)
	framesPath := fmt.Sprintf("%s/frames.jsonl", bundleDir)
	adminBefore := fmt.Sprintf("%s/admin-before.json", bundleDir)
	adminAfter := fmt.Sprintf("%s/admin-after.json", bundleDir)
	psBefore := fmt.Sprintf("%s/ps-before.txt", bundleDir)
	psAfter := fmt.Sprintf("%s/ps-after.txt", bundleDir)
	cpuSample := fmt.Sprintf("%s/slimference.sample.txt", bundleDir)
	workdayJSON := fmt.Sprintf("%s/workday-finish.json", bundleDir)

	fmt.Println("Slimference T272 host-resource proof plan")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Println("Purpose: prove the product stays cheap while saving tokens on a real CLI/Desktop workload.")
	fmt.Println("Privacy: content-free. Capture admin/resource counters and process profiles, not prompts or tool payloads.")
	fmt.Printf("Client:       %s\n", client)
	fmt.Printf("Bundle dir:   %s\n", bundleDir)
	fmt.Printf("Matrix file:  %s\n", matrixPath)
	fmt.Println("")
	fmt.Println("0. Clean local baseline:")
	fmt.Println("   go run ./scripts/ci")
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --maxx-check\n", root)
	fmt.Println("")
	if client != "codex_desktop" {
		fmt.Println("1. Run the automated CLI resource bundle:")
		fmt.Printf("   go run ./scripts/utils codex-capture-run --resource-profile-proof %s --workload-class host_resource_long_workday --expected-reducer host_budget_ok --codex-timeout=180s --exit-marker HOST_RESOURCE_DONE --quiet-codex-output -- exec \"<real repeat/search/tool-heavy host-resource workload, then print HOST_RESOURCE_DONE>\"\n", bundleDir)
		fmt.Println("")
		fmt.Println("2. Gates:")
		fmt.Printf("   go run ./scripts/utils wss-proof-matrix %s --required-workload=host_resource_long_workday --expected-reducer host_budget_ok --require-live-token-delta --min-positive=1 --json\n", matrixPath)
		fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --maxx-check\n", root)
		fmt.Println("   # Final release gate after BOTH codex_cli and codex_desktop host-resource bundles exist:")
		fmt.Printf("   go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures <clean-release-matrix.jsonl> --json\n")
		fmt.Printf("   go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> --resource-profile-proof <codex_cli_bundle> --resource-profile-proof <codex_desktop_bundle> --json\n")
		fmt.Println("")
		fmt.Println("3. Pass criteria: generated bundle contains admin-before/after, ps-before/after, workday-finish, slimference.sample, and matrix.jsonl; host_budget status ok; RSS <= 200 MB; CPU window <= 0.5% outside active work; no disk/state growth outside configured bounds; no parse/degrade/compression errors; and profile sample shows no hot Slimference loop dominating the operator workload.")
		return 0
	}
	fmt.Println("1. Start a bounded workday/resource window:")
	fmt.Printf("   mkdir -p %s\n", bundleDir)
	fmt.Println("   go run ./scripts/utils workday-savings start")
	fmt.Println("   pid=$(pgrep -n slimference)")
	fmt.Printf("   go run ./scripts/utils aggregate-savings --json > %s\n", adminBefore)
	fmt.Printf("   ps -p \"$pid\" -o pid,ppid,rss,vsz,pcpu,etime,command > %s\n", psBefore)
	fmt.Println("")
	fmt.Println("2. Run one real product workload through the selected client while Slimference is active:")
	fmt.Println("   # codex_cli:     slimference codex run --transport=auto -- <real repeat/search/tool-heavy task>")
	fmt.Printf("   # codex_desktop: SLIMFERENCE_WSS_AB_CAPTURE=%s slimference codex launch-desktop --transport=app-server --replace-existing, then run the operator prompts\n", framesPath)
	fmt.Println("")
	fmt.Println("3. Capture a CPU profile sample during the workload:")
	fmt.Printf("   /usr/bin/sample \"$pid\" 10 1 -file %s\n", cpuSample)
	fmt.Println("")
	fmt.Println("4. Close the workload and capture final resource state:")
	fmt.Printf("   go run ./scripts/utils aggregate-savings --json > %s\n", adminAfter)
	fmt.Printf("   ps -p \"$pid\" -o pid,ppid,rss,vsz,pcpu,etime,command > %s\n", psAfter)
	fmt.Printf("   go run ./scripts/utils workday-savings finish --json > %s\n", workdayJSON)
	fmt.Println("")
	fmt.Println("5. Append or collect the matching WSS proof row with host_budget_ok:")
	if client == "codex_desktop" {
		fmt.Printf("   go run ./scripts/utils wss-proof-live-row --matrix-row %s --frames %s --client desktop --workload-class host_resource_long_workday --expected-reducer host_budget_ok\n", matrixPath, framesPath)
	} else {
		fmt.Printf("   go run ./scripts/utils codex-capture-run --transport=wss --matrix-row %s --workload-class host_resource_long_workday --expected-reducer host_budget_ok -- exec <real host-resource workload prompt>\n", matrixPath)
	}
	fmt.Println("")
	fmt.Println("6. Gates:")
	fmt.Printf("   go run ./scripts/utils wss-proof-matrix %s --required-workload=host_resource_long_workday --expected-reducer host_budget_ok --require-live-token-delta --min-positive=1 --json\n", matrixPath)
	fmt.Printf("   go run ./scripts/benchmarks benchmark-corpus %s --maxx-check\n", root)
	fmt.Println("   # Final release gate after BOTH codex_cli and codex_desktop host-resource bundles exist:")
	fmt.Printf("   go run ./scripts/utils wss-proof-clean-matrix ~/.slimference/captures <clean-release-matrix.jsonl> --json\n")
	fmt.Printf("   go run ./scripts/utils release-proof-report <clean-release-matrix.jsonl> --resource-profile-proof <codex_cli_bundle> --resource-profile-proof <codex_desktop_bundle> --json\n")
	fmt.Println("")
	fmt.Println("7. Pass criteria: admin/workday host_budget status ok, RSS <= 200 MB, CPU window <= 0.5% outside active work, no disk/state growth outside configured bounds, no parse/degrade/compression errors, and profile sample shows no hot Slimference loop dominating the operator workload.")
	return 0
}

func releaseProofClients() []string {
	return []string{"codex_cli", "codex_desktop"}
}

func releaseProofWorkloads() []string {
	return []string{
		"repeat_read",
		"ranged_read",
		"search_loop",
		"git_status",
		"test_failure",
		"apply_patch_edit_read",
		"large_tool_output",
		"long_workday",
	}
}

func maxxProofWorkloads() []string {
	return []string{
		"chunk_dedup_similar_outputs",
		"chunk_dedup_log_output",
		"chunk_dedup_test_output",
		"ocrl_full_history",
		"output_reduce_aggressive",
		"output_reduce_ab",
		"tool_heavy",
		"provider_cache_long_session",
		"host_resource_long_workday",
	}
}

func normalizeLiveCorpusClient(category, client string) string {
	if isOCRLFullHistoryCategory(category) {
		return "full_history_http"
	}
	client = strings.TrimSpace(client)
	if client == "" {
		return "codex_cli"
	}
	return client
}

func isOCRLFullHistoryCategory(category string) bool {
	return strings.TrimSpace(category) == "ocrl_full_history"
}

func safePlanName(value string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.ToLower(value) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

func renderLiveCorpusMetadataSkeleton(category, client string) string {
	client = normalizeLiveCorpusClient(category, client)
	payload := map[string]any{
		"category":                            category,
		"description":                         "Real operator-captured session; replace this with the exact task shape before commit.",
		"synthetic":                           false,
		"evidence_level":                      "live_operator",
		"client_family":                       client,
		"workload_class":                      category,
		"language":                            "mixed",
		"tool_mix":                            client,
		"expected_savings_min":                0.10,
		"expected_savings_max":                0.90,
		"expected_request_count":              1,
		"expected_max_errors":                 0,
		"expected_latency_p95_max_ms":         1000,
		"expected_provider_cache_read_min":    0,
		"expected_output_reduce_applied_min":  0,
		"expected_reread_count_max":           0,
		"expected_planner_missed_max":         0,
		"expected_planner_bypass_applied_max": 0,
		"scenario_validators":                 liveCorpusScenarioValidators(category),
		"notes":                               "Scrubbed manually after T109 redaction; raw prompts, secrets, screenshots, and absolute paths verified absent.",
	}
	applyLiveCorpusWorkloadDefaults(payload, category)
	out, _ := json.MarshalIndent(payload, "   ", "  ")
	return string(out)
}

func liveCorpusScenarioValidators(workload string) []string {
	switch strings.TrimSpace(workload) {
	case "ocrl_full_history":
		return []string{"ocrl_full_history", "low_error"}
	case "output_reduce_aggressive":
		return []string{"output_reduce", "low_error"}
	case "output_reduce_ab":
		return []string{"output_reduce_ab", "low_error"}
	case "provider_cache_long_session":
		return []string{"cache_reuse", "low_error"}
	case "tool_heavy":
		return []string{"tool_heavy", "low_error"}
	case "host_resource_long_workday", "chunk_dedup_similar_outputs", "chunk_dedup_log_output", "chunk_dedup_test_output":
		return []string{"host_budget_ok", "low_error"}
	default:
		return []string{"low_error"}
	}
}

func applyLiveCorpusWorkloadDefaults(payload map[string]any, workload string) {
	switch strings.TrimSpace(workload) {
	case "output_reduce_aggressive":
		payload["expected_savings_min"] = 0.0
		payload["expected_output_reduce_applied_min"] = 1
	case "output_reduce_ab":
		payload["expected_savings_min"] = 0.0
		payload["expected_request_count"] = 0
		payload["expected_output_reduce_ab_pairs_min"] = 1
		payload["expected_output_reduce_ab_net_saved_min"] = 1
		payload["expected_output_reduce_ab_savings_pct_min"] = 1.0
	case "provider_cache_long_session":
		payload["expected_savings_min"] = 0.0
		payload["expected_provider_cache_read_min"] = 1
	case "tool_heavy", "chunk_dedup_similar_outputs", "chunk_dedup_log_output", "chunk_dedup_test_output", "host_resource_long_workday":
		payload["expected_savings_min"] = 0.0
		payload["expected_saved_tokens_min"] = 1
	case "ocrl_full_history":
		payload["description"] = "Real full-history HTTP-style operator-captured session proving model-facing OCRL applied; Codex WSS / Responses-delta sessions are shadow-only and do not satisfy this category."
		payload["client_family"] = "full_history_http"
		payload["tool_mix"] = "full_history_http_archive_backed"
		payload["expected_savings_min"] = 0.0
		payload["expected_saved_tokens_min"] = 1
	}
}
