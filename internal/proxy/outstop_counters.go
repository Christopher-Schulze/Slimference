package proxy

import "sync/atomic"

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
	}
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
	proxyLayer0HTTP                  proxyLayer0RouteCounters
	proxyLayer0WSSPhaseF             proxyLayer0RouteCounters

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

// RecordProxyLayer0 increments the proxy-side deterministic captured
// output compaction counters. savedTokens is token-count based, not
// bytes, because this path runs before provider billing.
func (c *OutputReduceCounters) RecordProxyLayer0(savedTokens int) {
	c.RecordProxyLayer0Stats(proxyLayer0Stats{TokensSaved: savedTokens, BlocksModified: 1})
}

func (c *OutputReduceCounters) RecordProxyLayer0Stats(stats proxyLayer0Stats) {
	if stats == (proxyLayer0Stats{}) {
		return
	}
	if routeCounters := c.proxyLayer0RouteCounters(stats.Route); routeCounters != nil {
		routeCounters.record(stats)
	}
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
	ProxyLayer0Routes                ProxyLayer0RoutesTelemetry `json:"proxy_layer0_routes"`
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

type ProxyLayer0RouteTelemetry struct {
	ToolResultBlocks      uint64 `json:"tool_result_blocks"`
	ToolUseUnresolved     uint64 `json:"tool_use_unresolved_blocks"`
	CommandResolvedBlocks uint64 `json:"command_resolved_blocks"`
	CommandUnresolved     uint64 `json:"command_unresolved_blocks"`
	ReadDeltaAttempts     uint64 `json:"read_delta_attempts"`
	ReadDeltaMisses       uint64 `json:"read_delta_misses"`
	RequestsModified      uint64 `json:"requests_modified"`
	TokensSaved           uint64 `json:"tokens_saved"`
	BlocksModified        uint64 `json:"blocks_modified"`
	ReadDeltaBlocks       uint64 `json:"read_delta_blocks"`
	CapturedBlocks        uint64 `json:"captured_output_blocks"`
	EnvelopeBlocks        uint64 `json:"codex_exec_envelope_blocks"`
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
		ProxyLayer0Routes: ProxyLayer0RoutesTelemetry{
			HTTP:      c.proxyLayer0HTTP.snapshot(),
			WSSPhaseF: c.proxyLayer0WSSPhaseF.snapshot(),
		},
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
