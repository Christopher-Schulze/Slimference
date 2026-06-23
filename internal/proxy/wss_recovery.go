package proxy

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	dbg "github.com/Christopher-Schulze/Slimference/internal/debug"
	"github.com/Christopher-Schulze/Slimference/internal/proxy/wsmitm"
	"github.com/Christopher-Schulze/Slimference/internal/toolprune"
	"github.com/Christopher-Schulze/Slimference/internal/types"
	"github.com/Christopher-Schulze/Slimference/internal/wscompact"
)

const wssRecoveryMaxChains = 256

type wssResponseChain []json.RawMessage

type wssRecoveryCandidate struct {
	RecoveryID         string
	SessionID          string
	PreviousResponseID string
	Model              string
	ClientFamily       string
	RetryPayload       []byte
	RetryBody          []byte
	ChainItems         int
	CurrentInputItems  int
	OriginalBytes      int
	RetryBytes         int
	ToolPruneApplied   bool
	ToolPrunePruned    int
	ToolPruneSaved     int
	Used               bool
}

type lockedWriteConn struct {
	net.Conn
	mu sync.Mutex
}

func newLockedWriteConn(conn net.Conn) net.Conn {
	if conn == nil {
		return nil
	}
	return &lockedWriteConn{Conn: conn}
}

func (c *lockedWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Conn.Write(p)
}

func writeMaskedWSTextFrame(w io.Writer, payload []byte) error {
	if w == nil {
		return errors.New("wss recovery writer is nil")
	}
	mask := make([]byte, 4)
	if _, err := rand.Read(mask); err != nil {
		return err
	}
	_, err := wscompact.WriteFrame(w, true, wscompact.OpcodeText, mask, payload)
	return err
}

func (a *wsPhaseFAdapter) setRecoveryWriter(writer func([]byte) error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.recoveryWriter = writer
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) markWSSHistoryMutationRecoveryLineage(responseID string) {
	if a == nil || strings.TrimSpace(responseID) == "" {
		return
	}
	a.mu.Lock()
	a.markWSSHistoryMutationRecoveryLineageLocked(responseID)
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) markWSSHistoryMutationRecoveryLineageLocked(responseID string) {
	responseID = strings.TrimSpace(responseID)
	if responseID == "" {
		return
	}
	if a.historyRecoveryGuardedResponseIDs == nil {
		a.historyRecoveryGuardedResponseIDs = make(map[string]struct{})
	}
	a.historyRecoveryGuardedResponseIDs[responseID] = struct{}{}
	if len(a.historyRecoveryGuardedResponseIDs) > wssRecoveryMaxChains {
		for id := range a.historyRecoveryGuardedResponseIDs {
			delete(a.historyRecoveryGuardedResponseIDs, id)
			break
		}
	}
}

func (a *wsPhaseFAdapter) rememberWSSHistoryRecoveryGuardRequest(guarded bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.pendingHistoryRecoveryGuarded = guarded
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) wssHistoryMutationRecoveryGuarded(previousResponseID string) bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if strings.TrimSpace(previousResponseID) == "" || len(a.historyRecoveryGuardedResponseIDs) == 0 {
		return false
	}
	_, ok := a.historyRecoveryGuardedResponseIDs[previousResponseID]
	return ok
}

func (a *wsPhaseFAdapter) markWSSHistoryStatelessMode() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.historyStatelessMode = true
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) wssHistoryStatelessMode() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.historyStatelessMode
}

func (a *wsPhaseFAdapter) wssStatelessHistoryContinuationBody(body []byte) ([]byte, bool) {
	if a == nil || len(body) == 0 {
		return body, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body, false
	}
	previousResponseID := wssPreviousResponseIDFromRaw(raw)
	if previousResponseID == "" {
		return body, false
	}
	currentInput, ok := wssInputItems(body)
	if !ok || len(currentInput) == 0 {
		return body, false
	}
	prior := a.wssStatelessContinuationChain(previousResponseID)
	if len(prior) == 0 {
		return body, false
	}
	fullInput := append(cloneWSSRawItems(prior), currentInput...)
	rewritten, ok := wssBodyWithInput(body, fullInput, true)
	if !ok {
		return body, false
	}
	return rewritten, true
}

