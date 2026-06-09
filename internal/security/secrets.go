package security

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// Detection records a single secret match found in a message.
type Detection struct {
	Pattern     *SecretPattern
	MatchedText string
	MessageIdx  int
}

// Detector scans messages for secrets and optionally redacts or blocks them.
type Detector struct {
	patterns       []SecretPattern
	mode           string // "redact" | "warn" | "block" | "off"
	customPatterns []SecretPattern
	allowlist      []string

	// suspendUntilNano (T59) is a per-process override that temporarily
	// treats the detector as if mode=="off" until the wall-clock time
	// expires. Updated via SuspendUntil, read on every ScanMessages call.
	suspendUntilNano atomic.Int64
}

// MaxSuspendDuration caps how long a detector can be suspended per
// SuspendUntil call. Keeps operators from accidentally disabling
// redaction indefinitely via the admin endpoint.
const MaxSuspendDuration = time.Hour

// SuspendUntil temporarily disables all secret detection until the given
// wall-clock time. Times in the past clear the suspension. Durations
// longer than MaxSuspendDuration are clamped down so an operator who asks
// for 24h still gets only an hour.
func (d *Detector) SuspendUntil(until time.Time) time.Time {
	now := time.Now()
	if until.Before(now) {
		d.suspendUntilNano.Store(0)
		return time.Time{}
	}
	maxUntil := now.Add(MaxSuspendDuration)
	if until.After(maxUntil) {
		until = maxUntil
	}
	d.suspendUntilNano.Store(until.UnixNano())
	return until
}

// SuspendState reports the current suspension status. active=false means
// the detector is operating normally; the `until` value is the wall-clock
// deadline when active=true and time.Time{} otherwise.
func (d *Detector) SuspendState() (active bool, until time.Time) {
	ns := d.suspendUntilNano.Load()
	if ns == 0 {
		return false, time.Time{}
	}
	t := time.Unix(0, ns)
	if time.Now().After(t) {
		// Expired: lazily clear so subsequent reads are cheap.
		d.suspendUntilNano.Store(0)
		return false, time.Time{}
	}
	return true, t
}

// Mode returns the configured detection mode ("off", "warn", "redact",
// "block"). Exposed for admin surfaces.
func (d *Detector) Mode() string { return d.mode }

// NewDetector constructs a Detector with the given mode, custom patterns, and allowlist.
// custom patterns are appended after DefaultPatterns.
// allowlist is a slice of literal substrings - any match text containing one is skipped.
// mode must be one of: "off", "warn", "redact", "block". Unknown modes default to "warn".
func NewDetector(mode string, custom []SecretPattern, allowlist []string) *Detector {
	switch mode {
	case "off", "warn", "redact", "block":
		// valid
	default:
		slog.Warn("security: unknown detector mode, defaulting to warn", "mode", mode)
		mode = "warn"
	}

	patterns := make([]SecretPattern, 0, len(DefaultPatterns)+len(custom))
	patterns = append(patterns, DefaultPatterns...)
	patterns = append(patterns, custom...)

	al := make([]string, len(allowlist))
	copy(al, allowlist)

	return &Detector{
		patterns:       patterns,
		mode:           mode,
		customPatterns: custom,
		allowlist:      al,
	}
}

// ScanMessages inspects all content blocks in messages for secrets.
//
// Behaviour by mode:
//   - "off":    returns original messages unchanged, empty detections, nil error.
//   - "warn":   returns original messages unchanged, populated detections, nil error.
//   - "redact": returns messages with matches replaced by [REDACTED:PatternName],
//     populated detections, nil error.
//   - "block":  returns nil messages and a non-nil error if any secret is found.
func (d *Detector) ScanMessages(messages []types.Message) ([]types.Message, []Detection, error) {
	if d.mode == "off" {
		return messages, nil, nil
	}
	// T59: temporary session-suspend overrides the configured mode.
	if active, _ := d.SuspendState(); active {
		return messages, nil, nil
	}

	out := make([]types.Message, len(messages))
	var all []Detection

	for i, msg := range messages {
		out[i] = msg
		out[i].Content = make([]types.ContentBlock, len(msg.Content))
		copy(out[i].Content, msg.Content)

		for j, block := range out[i].Content {
			if block.Text != "" {
				redacted, dets := d.scanText(block.Text, i)
				all = append(all, dets...)
				if d.mode == "redact" {
					out[i].Content[j].Text = redacted
				}
			}
			if block.ToolInput != "" {
				redacted, dets := d.scanText(block.ToolInput, i)
				all = append(all, dets...)
				if d.mode == "redact" {
					out[i].Content[j].ToolInput = redacted
				}
			}
		}
	}

	if d.mode == "block" && len(all) > 0 {
		names := make([]string, 0, len(all))
		for _, det := range all {
			names = append(names, det.Pattern.Name)
		}
		return nil, all, errors.New(
			fmt.Sprintf("security: request blocked - secrets detected: %s",
				strings.Join(names, ", ")),
		)
	}

	if len(all) > 0 {
		slog.Warn("security: secrets detected in messages",
			"count", len(all),
			"mode", d.mode,
		)
	}

	return out, all, nil
}

// scanText runs all patterns against text and returns the (possibly redacted) string
// along with any Detections found. msgIdx is the originating message index.
func (d *Detector) scanText(text string, msgIdx int) (string, []Detection) {
	var dets []Detection
	result := text

	for i := range d.patterns {
		p := &d.patterns[i]
		matches := p.Regex.FindAllString(result, -1)
		for _, match := range matches {
			if d.isAllowlisted(match) {
				continue
			}
			if p.MinEntropy > 0 && shannonEntropy(match) < p.MinEntropy {
				continue
			}
			dets = append(dets, Detection{
				Pattern:     p,
				MatchedText: match,
				MessageIdx:  msgIdx,
			})
			if d.mode == "redact" {
				replacement := fmt.Sprintf("[REDACTED:%s]", p.Name)
				result = strings.ReplaceAll(result, match, replacement)
			}
		}
	}

	return result, dets
}

// isAllowlisted reports whether match contains any allowlisted substring.
func (d *Detector) isAllowlisted(match string) bool {
	for _, entry := range d.allowlist {
		if strings.Contains(match, entry) {
			return true
		}
	}
	return false
}
