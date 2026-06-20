// Package servermirror tracks, per WSS session, the content Slimference has
// already forwarded upstream (= what the OpenAI Responses server now holds along
// the previous_response_id chain). A later client frame can then be diffed
// against known server state to reference content the model provably already
// has, which is lossless and therefore zero-drawdown by construction.
//
// This is the SHADOW core (T254 design + shadow gate): it OBSERVES forwarded
// content and PREDICTS potential savings. It never mutates a frame. The
// mutation gate (actually replacing frames using the mirror) is a separate,
// later, live-proven step.
//
// Safety invariant (no-false-elision): Predict marks an exact block as
// already-on-server ONLY when its exact content hash was recorded by a prior
// Observe for the same session. The normalized shadow path applies the same rule
// to normalized text segments such as Codex exec payloads with volatile headers
// stripped. Eviction/bounding can only make Predict UNDER-report savings (mark a
// truly-forwarded block/segment as novel); it can never cause a false elision.
package servermirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// maxBlocksPerSession bounds the per-session hash set so a long conversation
// cannot grow memory without limit. When full, new hashes are not recorded,
// which only under-reports future savings (never a false elision).
const maxBlocksPerSession = 50000

// Mirror is a concurrency-safe, per-session record of forwarded content hashes.
type Mirror struct {
	mu                 sync.Mutex
	sessions           map[string]map[string]struct{}
	normalizedSessions map[string]map[string]struct{}
}

// New returns an empty Mirror.
func New() *Mirror {
	return &Mirror{
		sessions:           make(map[string]map[string]struct{}),
		normalizedSessions: make(map[string]map[string]struct{}),
	}
}

// Observe records the content of msgs as now held by the server for sessionID.
// Only non-empty text blocks are recorded. Call this with exactly the messages
// Slimference forwarded upstream (post-mutation), never with local file bytes or
// unforwarded content.
func (m *Mirror) Observe(sessionID string, msgs []types.Message) {
	if m == nil || sessionID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	set := m.sessions[sessionID]
	if set == nil {
		set = make(map[string]struct{})
		m.sessions[sessionID] = set
	}
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			if len(set) >= maxBlocksPerSession {
				return
			}
			set[hashContent(b.Text)] = struct{}{}
		}
	}
	normalizedSet := m.normalizedSessions[sessionID]
	if normalizedSet == nil {
		normalizedSet = make(map[string]struct{})
		m.normalizedSessions[sessionID] = normalizedSet
	}
	for _, segment := range normalizedSegments(msgs) {
		if len(normalizedSet) >= maxBlocksPerSession {
			return
		}
		normalizedSet[hashContent(segment.Text)] = struct{}{}
	}
}

// Prediction classifies one content block of a new client frame.
type Prediction struct {
	Block            int
	AlreadyForwarded bool
	Bytes            int
}

// SegmentPrediction classifies a normalized text segment of a new client frame.
// It is shadow-only: normalized segments are never substituted into a frame.
type SegmentPrediction struct {
	Block            int
	Segment          int
	Kind             string
	AlreadyForwarded bool
	Bytes            int
}

// SegmentKindReport groups normalized shadow predictions by content kind.
type SegmentKindReport struct {
	Segments              int
	ReferenceableSegments int
	Bytes                 int
	PotentialSavedBytes   int
}

// Report summarises a Predict pass. PotentialSavedBytes is the byte total of
// blocks the server already holds (referenceable losslessly); it is a SHADOW
// estimate, no frame is changed.
type Report struct {
	Blocks                              int
	BlockBytes                          int
	ReferenceableBlocks                 int
	PotentialSavedBytes                 int
	NormalizedSegments                  int
	NormalizedBytes                     int
	NormalizedReferenceableSegments     int
	NormalizedPotentialSavedBytes       int
	Predictions                         []Prediction
	NormalizedPredictions               []SegmentPrediction
	NormalizedPotentialSavedBytesByKind map[string]SegmentKindReport
}

