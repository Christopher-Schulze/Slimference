package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"github.com/slimference/slimference/internal/config"
	dbg "github.com/slimference/slimference/internal/debug"
	"github.com/slimference/slimference/internal/proxy"
)

type ocrlLLMABFlags struct {
	model           string
	baseURL         string
	apiKeyEnv       string
	outputFormat    string
	includeNegative bool
	help            bool
}

type ocrlLLMABReport struct {
	Model        string              `json:"model"`
	BaseURL      string              `json:"base_url"`
	GatePassed   bool                `json:"gate_passed"`
	GateFailures []string            `json:"gate_failures,omitempty"`
	Scenarios    []ocrlLLMABScenario `json:"scenarios"`
}

type ocrlLLMABScenario struct {
	Name                 string       `json:"name"`
	ExpectEquivalence    bool         `json:"expect_equivalence"`
	Baseline             ocrlLLMABRun `json:"baseline"`
	OCRL                 ocrlLLMABRun `json:"ocrl"`
	Equivalent           bool         `json:"equivalent"`
	GatePassed           bool         `json:"gate_passed"`
	GateFailures         []string     `json:"gate_failures,omitempty"`
	BroadPromotionSignal string       `json:"broad_promotion_signal"`
}

type ocrlLLMABRun struct {
	HTTPStatus      int                      `json:"http_status"`
	Response        ocrlLLMDecision          `json:"response"`
	ParseError      string                   `json:"parse_error,omitempty"`
	OCRLAplied      bool                     `json:"ocrl_applied"`
	OCRLSavedTokens int                      `json:"ocrl_saved_tokens"`
	OCRLSummary     dbg.ContextLedgerSummary `json:"ocrl_summary,omitempty"`
	LatencyMs       float64                  `json:"latency_ms"`
}

type ocrlLLMDecision struct {
	Scenario string `json:"scenario"`
	Decision string `json:"decision"`
	Risk     string `json:"risk,omitempty"`
	Action   string `json:"action,omitempty"`
	Region   string `json:"region,omitempty"`
	Port     string `json:"port,omitempty"`
	Flag     string `json:"flag,omitempty"`
	Status   string `json:"status,omitempty"`
}

type ocrlLLMChatRequest struct {
	Model          string               `json:"model"`
	Messages       []ocrlLLMChatMessage `json:"messages"`
	ResponseFormat *ocrlLLMResponseMode `json:"response_format,omitempty"`
}

type ocrlLLMChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ocrlLLMResponseMode struct {
	Type string `json:"type"`
}

type ocrlLLMChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type ocrlLLMScenarioSpec struct {
	name              string
	oldAssistantText  string
	currentUserText   string
	expectEquivalence bool
}

const ocrlLLMABHelpText = `ocrl-llm-ab-proof: run real-LLM Baseline vs OCRL A/B decision proof

Usage:
  go run ./scripts/utils ocrl-llm-ab-proof --model MODEL [--base-url URL] [--api-key-env ENV] [--json] [--skip-negative]

The runner sends two full-history /v1/chat/completions requests per scenario
through the real Slimference proxy path: baseline with Layer 2 off, and OCRL
with Layer 2 on plus OCRL mode=max. It never prints the API key.

Gate meaning:
  - irrelevant_old_context must produce the same decision with and without OCRL.
  - detail_dependency_guard is an adversarial broad-promotion check. If OCRL
    applies and the decision changes, broad model-facing promotion is blocked.
    That is a product-safety finding, not a tooling failure.

No API key is bundled. Set OPENAI_API_KEY or pass --api-key-env.`

func runOCRLLLMABProof(args []string, stdout, stderr io.Writer) int {
	flags, err := parseOCRLLLMABFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 2
	}
	if flags.help {
		fmt.Fprintln(stdout, ocrlLLMABHelpText)
		return 0
	}
	apiKey := strings.TrimSpace(os.Getenv(flags.apiKeyEnv))
	if apiKey == "" {
		fmt.Fprintf(stderr, "%s is unset; real LLM OCRL A/B proof cannot run without a provider key\n", flags.apiKeyEnv)
		return 4
	}
	report, err := runOCRLLLMABProofWithKey(flags, apiKey)
	if err != nil {
		fmt.Fprintln(stderr, err.Error())
		return 1
	}
	if flags.outputFormat == outputJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintln(stderr, err.Error())
			return 1
		}
	} else {
		writeOCRLLLMABReport(stdout, report)
	}
	if !report.GatePassed {
		return 3
	}
	return 0
}

