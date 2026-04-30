package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/hooks"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/readcache"
	"github.com/slimference/slimference/internal/toolarchive"
	"github.com/slimference/slimference/internal/types"
)

func TestHandleCheckpointCmd_ErrorAndExtraPaths(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	origStderr := os.Stderr
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
		os.Stderr = origStderr
	}()

	t.Run("no args exits", func(t *testing.T) {
		code, exited := captureExit(func() { handleCheckpointCmd(nil) })
		if !exited || code != 1 {
			t.Fatalf("exit=%v code=%d", exited, code)
		}
	})

	t.Run("home error exits", func(t *testing.T) {
		osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
		code, exited := captureExit(func() { handleCheckpointCmd([]string{"list"}) })
		if !exited || code != 1 {
			t.Fatalf("exit=%v code=%d", exited, code)
		}
	})

	t.Run("list stats unknown restore", func(t *testing.T) {
		home := t.TempDir()
		osUserHomeDir = func() (string, error) { return home, nil }

		if _, err := checkpoints.Capture(checkpoints.DefaultDir(home), checkpoints.CaptureInput{Trigger: checkpoints.TriggerManual}); err != nil {
			t.Fatal(err)
		}

		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCheckpointCmd([]string{"list"})
		_ = w.Close()
		os.Stdout = origStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), "manual") {
			t.Fatalf("list output=%q", buf.String())
		}

		r, w, _ = os.Pipe()
		os.Stdout = w
		handleCheckpointCmd([]string{"stats"})
		_ = w.Close()
		os.Stdout = origStdout
		buf.Reset()
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), `"captures"`) {
			t.Fatalf("stats output=%q", buf.String())
		}

		code, exited := captureExit(func() { handleCheckpointCmd([]string{"restore", "missing"}) })
		if !exited || code != 1 {
			t.Fatalf("restore missing exit=%v code=%d", exited, code)
		}

		code, exited = captureExit(func() { handleCheckpointCmd([]string{"wat"}) })
		if !exited || code != 1 {
			t.Fatalf("unknown exit=%v code=%d", exited, code)
		}
	})
}

func TestHandleExpandCmd_ErrorPaths(t *testing.T) {
	origHome := osUserHomeDir
	defer func() { osUserHomeDir = origHome }()

	code, exited := captureExit(func() { handleExpandCmd(nil) })
	if !exited || code != 1 {
		t.Fatalf("usage exit=%v code=%d", exited, code)
	}

	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	code, exited = captureExit(func() { handleExpandCmd([]string{"id"}) })
	if !exited || code != 1 {
		t.Fatalf("home exit=%v code=%d", exited, code)
	}

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	code, exited = captureExit(func() { handleExpandCmd([]string{"missing"}) })
	if !exited || code != 1 {
		t.Fatalf("missing exit=%v code=%d", exited, code)
	}
}

