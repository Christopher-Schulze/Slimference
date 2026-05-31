package proxy

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/slimference/slimference/internal/analytics"
	"github.com/slimference/slimference/internal/savingspolicy"
)

type proxyLayer0RouteCounters struct {
	toolResultBlocks      atomic.Uint64
	toolUseUnresolved     atomic.Uint64
	commandResolvedBlocks atomic.Uint64
	commandUnresolved     atomic.Uint64
	readDeltaAttempts     atomic.Uint64
	readDeltaMisses       atomic.Uint64
	requestsModified      atomic.Uint64
	tokensSaved           atomic.Uint64
	blocksModified        atomic.Uint64
	readDeltaBlocks       atomic.Uint64
	capturedBlocks        atomic.Uint64
	envelopeBlocks        atomic.Uint64
	repeatedOutputBlocks  atomic.Uint64
	chunkDedupBlocks      atomic.Uint64
	chunkDedupReferences  atomic.Uint64
	chunkDedupRefBytes    atomic.Uint64
	chunkDedupInputBytes  atomic.Uint64
	ledgerCommandCapsules atomic.Uint64
	ledgerFileCapsules    atomic.Uint64
	ledgerSearchCapsules  atomic.Uint64
	ledgerFailureCapsules atomic.Uint64
	cacheMu               sync.Mutex
	cacheCounters         map[proxyLayer0CacheKey]uint64
}

func (c *proxyLayer0RouteCounters) record(stats proxyLayer0Stats) {
	if c == nil {
		return
	}
	if stats.ToolResultBlocks > 0 {
		c.toolResultBlocks.Add(uint64(stats.ToolResultBlocks))
	}
	if stats.ToolUseUnresolvedBlocks > 0 {
		c.toolUseUnresolved.Add(uint64(stats.ToolUseUnresolvedBlocks))
	}
	if stats.CommandResolvedBlocks > 0 {
		c.commandResolvedBlocks.Add(uint64(stats.CommandResolvedBlocks))
	}
	if stats.CommandUnresolvedBlocks > 0 {
		c.commandUnresolved.Add(uint64(stats.CommandUnresolvedBlocks))
	}
	if stats.ReadDeltaAttempts > 0 {
		c.readDeltaAttempts.Add(uint64(stats.ReadDeltaAttempts))
	}
	if stats.ReadDeltaMisses > 0 {
		c.readDeltaMisses.Add(uint64(stats.ReadDeltaMisses))
	}
	if stats.TokensSaved > 0 {
		c.requestsModified.Add(1)
		c.tokensSaved.Add(uint64(stats.TokensSaved))
		if stats.BlocksModified > 0 {
			c.blocksModified.Add(uint64(stats.BlocksModified))
		}
		if stats.ReadDeltaBlocks > 0 {
			c.readDeltaBlocks.Add(uint64(stats.ReadDeltaBlocks))
		}
		if stats.CapturedOutputBlocks > 0 {
			c.capturedBlocks.Add(uint64(stats.CapturedOutputBlocks))
		}
		if stats.CodexExecEnvelopeBlocks > 0 {
			c.envelopeBlocks.Add(uint64(stats.CodexExecEnvelopeBlocks))
		}
		if stats.RepeatedOutputBlocks > 0 {
			c.repeatedOutputBlocks.Add(uint64(stats.RepeatedOutputBlocks))
		}
		if stats.ChunkDedupBlocks > 0 {
			c.chunkDedupBlocks.Add(uint64(stats.ChunkDedupBlocks))
		}
		if stats.ChunkDedupReferences > 0 {
			c.chunkDedupReferences.Add(uint64(stats.ChunkDedupReferences))
		}
		if stats.ChunkDedupRefBytes > 0 {
			c.chunkDedupRefBytes.Add(uint64(stats.ChunkDedupRefBytes))
		}
		if stats.ChunkDedupInputBytes > 0 {
			c.chunkDedupInputBytes.Add(uint64(stats.ChunkDedupInputBytes))
		}
	}
	if stats.LedgerCommandCapsules > 0 {
		c.ledgerCommandCapsules.Add(uint64(stats.LedgerCommandCapsules))
	}
	if stats.LedgerFileCapsules > 0 {
		c.ledgerFileCapsules.Add(uint64(stats.LedgerFileCapsules))
	}
	if stats.LedgerSearchCapsules > 0 {
		c.ledgerSearchCapsules.Add(uint64(stats.LedgerSearchCapsules))
	}
	if stats.LedgerFailureCapsules > 0 {
		c.ledgerFailureCapsules.Add(uint64(stats.LedgerFailureCapsules))
	}
	if len(stats.CacheEvents) > 0 {
		c.recordCacheEvents(stats.Route, stats.CacheEvents)
	}
}

