package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/outputreduce"
)

func TestServeHTTP_OutputReduceInjectsBeforeUpstream(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":12}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("what is the current status? ", 2200) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive not injected: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 1 || snap.InputOverheadTokens == 0 || snap.OutputTokensObserved == 0 {
		t.Fatalf("output-reduce snapshot: %+v", snap)
	}
}

func TestServeHTTP_OutputReduceSkipsBelowMinTokens(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 10_000
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive injected below min: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "below_min_tokens" {
		t.Fatalf("output-reduce snapshot: %+v", snap)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(captured, &root); err != nil {
		t.Fatal(err)
	}
	if _, ok := root["system"]; ok {
		t.Fatalf("unexpected system field: %s", captured)
	}
}

func TestServeHTTP_NonStreamingOpenAIUsageOverridesEstimatedOutputTokens(t *testing.T) {
	t.Parallel()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"resp_1","usage":{"input_tokens":200,"input_tokens_details":{"cached_tokens":50},"output_tokens":123}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.OpenAI.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = false
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"gpt-5","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if snap := p.outputReduce.Snapshot(); snap.OutputTokensObserved != 123 {
		t.Fatalf("output tokens should use provider usage, got %+v", snap)
	}
}

func TestServeHTTP_OutputReduceInjectionErrorFallsBackToOriginal(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}]}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.CustomDirectivePath = "/definitely/missing/slimference-output-rules.md"
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":64,"messages":[{"role":"user","content":"` + strings.Repeat("what is the current status? ", 2200) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive injected after custom directive read error: %s", captured)
	}
	snap := p.outputReduce.Snapshot()
	if snap.InjectedTurns != 0 || snap.SkippedTurns != 1 || snap.LastReason != "error" {
		t.Fatalf("output-reduce snapshot after injection error: %+v", snap)
	}
}

func TestServeHTTP_OutputReduceCooldownFeedsPlannerAndSoftensProfile(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":500}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.Profile = string(outputreduce.ProfileAggressive)
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.AutoTuneEnabled = true
	cfg.Compression.OutputReduce.AutoTuneMinSamples = 1
	cfg.Compression.OutputReduce.MaxFailureRateDelta = 0.1
	cfg.Compression.OutputReduce.CooldownTurns = 3
	cfg.Secrets.Mode = "off"

	model := "claude-3-5-sonnet-20241022"
	p := New(cfg)
	p.outputReduce.ObserveOutcome(outputreduce.Outcome{
		Provider:  "anthropic",
		Model:     model,
		Profile:   string(outputreduce.ProfileAggressive),
		TaskShape: outputreduce.ShapeDirectAnswer,
		Applied:   true,
		Failed:    true,
	})

	body := `{"model":"` + model + `","max_tokens":512,"messages":[{"role":"user","content":"` + strings.Repeat("answer this operational question tersely ", 9000) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if !strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("directive not injected: %s", captured)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 || summaries[0].Plan == nil {
		t.Fatalf("missing planner summary: %#v", summaries)
	}
	if !hasPlanAction(summaries[0].Plan.Decisions, "l4_output", "cheap_only", "quality_cooldown_soften_layer4") {
		t.Fatalf("planner did not expose output-reduce cooldown: %+v", summaries[0].Plan.Decisions)
	}
	if summaries[0].OutputReduce.Profile != string(outputreduce.ProfileStandard) {
		t.Fatalf("cooldown should soften aggressive profile to standard, summary=%+v", summaries[0].OutputReduce)
	}
}

func TestServeHTTP_OutputReduceRepairFollowupImmediatelySoftensNextBucket(t *testing.T) {
	t.Parallel()
	var capturedBodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBodies = append(capturedBodies, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":500}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.Profile = string(outputreduce.ProfileAggressive)
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Compression.OutputReduce.AutoTuneEnabled = true
	cfg.Compression.OutputReduce.AutoTuneMinSamples = 30
	cfg.Compression.OutputReduce.MaxFailureRateDelta = 0.1
	cfg.Compression.OutputReduce.CooldownTurns = 3
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	model := "claude-3-5-sonnet-20241022"
	send := func(content string) {
		t.Helper()
		body := `{"model":"` + model + `","max_tokens":512,"messages":[{"role":"user","content":"` + content + `"}]}`
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("anthropic-trace-id", "output-reduce-repair-session")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		res := rec.Result()
		t.Cleanup(func() { _ = res.Body.Close() })
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
		}
	}

	direct := strings.Repeat("answer this operational question tersely ", 9000)
	send(direct)
	send(strings.Repeat("you skipped the requested detail, explain more ", 2000))
	send(direct)

	if len(capturedBodies) != 3 {
		t.Fatalf("captured %d bodies", len(capturedBodies))
	}
	if !strings.Contains(capturedBodies[0], "Aggressive output rules") {
		t.Fatalf("first direct turn should use aggressive directive: %s", capturedBodies[0])
	}
	if strings.Contains(capturedBodies[1], "#slimference-output-rules") {
		t.Fatalf("repair follow-up should skip injection: %s", capturedBodies[1])
	}
	if strings.Contains(capturedBodies[2], "Aggressive output rules") || !strings.Contains(capturedBodies[2], "Output rules:") {
		t.Fatalf("repair signal should immediately soften next matching bucket: %s", capturedBodies[2])
	}
}

func TestServeHTTP_OutputReduceRepairFollowupBreadthSignals(t *testing.T) {
	tests := []struct {
		name   string
		repair string
	}{
		{name: "german user reask", repair: "Du hast nicht geliefert, da fehlt der wichtige Output, nochmal ausführlicher bitte."},
		{name: "malformed patch repair", repair: "apply_patch failed with invalid patch; the patch could not apply."},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var capturedBodies []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				capturedBodies = append(capturedBodies, string(body))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":500}}`)
			}))
			defer upstream.Close()

			cfg := config.Defaults()
			cfg.Upstream.Anthropic.BaseURL = upstream.URL
			cfg.Compression.Layer1Enabled = false
			cfg.Compression.Layer2Enabled = false
			cfg.Compression.Layer3Enabled = false
			cfg.Compression.OutputReduce.Enabled = true
			cfg.Compression.OutputReduce.Profile = string(outputreduce.ProfileAggressive)
			cfg.Compression.OutputReduce.MinInputTokens = 1
			cfg.Compression.OutputReduce.AutoTuneEnabled = true
			cfg.Compression.OutputReduce.AutoTuneMinSamples = 30
			cfg.Compression.OutputReduce.MaxFailureRateDelta = 0.1
			cfg.Compression.OutputReduce.CooldownTurns = 3
			cfg.Secrets.Mode = "off"

			p := New(cfg)
			model := "claude-3-5-sonnet-20241022"
			send := func(content string) {
				t.Helper()
				payload := map[string]any{
					"model":      model,
					"max_tokens": 512,
					"messages": []map[string]string{
						{"role": "user", "content": content},
					},
				}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("anthropic-trace-id", "output-reduce-repair-breadth-session")
				rec := httptest.NewRecorder()
				p.ServeHTTP(rec, req)
				res := rec.Result()
				t.Cleanup(func() { _ = res.Body.Close() })
				if res.StatusCode != http.StatusOK {
					t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
				}
			}

			direct := strings.Repeat("answer this operational question tersely ", 9000)
			send(direct)
			send(strings.Repeat(tt.repair+" ", 2000))
			send(direct)

			if len(capturedBodies) != 3 {
				t.Fatalf("captured %d bodies", len(capturedBodies))
			}
			if !strings.Contains(capturedBodies[0], "Aggressive output rules") {
				t.Fatalf("first direct turn should use aggressive directive: %s", capturedBodies[0])
			}
			if strings.Contains(capturedBodies[1], "#slimference-output-rules") {
				t.Fatalf("repair follow-up should skip injection: %s", capturedBodies[1])
			}
			if strings.Contains(capturedBodies[2], "Aggressive output rules") || !strings.Contains(capturedBodies[2], "Output rules:") {
				t.Fatalf("repair signal should soften next matching bucket: %s", capturedBodies[2])
			}
		})
	}
}

