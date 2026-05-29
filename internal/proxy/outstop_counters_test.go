package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/slimference/slimference/internal/config"
)

func TestOutputReduceCountersStopSeqInjection(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordStopSeqInjection(4)
	c.RecordStopSeqInjection(2)
	c.RecordStopSeqInjection(0)  // no-op
	c.RecordStopSeqInjection(-1) // no-op
	s := c.Snapshot()
	if s.StopSeqRequestsModified != 2 {
		t.Errorf("requests modified = %d, want 2", s.StopSeqRequestsModified)
	}
	if s.StopSeqPhrasesAdded != 6 {
		t.Errorf("phrases added = %d, want 6", s.StopSeqPhrasesAdded)
	}
}

func TestOutputReduceCountersStreamcutFire(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordStreamcutFire(1000)
	c.RecordStreamcutFire(500)
	c.RecordStreamcutFire(0) // counted but no bytes added
	s := c.Snapshot()
	if s.StreamcutFired != 3 {
		t.Errorf("fired = %d, want 3", s.StreamcutFired)
	}
	if s.StreamcutBytesObserved != 1500 {
		t.Errorf("bytes observed = %d, want 1500", s.StreamcutBytesObserved)
	}
}

func TestOutputReduceCountersStaleReadAging(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordStaleReadAging(3, 250)
	c.RecordStaleReadAging(1, 0)   // counted but no bytes
	c.RecordStaleReadAging(0, 100) // no-op
	c.RecordStaleReadAging(-1, 99) // no-op
	s := c.Snapshot()
	if s.StaleReadBlocksReplaced != 4 {
		t.Errorf("blocks=%d want 4", s.StaleReadBlocksReplaced)
	}
	if s.StaleReadBytesReplaced != 250 {
		t.Errorf("bytes=%d want 250", s.StaleReadBytesReplaced)
	}
}

func TestOutputReduceCountersObsoleteReadPrune(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordObsoleteReadPrune(5, 400)
	c.RecordObsoleteReadPrune(2, 0)   // counted, no bytes added
	c.RecordObsoleteReadPrune(0, 100) // no-op
	c.RecordObsoleteReadPrune(-1, 50) // no-op
	s := c.Snapshot()
	if s.ObsoleteReadBlocksPruned != 7 {
		t.Errorf("blocks=%d want 7", s.ObsoleteReadBlocksPruned)
	}
	if s.ObsoleteReadBytesPruned != 400 {
		t.Errorf("bytes=%d want 400", s.ObsoleteReadBytesPruned)
	}
}

func TestOutputReduceCountersBeTerseHint(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordBeTerseInjection(80)
	c.RecordBeTerseInjection(0) // counted, no bytes
	s := c.Snapshot()
	if s.BeterseInjections != 2 {
		t.Errorf("injections=%d want 2", s.BeterseInjections)
	}
	if s.BeterseHintBytes != 80 {
		t.Errorf("bytes=%d want 80", s.BeterseHintBytes)
	}
}

