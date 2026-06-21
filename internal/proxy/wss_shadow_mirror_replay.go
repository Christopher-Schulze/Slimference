package proxy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/servermirror"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// WSSShadowMirrorReplayResult is a content-free offline replay of the WSS
// server-state mirror. It never mutates frames; it only re-runs the shadow
// accounting against captured request frames so old captures can be re-ranked
// after parser/classifier improvements.
type WSSShadowMirrorReplayResult struct {
	Frames                  int                        `json:"frames"`
	RequestTurns            int                        `json:"request_turns"`
	CapturedMutatedRequests int                        `json:"captured_mutated_requests,omitempty"`
	MissingSessionID        int                        `json:"missing_session_id,omitempty"`
	RequestShapes           WSSABReplayShapeCounts     `json:"request_shapes"`
	Exact                   WSSShadowMirrorReplayExact `json:"exact"`
	Normalized              WSSShadowMirrorReplayExact `json:"normalized"`
	SameRequestExact        WSSShadowMirrorReplayExact `json:"same_request_exact"`
	Rows                    []WSSShadowMirrorReplayRow `json:"rows,omitempty"`
	StatefulSafeRows        []WSSShadowMirrorReplayRow `json:"stateful_safe_rows,omitempty"`
	SameRequestRows         []WSSShadowMirrorReplayRow `json:"same_request_rows,omitempty"`
	Notes                   []string                   `json:"notes,omitempty"`
}

// WSSShadowMirrorReplayExact aggregates exact-block or normalized-segment
// shadow mirror accounting across request frames.
type WSSShadowMirrorReplayExact struct {
	BlocksOrSegments        int     `json:"blocks_or_segments"`
	Bytes                   int     `json:"bytes"`
	Referenceable           int     `json:"referenceable"`
	ReferenceableBytes      int     `json:"referenceable_bytes"`
	ReferenceableBytePct    float64 `json:"referenceable_byte_pct"`
	CandidateTokensEstimate int     `json:"candidate_tokens_estimate"`
}

// WSSShadowMirrorReplayRow aggregates referenceable normalized shadow mirror
// accounting for one request shape and kind.
type WSSShadowMirrorReplayRow struct {
	RequestShape            string  `json:"request_shape"`
	Kind                    string  `json:"kind"`
	Requests                int     `json:"requests"`
	ReferenceableRequests   int     `json:"referenceable_requests"`
	Segments                int     `json:"segments"`
	Bytes                   int     `json:"bytes"`
	ReferenceableSegments   int     `json:"referenceable_segments"`
	ReferenceableBytes      int     `json:"referenceable_bytes"`
	ReferenceableBytePct    float64 `json:"referenceable_byte_pct"`
	CandidateTokensEstimate int     `json:"candidate_tokens_estimate"`
}

// RunWSSShadowMirrorReplay replays captured WSS frames through the same
// shadow-only server mirror shape used by the runtime hot path. Captured
// post-mutation records are counted but skipped, because the preceding original
// request frame is the correct headroom surface.
func RunWSSShadowMirrorReplay(frames []WSSABReplayFrame) (WSSShadowMirrorReplayResult, error) {
	mirror := servermirror.New()
	out := WSSShadowMirrorReplayResult{Frames: len(frames)}
	rows := map[string]*WSSShadowMirrorReplayRow{}
	statefulRows := map[string]*WSSShadowMirrorReplayRow{}
	sameRequestRows := map[string]*WSSShadowMirrorReplayRow{}

	for i, frame := range frames {
		if frame.Direction != wsmitm.DirClientToServer {
			continue
		}
		if frame.Mutated {
			out.CapturedMutatedRequests++
			continue
		}
		body, ok, err := wssReplayRequestBody(frame.Payload)
		if err != nil {
			return WSSShadowMirrorReplayResult{}, fmt.Errorf("extract request body %d: %w", i, err)
		}
		if !ok {
			continue
		}
		messages, raw, err := extractMessages(types.CodexChatGPT, body)
		if err != nil {
			return WSSShadowMirrorReplayResult{}, fmt.Errorf("extract messages %d: %w", i, err)
		}
		if raw == nil {
			raw = map[string]json.RawMessage{}
			_ = json.Unmarshal(body, &raw)
		}
		meta := wssRequestMetaFromRaw(raw)
		if frame.SocketSeq > 0 {
			meta.SocketSeq = frame.SocketSeq
		}
		if len(messages) == 0 {
			messages = wssRawPartialMessages(body)
		}
		shape := wssRequestShape(meta, messages)
		out.RequestShapes.add(shape)
		out.RequestTurns++
		if meta.SessionID == "" {
			out.MissingSessionID++
			continue
		}

		rep := mirror.Predict(meta.SessionID, messages)
		mirror.Observe(meta.SessionID, messages)
		out.Exact.add(rep.Blocks, rep.BlockBytes, rep.ReferenceableBlocks, rep.PotentialSavedBytes)
		out.Normalized.add(rep.NormalizedSegments, rep.NormalizedBytes, rep.NormalizedReferenceableSegments, rep.NormalizedPotentialSavedBytes)
		addWSSShadowMirrorReplayRows(rows, shape, rep.NormalizedPotentialSavedBytesByKind)
		addWSSShadowMirrorReplayRows(statefulRows, shape, shadowMirrorStatefulSafeReports(&meta, rep, messages))
		sameRequest := servermirror.SameRequestExact(messages)
		out.SameRequestExact.add(sameRequest.Blocks, sameRequest.Bytes, sameRequest.ReferenceableBlocks, sameRequest.PotentialSavedBytes)
		addWSSShadowMirrorReplayRows(sameRequestRows, shape, sameRequest.PotentialSavedBytesByKind)
	}

	out.Exact.finalize()
	out.Normalized.finalize()
	out.SameRequestExact.finalize()
	out.Rows = sortedWSSShadowMirrorReplayRows(rows)
	out.StatefulSafeRows = sortedWSSShadowMirrorReplayRows(statefulRows)
	out.SameRequestRows = sortedWSSShadowMirrorReplayRows(sameRequestRows)
	if out.CapturedMutatedRequests > 0 {
		out.Notes = append(out.Notes, "captured post-mutation request records were skipped; the preceding original request record carries the headroom surface")
	}
	return out, nil
}

