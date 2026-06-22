package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// l1SidecarFilename is the per-category sidecar file name read by the
// corpus evaluator to count L1 server-state continuation savings in the
// real_current_local_savings_ratio gate. It mirrors the
// command_output_first.jsonl pattern used for L2 T418 savings.
const l1SidecarFilename = "server_state_continuation.jsonl"

// l1SidecarRow is one JSON line in the L1 sidecar. It records a single
// delta turn where previous_response_id was used: the server-side full
// context size (input_tokens, what the client would have sent without
// continuation) and the actually-sent body tokens (output_tokens), with
// saved_tokens = input_tokens - output_tokens.
type l1SidecarRow struct {
	Timestamp    string `json:"ts"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	SavedTokens  int64  `json:"saved_tokens"`
}

// recordL1ServerStateSidecar appends one JSON line to a per-session
// server_state_continuation_<session>.jsonl file in
// ~/.slimference/analytics/. This sidecar is read by the corpus
// evaluator to count L1 server-state continuation savings in the
// real_current_local_savings_ratio gate. Failures are silent (fail-open).
func recordL1ServerStateSidecar(sessionID string, inputTokens, outputTokens, savedTokens int64) {
	if sessionID == "" || savedTokens <= 0 {
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		return
	}
	dir := filepath.Join(homeDir, ".slimference", "analytics")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	path := filepath.Join(dir, "server_state_continuation_"+sessionID+".jsonl")
	row := l1SidecarRow{
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		SavedTokens:  savedTokens,
	}
	data, err := json.Marshal(row)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_ = appendToFile(path, data, 0644)
}

// appendToFile appends data to path, creating it if it does not exist.
func appendToFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}
