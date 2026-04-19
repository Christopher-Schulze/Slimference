package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/types"
)

func TestParsePromptCacheStatsArgs(t *testing.T) {
	t.Parallel()

	flags, err := parsePromptCacheStatsArgs([]string{"week", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if flags.period != "week" || !flags.json || flags.csv {
		t.Fatalf("flags=%+v", flags)
	}
	if _, err := parsePromptCacheStatsArgs([]string{"all", "--json", "--csv"}); err == nil {
		t.Fatal("expected conflicting output flags error")
	}
	if _, err := parsePromptCacheStatsArgs([]string{"nope"}); err == nil {
		t.Fatal("expected invalid period error")
	}
}

func TestHandleSubcommand_statsPromptCache(t *testing.T) {
	logDir := t.TempDir()
	t.Setenv("SLIMFERENCE_CONFIG", writeTestAnalyticsConfigToml(t, logDir))

	p, err := analytics.NewPersister(logDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.WriteEvent(types.AnalyticsEvent{
		Type:              types.EventRequestProcessed,
		Timestamp:         time.Now(),
		CacheReadTokens:   150,
		CacheCreateTokens: 20,
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"stats", "prompt-cache", "today"})
	_ = w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := buf.String()
	for _, want := range []string{"Prompt cache stats", "Cache read tokens:", "150"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}