func (a *wsPhaseFAdapter) prepareWSSRecoveryCandidate(env *wsmitm.Envelope, body []byte, meta wssRequestMeta) {
	if a == nil || env == nil || len(body) == 0 {
		return
	}
	currentInput, ok := wssInputItems(body)
	if !ok || len(currentInput) == 0 {
		a.clearPendingWSSRecovery()
		return
	}

	fullInput := cloneWSSRawItems(currentInput)
	var candidate *wssRecoveryCandidate
	if meta.ToolPrune.Applied {
		candidate = wssRecoveryCandidateFromBody(env, body, meta, fullInput, meta.PreviousResponseID != "", 0, len(currentInput))
	}
	if meta.PreviousResponseID != "" {
		prior := a.wssResponseChain(meta.PreviousResponseID)
		if len(prior) > 0 {
			fullInput = append(cloneWSSRawItems(prior), currentInput...)
			candidate = wssRecoveryCandidateFromBody(env, body, meta, fullInput, true, len(prior), len(currentInput))
		}
	}

	// Always export the chain to the persistent store. The chain is needed
	// for delta recovery on the NEXT turn: if a new WebSocket connection
	// (new adapter) handles the next turn, the in-memory store is empty and
	// only the persistent store has the chain. Without this, the first turn
	// of any session never exports its chain, so the second turn (delta with
	// previous_response_id) can never find it — blocking all delta mutation.
	exportStatelessChain := true
	a.mu.Lock()
	a.pendingChain = cloneWSSRawItems(fullInput)
	a.pendingOutput = nil
	a.pendingRecovery = candidate
	a.pendingStatelessChainExport = exportStatelessChain
	a.mu.Unlock()
}

func wssRecoveryCandidateFromBody(env *wsmitm.Envelope, body []byte, meta wssRequestMeta, input []json.RawMessage, dropPreviousResponse bool, chainItems, currentInputItems int) *wssRecoveryCandidate {
	if len(body) == 0 || len(input) == 0 {
		return nil
	}
	retryBody, bodyOK := wssBodyWithInput(body, input, dropPreviousResponse)
	if !bodyOK {
		return nil
	}
	payload, payloadOK := wssRetryEnvelopePayload(env, retryBody)
	if !payloadOK {
		return nil
	}
	return &wssRecoveryCandidate{
		SessionID:          meta.SessionID,
		PreviousResponseID: meta.PreviousResponseID,
		Model:              meta.Model,
		ClientFamily:       firstNonEmpty(meta.ClientFamily, "codex"),
		RetryPayload:       payload,
		RetryBody:          retryBody,
		ChainItems:         chainItems,
		CurrentInputItems:  currentInputItems,
		OriginalBytes:      len(body),
		RetryBytes:         len(retryBody),
		ToolPruneApplied:   meta.ToolPrune.Applied,
		ToolPrunePruned:    meta.ToolPrune.PrunedTools,
		ToolPruneSaved:     meta.ToolPrune.SavedTokens,
	}
}

func (a *wsPhaseFAdapter) clearPendingWSSRecovery() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.pendingChain = nil
	a.pendingOutput = nil
	a.pendingRecovery = nil
	a.pendingStatelessChainExport = false
	a.mu.Unlock()
}