func (s *WSSShadowMirrorReplayExact) add(total, bytes, referenceable, referenceableBytes int) {
	if s == nil {
		return
	}
	s.BlocksOrSegments += total
	s.Bytes += bytes
	s.Referenceable += referenceable
	s.ReferenceableBytes += referenceableBytes
}

func (s *WSSShadowMirrorReplayExact) finalize() {
	if s == nil {
		return
	}
	s.ReferenceableBytePct = percentInt(s.ReferenceableBytes, s.Bytes)
	s.CandidateTokensEstimate = bytesToTokenEstimate(s.ReferenceableBytes)
}

func addWSSShadowMirrorReplayRows(rows map[string]*WSSShadowMirrorReplayRow, shape string, byKind map[string]servermirror.SegmentKindReport) {
	for kind, report := range byKind {
		kind = strings.TrimSpace(kind)
		if kind == "" || report.Segments == 0 {
			continue
		}
		key := shape + "\x00" + kind
		row := rows[key]
		if row == nil {
			row = &WSSShadowMirrorReplayRow{RequestShape: shape, Kind: kind}
			rows[key] = row
		}
		row.Requests++
		row.Segments += report.Segments
		row.Bytes += report.Bytes
		row.ReferenceableSegments += report.ReferenceableSegments
		row.ReferenceableBytes += report.PotentialSavedBytes
		if report.PotentialSavedBytes > 0 || report.ReferenceableSegments > 0 {
			row.ReferenceableRequests++
		}
	}
}

func sortedWSSShadowMirrorReplayRows(rows map[string]*WSSShadowMirrorReplayRow) []WSSShadowMirrorReplayRow {
	if len(rows) == 0 {
		return nil
	}
	out := make([]WSSShadowMirrorReplayRow, 0, len(rows))
	for _, row := range rows {
		row.ReferenceableBytePct = percentInt(row.ReferenceableBytes, row.Bytes)
		row.CandidateTokensEstimate = bytesToTokenEstimate(row.ReferenceableBytes)
		if row.ReferenceableBytes > 0 {
			out = append(out, *row)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ReferenceableBytes != out[j].ReferenceableBytes {
			return out[i].ReferenceableBytes > out[j].ReferenceableBytes
		}
		if rankI, rankJ := wssShadowMirrorReplayShapeRank(out[i].RequestShape), wssShadowMirrorReplayShapeRank(out[j].RequestShape); rankI != rankJ {
			return rankI < rankJ
		}
		return out[i].Kind < out[j].Kind
	})
	return out
}

func wssShadowMirrorReplayShapeRank(shape string) int {
	switch strings.TrimSpace(shape) {
	case "full_history":
		return 0
	case "delta":
		return 1
	case "root":
		return 2
	default:
		return 3
	}
}

func percentInt(num, den int) float64 {
	if den <= 0 {
		return 0
	}
	return float64(num) * 100 / float64(den)
}

func bytesToTokenEstimate(bytes int) int {
	if bytes <= 0 {
		return 0
	}
	return (bytes + 3) / 4
}
