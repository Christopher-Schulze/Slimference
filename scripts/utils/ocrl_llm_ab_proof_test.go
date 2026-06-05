package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunOCRLLLMABProofRequiresKey(t *testing.T) {
	t.Setenv("SLIMFERENCE_TEST_LLM_KEY", "")
	var stdout, stderr bytes.Buffer
	code := runOCRLLLMABProof([]string{"--model", "test-model", "--api-key-env", "SLIMFERENCE_TEST_LLM_KEY"}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("code=%d want 4 stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "SLIMFERENCE_TEST_LLM_KEY is unset") {
		t.Fatalf("missing key error: %q", stderr.String())
	}
}

func TestOCRLLLMABProofBlocksBroadPromotionOnDetailLoss(t *testing.T) {
	t.Setenv("SLIMFERENCE_TEST_LLM_KEY", "test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		text := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(text, "irrelevant_old_context"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"scenario\":\"irrelevant_old_context\",\"decision\":\"ship\",\"risk\":\"low\",\"action\":\"ship\"}"}}]}`))
		case strings.Contains(text, "[ocrl:v1") && strings.Contains(text, "detail_dependency_guard"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"scenario\":\"detail_dependency_guard\",\"region\":\"unknown\",\"port\":\"unknown\",\"flag\":\"unknown\"}"}}]}`))
		case strings.Contains(text, "detail_dependency_guard"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"scenario\":\"detail_dependency_guard\",\"region\":\"eu-central-1\",\"port\":\"8443\",\"flag\":\"OCRL_SAFE_ENABLED\"}"}}]}`))
		default:
			t.Fatalf("unexpected upstream body: %s", text)
		}
	}))
	defer upstream.Close()

	var stdout, stderr bytes.Buffer
	code := runOCRLLLMABProof([]string{
		"--model=test-model",
		"--base-url=" + upstream.URL,
		"--api-key-env=SLIMFERENCE_TEST_LLM_KEY",
		"--json",
	}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d want 3 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		`"name": "irrelevant_old_context"`,
		`"gate_passed": true`,
		`"name": "detail_dependency_guard"`,
		`"broad_promotion_signal": "broad_promotion_blocked"`,
		`"detail_dependency_guard: detail-dependent decision changed after OCRL applied"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in report:\n%s", want, out)
		}
	}
}

func TestOCRLLLMABProofCanSkipNegativeScenario(t *testing.T) {
	t.Setenv("SLIMFERENCE_TEST_LLM_KEY", "test-key")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "irrelevant_old_context") {
			t.Fatalf("unexpected upstream body: %s", string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"scenario\":\"irrelevant_old_context\",\"decision\":\"ship\",\"risk\":\"low\",\"action\":\"ship\"}"}}]}`))
	}))
	defer upstream.Close()

	var stdout, stderr bytes.Buffer
	code := runOCRLLLMABProof([]string{
		"--model=test-model",
		"--base-url=" + upstream.URL,
		"--api-key-env=SLIMFERENCE_TEST_LLM_KEY",
		"--skip-negative",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d want 0 stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "detail_dependency_guard") {
		t.Fatalf("negative scenario should be skipped:\n%s", stdout.String())
	}
}