func (a *wsPhaseFAdapter) rememberWSSResponseState(env *wsmitm.Envelope) {
	if a == nil || env == nil {
		return
	}
	if env.Kind == wsmitm.FrameKindResponseOutputItemDone && len(env.Item) > 0 {
		a.mu.Lock()
		a.pendingOutput = append(a.pendingOutput, append(json.RawMessage(nil), env.Item...))
		a.mu.Unlock()
		return
	}
	if env.Kind != wsmitm.FrameKindResponseCompleted || len(env.Response) == 0 {
		return
	}
	responseID := wssResponseID(env.Response)
	if responseID == "" {
		return
	}
	output := wssResponseOutputItems(env.Response)
	var exportResponseID string
	var exportChain wssResponseChain

	a.mu.Lock()
	if len(output) == 0 {
		output = cloneWSSRawItems(a.pendingOutput)
	}
	if len(a.pendingChain) > 0 {
		chain := append(cloneWSSRawItems(a.pendingChain), output...)
		if a.responseChains == nil {
			a.responseChains = make(map[string]wssResponseChain)
		}
		// Track insertion order for FIFO eviction BEFORE inserting into the
		// map so the existence check is meaningful (otherwise the just-inserted
		// key would always report exists=true and never be appended to the
		// order slice, breaking eviction entirely).
		if _, exists := a.responseChains[responseID]; !exists {
			a.responseChainOrder = append(a.responseChainOrder, responseID)
		}
		a.responseChains[responseID] = chain
		if a.pendingHistoryRecoveryGuarded {
			a.markWSSHistoryMutationRecoveryLineageLocked(responseID)
		}
		if a.pendingStatelessChainExport {
			exportResponseID = responseID
			exportChain = wssResponseChain(cloneWSSRawItems(chain))
		}
		for len(a.responseChainOrder) > wssRecoveryMaxChains {
			oldest := a.responseChainOrder[0]
			a.responseChainOrder = a.responseChainOrder[1:]
			delete(a.responseChains, oldest)
		}
	}
	a.pendingHistoryRecoveryGuarded = false
	a.pendingStatelessChainExport = false
	a.pendingOutput = nil
	a.mu.Unlock()
	if exportResponseID != "" && a.p != nil {
		a.p.rememberWSSStatelessChain(exportResponseID, exportChain)
	}
}

func (a *wsPhaseFAdapter) tryWSSRecoveryRetry(status, errorType, message, errSummary string) bool {
	if a == nil || !wssRetryableInvalidRequest(status, errorType, message) {
		return false
	}
	a.mu.Lock()
	candidate := cloneWSSRecoveryCandidate(a.pendingRecovery)
	writer := a.recoveryWriter
	if a.pendingRecovery != nil && !a.pendingRecovery.Used {
		a.pendingRecovery.Used = true
	}
	a.mu.Unlock()

	if candidate == nil || candidate.Used || len(candidate.RetryPayload) == 0 || writer == nil {
		return false
	}
	missingToolRetry := candidate.ToolPruneApplied && wssLooksLikeMissingToolDefinitionError(status, message)
	if candidate.PreviousResponseID == "" && candidate.ToolPruneApplied && !missingToolRetry {
		return false
	}
	candidate.RecoveryID = newRequestIDFn()
	if err := writer(candidate.RetryPayload); err != nil {
		a.recordWSSRecoveryEvent("wss_upstream_recovery_failed", candidate, errSummary, map[string]string{
			"wss.recovery.phase": "send",
			"wss.recovery.error": err.Error(),
		})
		return false
	}
	if missingToolRetry && a.p != nil && a.p.toolPrune != nil {
		a.p.toolPrune.MarkMiss(candidate.SessionID)
		a.p.toolPrune.MarkRetry()
	}

	a.mu.Lock()
	a.activeRecovery = cloneWSSRecoveryCandidate(candidate)
	a.recoveryAccepted = false
	a.recoveryResponseID = ""
	a.markWSSHistoryMutationRecoveryLineageLocked(candidate.PreviousResponseID)
	a.historyStatelessMode = true
	a.mu.Unlock()

	a.recordWSSRecoveryEvent("wss_upstream_recovery_retry", candidate, errSummary, map[string]string{
		"wss.recovery.phase":                          "retry_sent",
		"wss.recovery.tool_prune_missing_tool":        strconv.FormatBool(missingToolRetry),
		"wss.recovery.stateless_history_continuation": "true",
	})
	slog.Info("codex wss upstream error recovered by full-context retry",
		"recovery_id", candidate.RecoveryID,
		"session", candidate.SessionID,
		"previous_response_id", candidate.PreviousResponseID,
		"chain_items", candidate.ChainItems,
		"current_input_items", candidate.CurrentInputItems,
		"retry_bytes", candidate.RetryBytes)
	return true
}