func TestHandleCheckpointCmd_GenericErrorPaths(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	t.Run("capture generic error", func(t *testing.T) {
		home := t.TempDir()
		dir := checkpoints.DefaultDir(home)
		if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		osUserHomeDir = func() (string, error) { return home, nil }
		code, exited := captureExit(func() { handleCheckpointCmd([]string{"capture"}) })
		if !exited || code != 1 {
			t.Fatalf("capture exit=%v code=%d", exited, code)
		}
	})

	t.Run("list generic error", func(t *testing.T) {
		home := t.TempDir()
		dir := checkpoints.DefaultDir(home)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		osUserHomeDir = func() (string, error) { return home, nil }
		code, exited := captureExit(func() { handleCheckpointCmd([]string{"list"}) })
		if !exited || code != 1 {
			t.Fatalf("list exit=%v code=%d", exited, code)
		}
	})

	t.Run("restore generic error", func(t *testing.T) {
		home := t.TempDir()
		osUserHomeDir = func() (string, error) { return home, nil }
		cp, err := checkpoints.Capture(checkpoints.DefaultDir(home), checkpoints.CaptureInput{Trigger: checkpoints.TriggerManual})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(checkpoints.DefaultDir(home), "stats.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, exited := captureExit(func() { handleCheckpointCmd([]string{"restore", cp.ID}) })
		if !exited || code != 1 {
			t.Fatalf("restore exit=%v code=%d", exited, code)
		}
		if err := checkpoints.SaveStats(checkpoints.DefaultDir(home), checkpoints.Stats{}); err != nil {
			t.Fatal(err)
		}

		r, w, _ := os.Pipe()
		os.Stdout = w
		handleCheckpointCmd([]string{"restore"})
		_ = w.Close()
		os.Stdout = origStdout
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		if !strings.Contains(buf.String(), "Slimference checkpoint") {
			t.Fatalf("restore-best output=%q", buf.String())
		}
	})

	t.Run("stats generic error", func(t *testing.T) {
		home := t.TempDir()
		dir := checkpoints.DefaultDir(home)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stats.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		osUserHomeDir = func() (string, error) { return home, nil }
		code, exited := captureExit(func() { handleCheckpointCmd([]string{"stats"}) })
		if !exited || code != 1 {
			t.Fatalf("stats exit=%v code=%d", exited, code)
		}
	})
}

func TestHandleExpandCmd_GenericAndWriteErrors(t *testing.T) {
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	osUserHomeDir = func() (string, error) { return home, nil }
	entry, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "archive-err",
		SessionID: "sess-1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive err=%v entry=%+v", err, entry)
	}

	if err := os.WriteFile(filepath.Join(toolarchive.DefaultDir(home), "entries", entry.ID+".txt.gz"), []byte("not-gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, exited := captureExit(func() { handleExpandCmd([]string{entry.ID}) })
	if !exited || code != 1 {
		t.Fatalf("generic expand exit=%v code=%d", exited, code)
	}

	entry, err = toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "archive-write",
		SessionID: "sess-1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive err=%v entry=%+v", err, entry)
	}
	_, w, _ := os.Pipe()
	_ = w.Close()
	os.Stdout = w
	code, exited = captureExit(func() { handleExpandCmd([]string{entry.ID}) })
	os.Stdout = origStdout
	if !exited || code != 1 {
		t.Fatalf("write exit=%v code=%d", exited, code)
	}
}

