// Package indist implements the T190 indistinguishability harness.
// Two execution modes:
//
//  1. Capture mode: snapshot Codex baseline traffic via tshark. We
//     produce a deterministic JSON record per TLS connection with
//     fields covering TLS ClientHello, ALPN, HTTP/2 SETTINGS, and
//     WebSocket Upgrade headers.
//
//  2. Diff mode: take two recorded snapshots (Codex baseline + ours)
//     and emit a Drift report - empty when indistinguishable, one
//     entry per observable divergence otherwise.
//
// This file contains the pure-Go data structures + diff function.
// The tshark wrapping lives in `scripts/utils/indist_probe/` and
// invokes this package.
package indist

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Capture is the canonical record for one outbound TLS connection.
// Field types are kept primitive so JSON round-trip is byte-stable
// across captures.
type Capture struct {
	Label string `json:"label"`

	// TLS layer.
	JA3          string   `json:"ja3"`
	JA3Hash      string   `json:"ja3_hash"`
	JA4          string   `json:"ja4"`
	ALPN         []string `json:"alpn"`
	SNI          string   `json:"sni"`
	CipherIDs    []uint16 `json:"cipher_ids"`
	ExtensionIDs []uint16 `json:"extension_ids"`
	CurveIDs     []uint16 `json:"curve_ids"`
	GREASE       bool     `json:"grease"`

	// HTTP/2 layer (when negotiated).
	H2Settings          []H2Setting `json:"h2_settings,omitempty"`
	H2PseudoHeaderOrder []string    `json:"h2_pseudo_header_order,omitempty"`

	// HTTP/1.1 or WebSocket Upgrade layer.
	HeaderOrder   []string `json:"header_order,omitempty"`
	WSExtensions  string   `json:"ws_extensions,omitempty"`
	WSSubprotocol string   `json:"ws_subprotocol,omitempty"`
	WSVersion     string   `json:"ws_version,omitempty"`

	// Timing markers (relative to connect, in milliseconds).
	TLSHandshakeMs int64 `json:"tls_handshake_ms"`
	FirstAppByteMs int64 `json:"first_app_byte_ms"`
}

// H2Setting is a single key/value entry from the HTTP/2 SETTINGS
// frame the client sent. Both fields are kept as-is so the diff
// catches any drift in either key ordering or value.
type H2Setting struct {
	ID    uint16 `json:"id"`
	Value uint32 `json:"value"`
}

// Drift is one observed difference between two Captures. Multiple
// Drift entries can exist per diff run.
type Drift struct {
	Field string `json:"field"`
	Want  string `json:"want"`
	Got   string `json:"got"`
}

// Report is the full output of Diff. Empty Drifts means
// indistinguishable.
type Report struct {
	BaselineLabel string  `json:"baseline_label"`
	ProxyLabel    string  `json:"proxy_label"`
	Drifts        []Drift `json:"drifts"`
	BaselineHash  string  `json:"baseline_hash"`
	ProxyHash     string  `json:"proxy_hash"`
}

// OK reports whether the diff found no drifts.
func (r Report) OK() bool { return len(r.Drifts) == 0 }

// Summary renders the report as a one-line operator-readable status.
func (r Report) Summary() string {
	if r.OK() {
		return "indistinguishable (" + r.BaselineHash[:12] + " ≡ " + r.ProxyHash[:12] + ")"
	}
	return fmt.Sprintf("drift in %d field(s) (%s vs %s)",
		len(r.Drifts), r.BaselineHash[:12], r.ProxyHash[:12])
}