func wssLooksLikeMissingToolDefinitionError(status, message string) bool {
	statusCode, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		return false
	}
	return toolprune.LooksLikeMissingToolError(statusCode, []byte(message))
}

func (a *wsPhaseFAdapter) observeWSSRecoveryResponse(env *wsmitm.Envelope) {
	if a == nil || env == nil {
		return
	}
	if env.Kind == wsmitm.FrameKindUnknown || env.Kind.IsControl() ||
		env.Kind == wsmitm.FrameKindError || env.Kind == wsmitm.FrameKindResponseFailed ||
		env.Kind == wsmitm.FrameKindResponseIncomplete {
		return
	}

	responseID := wssResponseID(env.Response)
	var accepted *wssRecoveryCandidate
	var succeeded *wssRecoveryCandidate
	var acceptedFacts map[string]string
	var succeededFacts map[string]string

	a.mu.Lock()
	if a.activeRecovery != nil {
		if responseID != "" {
			a.recoveryResponseID = responseID
		}
		if !a.recoveryAccepted {
			a.recoveryAccepted = true
			accepted = cloneWSSRecoveryCandidate(a.activeRecovery)
			acceptedFacts = map[string]string{
				"wss.recovery.phase":          "accepted",
				"wss.recovery.accepted_frame": string(env.Kind),
				"wss.recovery.accepted":       "true",
				"wss.recovery.response_id":    a.recoveryResponseID,
			}
		}
		if env.Kind == wsmitm.FrameKindResponseCompleted {
			succeeded = cloneWSSRecoveryCandidate(a.activeRecovery)
			if responseID != "" {
				a.markWSSHistoryMutationRecoveryLineageLocked(responseID)
			}
			a.historyStatelessMode = true
			succeededFacts = map[string]string{
				"wss.recovery.phase":                          "completed",
				"wss.recovery.terminal_frame":                 string(env.Kind),
				"wss.recovery.accepted":                       strconv.FormatBool(a.recoveryAccepted),
				"wss.recovery.response_id":                    a.recoveryResponseID,
				"wss.recovery.stateless_history_continuation": "true",
			}
			a.activeRecovery = nil
			a.recoveryAccepted = false
			a.recoveryResponseID = ""
		}
	}
	a.mu.Unlock()

	if accepted != nil {
		a.recordWSSRecoveryEvent("wss_upstream_recovery_accepted", accepted, "", acceptedFacts)
	}
	if succeeded != nil {
		a.recordWSSRecoveryEvent("wss_upstream_recovery_succeeded", succeeded, "", succeededFacts)
	}
}

func (a *wsPhaseFAdapter) failActiveWSSRecovery(errSummary, status, errorType, message string, kind wsmitm.FrameKind) bool {
	if a == nil {
		return false
	}
	var failed *wssRecoveryCandidate
	a.mu.Lock()
	if a.activeRecovery != nil {
		failed = cloneWSSRecoveryCandidate(a.activeRecovery)
		a.activeRecovery = nil
		a.recoveryAccepted = false
		a.recoveryResponseID = ""
	}
	a.mu.Unlock()
	if failed == nil {
		return false
	}
	a.recordWSSRecoveryEvent("wss_upstream_recovery_failed", failed, errSummary, map[string]string{
		"wss.recovery.phase":        "upstream_rejected_retry",
		"wss.recovery.error_kind":   string(kind),
		"wss.recovery.error_status": status,
		"wss.recovery.error_type":   errorType,
		"wss.recovery.error_msg":    message,
	})
	return true
}