func parseOCRLLLMABFlags(args []string) (ocrlLLMABFlags, error) {
	flags := ocrlLLMABFlags{
		baseURL:         "https://api.openai.com",
		apiKeyEnv:       "OPENAI_API_KEY",
		outputFormat:    outputText,
		includeNegative: true,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			flags.help = true
		case arg == "--json":
			flags.outputFormat = outputJSON
		case arg == "--skip-negative":
			flags.includeNegative = false
		case strings.HasPrefix(arg, "--model="):
			flags.model = strings.TrimSpace(strings.TrimPrefix(arg, "--model="))
		case arg == "--model":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--model requires a value")
			}
			flags.model = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "--base-url="):
			flags.baseURL = strings.TrimRight(strings.TrimSpace(strings.TrimPrefix(arg, "--base-url=")), "/")
		case arg == "--base-url":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--base-url requires a value")
			}
			flags.baseURL = strings.TrimRight(strings.TrimSpace(args[i]), "/")
		case strings.HasPrefix(arg, "--api-key-env="):
			flags.apiKeyEnv = strings.TrimSpace(strings.TrimPrefix(arg, "--api-key-env="))
		case arg == "--api-key-env":
			i++
			if i >= len(args) {
				return flags, fmt.Errorf("--api-key-env requires a value")
			}
			flags.apiKeyEnv = strings.TrimSpace(args[i])
		case strings.HasPrefix(arg, "-"):
			return flags, fmt.Errorf("unknown flag: %s", arg)
		default:
			if flags.model != "" {
				return flags, fmt.Errorf("multiple models provided")
			}
			flags.model = strings.TrimSpace(arg)
		}
	}
	if flags.help {
		return flags, nil
	}
	if flags.model == "" {
		if envModel := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); envModel != "" {
			flags.model = envModel
		}
	}
	if flags.model == "" {
		return flags, fmt.Errorf("--model or OPENAI_MODEL is required")
	}
	if flags.apiKeyEnv == "" {
		return flags, fmt.Errorf("--api-key-env must not be empty")
	}
	if flags.baseURL == "" {
		return flags, fmt.Errorf("--base-url must not be empty")
	}
	return flags, nil
}

func runOCRLLLMABProofWithKey(flags ocrlLLMABFlags, apiKey string) (ocrlLLMABReport, error) {
	report := ocrlLLMABReport{Model: flags.model, BaseURL: flags.baseURL, GatePassed: true}
	scenarios := []ocrlLLMScenarioSpec{ocrlIrrelevantScenario()}
	if flags.includeNegative {
		scenarios = append(scenarios, ocrlDetailDependencyScenario())
	}
	for _, scenario := range scenarios {
		result, err := runOCRLLLMABScenario(flags, apiKey, scenario)
		if err != nil {
			return report, err
		}
		if !result.GatePassed {
			report.GatePassed = false
			for _, failure := range result.GateFailures {
				report.GateFailures = append(report.GateFailures, scenario.name+": "+failure)
			}
		}
		report.Scenarios = append(report.Scenarios, result)
	}
	return report, nil
}

func runOCRLLLMABScenario(flags ocrlLLMABFlags, apiKey string, scenario ocrlLLMScenarioSpec) (ocrlLLMABScenario, error) {
	baseline, err := runOCRLLLMVariant(flags, apiKey, scenario, false)
	if err != nil {
		return ocrlLLMABScenario{}, fmt.Errorf("%s baseline: %w", scenario.name, err)
	}
	ocrl, err := runOCRLLLMVariant(flags, apiKey, scenario, true)
	if err != nil {
		return ocrlLLMABScenario{}, fmt.Errorf("%s ocrl: %w", scenario.name, err)
	}
	out := ocrlLLMABScenario{
		Name:              scenario.name,
		ExpectEquivalence: scenario.expectEquivalence,
		Baseline:          baseline,
		OCRL:              ocrl,
		Equivalent:        baseline.Response == ocrl.Response,
		GatePassed:        true,
	}
	if baseline.ParseError != "" {
		out.GateFailures = append(out.GateFailures, "baseline response was not parseable JSON: "+baseline.ParseError)
	}
	if ocrl.ParseError != "" {
		out.GateFailures = append(out.GateFailures, "ocrl response was not parseable JSON: "+ocrl.ParseError)
	}
	if scenario.expectEquivalence && !out.Equivalent {
		out.GateFailures = append(out.GateFailures, "decision changed under OCRL")
	}
	if scenario.expectEquivalence && !ocrl.OCRLAplied {
		out.GateFailures = append(out.GateFailures, "OCRL did not apply, so no model-facing proof was measured")
	}
	if !scenario.expectEquivalence {
		switch {
		case !ocrl.OCRLAplied:
			out.BroadPromotionSignal = "safe_full_pass"
		case out.Equivalent:
			out.BroadPromotionSignal = "detail_equivalent"
		default:
			out.BroadPromotionSignal = "broad_promotion_blocked"
			out.GateFailures = append(out.GateFailures, "detail-dependent decision changed after OCRL applied")
		}
	}
	if len(out.GateFailures) > 0 {
		out.GatePassed = false
	}
	return out, nil
}