func (c *proxyLayer0RouteCounters) snapshot() ProxyLayer0RouteTelemetry {
	if c == nil {
		return ProxyLayer0RouteTelemetry{}
	}
	return ProxyLayer0RouteTelemetry{
		ToolResultBlocks:      c.toolResultBlocks.Load(),
		ToolUseUnresolved:     c.toolUseUnresolved.Load(),
		CommandResolvedBlocks: c.commandResolvedBlocks.Load(),
		CommandUnresolved:     c.commandUnresolved.Load(),
		ReadDeltaAttempts:     c.readDeltaAttempts.Load(),
		ReadDeltaMisses:       c.readDeltaMisses.Load(),
		RequestsModified:      c.requestsModified.Load(),
		TokensSaved:           c.tokensSaved.Load(),
		BlocksModified:        c.blocksModified.Load(),
		ReadDeltaBlocks:       c.readDeltaBlocks.Load(),
		CapturedBlocks:        c.capturedBlocks.Load(),
		EnvelopeBlocks:        c.envelopeBlocks.Load(),
		RepeatedOutputBlocks:  c.repeatedOutputBlocks.Load(),
		ChunkDedupBlocks:      c.chunkDedupBlocks.Load(),
		ChunkDedupReferences:  c.chunkDedupReferences.Load(),
		ChunkDedupRefBytes:    c.chunkDedupRefBytes.Load(),
		ChunkDedupInputBytes:  c.chunkDedupInputBytes.Load(),
		LedgerCommandCapsules: c.ledgerCommandCapsules.Load(),
		LedgerFileCapsules:    c.ledgerFileCapsules.Load(),
		LedgerSearchCapsules:  c.ledgerSearchCapsules.Load(),
		LedgerFailureCapsules: c.ledgerFailureCapsules.Load(),
		Cache:                 c.snapshotCacheEvents(),
	}
}

