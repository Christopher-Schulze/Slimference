package summarization

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/slimference/slimference/internal/security"
	"github.com/slimference/slimference/internal/types"
)

// RedactionMode controls how aggressively outbound content is sanitised
// before it leaves the proxy for an external summarisation provider. The
// values map 1:1 to the [compression.summary] outbound_redaction config
// field.
const (
	RedactionModeOff     = "off"
	RedactionModeDefault = "default"
	RedactionModeStrict  = "strict"
)

// RedactOptions describes one redactor invocation.
type RedactOptions struct {
	// Mode is one of RedactionMode* constants. Empty defaults to
	// RedactionModeDefault.
	Mode string
	// HomeDir is the operator's home directory. When non-empty, absolute
	// paths under this prefix are normalised to <HOME>. Detection of
	// /home/<seg> and /Users/<seg> shapes happens regardless via regex.
	HomeDir string
	// ExtraPatterns are operator-supplied secret patterns appended to the
	// detector's defaults. They run with the same allowlist semantics as
	// the built-in patterns.
	ExtraPatterns []security.SecretPattern
	// Detector is the security detector reused for pattern sweeping. When
	// nil, the redactor builds a fresh one in "redact" mode from the
	// supplied ExtraPatterns. Reusing the detector keeps the pattern
	// inventory consistent with the inbound proxy hot-path.
	Detector *security.Detector
	// ReplaceAbsPaths toggles absolute-path normalisation. Default true
	// under modes != off.
	ReplaceAbsPaths bool
	// StripAuthHeaders toggles Authorization/Cookie/Set-Cookie/X-Api-Key
	// stripping in tool-result text. Default true under modes != off.
	StripAuthHeaders bool
	// DropToolInputs toggles full removal of tool_input bodies (only the
	// tool name is kept). Activates under RedactionModeStrict.
	DropToolInputs bool
}

// RedactStats records what the redactor did to a single Redact call.
// All fields are cumulative across the messages slice; zero values mean
// no redaction of that kind happened.
type RedactStats struct {
	SecretsRedacted   int
	PathsNormalised   int
	HeadersStripped   int
	JSONKeyRedacted   int
	ToolInputsDropped int
}

// Redactor sanitises a message slice for outbound summarisation. It is
// safe for concurrent use as long as the supplied Detector is.
type Redactor struct {
	opts    RedactOptions
	homeRe  *regexp.Regexp
	tmpRe   *regexp.Regexp
	headers []*regexp.Regexp
	jsonRe  *regexp.Regexp
}

// authHeaderNames is the canonical list of HTTP header names whose
// values must never reach an external summariser.
var authHeaderNames = []string{
	"Authorization",
	"Cookie",
	"Set-Cookie",
	"X-Api-Key",
	"X-Auth-Token",
	"Proxy-Authorization",
}

// jsonCredentialKeys is the set of JSON object keys whose values are
// always replaced with [REDACTED] in outbound content.
var jsonCredentialKeys = []string{
	"api_key", "apikey", "api-key",
	"access_token", "auth_token", "authtoken",
	"client_secret", "client_id",
	"password", "passwd", "secret",
	"token",
	"refresh_token",
	"private_key",
	"id_token",
}

// NewRedactor returns a Redactor configured per opts. Callers should
// reuse one Redactor for the lifetime of a Layer2 instance.
func NewRedactor(opts RedactOptions) *Redactor {
	if opts.Mode == "" {
		opts.Mode = RedactionModeDefault
	}
	r := &Redactor{opts: opts}

	switch opts.Mode {
	case RedactionModeOff:
		return r
	case RedactionModeStrict:
		opts.DropToolInputs = true
		fallthrough
	case RedactionModeDefault:
		if !opts.ReplaceAbsPaths {
			opts.ReplaceAbsPaths = true
		}
		if !opts.StripAuthHeaders {
			opts.StripAuthHeaders = true
		}
	}
	r.opts = opts

	if opts.Detector == nil {
		r.opts.Detector = security.NewDetector("redact", opts.ExtraPatterns, nil)
	}

	// Path normalisation regexes. We deliberately split the home matcher
	// from the tmp matcher so the placeholders stay distinguishable.
	r.homeRe = regexp.MustCompile(`(?:/Users/[^/\s"':;,]+|/home/[^/\s"':;,]+|[A-Za-z]:\\Users\\[^\\\s"':;,]+)`)
	r.tmpRe = regexp.MustCompile(`/var/folders/[A-Za-z0-9_]{2}/[A-Za-z0-9_]+/[A-Z]/?`)

	// Per-header line matchers. Match the header name + colon + value
	// terminated by newline OR end-of-string.
	r.headers = make([]*regexp.Regexp, 0, len(authHeaderNames))
	for _, name := range authHeaderNames {
		// (?im) - case-insensitive, multi-line.
		re := regexp.MustCompile(`(?im)^[\t ]*` + regexp.QuoteMeta(name) + `[\t ]*:[\t ]*[^\r\n]+`)
		r.headers = append(r.headers, re)
	}

	// JSON credential key matcher. Captures the key in group 1 so we can
	// rebuild the line with the redacted value.
	jsonAlt := strings.Join(quoteAll(jsonCredentialKeys), "|")
	r.jsonRe = regexp.MustCompile(`(?i)"(` + jsonAlt + `)"\s*:\s*("(?:\\.|[^"\\])*"|[^,}\s]+)`)

	return r
}