func (a *wsPhaseFAdapter) wssRecoveryDebugFacts(status, errorType, message string) map[string]string {
	retryable := wssRetryableInvalidRequest(status, errorType, message)
	facts := map[string]string{
		"wss.recovery.retryable": strconv.FormatBool(retryable),
	}
	if a == nil {
		facts["wss.recovery.no_retry_reason"] = "adapter_nil"
		return facts
	}
	if !retryable {
		facts["wss.recovery.no_retry_reason"] = "not_retryable"
		return facts
	}

	a.mu.Lock()
	candidate := cloneWSSRecoveryCandidate(a.pendingRecovery)
	writerSet := a.recoveryWriter != nil
	a.mu.Unlock()

	facts["wss.recovery.writer_present"] = strconv.FormatBool(writerSet)
	facts["wss.recovery.candidate_present"] = strconv.FormatBool(candidate != nil)
	if candidate == nil {
		facts["wss.recovery.no_retry_reason"] = "candidate_missing"
		return facts
	}
	facts["wss.recovery.candidate_used"] = strconv.FormatBool(candidate.Used)
	facts["wss.recovery.previous_response_id"] = candidate.PreviousResponseID
	facts["wss.recovery.chain_items"] = strconv.Itoa(candidate.ChainItems)
	facts["wss.recovery.current_input_items"] = strconv.Itoa(candidate.CurrentInputItems)
	if candidate.Used {
		facts["wss.recovery.no_retry_reason"] = "candidate_already_used"
		return facts
	}
	if len(candidate.RetryPayload) == 0 {
		facts["wss.recovery.no_retry_reason"] = "retry_payload_missing"
		return facts
	}
	if !writerSet {
		facts["wss.recovery.no_retry_reason"] = "writer_missing"
		return facts
	}
	facts["wss.recovery.no_retry_reason"] = "none"
	return facts
}

func wssRetryableInvalidRequest(status, errorType, message string) bool {
	status = strings.TrimSpace(status)
	if status != "" && status != "400" {
		return false
	}
	errorType = strings.TrimSpace(errorType)
	if errorType != "" && errorType != "invalid_request_error" {
		return false
	}
	low := strings.ToLower(strings.TrimSpace(message))
	if low == "" || low == "invalid request" {
		return true
	}
	blocked := []string{
		"context window",
		"connection limit",
		"rate limit",
		"unauthorized",
		"forbidden",
		"authentication",
	}
	for _, marker := range blocked {
		if strings.Contains(low, marker) {
			return false
		}
	}
	if wssLooksLikeMissingToolDefinitionError(status, low) {
		return true
	}
	return strings.Contains(low, "previous_response_id") ||
		strings.Contains(low, "response not found") ||
		strings.Contains(low, "conversation not found") ||
		strings.Contains(low, "unknown response")
}

func (a *wsPhaseFAdapter) wssResponseChain(responseID string) wssResponseChain {
	if a == nil || responseID == "" {
		return nil
	}
	a.mu.Lock()
	chain := wssResponseChain(cloneWSSRawItems(a.responseChains[responseID]))
	a.mu.Unlock()
	if len(chain) > 0 {
		return chain
	}
	if a.p == nil {
		return nil
	}
	return a.p.wssStatelessChain(responseID)
}

func (a *wsPhaseFAdapter) wssStatelessContinuationChain(responseID string) wssResponseChain {
	if a == nil || responseID == "" {
		return nil
	}
	if a.wssHistoryStatelessMode() {
		return a.wssResponseChain(responseID)
	}
	if a.p == nil {
		return nil
	}
	return a.p.wssStatelessChain(responseID)
}

func wssInputItems(body []byte) ([]json.RawMessage, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false
	}
	inputRaw := raw["input"]
	if len(inputRaw) == 0 {
		return nil, false
	}
	var items []json.RawMessage
	if err := json.Unmarshal(inputRaw, &items); err != nil {
		return nil, false
	}
	return cloneWSSRawItems(items), true
}