func TestPromptCacheCLIAndAdapterCoverage(t *testing.T) {
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

	if _, err := parsePromptCacheStatsArgs([]string{"week", "month"}); err == nil {
		t.Fatal("expected extra arg error")
	}
	if _, err := parsePromptCacheStatsArgs([]string{"--bad"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	flags, err := parsePromptCacheStatsArgs([]string{""})
	if err != nil || flags.period != "today" {
		t.Fatalf("empty arg flags=%+v err=%v", flags, err)
	}

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	handlePromptCacheStatsCmd(logDir, []string{"today", "--json"})
	_ = w.Close()
	os.Stdout = oldStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), `"cache_read_tokens": 150`) {
		t.Fatalf("json output=%q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handlePromptCacheStatsCmd(logDir, []string{"today", "--csv"})
	_ = w.Close()
	os.Stdout = oldStdout
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "period,total_requests") {
		t.Fatalf("csv output=%q", buf.String())
	}

	r, w, _ = os.Pipe()
	os.Stdout = w
	handlePromptCacheStatsCmd(t.TempDir(), []string{"today"})
	_ = w.Close()
	os.Stdout = oldStdout
	buf.Reset()
	_, _ = io.Copy(&buf, r)
	if !strings.Contains(buf.String(), "No prompt-cache stats") {
		t.Fatalf("empty output=%q", buf.String())
	}

	code, exited := captureExit(func() { handlePromptCacheStatsCmd(logDir, []string{"--json", "--csv"}) })
	if !exited || code != 1 {
		t.Fatalf("invalid flags exit=%v code=%d", exited, code)
	}

	badLogDir := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(badLogDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, exited = captureExit(func() { handlePromptCacheStatsCmd(badLogDir, []string{"all"}) })
	if !exited || code != 1 {
		t.Fatalf("report error exit=%v code=%d", exited, code)
	}

	oldStdout = os.Stdout
	_, wClosed, _ := os.Pipe()
	_ = wClosed.Close()
	os.Stdout = wClosed
	code, exited = captureExit(func() { handlePromptCacheStatsCmd(logDir, []string{"today", "--csv"}) })
	os.Stdout = oldStdout
	if !exited || code != 1 {
		t.Fatalf("csv write exit=%v code=%d", exited, code)
	}

	origMarshal := promptCacheMarshalIndent
	promptCacheMarshalIndent = func(any, string, string) ([]byte, error) { return nil, errors.New("marshal") }
	defer func() { promptCacheMarshalIndent = origMarshal }()
	code, exited = captureExit(func() { handlePromptCacheStatsCmd(logDir, []string{"today", "--json"}) })
	if !exited || code != 1 {
		t.Fatalf("json marshal exit=%v code=%d", exited, code)
	}

}

func TestLocalAndRemoteAdapterCheckpointArchiveStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if _, err := checkpoints.Capture(checkpoints.DefaultDir(home), checkpoints.CaptureInput{Trigger: checkpoints.TriggerManual}); err != nil {
		t.Fatal(err)
	}
	if _, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "tool-1",
		SessionID: "sess-1",
		Command:   "npm test",
		Output:    strings.Repeat("line\n", 800),
	}); err != nil {
		t.Fatal(err)
	}
	if err := readcache.RecordDecision(readcache.DefaultDir(home), readcache.Decision{Type: readcache.DecisionBlock, BlockKind: readcache.BlockKindDelta}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	pa := &proxyAdapter{p: proxy.New(cfg)}
	if got := pa.GetReadCacheStatus(); got.Blocks != 1 || got.DeltaBlocks != 1 {
		t.Fatalf("read status=%+v", got)
	}
	if got := pa.GetCheckpointStatus(); got.Count != 1 || got.Captures != 1 {
		t.Fatalf("checkpoint status=%+v", got)
	}
	if got := pa.GetToolArchiveStatus(); got.Count != 1 || got.Archived != 1 {
		t.Fatalf("archive status=%+v", got)
	}

	rpa := newRemoteProxyAdapter(cfg)
	rpa.mu.Lock()
	rpa.status.Checkpoints = proxy.AdminCheckpointStatus{Count: 4, Captures: 5, Restores: 6, Bytes: 7}
	rpa.status.ToolArchive = proxy.AdminToolArchiveStatus{Count: 8, Archived: 9, Expanded: 10, BytesRaw: 11, BytesStored: 12}
	rpa.lastRefresh = time.Now()
	rpa.mu.Unlock()

	if got := rpa.GetCheckpointStatus(); got.Count != 4 || got.Restores != 6 {
		t.Fatalf("remote checkpoint status=%+v", got)
	}
	if got := rpa.GetToolArchiveStatus(); got.Count != 8 || got.Expanded != 10 {
		t.Fatalf("remote archive status=%+v", got)
	}

	// T77 quality adapters: proxyAdapter pulls from QualitySnapshot,
	// remoteProxyAdapter mirrors the admin quality block.
	if got := pa.GetQualityStatus(); got.NetSaved != 0 || got.SpikeActive {
		t.Fatalf("default quality status=%+v", got)
	}
	rpa.mu.Lock()
	rpa.status.Quality.ReRead.Sessions = 5
	rpa.status.Quality.ReRead.TotalChecks = 100
	rpa.status.Quality.ReRead.TotalHits = 7
	rpa.status.Quality.ReRead.Rate = 0.07
	rpa.status.Quality.CacheMissSpike.Active = true
	rpa.status.Quality.CacheMissSpike.LastSpikeUnix = 1234
	rpa.status.Quality.CacheMissSpike.TotalSpikeCount = 9
	rpa.status.Quality.CacheMissSpike.BaselineRate = 0.8
	rpa.status.Quality.NetSavings.TotalSaved = 1000
	rpa.status.Quality.NetSavings.TotalInvalidation = 200
	rpa.status.Quality.NetSavings.NetSaved = 800
	rpa.lastRefresh = time.Now()
	rpa.mu.Unlock()
	got := rpa.GetQualityStatus()
	if got.ReReadSessions != 5 || got.ReReadTotalChecks != 100 || got.ReReadTotalHits != 7 {
		t.Fatalf("remote quality reread=%+v", got)
	}
	if !got.SpikeActive || got.LastSpikeUnix != 1234 || got.TotalSpikeCount != 9 {
		t.Fatalf("remote quality spike=%+v", got)
	}
	if got.NetSaved != 800 || got.TotalSaved != 1000 || got.TotalInvalidation != 200 {
		t.Fatalf("remote quality net=%+v", got)
	}
}