// quoteAll escapes regex metachars in each entry. Used when building an
// alternation group from a static list.
func quoteAll(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = regexp.QuoteMeta(s)
	}
	return out
}

// Redact returns a sanitised slice plus stats. The original slice is never
// mutated in redacting modes. Under RedactionModeOff the function returns the
// input slice unchanged, avoiding hot-path copies when the operator explicitly
// disabled outbound redaction.
func (r *Redactor) Redact(messages []types.Message) ([]types.Message, RedactStats) {
	if len(messages) == 0 {
		return messages, RedactStats{}
	}
	if r.opts.Mode == RedactionModeOff {
		return messages, RedactStats{}
	}
	out := make([]types.Message, 0, len(messages))
	var stats RedactStats

	for _, msg := range messages {
		copyMsg := msg
		copyMsg.Content = make([]types.ContentBlock, 0, len(msg.Content))

		for _, block := range msg.Content {
			redactedBlock, blockStats := r.redactBlock(block)
			stats.SecretsRedacted += blockStats.SecretsRedacted
			stats.PathsNormalised += blockStats.PathsNormalised
			stats.HeadersStripped += blockStats.HeadersStripped
			stats.JSONKeyRedacted += blockStats.JSONKeyRedacted
			stats.ToolInputsDropped += blockStats.ToolInputsDropped
			copyMsg.Content = append(copyMsg.Content, redactedBlock)
		}
		out = append(out, copyMsg)
	}
	return out, stats
}

// redactBlock returns a sanitised copy of a single content block. The
// returned block is independent of the input.
func (r *Redactor) redactBlock(block types.ContentBlock) (types.ContentBlock, RedactStats) {
	out := block
	var stats RedactStats

	if block.Text != "" {
		redacted, blockStats := r.redactText(block.Text)
		out.Text = redacted
		stats.SecretsRedacted += blockStats.SecretsRedacted
		stats.PathsNormalised += blockStats.PathsNormalised
		stats.HeadersStripped += blockStats.HeadersStripped
		stats.JSONKeyRedacted += blockStats.JSONKeyRedacted
	}
	if block.ToolInput != "" {
		if r.opts.DropToolInputs {
			out.ToolInput = ""
			stats.ToolInputsDropped++
		} else {
			redacted, blockStats := r.redactText(block.ToolInput)
			out.ToolInput = redacted
			stats.SecretsRedacted += blockStats.SecretsRedacted
			stats.PathsNormalised += blockStats.PathsNormalised
			stats.HeadersStripped += blockStats.HeadersStripped
			stats.JSONKeyRedacted += blockStats.JSONKeyRedacted
		}
	}
	return out, stats
}