// Diff compares two captures and returns a Report. Field-by-field
// comparison is order-aware where ordering matters (cipher list,
// extension list, header order, h2 settings) and order-insensitive
// where it doesn't (ALPN typically matches order but is reported as
// a single concatenated string for diff readability).
func Diff(baseline, proxy Capture) Report {
	r := Report{
		BaselineLabel: baseline.Label,
		ProxyLabel:    proxy.Label,
		BaselineHash:  baseline.Fingerprint(),
		ProxyHash:     proxy.Fingerprint(),
	}
	if baseline.JA3 != proxy.JA3 {
		r.Drifts = append(r.Drifts, Drift{Field: "ja3", Want: baseline.JA3, Got: proxy.JA3})
	}
	if baseline.JA4 != proxy.JA4 {
		r.Drifts = append(r.Drifts, Drift{Field: "ja4", Want: baseline.JA4, Got: proxy.JA4})
	}
	if !equalStrings(baseline.ALPN, proxy.ALPN) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "alpn", Want: strings.Join(baseline.ALPN, ","),
			Got: strings.Join(proxy.ALPN, ","),
		})
	}
	if baseline.SNI != proxy.SNI {
		r.Drifts = append(r.Drifts, Drift{Field: "sni", Want: baseline.SNI, Got: proxy.SNI})
	}
	if !equalUint16Slices(baseline.CipherIDs, proxy.CipherIDs) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "cipher_ids", Want: formatUint16s(baseline.CipherIDs),
			Got: formatUint16s(proxy.CipherIDs),
		})
	}
	if !equalUint16Slices(baseline.ExtensionIDs, proxy.ExtensionIDs) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "extension_ids", Want: formatUint16s(baseline.ExtensionIDs),
			Got: formatUint16s(proxy.ExtensionIDs),
		})
	}
	if !equalUint16Slices(baseline.CurveIDs, proxy.CurveIDs) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "curve_ids", Want: formatUint16s(baseline.CurveIDs),
			Got: formatUint16s(proxy.CurveIDs),
		})
	}
	if baseline.GREASE != proxy.GREASE {
		r.Drifts = append(r.Drifts, Drift{
			Field: "grease",
			Want:  fmt.Sprintf("%v", baseline.GREASE),
			Got:   fmt.Sprintf("%v", proxy.GREASE),
		})
	}
	if !equalH2Settings(baseline.H2Settings, proxy.H2Settings) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "h2_settings", Want: formatH2Settings(baseline.H2Settings),
			Got: formatH2Settings(proxy.H2Settings),
		})
	}
	if !equalStrings(baseline.H2PseudoHeaderOrder, proxy.H2PseudoHeaderOrder) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "h2_pseudo_header_order",
			Want:  strings.Join(baseline.H2PseudoHeaderOrder, ","),
			Got:   strings.Join(proxy.H2PseudoHeaderOrder, ","),
		})
	}
	if !equalStrings(baseline.HeaderOrder, proxy.HeaderOrder) {
		r.Drifts = append(r.Drifts, Drift{
			Field: "header_order",
			Want:  strings.Join(baseline.HeaderOrder, ","),
			Got:   strings.Join(proxy.HeaderOrder, ","),
		})
	}
	if baseline.WSExtensions != proxy.WSExtensions {
		r.Drifts = append(r.Drifts, Drift{
			Field: "ws_extensions", Want: baseline.WSExtensions, Got: proxy.WSExtensions,
		})
	}
	if baseline.WSSubprotocol != proxy.WSSubprotocol {
		r.Drifts = append(r.Drifts, Drift{
			Field: "ws_subprotocol", Want: baseline.WSSubprotocol, Got: proxy.WSSubprotocol,
		})
	}
	if baseline.WSVersion != proxy.WSVersion {
		r.Drifts = append(r.Drifts, Drift{
			Field: "ws_version", Want: baseline.WSVersion, Got: proxy.WSVersion,
		})
	}
	return r
}

// Fingerprint returns a deterministic content-hash of the
// fingerprintable fields. Used to identify captures across runs.
// Timing fields are deliberately excluded - they jitter run-to-run.
func (c Capture) Fingerprint() string {
	var b strings.Builder
	b.WriteString(c.JA3)
	b.WriteString("|")
	b.WriteString(c.JA4)
	b.WriteString("|")
	b.WriteString(strings.Join(c.ALPN, ","))
	b.WriteString("|")
	b.WriteString(formatUint16s(c.CipherIDs))
	b.WriteString("|")
	b.WriteString(formatUint16s(c.ExtensionIDs))
	b.WriteString("|")
	b.WriteString(formatUint16s(c.CurveIDs))
	b.WriteString("|")
	b.WriteString(fmt.Sprintf("%v", c.GREASE))
	b.WriteString("|")
	b.WriteString(formatH2Settings(c.H2Settings))
	b.WriteString("|")
	b.WriteString(strings.Join(c.H2PseudoHeaderOrder, ","))
	b.WriteString("|")
	b.WriteString(strings.Join(c.HeaderOrder, ","))
	b.WriteString("|")
	b.WriteString(c.WSExtensions)
	b.WriteString("|")
	b.WriteString(c.WSSubprotocol)
	b.WriteString("|")
	b.WriteString(c.WSVersion)
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// ErrEmptyCapture is returned by Validate on a Capture that is
// missing the load-bearing fields (JA3 is the minimum signal).
var ErrEmptyCapture = errors.New("indist: capture missing JA3 fingerprint")

// Validate checks the minimum fields needed for a meaningful diff.
func (c Capture) Validate() error {
	if c.JA3 == "" {
		return ErrEmptyCapture
	}
	return nil
}

// formatUint16s renders a uint16 slice as comma-separated hex for
// easy diff-readability.
func formatUint16s(in []uint16) string {
	if len(in) == 0 {
		return ""
	}
	parts := make([]string, len(in))
	for i, v := range in {
		parts[i] = fmt.Sprintf("0x%04x", v)
	}
	return strings.Join(parts, ",")
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func equalUint16Slices(a, b []uint16) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

func equalH2Settings(a, b []H2Setting) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v.ID != b[i].ID || v.Value != b[i].Value {
			return false
		}
	}
	return true
}

func formatH2Settings(in []H2Setting) string {
	if len(in) == 0 {
		return ""
	}
	parts := make([]string, len(in))
	for i, s := range in {
		parts[i] = fmt.Sprintf("0x%04x=0x%08x", s.ID, s.Value)
	}
	return strings.Join(parts, ",")
}

// SortedH2Settings returns a new slice with settings sorted by ID.
// Used when the diff is "order-insensitive but values must match".
// We don't currently use this for the load-bearing diff (HTTP/2
// SETTINGS order IS observable on the wire) but expose it for the
// audit harness to compare against a tolerant variant.
func SortedH2Settings(in []H2Setting) []H2Setting {
	out := make([]H2Setting, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