func TestOutputReduceCountersProxyLayer0(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordProxyLayer0(128)
	c.RecordProxyLayer0Stats(proxyLayer0Stats{
		ToolResultBlocks:        4,
		ToolUseUnresolvedBlocks: 1,
		CommandResolvedBlocks:   3,
		CommandUnresolvedBlocks: 1,
		ReadDeltaAttempts:       2,
		ReadDeltaMisses:         1,
		TokensSaved:             256,
		BlocksModified:          3,
		ReadDeltaBlocks:         2,
		CapturedOutputBlocks:    1,
		CodexExecEnvelopeBlocks: 1,
	})
	c.RecordProxyLayer0(0)
	c.RecordProxyLayer0(-1)
	c.RecordProxyLayer0Stats(proxyLayer0Stats{TokensSaved: 0, BlocksModified: 9})
	s := c.Snapshot()
	if s.ProxyLayer0RequestsModified != 2 {
		t.Errorf("proxy layer0 requests=%d want 2", s.ProxyLayer0RequestsModified)
	}
	if s.ProxyLayer0TokensSaved != 384 {
		t.Errorf("proxy layer0 saved=%d want 384", s.ProxyLayer0TokensSaved)
	}
	if s.ProxyLayer0ToolResultBlocks != 4 {
		t.Errorf("proxy layer0 tool-result blocks=%d want 4", s.ProxyLayer0ToolResultBlocks)
	}
	if s.ProxyLayer0ToolUseUnresolved != 1 {
		t.Errorf("proxy layer0 unresolved tool-use blocks=%d want 1", s.ProxyLayer0ToolUseUnresolved)
	}
	if s.ProxyLayer0CommandResolvedBlocks != 3 {
		t.Errorf("proxy layer0 command blocks=%d want 3", s.ProxyLayer0CommandResolvedBlocks)
	}
	if s.ProxyLayer0CommandUnresolved != 1 {
		t.Errorf("proxy layer0 unresolved command blocks=%d want 1", s.ProxyLayer0CommandUnresolved)
	}
	if s.ProxyLayer0ReadDeltaAttempts != 2 {
		t.Errorf("proxy layer0 read attempts=%d want 2", s.ProxyLayer0ReadDeltaAttempts)
	}
	if s.ProxyLayer0ReadDeltaMisses != 1 {
		t.Errorf("proxy layer0 read misses=%d want 1", s.ProxyLayer0ReadDeltaMisses)
	}
	if s.ProxyLayer0BlocksModified != 4 {
		t.Errorf("proxy layer0 blocks=%d want 4", s.ProxyLayer0BlocksModified)
	}
	if s.ProxyLayer0ReadDeltaBlocks != 2 {
		t.Errorf("proxy layer0 read-delta blocks=%d want 2", s.ProxyLayer0ReadDeltaBlocks)
	}
	if s.ProxyLayer0CapturedBlocks != 1 {
		t.Errorf("proxy layer0 captured blocks=%d want 1", s.ProxyLayer0CapturedBlocks)
	}
	if s.ProxyLayer0EnvelopeBlocks != 1 {
		t.Errorf("proxy layer0 envelope blocks=%d want 1", s.ProxyLayer0EnvelopeBlocks)
	}
}

func TestOutputReduceCountersProxyLayer0Routes(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordProxyLayer0Stats(proxyLayer0Stats{
		Route:                   codexLayer0RouteWSSPhaseF,
		ToolResultBlocks:        4,
		ToolUseUnresolvedBlocks: 1,
		CommandResolvedBlocks:   3,
		CommandUnresolvedBlocks: 1,
		ReadDeltaAttempts:       2,
		ReadDeltaMisses:         1,
		TokensSaved:             256,
		BlocksModified:          3,
		ReadDeltaBlocks:         2,
		CapturedOutputBlocks:    1,
		CodexExecEnvelopeBlocks: 1,
	})
	c.RecordProxyLayer0Stats(proxyLayer0Stats{
		Route:                   codexLayer0RouteHTTP,
		ToolResultBlocks:        2,
		ToolUseUnresolvedBlocks: 2,
		CommandUnresolvedBlocks: 2,
		ReadDeltaAttempts:       1,
		ReadDeltaMisses:         1,
	})
	s := c.Snapshot()
	if s.ProxyLayer0Routes.WSSPhaseF.TokensSaved != 256 ||
		s.ProxyLayer0Routes.WSSPhaseF.BlocksModified != 3 ||
		s.ProxyLayer0Routes.WSSPhaseF.ReadDeltaBlocks != 2 {
		t.Fatalf("wss route counters mismatch: %+v", s.ProxyLayer0Routes.WSSPhaseF)
	}
	if s.ProxyLayer0Routes.HTTP.ToolResultBlocks != 2 ||
		s.ProxyLayer0Routes.HTTP.ToolUseUnresolved != 2 ||
		s.ProxyLayer0Routes.HTTP.CommandUnresolved != 2 ||
		s.ProxyLayer0Routes.HTTP.ReadDeltaMisses != 1 ||
		s.ProxyLayer0Routes.HTTP.TokensSaved != 0 {
		t.Fatalf("http route counters mismatch: %+v", s.ProxyLayer0Routes.HTTP)
	}
}

