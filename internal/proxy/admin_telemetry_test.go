package proxy

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/repetition"
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
	t.Parallel()
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