func wssBodyWithInput(body []byte, input []json.RawMessage, dropPreviousResponse bool) ([]byte, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, false
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, false
	}
	raw["input"] = inputJSON
	if dropPreviousResponse {
		delete(raw, "previous_response_id")
	}
	out, err := json.Marshal(raw)
	return out, err == nil
}

func wssRetryEnvelopePayload(env *wsmitm.Envelope, retryBody []byte) ([]byte, bool) {
	if env == nil || len(retryBody) == 0 {
		return nil, false
	}
	clone := *env
	if env.Fields != nil {
		clone.Fields = make(map[string]json.RawMessage, len(env.Fields))
		for k, v := range env.Fields {
			clone.Fields[k] = append(json.RawMessage(nil), v...)
		}
	}
	_, replace, ok := wsRequestBody(&clone)
	if !ok {
		return nil, false
	}
	if err := replace(retryBody); err != nil {
		return nil, false
	}
	out, err := clone.Marshal()
	return out, err == nil
}

func wssResponseID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return ""
	}
	return rawJSONString(response["id"])
}

func wssResponseOutputItems(raw json.RawMessage) []json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(raw, &response); err != nil {
		return nil
	}
	var output []json.RawMessage
	if err := json.Unmarshal(response["output"], &output); err != nil {
		return nil
	}
	return cloneWSSRawItems(output)
}

func cloneWSSRawItems(items []json.RawMessage) []json.RawMessage {
	if len(items) == 0 {
		return nil
	}
	out := make([]json.RawMessage, len(items))
	for i, item := range items {
		out[i] = append(json.RawMessage(nil), item...)
	}
	return out
}

func cloneWSSRecoveryCandidate(in *wssRecoveryCandidate) *wssRecoveryCandidate {
	if in == nil {
		return nil
	}
	out := *in
	out.RetryPayload = append([]byte(nil), in.RetryPayload...)
	out.RetryBody = append([]byte(nil), in.RetryBody...)
	return &out
}

func (a *wsPhaseFAdapter) recordWSSRecoveryEvent(reason string, candidate *wssRecoveryCandidate, errSummary string, extra map[string]string) {
	if a == nil || a.p == nil || a.p.debugRecorder == nil || candidate == nil {
		return
	}
	facts := map[string]string{
		"wss.recovery.id":                   candidate.RecoveryID,
		"wss.recovery.previous_response_id": candidate.PreviousResponseID,
		"wss.recovery.chain_items":          strconv.Itoa(candidate.ChainItems),
		"wss.recovery.current_input_items":  strconv.Itoa(candidate.CurrentInputItems),
		"wss.recovery.original_bytes":       strconv.Itoa(candidate.OriginalBytes),
		"wss.recovery.retry_bytes":          strconv.Itoa(candidate.RetryBytes),
		"wss.recovery.tool_prune_applied":   strconv.FormatBool(candidate.ToolPruneApplied),
		"wss.recovery.tool_prune_pruned":    strconv.Itoa(candidate.ToolPrunePruned),
		"wss.recovery.tool_prune_saved":     strconv.Itoa(candidate.ToolPruneSaved),
	}
	maps.Copy(facts, extra)
	summary := dbg.RequestSummary{
		RequestID:    newRequestIDFn(),
		Timestamp:    time.Now(),
		SessionID:    candidate.SessionID,
		TurnID:       candidate.PreviousResponseID,
		Source:       "proxy",
		Provider:     types.CodexChatGPT.String(),
		Path:         "/backend-api/codex/responses",
		ClientFamily: firstNonEmpty(candidate.ClientFamily, a.summaryClientFamily(), "codex"),
		RouteMode:    "websocket_phasef",
		BypassReason: reason,
		Model:        candidate.Model,
		DebugFacts:   facts,
	}
	if errSummary != "" {
		summary.Errors = []string{errSummary}
	}
	a.p.debugRecorder.Record(summary)
}
