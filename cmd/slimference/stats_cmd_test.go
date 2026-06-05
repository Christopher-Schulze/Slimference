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

	"github.com/slimference/slimference/internal/analytics"
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
