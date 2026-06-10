package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/analytics"
	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
)

func TestHandleSubcommand_stats_today_withSnapshot(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	snap := analytics.AnalyticsSnapshot{
		SessionStart:     time.Now().UTC(),
		TotalRequests:    2,
		TotalInputTokens: 100,
		CacheHits:        1,
	}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"stats", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Slimference Stats") || !strings.Contains(out, "Messages sent:") {
		t.Fatalf("stdout: %q", out)
	}
}

func TestHandleSubcommand_stats_week_and_month(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	snap := analytics.AnalyticsSnapshot{
		SessionStart:     time.Now().UTC(),
		TotalRequests:    1,
		TotalInputTokens: 42,
	}
	if err := p.WriteSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	p.Close()

	for _, period := range []string{"week", "month"} {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleSubcommand([]string{"stats", period})
		_ = w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		out := buf.String()
		if !strings.Contains(out, "Slimference Stats") || !strings.Contains(out, "Messages sent:") {
			t.Fatalf("stats %s: %q", period, out)
		}
	}
}

func TestHandleSubcommand_stats_emptyLogDir_messages(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	cases := []struct {
		args []string
		want string
	}{
		{[]string{"stats", "today"}, "No stats for today yet."},
		{[]string{"stats", "week"}, "No stats for this week."},
		{[]string{"stats", "month"}, "No stats for this month."},
	}
	for _, tc := range cases {
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		handleSubcommand(tc.args)
		_ = w.Close()
		os.Stdout = old
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), tc.want) {
			t.Fatalf("%v: want %q, got %q", tc.args, tc.want, buf.String())
		}
	}
}

func TestHandleSubcommand_stats_configLoadErrorExits1(t *testing.T) {
	if os.Getenv("TP_STATS_BAD_CFG") == "1" {
		t.Setenv("SLIMFERENCE_CONFIG", os.Getenv("TP_STATS_BAD_CFG_FILE"))
		handleSubcommand([]string{"stats", "today"})
		return
	}
	tmp := t.TempDir()
	badPath := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(badPath, []byte("this is not valid toml [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_stats_configLoadErrorExits1")
	cmd.Env = append(os.Environ(), "TP_STATS_BAD_CFG=1", "TP_STATS_BAD_CFG_FILE="+badPath)
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_statsUsageExits1(t *testing.T) {
	if os.Getenv("TP_SUB_STATS_USAGE") == "1" {
		handleSubcommand([]string{"stats"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_statsUsageExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_STATS_USAGE=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestHandleSubcommand_statsUnknownExits1(t *testing.T) {
	if os.Getenv("TP_SUB_STATS_BAD") == "1" {
		handleSubcommand([]string{"stats", "not-a-period"})
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHandleSubcommand_statsUnknownExits1")
	cmd.Env = append(os.Environ(), "TP_SUB_STATS_BAD=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 {
		t.Fatalf("want exit 1, got err=%v", err)
	}
}

func TestPrintStatsTable_smoke(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printStatsTable([]analytics.AnalyticsSnapshot{
		{
			SessionStart:      time.Now(),
			TotalRequests:     3,
			TotalInputTokens:  1000,
			SavedInputTokens:  100,
			TotalOutputTokens: 50,
			CacheHits:         1,
			SecretsRedacted:   0,
		},
	})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "Slimference Stats") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestHandleSubcommand_statsIncludesWSSDecisionSavings(t *testing.T) {
	logDir := t.TempDir()
	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WriteSnapshot(analytics.AnalyticsSnapshot{
		SessionStart:      time.Now().Add(-time.Minute),
		TotalRequests:     1,
		TotalInputTokens:  1000,
		SavedInputTokens:  10,
		TotalOutputTokens: 25,
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()

	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "wss-saved",
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		RouteMode: "websocket_phasef",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    600,
			Saved:    400,
		},
	})
	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "http-already-in-analytics",
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		RouteMode: "http",
		Tokens: dbg.TokenCounts{
			Original: 1000,
			Final:    100,
			Saved:    900,
		},
	})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"stats", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if !strings.Contains(out, "Input tokens (orig): 2.0K") {
		t.Fatalf("missing combined original input: %q", out)
	}
	if !strings.Contains(out, "Input tokens saved:  410 (20%)") {
		t.Fatalf("missing combined savings: %q", out)
	}
	if !strings.Contains(out, "WSS decision saved:  400 (40%, 1 req)") {
		t.Fatalf("missing WSS decision savings: %q", out)
	}
	if strings.Contains(out, "1.3K") {
		t.Fatalf("HTTP decision savings should not be double-counted: %q", out)
	}
}

func TestHandleSubcommand_statsWSSDecisionSavingsWithoutAnalytics(t *testing.T) {
	logDir := t.TempDir()
	decisionsPath := filepath.Join(t.TempDir(), "decisions.jsonl")
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))
	t.Setenv("SLIMFERENCE_DEBUG_DECISIONS_LOG", decisionsPath)

	writeDecisionSummary(t, decisionsPath, dbg.RequestSummary{
		RequestID: "wss-only",
		Timestamp: time.Now(),
		Source:    "proxy",
		Provider:  "codex_chatgpt",
		RouteMode: "websocket_phasef",
		Tokens: dbg.TokenCounts{
			Original: 2000,
			Final:    1000,
			Saved:    1000,
		},
	})

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"stats", "today"})
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	if strings.Contains(out, "No stats") {
		t.Fatalf("WSS-only decision stats should print a report: %q", out)
	}
	if !strings.Contains(out, "Input tokens saved:  1.0K (50%)") ||
		!strings.Contains(out, "WSS decision saved:  1.0K (50%, 1 req)") {
		t.Fatalf("missing WSS-only savings: %q", out)
	}
}

func TestPrintStatsTable_empty(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	printStatsTable(nil)
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if buf.Len() != 0 {
		t.Fatalf("expected no output for empty snapshots, got %q", buf.String())
	}
}