// Predict reports, without mutating, which blocks of msgs the server already
// holds for sessionID (and are therefore losslessly referenceable) versus novel
// content that must be sent. It enforces no-false-elision: a block is marked
// AlreadyForwarded only if its exact content hash was previously Observed.
func (m *Mirror) Predict(sessionID string, msgs []types.Message) Report {
	var rep Report
	if m == nil || sessionID == "" {
		// Count blocks so callers still see the shape; nothing referenceable.
		for _, msg := range msgs {
			for _, b := range msg.Content {
				if b.Text == "" {
					continue
				}
				rep.Blocks++
			}
		}
		return rep
	}
	m.mu.Lock()
	set := m.sessions[sessionID]
	known := make(map[string]struct{}, len(set))
	for h := range set {
		known[h] = struct{}{}
	}
	normalizedSet := m.normalizedSessions[sessionID]
	normalizedKnown := make(map[string]struct{}, len(normalizedSet))
	for h := range normalizedSet {
		normalizedKnown[h] = struct{}{}
	}
	m.mu.Unlock()

	blockIdx := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			rep.Blocks++
			idx := blockIdx
			blockIdx++
			_, forwarded := known[hashContent(b.Text)]
			rep.BlockBytes += len(b.Text)
			rep.Predictions = append(rep.Predictions, Prediction{
				Block:            idx,
				AlreadyForwarded: forwarded,
				Bytes:            len(b.Text),
			})
			if forwarded {
				rep.ReferenceableBlocks++
				rep.PotentialSavedBytes += len(b.Text)
			}
		}
	}
	for _, segment := range normalizedSegments(msgs) {
		rep.NormalizedSegments++
		rep.NormalizedBytes += len(segment.Text)
		_, forwarded := normalizedKnown[hashContent(segment.Text)]
		rep.NormalizedPredictions = append(rep.NormalizedPredictions, SegmentPrediction{
			Block:            segment.Block,
			Segment:          segment.Segment,
			Kind:             segment.Kind,
			AlreadyForwarded: forwarded,
			Bytes:            len(segment.Text),
		})
		if rep.NormalizedPotentialSavedBytesByKind == nil {
			rep.NormalizedPotentialSavedBytesByKind = map[string]SegmentKindReport{}
		}
		kindReport := rep.NormalizedPotentialSavedBytesByKind[segment.Kind]
		kindReport.Segments++
		kindReport.Bytes += len(segment.Text)
		if forwarded {
			rep.NormalizedReferenceableSegments++
			rep.NormalizedPotentialSavedBytes += len(segment.Text)
			kindReport.ReferenceableSegments++
			kindReport.PotentialSavedBytes += len(segment.Text)
		}
		rep.NormalizedPotentialSavedBytesByKind[segment.Kind] = kindReport
	}
	return rep
}

// Reset clears a session's recorded state (e.g. on cache flush).
func (m *Mirror) Reset(sessionID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	delete(m.sessions, sessionID)
	delete(m.normalizedSessions, sessionID)
	m.mu.Unlock()
}

