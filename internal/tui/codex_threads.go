package tui

import (
	"sync"
	"time"

	"github.com/slimference/slimference/internal/codexthreads"
	dbg "github.com/slimference/slimference/internal/debug"
)

type codexThreadMetadata = codexthreads.Metadata

var (
	loadCodexThreadMetadataFunc  = loadCodexThreadMetadata
	codexThreadMetadataCacheTTL  = 2 * time.Second
	codexThreadMetadataCacheMu   sync.Mutex
	codexThreadMetadataCacheAt   time.Time
	codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
)

func lookupCodexThreadMetadataForFlights(flights []dbg.FlightRequestSummary) map[string]codexThreadMetadata {
	ids := make([]string, 0, len(flights))
	for _, flight := range flights {
		if id := normalizeCodexSessionID(flight.SessionID); id != "" {
			ids = append(ids, id)
		}
	}
	return lookupCodexThreadMetadata(ids)
}

func lookupCodexThreadMetadata(sessionIDs []string) map[string]codexThreadMetadata {
	unique := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		id := normalizeCodexSessionID(sessionID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return map[string]codexThreadMetadata{}
	}

	now := time.Now()
	codexThreadMetadataCacheMu.Lock()
	defer codexThreadMetadataCacheMu.Unlock()

	if now.Sub(codexThreadMetadataCacheAt) <= codexThreadMetadataCacheTTL {
		if out, ok := cachedCodexThreads(unique); ok {
			return out
		}
	}

	loaded, err := loadCodexThreadMetadataFunc(unique)
	if err == nil {
		if codexThreadMetadataCacheData == nil {
			codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
		}
		for id, meta := range loaded {
			codexThreadMetadataCacheData[id] = meta
		}
		codexThreadMetadataCacheAt = now
	}
	out, _ := cachedCodexThreads(unique)
	return out
}

func cachedCodexThreads(ids []string) (map[string]codexThreadMetadata, bool) {
	out := make(map[string]codexThreadMetadata, len(ids))
	for _, id := range ids {
		meta, ok := codexThreadMetadataCacheData[id]
		if !ok {
			return out, false
		}
		out[id] = meta
	}
	return out, true
}

func loadCodexThreadMetadata(sessionIDs []string) (map[string]codexThreadMetadata, error) {
	home, err := userHomeDirFn()
	if err != nil {
		return nil, err
	}
	return codexthreads.Lookup(home, sessionIDs)
}

func normalizeCodexSessionID(value string) string {
	return codexthreads.NormalizeSessionID(value)
}

func resetCodexThreadMetadataCacheForTest() {
	codexThreadMetadataCacheMu.Lock()
	defer codexThreadMetadataCacheMu.Unlock()
	codexThreadMetadataCacheAt = time.Time{}
	codexThreadMetadataCacheData = map[string]codexThreadMetadata{}
}