func runOCRLLLMVariant(flags ocrlLLMABFlags, apiKey string, scenario ocrlLLMScenarioSpec, useOCRL bool) (ocrlLLMABRun, error) {
	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = flags.baseURL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = useOCRL
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.MinMessagesForCompression = 1
	cfg.Compression.MinTokensForLayer2 = 1
	cfg.Compression.SlidingWindow = 1
	cfg.Compression.OCRL.Mode = "max"
	cfg.Compression.OCRL.MinNetSavedTokens = 1
	cfg.Compression.OCRL.MaxCapsules = 16
	cfg.Secrets.Mode = "off"
	p := proxy.New(cfg)

	body, err := json.Marshal(ocrlLLMChatRequest{
		Model: flags.model,
		Messages: []ocrlLLMChatMessage{
			{Role: "system", Content: "Return only one compact JSON object. No markdown. No prose."},
			{Role: "assistant", Content: scenario.oldAssistantText},
			{Role: "assistant", Content: strings.Repeat("Recent assistant tail confirms the next user request is the active task. ", 12)},
			{Role: "user", Content: scenario.currentUserText},
		},
		ResponseFormat: &ocrlLLMResponseMode{Type: "json_object"},
	})
	if err != nil {
		return ocrlLLMABRun{}, err
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	start := time.Now()
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	run := ocrlLLMABRun{HTTPStatus: rec.Code, LatencyMs: float64(time.Since(start).Microseconds()) / 1000.0}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) == 1 {
		run.OCRLSummary = summaries[0].ContextLedger
		run.OCRLAplied = summaries[0].ContextLedger.OCRLReason == "applied" && !summaries[0].ContextLedger.OCRLShadowOnly
		run.OCRLSavedTokens = summaries[0].ContextLedger.OCRLShadowSavedTokens
	}
	if rec.Code < 200 || rec.Code >= 300 {
		return run, fmt.Errorf("upstream status %d: %s", rec.Code, strings.TrimSpace(rec.Body.String()))
	}
	decision, parseErr := parseOCRLLLMDecision(rec.Body.Bytes())
	run.Response = decision
	if parseErr != nil {
		run.ParseError = parseErr.Error()
	}
	return run, nil
}

func parseOCRLLLMDecision(body []byte) (ocrlLLMDecision, error) {
	var resp ocrlLLMChatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ocrlLLMDecision{}, err
	}
	if resp.Error != nil {
		return ocrlLLMDecision{}, fmt.Errorf("%s: %s", resp.Error.Type, resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return ocrlLLMDecision{}, fmt.Errorf("missing choices")
	}
	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return ocrlLLMDecision{}, fmt.Errorf("missing JSON object in %q", content)
	}
	var decision ocrlLLMDecision
	if err := json.Unmarshal([]byte(content[start:end+1]), &decision); err != nil {
		return ocrlLLMDecision{}, err
	}
	return decision, nil
}

func writeOCRLLLMABReport(w io.Writer, report ocrlLLMABReport) {
	fmt.Fprintln(w, "OCRL LLM A/B proof")
	fmt.Fprintf(w, "  model:       %s\n", report.Model)
	fmt.Fprintf(w, "  base_url:    %s\n", report.BaseURL)
	fmt.Fprintf(w, "  gate:        %s\n", ocrlLLMPassFail(report.GatePassed))
	for _, scenario := range report.Scenarios {
		fmt.Fprintf(w, "\n%s\n", scenario.Name)
		fmt.Fprintf(w, "  equivalent:  %t\n", scenario.Equivalent)
		fmt.Fprintf(w, "  ocrl_applied:%t\n", scenario.OCRL.OCRLAplied)
		fmt.Fprintf(w, "  ocrl_saved:  %d\n", scenario.OCRL.OCRLSavedTokens)
		if scenario.BroadPromotionSignal != "" {
			fmt.Fprintf(w, "  signal:      %s\n", scenario.BroadPromotionSignal)
		}
		fmt.Fprintf(w, "  gate:        %s\n", ocrlLLMPassFail(scenario.GatePassed))
		for _, failure := range scenario.GateFailures {
			fmt.Fprintf(w, "  - %s\n", failure)
		}
	}
}

func ocrlLLMPassFail(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func ocrlIrrelevantScenario() ocrlLLMScenarioSpec {
	return ocrlLLMScenarioSpec{
		name: "irrelevant_old_context",
		oldAssistantText: strings.Repeat(
			"Archived operational log: package inventory was green, old benchmark rows were informational only, and no current decision should use this stale block. ",
			180,
		),
		currentUserText:   "Scenario irrelevant_old_context. Use only this latest instruction: status is green, risk is low, action is ship. Return JSON with scenario, decision, risk, and action.",
		expectEquivalence: true,
	}
}

func ocrlDetailDependencyScenario() ocrlLLMScenarioSpec {
	return ocrlLLMScenarioSpec{
		name: "detail_dependency_guard",
		oldAssistantText: strings.Repeat(
			"Critical archived deployment fact for scenario detail_dependency_guard: region=eu-central-1, port=8443, flag=OCRL_SAFE_ENABLED. ",
			180,
		),
		currentUserText:   "Scenario detail_dependency_guard. Return JSON with scenario, region, port, and flag from the earlier archived deployment fact.",
		expectEquivalence: false,
	}
}