func hashContent(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

type normalizedSegment struct {
	Block   int
	Segment int
	Kind    string
	Text    string
}

func normalizedSegments(msgs []types.Message) []normalizedSegment {
	var out []normalizedSegment
	toolUses := toolUseIndexFromMessages(msgs)
	blockIdx := 0
	for _, msg := range msgs {
		for _, b := range msg.Content {
			if b.Text == "" {
				continue
			}
			resolved := blockWithResolvedToolUse(b, toolUses)
			if _, payload, ok := splitCodexExecEnvelope(b.Text); ok {
				out = append(out, normalizedSegment{
					Block:   blockIdx,
					Segment: 0,
					Kind:    normalizedCodexExecPayloadKind(resolved, payload),
					Text:    payload,
				})
			} else {
				out = append(out, normalizedSegment{
					Block:   blockIdx,
					Segment: 0,
					Kind:    normalizedSegmentKind(msg, resolved),
					Text:    b.Text,
				})
			}
			blockIdx++
		}
	}
	return out
}

func toolUseIndexFromMessages(msgs []types.Message) map[string]types.ContentBlock {
	var out map[string]types.ContentBlock
	for _, msg := range msgs {
		for _, block := range msg.Content {
			if strings.TrimSpace(block.Type) != "tool_use" {
				continue
			}
			id := strings.TrimSpace(block.ToolUseID)
			if id == "" {
				continue
			}
			if out == nil {
				out = make(map[string]types.ContentBlock)
			}
			out[id] = block
		}
	}
	return out
}

func blockWithResolvedToolUse(block types.ContentBlock, toolUses map[string]types.ContentBlock) types.ContentBlock {
	id := strings.TrimSpace(block.ToolResultID)
	if id == "" || len(toolUses) == 0 {
		return block
	}
	use, ok := toolUses[id]
	if !ok {
		return block
	}
	if strings.TrimSpace(block.ToolInput) == "" {
		block.ToolInput = use.ToolInput
	}
	if strings.TrimSpace(block.ToolName) == "" {
		block.ToolName = use.ToolName
	}
	if strings.TrimSpace(block.ToolUseID) == "" {
		block.ToolUseID = use.ToolUseID
	}
	return block
}

func normalizedSegmentKind(msg types.Message, block types.ContentBlock) string {
	if kind := normalizedToolResultKind(block); kind != "" {
		return kind
	}
	if kind := strings.TrimSpace(block.Type); kind != "" {
		return kind
	}
	if role := strings.TrimSpace(msg.Role); role != "" {
		return role
	}
	return "text"
}

func normalizedToolResultKind(block types.ContentBlock) string {
	if strings.TrimSpace(block.Type) != "tool_result" {
		return ""
	}
	if base := sanitizedCommandBase(commandLineFromToolInput(block.ToolInput)); base != "" {
		return "tool_result_command_" + base
	}
	if tool := sanitizeKindSuffix(block.ToolName); tool != "" {
		return "tool_result_tool_" + tool
	}
	return ""
}

func normalizedCodexExecPayloadKind(block types.ContentBlock, payload string) string {
	base := sanitizedCommandBase(commandLineFromToolInput(block.ToolInput))
	if base == "" {
		base = sanitizedCommandBase(inferCommandLineFromCodexExecPayload(payload))
	}
	if base == "" {
		return "codex_exec_payload"
	}
	return "codex_exec_payload_command_" + base
}

func inferCommandLineFromCodexExecPayload(payload string) string {
	if strings.TrimSpace(payload) == "" {
		return ""
	}
	switch {
	case payloadLooksLikeGoTestOutput(payload):
		return "go test"
	case payloadLooksLikeSARIFOutput(payload):
		return "sarif"
	case payloadLooksLikePackageInstallOutput(payload):
		return "npm install"
	case payloadLooksLikeTerraformPlanOutput(payload):
		return "terraform plan"
	case payloadLooksLikeKubectlGetOutput(payload):
		return "kubectl get"
	case payloadLooksLikeLsLongOutput(payload):
		return "ls -l"
	case payloadLooksLikeTreeOutput(payload):
		return "tree -L"
	case payloadLooksLikeSearchOutput(payload):
		return "rg"
	case payloadLooksLikeGitStatusOutput(payload):
		return "git status --short"
	case payloadLooksLikeGitShowStatOutput(payload):
		return "git show --stat"
	case payloadLooksLikeGitDiffStatOutput(payload):
		return "git diff --stat"
	case payloadLooksLikeGitDiffNameStatusOutput(payload):
		return "git diff --name-status"
	case payloadLooksLikeGitLogOnelineOutput(payload):
		return "git log --oneline"
	case payloadLooksLikeWcOutput(payload):
		return "wc -l"
	case payloadLooksLikePlainPathListOutput(payload):
		return "find"
	default:
		return ""
	}
}

func commandLineFromToolInput(input string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return ""
	}
	var raw string
	if err := json.Unmarshal([]byte(input), &raw); err == nil {
		return strings.TrimSpace(raw)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(input), &obj); err == nil {
		for _, key := range []string{"command", "cmd", "command_line", "cmdline", "commandLine", "shell_command", "shellCommand"} {
			if value := rawJSONString(obj[key]); value != "" {
				return value
			}
		}
		for _, key := range []string{"command", "argv", "args", "cmd_args", "command_args"} {
			if argv := rawStringArray(obj[key]); len(argv) > 0 {
				return strings.Join(argv, " ")
			}
		}
		return ""
	}
	return input
}

func rawJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func rawStringArray(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func sanitizedCommandBase(commandLine string) string {
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return ""
	}
	base := commandBaseFromFields(strings.Fields(commandLine))
	return sanitizeKindSuffix(base)
}