func TestHandleReadHookCmd_ArgAndEncodeEdges(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	file := filepath.Join(home, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	termIsTerminalFn = func(int) bool { return false }
	osUserHomeDir = func() (string, error) { return home, nil }
	payload := []byte(`{"session_id":"s1","tool_input":{"file_path":"` + file + `"}}`)
	readStdinAll = func() ([]byte, error) { return payload, nil }
	handleReadHookCmd([]string{"", "--", "claude"})
	readStdinAll = func() ([]byte, error) { return payload, nil }

	_, w, _ := os.Pipe()
	_ = w.Close()
	os.Stdout = w
	code, exited := captureExit(func() { handleReadHookCmd([]string{"", "--", "claude"}) })
	os.Stdout = origStdout
	if !exited || code != 1 {
		t.Fatalf("readhook encode exit=%v code=%d", exited, code)
	}

	badHome := t.TempDir()
	readCacheDir := filepath.Join(badHome, ".slimference")
	if err := os.MkdirAll(readCacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(readCacheDir, "read-cache"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	osUserHomeDir = func() (string, error) { return badHome, nil }
	readStdinAll = func() ([]byte, error) {
		return []byte(`{"session_id":"s2","tool_input":{"file_path":"` + file + `"}}`), nil
	}
	code, exited = captureExit(func() { handleReadHookCmd([]string{"claude"}) })
	if !exited || code != 1 {
		t.Fatalf("readhook evaluate exit=%v code=%d", exited, code)
	}
}

func TestHandleSubcommandAndPostToolEncodeCoverage(t *testing.T) {
	origTerm := termIsTerminalFn
	origRead := readStdinAll
	origHome := osUserHomeDir
	origStdout := os.Stdout
	defer func() {
		termIsTerminalFn = origTerm
		readStdinAll = origRead
		osUserHomeDir = origHome
		os.Stdout = origStdout
	}()

	home := t.TempDir()
	file := filepath.Join(home, "main.go")
	if err := os.WriteFile(file, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	termIsTerminalFn = func(int) bool { return false }
	osUserHomeDir = func() (string, error) { return home, nil }
	_ = newRemoteProxyFn(config.Defaults())
	readStdinAll = func() ([]byte, error) {
		return []byte(`{"session_id":"s1","tool_input":{"file_path":"` + file + `"}}`), nil
	}
	handleSubcommand([]string{"readhook", "claude"})

	if _, err := checkpoints.Capture(checkpoints.DefaultDir(home), checkpoints.CaptureInput{Trigger: checkpoints.TriggerManual}); err != nil {
		t.Fatal(err)
	}
	r, w, _ := os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"checkpoint", "list"})
	_ = w.Close()
	os.Stdout = origStdout
	_ = r.Close()

	entry, err := toolarchive.Archive(toolarchive.DefaultDir(home), toolarchive.Input{
		ToolName:  "Bash",
		ToolUseID: "dispatch-expand",
		SessionID: "sess",
		Command:   "cmd",
		Output:    strings.Repeat("line\n", 700),
	})
	if err != nil || entry == nil {
		t.Fatalf("archive err=%v entry=%+v", err, entry)
	}
	r, w, _ = os.Pipe()
	os.Stdout = w
	handleSubcommand([]string{"expand", entry.ID})
	_ = w.Close()
	os.Stdout = origStdout
	_ = r.Close()

	cfg := config.Defaults()
	cfg.Filter.PassthroughMaxChars = 10
	configLoadFn = func() (*config.Config, error) { return cfg, nil }
	defer func() { configLoadFn = config.Load }()
	payload, err := json.Marshal(map[string]string{
		"session_id":    "sess-1",
		"tool_name":     "Bash",
		"tool_use_id":   "tool-posttool-encode",
		"command":       "npm test",
		"tool_response": strings.Repeat("line\n", 800),
	})
	if err != nil {
		t.Fatal(err)
	}
	readStdinAll = func() ([]byte, error) { return payload, nil }
	_, closedW, _ := os.Pipe()
	_ = closedW.Close()
	os.Stdout = closedW
	code, exited := captureExit(func() { handlePostToolCmd(nil) })
	os.Stdout = origStdout
	if !exited || code != 1 {
		t.Fatalf("posttool encode exit=%v code=%d", exited, code)
	}
}

func TestHandleHookCmd_CheckUpstreamBranch(t *testing.T) {
	origHome := osUserHomeDir
	origDetect := hookDetectDriftFn
	defer func() {
		osUserHomeDir = origHome
		hookDetectDriftFn = origDetect
	}()

	osUserHomeDir = func() (string, error) { return t.TempDir(), nil }
	hookDetectDriftFn = func(_ context.Context) []hooks.DriftReport {
		return []hooks.DriftReport{
			{
				CLI:           "claude",
				BinaryFound:   true,
				VersionRaw:    "3.9.9",
				VersionParsed: "3.9.9",
				MinSupported:  "1.0.0",
				MaxTested:     "3.0.0",
				Status:        hooks.DriftAbove,
				Notes:         "drift",
			},
		}
	}
	code, exited := captureExit(func() { handleHookCmd([]string{"check-upstream"}) })
	if !exited || code != 1 {
		t.Fatalf("check-upstream exit=%v code=%d", exited, code)
	}
}
