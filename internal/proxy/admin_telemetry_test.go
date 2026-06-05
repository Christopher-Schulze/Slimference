package proxy

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/filter"
	"github.com/slimference/slimference/internal/repetition"
	"github.com/slimference/slimference/internal/types"
)

func TestAdminStatusSnapshot_RepetitionPopulated(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Seed the repetition DB so adminStatusSnapshot's openRepetitionDB
	// path returns a valid handle.
	db, err := repetition.Open(repetition.DefaultPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repetition.Record(db, repetition.Key{
		SessionID: "s",
		ToolName:  "Bash",
		Output:    "long enough payload to be eligible content",
	}, 1); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	p := New(config.Defaults())
	snap := p.adminStatusSnapshot()
	if snap.Repetition.Rows != 1 {
		t.Fatalf("rows: %+v", snap.Repetition)
	}
}

func TestAdminStatusSnapshot_NilToolPruneAndServerState(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.toolPrune = nil
	p.serverState = nil
	snap := p.adminStatusSnapshot()
	if snap.ToolPrune.Sessions != 0 {
		t.Fatalf("nil toolPrune must zero: %+v", snap.ToolPrune)
	}
	if snap.ServerState.Sessions != 0 {
		t.Fatalf("nil serverState must zero: %+v", snap.ServerState)
	}
}

func TestAdminStatusSnapshot_NewTelemetryBlocks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := New(config.Defaults())
	snap := p.adminStatusSnapshot()
	// All new blocks must be present and zero-valued by default.
	if snap.Repetition.Rows != 0 {
		t.Fatalf("repetition.rows: %d", snap.Repetition.Rows)
	}
	if snap.ToolPrune.Sessions != 0 {
		t.Fatalf("tool_prune.sessions: %d", snap.ToolPrune.Sessions)
	}
	if snap.ServerState.Sessions != 0 {
		t.Fatalf("server_state.sessions: %d", snap.ServerState.Sessions)
	}
	if snap.BypassDetail.Enabled {
		t.Fatal("bypass_detail.enabled must default false")
	}
	filter.GlobalFilterObservability().Record(filter.FilterStats{
		Name:     "proxy_admin_test_filter",
		Elapsed:  time.Millisecond,
		Matched:  true,
		InBytes:  100,
		OutBytes: 40,
	})
	snap = p.adminStatusSnapshot()
	if got := snap.Layer0["proxy_admin_test_filter"]; got.Attempts == 0 || got.BytesSaved != 60 {
		t.Fatalf("layer0 telemetry not surfaced: %+v", got)
	}
}

// TestAdminStatusSnapshot_Layer2RedactionWired covers T109 surface:
// the Redaction block must always be present in the snapshot, mode
// reflects the configured value, and counters start at zero.
func TestAdminStatusSnapshot_Layer2RedactionWired(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	snap := p.adminStatusSnapshot()
	if snap.Layer2.Redaction.Mode == "" {
		t.Fatalf("expected mode to be set on default config, got empty")
	}
	if snap.Layer2.Redaction.Secrets != 0 ||
		snap.Layer2.Redaction.Paths != 0 ||
		snap.Layer2.Redaction.Headers != 0 ||
		snap.Layer2.Redaction.JSONKeys != 0 ||
		snap.Layer2.Redaction.Inputs != 0 {
		t.Fatalf("expected zero counters, got %+v", snap.Layer2.Redaction)
	}
}

// TestAdminStatusSnapshot_Layer2RedactionNilLayer2 covers the
// defensive zero-value path when the Layer2 receiver is nil.
func TestAdminStatusSnapshot_Layer2RedactionNilLayer2(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	p.layer2 = nil
	snap := p.adminStatusSnapshot()
	if snap.Layer2.Redaction.Mode != "" {
		t.Fatalf("expected empty mode for nil layer2, got %q", snap.Layer2.Redaction.Mode)
	}
}

func TestBypassExpiresUnix(t *testing.T) {
	t.Parallel()
	p := New(config.Defaults())
	if got := bypassExpiresUnix(p); got != 0 {
		t.Fatalf("default: %d", got)
	}
	p.SetBypassFor(time.Hour)
	if got := bypassExpiresUnix(p); got <= time.Now().Unix() {
		t.Fatalf("expected non-zero deadline, got %d", got)
	}
}

func TestOpenRepetitionDB_AbsentReturnsNil(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := openRepetitionDB(dir)
	if err != nil || db != nil {
		t.Fatalf("absent file: db=%v err=%v", db, err)
	}
}

func TestOpenRepetitionDB_PresentRoundTrip(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	// Seed the DB by opening it directly so the file exists.
	db, err := repetition.Open(repetition.DefaultPath(home))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := repetition.Record(db, repetition.Key{
		SessionID: "s",
		ToolName:  "x",
		Output:    "y-content",
	}, 1); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	got, err := openRepetitionDB(home)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected non-nil db")
	}
	defer got.Close()
	stats, err := repetitionSnapshot(got)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != 1 {
		t.Fatalf("rows: %+v", stats)
	}
}

func TestRepetitionSnapshot_EmptyDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	db, err := repetition.Open(filepath.Join(dir, "rep.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	stats, err := repetitionSnapshot(db)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Rows != 0 {
		t.Fatalf("empty rows: %d", stats.Rows)
	}
}

func TestExtractUsedToolNames(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "Read"},
			{Type: "text", Text: "hello"},
		}},
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "Bash"},
		}},
		{Content: []types.ContentBlock{
			{Type: "tool_use", ToolName: "Read"},
		}},
	}
	names := extractUsedToolNames(msgs)
	if len(names) != 2 || names[0] != "Read" || names[1] != "Bash" {
		t.Fatalf("got %v", names)
	}
}

func TestExtractUsedToolNamesWithResolvedToolResults(t *testing.T) {
	t.Parallel()
	msgs := []types.Message{
		{Content: []types.ContentBlock{
			{Type: "tool_result", ToolResultID: "call-1", Text: "ok"},
			{Type: "tool_result", ToolUseID: "call-2", Text: "ok"},
			{Type: "tool_result", ToolResultID: "missing", Text: "ok"},
			{Type: "tool_use", ToolName: "Read"},
		}},
	}
	remembered := map[string]types.ContentBlock{
		"call-1": {Type: "tool_use", ToolUseID: "call-1", ToolName: "exec_command"},
		"call-2": {Type: "tool_use", ToolUseID: "call-2", ToolName: "apply_patch"},
	}
	names := extractUsedToolNamesWithResolved(msgs, remembered)
	if len(names) != 3 || names[0] != "exec_command" || names[1] != "apply_patch" || names[2] != "Read" {
		t.Fatalf("got %v", names)
	}
}

func TestExtractUsedToolNames_Empty(t *testing.T) {
	t.Parallel()
	names := extractUsedToolNames(nil)
	if len(names) != 0 {
		t.Fatalf("expected empty, got %v", names)
	}
	names = extractUsedToolNames([]types.Message{})
	if len(names) != 0 {
		t.Fatalf("expected empty, got %v", names)
	}
}