func sanitizeKindSuffix(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune('_')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func commandBaseFromFields(fields []string) string {
	if len(fields) == 0 {
		return ""
	}
	base := filepath.Base(strings.Trim(fields[0], `"'`))
	switch base {
	case "env":
		for _, field := range fields[1:] {
			if strings.HasPrefix(field, "-") || strings.Contains(field, "=") {
				continue
			}
			return commandBaseFromFields([]string{field})
		}
	case "bash", "sh", "zsh":
		for i := 1; i < len(fields)-1; i++ {
			if fields[i] == "-c" || fields[i] == "-lc" {
				return commandBaseFromFields(strings.Fields(strings.Trim(fields[i+1], `"'`)))
			}
		}
	}
	return base
}

func splitCodexExecEnvelope(text string) (header, payload string, ok bool) {
	if !strings.Contains(text, "Process exited with code ") {
		return "", "", false
	}
	for _, marker := range []string{"\nOutput:\n", "\r\nOutput:\r\n"} {
		idx := strings.Index(text, marker)
		if idx < 0 {
			continue
		}
		header = text[:idx+len(marker)]
		payload = text[idx+len(marker):]
		return header, payload, payload != ""
	}
	return "", "", false
}

func payloadLooksLikeGoTestOutput(payload string) bool {
	if strings.Contains(payload, "=== RUN") {
		for _, marker := range []string{"\n--- PASS:", "\n--- FAIL:", "\nFAIL\t", "\nPASS\n", "\nFAIL\n"} {
			if strings.Contains(payload, marker) {
				return true
			}
		}
	}

	nonEmpty := 0
	matches := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		fields := strings.Fields(line)
		if len(fields) >= 2 && (fields[0] == "ok" || fields[0] == "?" || fields[0] == "FAIL") &&
			strings.Contains(fields[1], "/") {
			matches++
		}
	}
	return matches >= 2 && matches*2 >= nonEmpty
}

func payloadLooksLikeSARIFOutput(payload string) bool {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(payload, "{") {
		return false
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &doc); err != nil {
		return false
	}
	var version string
	if err := json.Unmarshal(doc["version"], &version); err != nil || version != "2.1.0" {
		return false
	}
	var runs []json.RawMessage
	if err := json.Unmarshal(doc["runs"], &runs); err != nil || len(runs) == 0 {
		return false
	}
	var schema string
	if err := json.Unmarshal(doc["$schema"], &schema); err == nil && strings.Contains(strings.ToLower(schema), "sarif") {
		return true
	}
	for _, rawRun := range runs {
		var run map[string]json.RawMessage
		if err := json.Unmarshal(rawRun, &run); err == nil && (len(run["tool"]) > 0 || len(run["results"]) > 0) {
			return true
		}
	}
	return false
}

func payloadLooksLikePackageInstallOutput(payload string) bool {
	lower := strings.ToLower(payload)
	return strings.Contains(lower, "added ") &&
		strings.Contains(lower, " packages") &&
		strings.Contains(lower, "audited ") &&
		strings.Contains(lower, " vulnerabilities")
}

func payloadLooksLikeTerraformPlanOutput(payload string) bool {
	return strings.Contains(payload, "Terraform will perform the following actions:") &&
		strings.Contains(payload, "\nPlan:") &&
		(strings.Contains(payload, " to add") || strings.Contains(payload, " to change") || strings.Contains(payload, " to destroy"))
}

func payloadLooksLikeKubectlGetOutput(payload string) bool {
	nonEmpty := 0
	rows := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		nonEmpty++
		if nonEmpty == 1 {
			if len(fields) < 4 || fields[0] != "NAME" {
				return false
			}
			continue
		}
		if len(fields) >= 4 {
			rows++
		}
	}
	return rows >= 2
}

func payloadLooksLikeLsLongOutput(payload string) bool {
	nonEmpty := 0
	longRows := 0
	for len(payload) > 0 && nonEmpty < 32 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "total ") {
			continue
		}
		nonEmpty++
		fields := strings.Fields(line)
		if len(fields) >= 8 && looksLikeLsMode(fields[0]) && allDecimal(fields[1]) {
			longRows++
		}
	}
	return longRows >= 5 && longRows*2 >= nonEmpty
}

func looksLikeLsMode(value string) bool {
	if len(value) < 10 {
		return false
	}
	switch value[0] {
	case '-', 'd', 'l', 'b', 'c', 'p', 's':
	default:
		return false
	}
	for _, ch := range value[1:10] {
		if !strings.ContainsRune("rwxstST-", ch) {
			return false
		}
	}
	return true
}