func (c *proxyLayer0RouteCounters) recordCacheEvents(route codexLayer0Route, events []proxyLayer0CacheEvent) {
	if c == nil || len(events) == 0 {
		return
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	if c.cacheCounters == nil {
		c.cacheCounters = make(map[proxyLayer0CacheKey]uint64, len(events))
	}
	for _, event := range events {
		if event.Mechanism == "" || event.Action == "" || event.Reason == "" {
			continue
		}
		c.cacheCounters[proxyLayer0CacheKey{
			route:     route,
			mechanism: event.Mechanism,
			action:    event.Action,
			reason:    event.Reason,
		}]++
	}
}

func (c *proxyLayer0RouteCounters) snapshotCacheEvents() []ProxyLayer0CacheEntry {
	if c == nil {
		return nil
	}
	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	return proxyLayer0CacheEntriesFromMap(c.cacheCounters)
}

// OutputReduceCounters surfaces the cumulative observable state of the
// T165/T166/T167 mechanisms (stop-sequence injection, streamcut fire,
// repdet rewrites). All fields are monotonic-only - resets happen only
// at process restart. Operators read this struct via admin /status.
type OutputReduceCounters struct {
	proxyLayer0ToolResultBlocks      atomic.Uint64
	proxyLayer0ToolUseUnresolved     atomic.Uint64
	proxyLayer0CommandResolvedBlocks atomic.Uint64
	proxyLayer0CommandUnresolved     atomic.Uint64
	proxyLayer0ReadDeltaAttempts     atomic.Uint64
	proxyLayer0ReadDeltaMisses       atomic.Uint64
	proxyLayer0RequestsModified      atomic.Uint64
	proxyLayer0TokensSaved           atomic.Uint64
	proxyLayer0BlocksModified        atomic.Uint64
	proxyLayer0ReadDeltaBlocks       atomic.Uint64
	proxyLayer0CapturedBlocks        atomic.Uint64
	proxyLayer0EnvelopeBlocks        atomic.Uint64
	proxyLayer0RepeatedOutputBlocks  atomic.Uint64
	proxyLayer0ChunkDedupBlocks      atomic.Uint64
	proxyLayer0ChunkDedupReferences  atomic.Uint64
	proxyLayer0ChunkDedupRefBytes    atomic.Uint64
	proxyLayer0ChunkDedupInputBytes  atomic.Uint64
	proxyLayer0LedgerCommandCapsules atomic.Uint64
	proxyLayer0LedgerFileCapsules    atomic.Uint64
	proxyLayer0LedgerSearchCapsules  atomic.Uint64
	proxyLayer0LedgerFailureCapsules atomic.Uint64
	proxyLayer0HTTP                  proxyLayer0RouteCounters
	proxyLayer0WSSPhaseF             proxyLayer0RouteCounters
	proxyLayer0PolicyMu              sync.Mutex
	proxyLayer0PolicyCounters        map[proxyLayer0PolicyKey]uint64
	proxyLayer0CacheMu               sync.Mutex
	proxyLayer0CacheCounters         map[proxyLayer0CacheKey]uint64
	proxyLayer0LatencyMu             sync.Mutex
	proxyLayer0Latency               map[proxyLayer0LatencyKey]*analytics.PhaseHistogram

	stopSeqRequestsModified atomic.Uint64
	stopSeqPhrasesAdded     atomic.Uint64

	streamcutFired         atomic.Uint64
	streamcutBytesObserved atomic.Uint64

	repdetResponsesRewritten atomic.Uint64
	repdetMatchesRewritten   atomic.Uint64
	repdetBytesSaved         atomic.Uint64

	staleReadBlocksReplaced atomic.Uint64
	staleReadBytesReplaced  atomic.Uint64

	obsoleteReadBlocksPruned atomic.Uint64
	obsoleteReadBytesPruned  atomic.Uint64

	beterseInjections atomic.Uint64
	beterseHintBytes  atomic.Uint64
}

type proxyLayer0PolicyKey struct {
	route       codexLayer0Route
	mechanism   savingspolicy.CodexMechanism
	action      savingspolicy.CodexPolicyAction
	reason      string
	blockReason string
}

type proxyLayer0CacheKey struct {
	route     codexLayer0Route
	mechanism savingspolicy.CodexMechanism
	action    proxyLayer0CacheAction
	reason    string
}

type proxyLayer0LatencyKey struct {
	route     codexLayer0Route
	mechanism string
}

// RecordProxyLayer0 increments the proxy-side deterministic captured
// output compaction counters. savedTokens is token-count based, not
// bytes, because this path runs before provider billing.
func (c *OutputReduceCounters) RecordProxyLayer0(savedTokens int) {
	c.RecordProxyLayer0Stats(proxyLayer0Stats{TokensSaved: savedTokens, BlocksModified: 1})
}

func (c *OutputReduceCounters) RecordProxyLayer0Stats(stats proxyLayer0Stats) {
	if proxyLayer0StatsEmpty(stats) {
		return
	}
	if routeCounters := c.proxyLayer0RouteCounters(stats.Route); routeCounters != nil {
		routeCounters.record(stats)
	}
	if len(stats.PolicyDecisions) > 0 {
		c.recordProxyLayer0Policy(stats.Route, stats.PolicyDecisions)
	}
	if len(stats.CacheEvents) > 0 {
		c.recordProxyLayer0Cache(stats.Route, stats.CacheEvents)
	}
	c.recordProxyLayer0Latencies(stats)
	if stats.ToolResultBlocks > 0 {
		c.proxyLayer0ToolResultBlocks.Add(uint64(stats.ToolResultBlocks))
	}
	if stats.ToolUseUnresolvedBlocks > 0 {
		c.proxyLayer0ToolUseUnresolved.Add(uint64(stats.ToolUseUnresolvedBlocks))
	}
	if stats.CommandResolvedBlocks > 0 {
		c.proxyLayer0CommandResolvedBlocks.Add(uint64(stats.CommandResolvedBlocks))
	}
	if stats.CommandUnresolvedBlocks > 0 {
		c.proxyLayer0CommandUnresolved.Add(uint64(stats.CommandUnresolvedBlocks))
	}
	if stats.ReadDeltaAttempts > 0 {
		c.proxyLayer0ReadDeltaAttempts.Add(uint64(stats.ReadDeltaAttempts))
	}
	if stats.ReadDeltaMisses > 0 {
		c.proxyLayer0ReadDeltaMisses.Add(uint64(stats.ReadDeltaMisses))
	}
	if stats.TokensSaved > 0 {
		c.proxyLayer0RequestsModified.Add(1)
		c.proxyLayer0TokensSaved.Add(uint64(stats.TokensSaved))
		if stats.BlocksModified > 0 {
			c.proxyLayer0BlocksModified.Add(uint64(stats.BlocksModified))
		}
		if stats.ReadDeltaBlocks > 0 {
			c.proxyLayer0ReadDeltaBlocks.Add(uint64(stats.ReadDeltaBlocks))
		}
		if stats.CapturedOutputBlocks > 0 {
			c.proxyLayer0CapturedBlocks.Add(uint64(stats.CapturedOutputBlocks))
		}
		if stats.CodexExecEnvelopeBlocks > 0 {
			c.proxyLayer0EnvelopeBlocks.Add(uint64(stats.CodexExecEnvelopeBlocks))
		}
		if stats.RepeatedOutputBlocks > 0 {
			c.proxyLayer0RepeatedOutputBlocks.Add(uint64(stats.RepeatedOutputBlocks))
		}
		if stats.ChunkDedupBlocks > 0 {
			c.proxyLayer0ChunkDedupBlocks.Add(uint64(stats.ChunkDedupBlocks))
		}
		if stats.ChunkDedupReferences > 0 {
			c.proxyLayer0ChunkDedupReferences.Add(uint64(stats.ChunkDedupReferences))
		}
		if stats.ChunkDedupRefBytes > 0 {
			c.proxyLayer0ChunkDedupRefBytes.Add(uint64(stats.ChunkDedupRefBytes))
		}
		if stats.ChunkDedupInputBytes > 0 {
			c.proxyLayer0ChunkDedupInputBytes.Add(uint64(stats.ChunkDedupInputBytes))
		}
	}
	if stats.LedgerCommandCapsules > 0 {
		c.proxyLayer0LedgerCommandCapsules.Add(uint64(stats.LedgerCommandCapsules))
	}
	if stats.LedgerFileCapsules > 0 {
		c.proxyLayer0LedgerFileCapsules.Add(uint64(stats.LedgerFileCapsules))
	}
	if stats.LedgerSearchCapsules > 0 {
		c.proxyLayer0LedgerSearchCapsules.Add(uint64(stats.LedgerSearchCapsules))
	}
	if stats.LedgerFailureCapsules > 0 {
		c.proxyLayer0LedgerFailureCapsules.Add(uint64(stats.LedgerFailureCapsules))
	}
}

func (c *OutputReduceCounters) recordProxyLayer0Latencies(stats proxyLayer0Stats) {
	if c == nil {
		return
	}
	c.recordProxyLayer0Latency(stats.Route, "total", stats.TotalLatencyNs)
	c.recordProxyLayer0Latency(stats.Route, string(savingspolicy.CodexMechanismReadDelta), stats.ReadDeltaLatencyNs)
	c.recordProxyLayer0Latency(stats.Route, "structured_filter", stats.FilterLatencyNs)
	c.recordProxyLayer0Latency(stats.Route, string(savingspolicy.CodexMechanismRepeatedOutput), stats.RepeatedOutputLatencyNs)
	c.recordProxyLayer0Latency(stats.Route, string(savingspolicy.CodexMechanismChunkDedup), stats.ChunkDedupLatencyNs)
}

func (c *OutputReduceCounters) recordProxyLayer0Latency(route codexLayer0Route, mechanism string, ns int64) {
	if c == nil || ns <= 0 || mechanism == "" {
		return
	}
	key := proxyLayer0LatencyKey{route: route, mechanism: mechanism}
	c.proxyLayer0LatencyMu.Lock()
	h := c.proxyLayer0Latency[key]
	if h == nil {
		if c.proxyLayer0Latency == nil {
			c.proxyLayer0Latency = make(map[proxyLayer0LatencyKey]*analytics.PhaseHistogram)
		}
		h = analytics.NewPhaseHistogram(string(route)+":"+mechanism, 200)
		c.proxyLayer0Latency[key] = h
	}
	c.proxyLayer0LatencyMu.Unlock()
	h.Record(time.Duration(ns))
}

func proxyLayer0StatsEmpty(stats proxyLayer0Stats) bool {
	return stats.Route == "" &&
		stats.TokensSaved == 0 &&
		stats.BlocksModified == 0 &&
		stats.ToolResultBlocks == 0 &&
		stats.ToolUseUnresolvedBlocks == 0 &&
		stats.CommandResolvedBlocks == 0 &&
		stats.CommandUnresolvedBlocks == 0 &&
		stats.ReadDeltaAttempts == 0 &&
		stats.ReadDeltaMisses == 0 &&
		stats.CapturedOutputBlocks == 0 &&
		stats.CodexExecEnvelopeBlocks == 0 &&
		stats.RepeatedOutputBlocks == 0 &&
		stats.ChunkDedupBlocks == 0 &&
		stats.ChunkDedupReferences == 0 &&
		stats.ChunkDedupRefBytes == 0 &&
		stats.ChunkDedupInputBytes == 0 &&
		stats.LedgerCommandCapsules == 0 &&
		stats.LedgerFileCapsules == 0 &&
		stats.LedgerSearchCapsules == 0 &&
		stats.LedgerFailureCapsules == 0 &&
		stats.ReadDeltaBlocks == 0 &&
		len(stats.ReadDeltaKeys) == 0 &&
		len(stats.PolicyDecisions) == 0 &&
		len(stats.CacheEvents) == 0
}

func (c *OutputReduceCounters) recordProxyLayer0Policy(route codexLayer0Route, decisions []savingspolicy.CodexMechanismDecision) {
	if c == nil || len(decisions) == 0 {
		return
	}
	c.proxyLayer0PolicyMu.Lock()
	defer c.proxyLayer0PolicyMu.Unlock()
	if c.proxyLayer0PolicyCounters == nil {
		c.proxyLayer0PolicyCounters = make(map[proxyLayer0PolicyKey]uint64, len(decisions))
	}
	for _, decision := range decisions {
		if decision.Mechanism == "" || decision.Action == "" {
			continue
		}
		key := proxyLayer0PolicyKey{
			route:       route,
			mechanism:   decision.Mechanism,
			action:      decision.Action,
			reason:      decision.Reason,
			blockReason: decision.BlockReason,
		}
		c.proxyLayer0PolicyCounters[key]++
	}
}

func (c *OutputReduceCounters) recordProxyLayer0Cache(route codexLayer0Route, events []proxyLayer0CacheEvent) {
	if c == nil || len(events) == 0 {
		return
	}
	c.proxyLayer0CacheMu.Lock()
	defer c.proxyLayer0CacheMu.Unlock()
	if c.proxyLayer0CacheCounters == nil {
		c.proxyLayer0CacheCounters = make(map[proxyLayer0CacheKey]uint64, len(events))
	}
	for _, event := range events {
		if event.Mechanism == "" || event.Action == "" || event.Reason == "" {
			continue
		}
		key := proxyLayer0CacheKey{
			route:     route,
			mechanism: event.Mechanism,
			action:    event.Action,
			reason:    event.Reason,
		}
		c.proxyLayer0CacheCounters[key]++
	}
}

func (c *OutputReduceCounters) proxyLayer0RouteCounters(route codexLayer0Route) *proxyLayer0RouteCounters {
	switch route {
	case codexLayer0RouteHTTP:
		return &c.proxyLayer0HTTP
	case codexLayer0RouteWSSPhaseF:
		return &c.proxyLayer0WSSPhaseF
	default:
		return nil
	}
}

// RecordStopSeqInjection increments the stop-sequence counters when
// outstop.MergeIntoBody added at least one phrase.
func (c *OutputReduceCounters) RecordStopSeqInjection(addedPhrases int) {
	if addedPhrases <= 0 {
		return
	}
	c.stopSeqRequestsModified.Add(1)
	c.stopSeqPhrasesAdded.Add(uint64(addedPhrases))
}

// RecordStreamcutFire increments the streamcut counters when the
// cutter fired on a response. bytesObserved is the upstream byte count
// the relay forwarded before terminating.
func (c *OutputReduceCounters) RecordStreamcutFire(bytesObserved int64) {
	c.streamcutFired.Add(1)
	if bytesObserved > 0 {
		c.streamcutBytesObserved.Add(uint64(bytesObserved))
	}
}

// RecordStaleReadAging increments the stale-read aging counters
// when staleread.AgeMessages produced at least one replacement.
// blocksReplaced is the number of tool_result blocks rewritten,
// bytesReplaced is the cumulative byte saving across them.
func (c *OutputReduceCounters) RecordStaleReadAging(blocksReplaced, bytesReplaced int) {
	if blocksReplaced <= 0 {
		return
	}
	c.staleReadBlocksReplaced.Add(uint64(blocksReplaced))
	if bytesReplaced > 0 {
		c.staleReadBytesReplaced.Add(uint64(bytesReplaced))
	}
}

// RecordBeTerseInjection increments the T169 be-terse counters
// every time the qualityab-gated hint was injected into a treatment
// request.
func (c *OutputReduceCounters) RecordBeTerseInjection(hintBytes int) {
	c.beterseInjections.Add(1)
	if hintBytes > 0 {
		c.beterseHintBytes.Add(uint64(hintBytes))
	}
}

// RecordObsoleteReadPrune increments the T174 obsolete-prune
// counters when PruneObsoleteReads produced at least one
// replacement.
func (c *OutputReduceCounters) RecordObsoleteReadPrune(blocksPruned, bytesPruned int) {
	if blocksPruned <= 0 {
		return
	}
	c.obsoleteReadBlocksPruned.Add(uint64(blocksPruned))
	if bytesPruned > 0 {
		c.obsoleteReadBytesPruned.Add(uint64(bytesPruned))
	}
}

// RecordRepdetRewrite increments the repdet counters when one response
// body was rewritten. savedBytes is the sum of replaced-span lengths
// (input span size, before marker substitution).
func (c *OutputReduceCounters) RecordRepdetRewrite(matchCount int, savedBytes int) {
	if matchCount <= 0 || savedBytes <= 0 {
		return
	}
	c.repdetResponsesRewritten.Add(1)
	c.repdetMatchesRewritten.Add(uint64(matchCount))
	c.repdetBytesSaved.Add(uint64(savedBytes))
}

// OutputReduceTelemetry is the JSON shape served on /admin/status under
// the "output_reduce_counters" key.
type OutputReduceTelemetry struct {
	ProxyLayer0ToolResultBlocks      uint64                     `json:"proxy_layer0_tool_result_blocks"`
	ProxyLayer0ToolUseUnresolved     uint64                     `json:"proxy_layer0_tool_use_unresolved_blocks"`
	ProxyLayer0CommandResolvedBlocks uint64                     `json:"proxy_layer0_command_resolved_blocks"`
	ProxyLayer0CommandUnresolved     uint64                     `json:"proxy_layer0_command_unresolved_blocks"`
	ProxyLayer0ReadDeltaAttempts     uint64                     `json:"proxy_layer0_read_delta_attempts"`
	ProxyLayer0ReadDeltaMisses       uint64                     `json:"proxy_layer0_read_delta_misses"`
	ProxyLayer0RequestsModified      uint64                     `json:"proxy_layer0_requests_modified"`
	ProxyLayer0TokensSaved           uint64                     `json:"proxy_layer0_tokens_saved"`
	ProxyLayer0BlocksModified        uint64                     `json:"proxy_layer0_blocks_modified"`
	ProxyLayer0ReadDeltaBlocks       uint64                     `json:"proxy_layer0_read_delta_blocks"`
	ProxyLayer0CapturedBlocks        uint64                     `json:"proxy_layer0_captured_output_blocks"`
	ProxyLayer0EnvelopeBlocks        uint64                     `json:"proxy_layer0_codex_exec_envelope_blocks"`
	ProxyLayer0RepeatedOutputBlocks  uint64                     `json:"proxy_layer0_repeated_output_blocks"`
	ProxyLayer0ChunkDedupBlocks      uint64                     `json:"proxy_layer0_chunk_dedup_blocks"`
	ProxyLayer0ChunkDedupReferences  uint64                     `json:"proxy_layer0_chunk_dedup_references"`
	ProxyLayer0ChunkDedupRefBytes    uint64                     `json:"proxy_layer0_chunk_dedup_referenced_bytes"`
	ProxyLayer0ChunkDedupInputBytes  uint64                     `json:"proxy_layer0_chunk_dedup_input_bytes"`
	ProxyLayer0LedgerCommandCapsules uint64                     `json:"proxy_layer0_ledger_command_capsules"`
	ProxyLayer0LedgerFileCapsules    uint64                     `json:"proxy_layer0_ledger_file_capsules"`
	ProxyLayer0LedgerSearchCapsules  uint64                     `json:"proxy_layer0_ledger_search_capsules"`
	ProxyLayer0LedgerFailureCapsules uint64                     `json:"proxy_layer0_ledger_failure_capsules"`
	ProxyLayer0Routes                ProxyLayer0RoutesTelemetry `json:"proxy_layer0_routes"`
	ProxyLayer0Policy                []ProxyLayer0PolicyEntry   `json:"proxy_layer0_policy"`
	ProxyLayer0Cache                 []ProxyLayer0CacheEntry    `json:"proxy_layer0_cache"`
	ProxyLayer0Latency               []ProxyLayer0LatencyEntry  `json:"proxy_layer0_latency"`
	StopSeqRequestsModified          uint64                     `json:"stop_seq_requests_modified"`
	StopSeqPhrasesAdded              uint64                     `json:"stop_seq_phrases_added"`
	StreamcutFired                   uint64                     `json:"streamcut_fired"`
	StreamcutBytesObserved           uint64                     `json:"streamcut_bytes_observed"`
	RepdetResponsesRewritten         uint64                     `json:"repdet_responses_rewritten"`
	RepdetMatchesRewritten           uint64                     `json:"repdet_matches_rewritten"`
	RepdetBytesSaved                 uint64                     `json:"repdet_bytes_saved"`
	StaleReadBlocksReplaced          uint64                     `json:"stale_read_blocks_replaced"`
	StaleReadBytesReplaced           uint64                     `json:"stale_read_bytes_replaced"`
	ObsoleteReadBlocksPruned         uint64                     `json:"obsolete_read_blocks_pruned"`
	ObsoleteReadBytesPruned          uint64                     `json:"obsolete_read_bytes_pruned"`
	BeterseInjections                uint64                     `json:"beterse_injections"`
	BeterseHintBytes                 uint64                     `json:"beterse_hint_bytes"`
}

type ProxyLayer0PolicyEntry struct {
	Route       string `json:"route"`
	Mechanism   string `json:"mechanism"`
	Action      string `json:"action"`
	Reason      string `json:"reason"`
	BlockReason string `json:"block_reason,omitempty"`
	Count       uint64 `json:"count"`
}

type ProxyLayer0CacheEntry struct {
	Route     string `json:"route"`
	Mechanism string `json:"mechanism"`
	Action    string `json:"action"`
	Reason    string `json:"reason"`
	Count     uint64 `json:"count"`
}

type ProxyLayer0LatencyEntry struct {
	Route      string  `json:"route"`
	Mechanism  string  `json:"mechanism"`
	Count      int64   `json:"count"`
	P50Ms      float64 `json:"p50_ms"`
	P95Ms      float64 `json:"p95_ms"`
	MaxMs      float64 `json:"max_ms"`
	AvgMs      float64 `json:"avg_ms"`
	SampleSize int     `json:"sample_size"`
}

type ProxyLayer0RouteTelemetry struct {
	ToolResultBlocks      uint64                  `json:"tool_result_blocks"`
	ToolUseUnresolved     uint64                  `json:"tool_use_unresolved_blocks"`
	CommandResolvedBlocks uint64                  `json:"command_resolved_blocks"`
	CommandUnresolved     uint64                  `json:"command_unresolved_blocks"`
	ReadDeltaAttempts     uint64                  `json:"read_delta_attempts"`
	ReadDeltaMisses       uint64                  `json:"read_delta_misses"`
	RequestsModified      uint64                  `json:"requests_modified"`
	TokensSaved           uint64                  `json:"tokens_saved"`
	BlocksModified        uint64                  `json:"blocks_modified"`
	ReadDeltaBlocks       uint64                  `json:"read_delta_blocks"`
	CapturedBlocks        uint64                  `json:"captured_output_blocks"`
	EnvelopeBlocks        uint64                  `json:"codex_exec_envelope_blocks"`
	RepeatedOutputBlocks  uint64                  `json:"repeated_output_blocks"`
	ChunkDedupBlocks      uint64                  `json:"chunk_dedup_blocks"`
	ChunkDedupReferences  uint64                  `json:"chunk_dedup_references"`
	ChunkDedupRefBytes    uint64                  `json:"chunk_dedup_referenced_bytes"`
	ChunkDedupInputBytes  uint64                  `json:"chunk_dedup_input_bytes"`
	LedgerCommandCapsules uint64                  `json:"ledger_command_capsules"`
	LedgerFileCapsules    uint64                  `json:"ledger_file_capsules"`
	LedgerSearchCapsules  uint64                  `json:"ledger_search_capsules"`
	LedgerFailureCapsules uint64                  `json:"ledger_failure_capsules"`
	Cache                 []ProxyLayer0CacheEntry `json:"cache,omitempty"`
}

type ProxyLayer0RoutesTelemetry struct {
	HTTP      ProxyLayer0RouteTelemetry `json:"http"`
	WSSPhaseF ProxyLayer0RouteTelemetry `json:"wss_phasef"`
}

// Snapshot returns a value-copy of the current counters - safe to
// serialise and emit without holding a lock on the live struct.
func (c *OutputReduceCounters) Snapshot() OutputReduceTelemetry {
	return OutputReduceTelemetry{
		ProxyLayer0ToolResultBlocks:      c.proxyLayer0ToolResultBlocks.Load(),
		ProxyLayer0ToolUseUnresolved:     c.proxyLayer0ToolUseUnresolved.Load(),
		ProxyLayer0CommandResolvedBlocks: c.proxyLayer0CommandResolvedBlocks.Load(),
		ProxyLayer0CommandUnresolved:     c.proxyLayer0CommandUnresolved.Load(),
		ProxyLayer0ReadDeltaAttempts:     c.proxyLayer0ReadDeltaAttempts.Load(),
		ProxyLayer0ReadDeltaMisses:       c.proxyLayer0ReadDeltaMisses.Load(),
		ProxyLayer0RequestsModified:      c.proxyLayer0RequestsModified.Load(),
		ProxyLayer0TokensSaved:           c.proxyLayer0TokensSaved.Load(),
		ProxyLayer0BlocksModified:        c.proxyLayer0BlocksModified.Load(),
		ProxyLayer0ReadDeltaBlocks:       c.proxyLayer0ReadDeltaBlocks.Load(),
		ProxyLayer0CapturedBlocks:        c.proxyLayer0CapturedBlocks.Load(),
		ProxyLayer0EnvelopeBlocks:        c.proxyLayer0EnvelopeBlocks.Load(),
		ProxyLayer0RepeatedOutputBlocks:  c.proxyLayer0RepeatedOutputBlocks.Load(),
		ProxyLayer0ChunkDedupBlocks:      c.proxyLayer0ChunkDedupBlocks.Load(),
		ProxyLayer0ChunkDedupReferences:  c.proxyLayer0ChunkDedupReferences.Load(),
		ProxyLayer0ChunkDedupRefBytes:    c.proxyLayer0ChunkDedupRefBytes.Load(),
		ProxyLayer0ChunkDedupInputBytes:  c.proxyLayer0ChunkDedupInputBytes.Load(),
		ProxyLayer0LedgerCommandCapsules: c.proxyLayer0LedgerCommandCapsules.Load(),
		ProxyLayer0LedgerFileCapsules:    c.proxyLayer0LedgerFileCapsules.Load(),
		ProxyLayer0LedgerSearchCapsules:  c.proxyLayer0LedgerSearchCapsules.Load(),
		ProxyLayer0LedgerFailureCapsules: c.proxyLayer0LedgerFailureCapsules.Load(),
		ProxyLayer0Routes: ProxyLayer0RoutesTelemetry{
			HTTP:      c.proxyLayer0HTTP.snapshot(),
			WSSPhaseF: c.proxyLayer0WSSPhaseF.snapshot(),
		},
		ProxyLayer0Policy:        c.snapshotProxyLayer0Policy(),
		ProxyLayer0Cache:         c.snapshotProxyLayer0Cache(),
		ProxyLayer0Latency:       c.snapshotProxyLayer0Latency(),
		StopSeqRequestsModified:  c.stopSeqRequestsModified.Load(),
		StopSeqPhrasesAdded:      c.stopSeqPhrasesAdded.Load(),
		StreamcutFired:           c.streamcutFired.Load(),
		StreamcutBytesObserved:   c.streamcutBytesObserved.Load(),
		RepdetResponsesRewritten: c.repdetResponsesRewritten.Load(),
		RepdetMatchesRewritten:   c.repdetMatchesRewritten.Load(),
		RepdetBytesSaved:         c.repdetBytesSaved.Load(),
		StaleReadBlocksReplaced:  c.staleReadBlocksReplaced.Load(),
		StaleReadBytesReplaced:   c.staleReadBytesReplaced.Load(),
		ObsoleteReadBlocksPruned: c.obsoleteReadBlocksPruned.Load(),
		ObsoleteReadBytesPruned:  c.obsoleteReadBytesPruned.Load(),
		BeterseInjections:        c.beterseInjections.Load(),
		BeterseHintBytes:         c.beterseHintBytes.Load(),
	}
}

func (c *OutputReduceCounters) snapshotProxyLayer0Latency() []ProxyLayer0LatencyEntry {
	if c == nil {
		return nil
	}
	c.proxyLayer0LatencyMu.Lock()
	keys := make([]proxyLayer0LatencyKey, 0, len(c.proxyLayer0Latency))
	hists := make(map[proxyLayer0LatencyKey]*analytics.PhaseHistogram, len(c.proxyLayer0Latency))
	for key, hist := range c.proxyLayer0Latency {
		keys = append(keys, key)
		hists[key] = hist
	}
	c.proxyLayer0LatencyMu.Unlock()
	if len(keys) == 0 {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].route != keys[j].route {
			return keys[i].route < keys[j].route
		}
		return keys[i].mechanism < keys[j].mechanism
	})
	out := make([]ProxyLayer0LatencyEntry, 0, len(keys))
	for _, key := range keys {
		snap := hists[key].Snapshot()
		out = append(out, ProxyLayer0LatencyEntry{
			Route:      string(key.route),
			Mechanism:  key.mechanism,
			Count:      snap.Count,
			P50Ms:      snap.P50Ms,
			P95Ms:      snap.P95Ms,
			MaxMs:      snap.MaxMs,
			AvgMs:      snap.AvgMs,
			SampleSize: snap.SampleSize,
		})
	}
	return out
}

