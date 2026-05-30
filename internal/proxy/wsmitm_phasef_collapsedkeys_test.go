package proxy

import "testing"

// TestWSPhaseF_CollapsedKeysPersistenceSurvivesReconnect proves the scan/read
// re-read recovery survives a WSS socket reconnect: a collapsed read key learned
// on one socket is rehydrated on a fresh adapter, so a re-read of the same key is
// still recognized as a post-collapse re-read and full-passes (recovers the
// elided bodies) instead of being scan-compacted again.
func TestWSPhaseF_CollapsedKeysPersistenceSurvivesReconnect(t *testing.T) {
	home := t.TempDir()
	orig := proxyUserHomeDir
	proxyUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { proxyUserHomeDir = orig })

	const sid = "codex-wss:reconnect-collapsed-1"
	const key = "read:cat /tmp/a.go"

	// Adapter 1: a fresh socket collapses a read and persists the key.
	a1 := &wsPhaseFAdapter{}
	a1.hydrateToolUses(sid) // sets sessionID
	a1.rememberCollapsedReadKeys([]string{key})

	// Adapter 2: a reconnect (new socket, empty in-memory set) rehydrates, so the
	// same key is recognized as a post-collapse re-read.
	a2 := &wsPhaseFAdapter{}
	a2.hydrateToolUses(sid)
	if got := a2.restoreKeysForReReads(map[string]struct{}{key: {}}); got == nil {
		t.Fatalf("reconnect did not rehydrate the collapsed key for re-read recovery")
	} else if _, ok := got[key]; !ok {
		t.Fatalf("rehydrated set missing the collapsed key: %+v", got)
	}

	// A different session must not see it (per-session isolation).
	a3 := &wsPhaseFAdapter{}
	a3.hydrateToolUses("codex-wss:other-collapsed")
	if got := a3.restoreKeysForReReads(map[string]struct{}{key: {}}); len(got) != 0 {
		t.Fatalf("different session leaked a collapsed key: %+v", got)
	}
}