// redactText runs the full cleanup pipeline on a single string. Order
// matters: structural first (headers, then JSON keys), then pattern-
// based secrets (which can include "(?i)token\s*[:=]" rules that would
// otherwise corrupt header line shapes before the header stripper
// could see them), then path normalisation (broadest, runs last so
// placeholder text is consistent). Each stage operates on the output
// of the previous one. Caller guarantees text != "".
func (r *Redactor) redactText(text string) (string, RedactStats) {
	var stats RedactStats
	out := text

	// 1) HTTP-style header lines. Runs before the pattern sweep so the
	// header *value* is already a static [REDACTED] placeholder by the
	// time generic credential-substring patterns get a chance to chew
	// on it. Without this ordering, a pattern that matches the value
	// would dissolve the surrounding header name and the line shape
	// would be lost.
	if r.opts.StripAuthHeaders {
		for _, headerRe := range r.headers {
			matches := headerRe.FindAllString(out, -1)
			if len(matches) == 0 {
				continue
			}
			out = headerRe.ReplaceAllStringFunc(out, func(line string) string {
				colon := strings.Index(line, ":")
				return line[:colon+1] + " [REDACTED]"
			})
			stats.HeadersStripped += len(matches)
		}
	}

	// 2) JSON credential keys. Same logic as headers: a structural
	// match is more reliable than a byte-pattern match, so we resolve
	// the structure first.
	if r.jsonRe != nil {
		matches := r.jsonRe.FindAllStringIndex(out, -1)
		if len(matches) > 0 {
			out = r.jsonRe.ReplaceAllStringFunc(out, func(match string) string {
				colon := strings.Index(match, ":")
				return match[:colon+1] + ` "[REDACTED]"`
			})
			stats.JSONKeyRedacted += len(matches)
		}
	}

	// 3) Pattern-based secrets via the security detector. Picks up any
	// remaining tokens, keys, or high-entropy strings that survived the
	// structural passes (free-form text, code listings, dumped env
	// vars, etc.).
	if r.opts.Detector != nil {
		redacted, dets, _ := r.opts.Detector.ScanMessages([]types.Message{
			{Index: 0, Role: "system", Content: []types.ContentBlock{{Type: "text", Text: out}}},
		})
		if len(redacted) == 1 && len(redacted[0].Content) == 1 {
			out = redacted[0].Content[0].Text
		}
		stats.SecretsRedacted += len(dets)
	}

	// 4) Absolute-path normalisation. Replace home/tmp prefixes with
	// stable placeholders so the model can still correlate references
	// without seeing the operator's filesystem layout.
	if r.opts.ReplaceAbsPaths {
		homeMatches := r.homeRe.FindAllString(out, -1)
		if len(homeMatches) > 0 {
			out = r.homeRe.ReplaceAllStringFunc(out, func(_ string) string {
				return "<HOME>"
			})
			stats.PathsNormalised += len(homeMatches)
		}
		tmpMatches := r.tmpRe.FindAllString(out, -1)
		if len(tmpMatches) > 0 {
			out = r.tmpRe.ReplaceAllStringFunc(out, func(_ string) string {
				return "<TMP>/"
			})
			stats.PathsNormalised += len(tmpMatches)
		}
	}

	// 5) Strict-mode structural JSON sweep. When the (now-redacted)
	// content parses as JSON we walk the document and replace every
	// credential-keyed value at any depth. This catches outputs that
	// nest a credential inside a deep tree the line scanner would
	// miss (e.g. a wrapped API response with nested auth blocks).
	// Mode==strict is opt-in; default mode skips this pass to avoid
	// re-parsing every textual block.
	if r.opts.Mode == RedactionModeStrict {
		if redacted, n, ok := validateJSONOutbound(out); ok {
			out = redacted
			stats.JSONKeyRedacted += n
		}
	}

	return out, stats
}

// validateJSONOutbound is a hardening hook called by the strict-mode
// path: when a tool_result body parses as JSON, every value at any
// depth that contains a credential-style string is replaced. This
// catches outputs that wrap a header in nested JSON the line-based
// stripper would miss.
func validateJSONOutbound(text string) (string, int, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return text, 0, false
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return text, 0, false
	}
	var doc any
	if err := json.Unmarshal([]byte(trimmed), &doc); err != nil {
		return text, 0, false
	}
	count := 0
	walked := walkRedactJSON(doc, &count)
	// walkRedactJSON only emits maps / slices / strings / json-native
	// scalars sourced from json.Unmarshal output, so json.Marshal is
	// guaranteed to succeed on the result. Ignore the error explicitly
	// instead of branching on it.
	out, _ := json.Marshal(walked)
	return string(out), count, count > 0
}

// walkRedactJSON recursively replaces credential-key values in a JSON
// document. Unrecognised types pass through unchanged.
func walkRedactJSON(node any, count *int) any {
	switch v := node.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			if isCredentialKey(k) {
				out[k] = "[REDACTED]"
				*count++
				continue
			}
			out[k] = walkRedactJSON(val, count)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = walkRedactJSON(val, count)
		}
		return out
	default:
		return v
	}
}

// isCredentialKey reports whether name (case-insensitive) is in the
// curated jsonCredentialKeys list.
func isCredentialKey(name string) bool {
	lower := strings.ToLower(name)
	for _, k := range jsonCredentialKeys {
		if lower == strings.ToLower(k) {
			return true
		}
	}
	return false
}
