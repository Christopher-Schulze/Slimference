package main

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunWSSProofLiveRowAppendsStatusMergedCounters(t *testing.T) {
	dir := t.TempDir()
	framesPath := filepath.Join(dir, "frames.jsonl")
	if err := os.WriteFile(framesPath, []byte(`{"direction":"c2s"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	matrixPath := filepath.Join(dir, "matrix.jsonl")
	mux := http.NewServeMux()
	mux.HandleFunc("/_slimference/admin/state", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "savings": {},
		  "wss": {"phasef_mutations": 1, "compressed_messages_mutated": 1},
		  "host_budget": {"status":"ok","exceeded":false,"compression_ok":true,"degradation_ok":true}
		}`))
	})
	mux.HandleFunc("/_slimference/admin/status", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
		  "tool_prune": {
		    "pruned_total": 1,
		    "always_keep_total": 12,
		    "tokens_saved_sum": 26
		  }
		}`))
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux}
	defer server.Close()
	go func() {
		_ = server.Serve(listener)
	}()
	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := runWSSProofLiveRow([]string{
		"--matrix-row", matrixPath,
		"--frames", framesPath,
		"--id", "desktop-tool-heavy",
		"--client", "desktop",
		"--workload-class", "tool_heavy",
		"--expected-reducer", "tool_prune",
		"--expected-reducer", "tool_prune_tokens_saved",
		"--expected-reducer", "host_budget_ok",
		"--host", host,
		"--port", port,
		"--model", "gpt-5.5",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "tool_prune_tokens_saved: 26") {
		t.Fatalf("summary missing tool-prune counter: %s", stdout.String())
	}
	rows, err := readWSSProofMatrixRecords(matrixPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "desktop-tool-heavy" || rows[0].Client != "desktop" ||
		rows[0].WorkloadClass != "tool_heavy" || rows[0].FramesPath != framesPath {
		t.Fatalf("bad row: %+v", rows)
	}
	if rows[0].LiveDelta == nil || rows[0].LiveDelta.ToolPrunePruned != 1 ||
		rows[0].LiveDelta.ToolPruneTokensSaved != 26 ||
		rows[0].LiveDelta.HostBudgetStatus != "ok" {
		t.Fatalf("bad live delta: %+v", rows[0].LiveDelta)
	}
}

func TestParseWSSProofLiveRowFlagsRejectsMissingFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWSSProofLiveRow([]string{"--frames", "/missing"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--matrix-row is required") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