func TestServeHTTP_OutputReduceCapsAggressiveCodeEditProfile(t *testing.T) {
	t.Parallel()
	var captured []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"m1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"output_tokens":500}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.Layer3Enabled = false
	cfg.Compression.OutputReduce.Enabled = true
	cfg.Compression.OutputReduce.Profile = string(outputreduce.ProfileAggressive)
	cfg.Compression.OutputReduce.MinInputTokens = 1
	cfg.Secrets.Mode = "off"

	p := New(cfg)
	body := `{"model":"claude-3-5-sonnet-20241022","max_tokens":512,"messages":[{"role":"user","content":"` + strings.Repeat("apply_patch this bug and preserve exact paths ", 80) + `"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)

	res := rec.Result()
	t.Cleanup(func() { _ = res.Body.Close() })
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", res.StatusCode, rec.Body.String())
	}
	if strings.Contains(string(captured), "Aggressive output rules") || strings.Contains(string(captured), "fewest complete words") {
		t.Fatalf("code-edit request received aggressive output directive: %s", captured)
	}
	if strings.Contains(string(captured), "#slimference-output-rules") {
		t.Fatalf("code-edit request must not receive any output-reduce directive without paired A/B proof: %s", captured)
	}
	summaries := p.DebugRecorder().Last(1, false)
	if len(summaries) != 1 {
		t.Fatalf("missing summary: %#v", summaries)
	}
	if summaries[0].OutputReduce.Profile != string(outputreduce.ProfileStandard) || summaries[0].OutputReduce.TaskShape != string(outputreduce.ShapeCodeEdit) {
		t.Fatalf("code-edit output-reduce safety cap missing: %+v", summaries[0].OutputReduce)
	}
	if summaries[0].OutputReduce.Applied || summaries[0].OutputReduce.Reason != "unproven_task_shape_ab_required" {
		t.Fatalf("code-edit output-reduce should be fully gated without A/B proof: %+v", summaries[0].OutputReduce)
	}
}