func payloadLooksLikeTreeOutput(payload string) bool {
	if !(strings.Contains(payload, "\n├") || strings.Contains(payload, "\n└") || strings.Contains(payload, "\n|-- ") || strings.Contains(payload, "\n`-- ")) {
		return false
	}
	return strings.Contains(payload, " directories") && strings.Contains(payload, " files")
}

func payloadLooksLikeSearchOutput(payload string) bool {
	nonEmpty := 0
	matches := 0
	for len(payload) > 0 && nonEmpty < 12 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Total output lines:") {
			continue
		}
		nonEmpty++
		if payloadLooksLikeSearchResultLine(line) {
			matches++
		}
	}
	return matches >= 3 && matches*2 >= nonEmpty
}

func payloadLooksLikeSearchResultLine(line string) bool {
	first := strings.IndexByte(line, ':')
	if first <= 0 {
		return false
	}
	second := strings.IndexByte(line[first+1:], ':')
	if second <= 0 {
		return false
	}
	lineNo := line[first+1 : first+1+second]
	if lineNo == "" {
		return false
	}
	for _, ch := range lineNo {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	path := line[:first]
	return strings.TrimSpace(line[first+1+second+1:]) != "" &&
		(strings.Contains(path, "/") || strings.Contains(path, "."))
}

func payloadLooksLikeGitStatusOutput(payload string) bool {
	nonEmpty := 0
	statusLines := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty++
		if len(line) >= 4 && gitStatusXY(line[:2]) && (line[2] == ' ' || line[2] == '\t') {
			statusLines++
		}
	}
	return statusLines >= 3 && statusLines*2 >= nonEmpty
}

func gitStatusXY(value string) bool {
	if len(value) != 2 {
		return false
	}
	return gitStatusChar(value[0]) && gitStatusChar(value[1])
}

func gitStatusChar(ch byte) bool {
	return ch == ' ' || ch == '?' || ch == '!' || ch == 'M' || ch == 'A' ||
		ch == 'D' || ch == 'R' || ch == 'C' || ch == 'U' || ch == 'T'
}

func payloadLooksLikeGitDiffStatOutput(payload string) bool {
	return strings.Contains(payload, " | ") &&
		(strings.Contains(payload, " changed") || strings.Contains(payload, " insertion") || strings.Contains(payload, " deletion"))
}

func payloadLooksLikeGitShowStatOutput(payload string) bool {
	return (strings.HasPrefix(payload, "commit ") || strings.Contains(payload, "\ncommit ")) &&
		payloadLooksLikeGitDiffStatOutput(payload)
}

func payloadLooksLikeGitDiffNameStatusOutput(payload string) bool {
	nonEmpty := 0
	matches := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		if len(line) >= 3 && strings.ContainsRune("MADRCUT", rune(line[0])) && (line[1] == '\t' || line[1] == ' ') {
			matches++
		}
	}
	return matches >= 3 && matches*2 >= nonEmpty
}

func payloadLooksLikeGitLogOnelineOutput(payload string) bool {
	nonEmpty := 0
	matches := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		nonEmpty++
		fields := strings.Fields(line)
		if len(fields) >= 2 && len(fields[0]) >= 7 && isHex(fields[0]) {
			matches++
		}
	}
	return matches >= 3 && matches*2 >= nonEmpty
}

func isHex(value string) bool {
	for _, ch := range value {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')) {
			return false
		}
	}
	return value != ""
}

func payloadLooksLikeWcOutput(payload string) bool {
	nonEmpty := 0
	matches := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		nonEmpty++
		if allDecimal(fields[0]) {
			matches++
		}
	}
	return matches >= 2 && matches*2 >= nonEmpty
}

func allDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func payloadLooksLikePlainPathListOutput(payload string) bool {
	nonEmpty := 0
	pathLike := 0
	for len(payload) > 0 && nonEmpty < 24 {
		line := payload
		if idx := strings.IndexByte(payload, '\n'); idx >= 0 {
			line = payload[:idx]
			payload = payload[idx+1:]
		} else {
			payload = ""
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.ContainsAny(line, "\t:") || strings.Contains(line, " | ") {
			continue
		}
		nonEmpty++
		if strings.Contains(line, "/") || strings.Contains(line, ".") {
			pathLike++
		}
	}
	return pathLike >= 5 && pathLike*2 >= nonEmpty
}