func (c *OutputReduceCounters) snapshotProxyLayer0Cache() []ProxyLayer0CacheEntry {
	if c == nil {
		return nil
	}
	c.proxyLayer0CacheMu.Lock()
	defer c.proxyLayer0CacheMu.Unlock()
	return proxyLayer0CacheEntriesFromMap(c.proxyLayer0CacheCounters)
}

func proxyLayer0CacheEntriesFromMap(counts map[proxyLayer0CacheKey]uint64) []ProxyLayer0CacheEntry {
	if len(counts) == 0 {
		return nil
	}
	out := make([]ProxyLayer0CacheEntry, 0, len(counts))
	for key, count := range counts {
		out = append(out, ProxyLayer0CacheEntry{
			Route:     string(key.route),
			Mechanism: string(key.mechanism),
			Action:    string(key.action),
			Reason:    key.reason,
			Count:     count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		if out[i].Mechanism != out[j].Mechanism {
			return out[i].Mechanism < out[j].Mechanism
		}
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		return out[i].Reason < out[j].Reason
	})
	return out
}

func (c *OutputReduceCounters) snapshotProxyLayer0Policy() []ProxyLayer0PolicyEntry {
	if c == nil {
		return nil
	}
	c.proxyLayer0PolicyMu.Lock()
	defer c.proxyLayer0PolicyMu.Unlock()
	if len(c.proxyLayer0PolicyCounters) == 0 {
		return nil
	}
	out := make([]ProxyLayer0PolicyEntry, 0, len(c.proxyLayer0PolicyCounters))
	for key, count := range c.proxyLayer0PolicyCounters {
		out = append(out, ProxyLayer0PolicyEntry{
			Route:       string(key.route),
			Mechanism:   string(key.mechanism),
			Action:      string(key.action),
			Reason:      key.reason,
			BlockReason: key.blockReason,
			Count:       count,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Route != out[j].Route {
			return out[i].Route < out[j].Route
		}
		if out[i].Mechanism != out[j].Mechanism {
			return out[i].Mechanism < out[j].Mechanism
		}
		if out[i].Action != out[j].Action {
			return out[i].Action < out[j].Action
		}
		if out[i].Reason != out[j].Reason {
			return out[i].Reason < out[j].Reason
		}
		return out[i].BlockReason < out[j].BlockReason
	})
	return out
}