func TestOutputReduceCountersRepdetRewrite(t *testing.T) {
	c := &OutputReduceCounters{}
	c.RecordRepdetRewrite(2, 400)
	c.RecordRepdetRewrite(1, 200)
	c.RecordRepdetRewrite(0, 100) // no-op: zero matches
	c.RecordRepdetRewrite(1, 0)   // no-op: zero saved
	s := c.Snapshot()
	if s.RepdetResponsesRewritten != 2 {
		t.Errorf("responses = %d, want 2", s.RepdetResponsesRewritten)
	}
	if s.RepdetMatchesRewritten != 3 {
		t.Errorf("matches = %d, want 3", s.RepdetMatchesRewritten)
	}
	if s.RepdetBytesSaved != 600 {
		t.Errorf("bytes saved = %d, want 600", s.RepdetBytesSaved)
	}
}

func TestOutputReduceCountersConcurrent(t *testing.T) {
	// Race detector smoke: spawn N goroutines each hitting all three
	// recorders. Snapshot should reflect the total.
	c := &OutputReduceCounters{}
	const goroutines = 32
	const perG = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				c.RecordStopSeqInjection(1)
				c.RecordStreamcutFire(10)
				c.RecordRepdetRewrite(1, 5)
			}
		}()
	}
	wg.Wait()
	s := c.Snapshot()
	if s.StopSeqRequestsModified != goroutines*perG {
		t.Errorf("stop_seq_requests = %d, want %d", s.StopSeqRequestsModified, goroutines*perG)
	}
	if s.StreamcutFired != goroutines*perG {
		t.Errorf("streamcut_fired = %d, want %d", s.StreamcutFired, goroutines*perG)
	}
	if s.RepdetBytesSaved != uint64(goroutines)*uint64(perG)*5 {
		t.Errorf("repdet_bytes_saved = %d, want %d", s.RepdetBytesSaved, goroutines*perG*5)
	}
}

// TestAdminStatusExposesOutputReduceCounters wires the full proxy and
// asserts the /admin/status JSON includes the new counters block.
func TestAdminStatusExposesOutputReduceCounters(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"x","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer upstream.Close()

	cfg := config.Defaults()
	cfg.Upstream.Anthropic.BaseURL = upstream.URL
	cfg.Compression.Layer1Enabled = false
	cfg.Compression.Layer2Enabled = false
	cfg.Compression.OutputReduce.StopSequencesEnabled = true
	p := New(cfg)

	// Fire one stop-seq injection through a real request.
	req := httptest.NewRequest(http.MethodPost, "/v1/messages",
		strings.NewReader(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("x-api-key", "test")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}

	// Snapshot via the same accessor admin handler uses.
	snap := p.outputReduceCounters.Snapshot()
	if snap.StopSeqRequestsModified == 0 {
		t.Errorf("expected stop_seq_requests_modified > 0 after one request")
	}

	// Verify telemetry shape round-trips through JSON marshalling.
	out, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"stop_seq_requests_modified"`) {
		t.Errorf("JSON missing stop_seq_requests_modified key: %s", out)
	}
	if !strings.Contains(string(out), `"streamcut_fired"`) {
		t.Errorf("JSON missing streamcut_fired key: %s", out)
	}
	if !strings.Contains(string(out), `"repdet_responses_rewritten"`) {
		t.Errorf("JSON missing repdet_responses_rewritten key: %s", out)
	}
}
