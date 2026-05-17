package indist

import (
	"encoding/json"
	"strings"
	"testing"
)

// codexBaseline is a canonical captured-Codex-shaped capture used as
// the baseline in tests. Values are realistic (rustls + tokio-tungstenite
// produces deterministic shapes) but synthetic.
func codexBaseline() Capture {
	return Capture{
		Label:        "codex_cli_rs_0_130_baseline",
		JA3:          "771,4865-4866-4867-49195-49199-49196-49200-52393-52392-49171-49172-156-157-47-53,5-10-11-13-16-18-23-27-43-45-51-65281,29-23-24,0",
		JA3Hash:      "abc123",
		JA4:          "t13d3112h2_55b375c5d22e_c4a0f7e5fda3",
		ALPN:         []string{"h2", "http/1.1"},
		SNI:          "chatgpt.com",
		CipherIDs:    []uint16{0x1301, 0x1302, 0x1303},
		ExtensionIDs: []uint16{0x0005, 0x000a, 0x000b, 0x000d},
		CurveIDs:     []uint16{0x001d, 0x0017, 0x0018},
		GREASE:       false,
		H2Settings: []H2Setting{
			{ID: 0x1, Value: 0x10000},
			{ID: 0x4, Value: 0x100000},
		},
		H2PseudoHeaderOrder: []string{":method", ":path", ":authority", ":scheme"},
		HeaderOrder:         []string{"user-agent", "accept", "authorization"},
		WSExtensions:        "permessage-deflate; client_max_window_bits",
		WSSubprotocol:       "responses_websockets=2026-02-06",
		WSVersion:           "13",
	}
}

func TestDiffIndistinguishable(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.Label = "ours"
	report := Diff(a, b)
	if !report.OK() {
		t.Fatalf("expected indistinguishable; drifts=%+v", report.Drifts)
	}
	if !strings.Contains(report.Summary(), "indistinguishable") {
		t.Errorf("summary: %q", report.Summary())
	}
}

func TestDiffJA3Drift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.JA3 = "different"
	report := Diff(a, b)
	if report.OK() {
		t.Fatal("expected drift")
	}
	if report.Drifts[0].Field != "ja3" {
		t.Errorf("expected ja3 drift, got %s", report.Drifts[0].Field)
	}
}

func TestDiffJA4Drift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.JA4 = "x"
	report := Diff(a, b)
	if report.OK() {
		t.Fatal("expected drift")
	}
}

func TestDiffALPNDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.ALPN = []string{"h2"} // missing http/1.1
	report := Diff(a, b)
	if report.OK() {
		t.Fatal("expected drift")
	}
}

func TestDiffSNIDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.SNI = "elsewhere.com"
	report := Diff(a, b)
	if !containsDrift(report.Drifts, "sni") {
		t.Errorf("expected sni drift, got %+v", report.Drifts)
	}
}

func TestDiffCipherOrderMatters(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.CipherIDs = []uint16{0x1303, 0x1302, 0x1301} // reordered
	report := Diff(a, b)
	if !containsDrift(report.Drifts, "cipher_ids") {
		t.Errorf("cipher reordering should drift")
	}
}

func TestDiffExtensionOrderMatters(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.ExtensionIDs = []uint16{0x000d, 0x000a, 0x000b, 0x0005}
	report := Diff(a, b)
	if !containsDrift(report.Drifts, "extension_ids") {
		t.Errorf("expected extension_ids drift")
	}
}

func TestDiffCurveDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.CurveIDs = []uint16{0x0017, 0x001d}
	report := Diff(a, b)
	if !containsDrift(report.Drifts, "curve_ids") {
		t.Errorf("expected curve_ids drift")
	}
}

func TestDiffGREASEDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.GREASE = true
	if !containsDrift(Diff(a, b).Drifts, "grease") {
		t.Errorf("expected grease drift")
	}
}

func TestDiffH2SettingsDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.H2Settings = []H2Setting{{ID: 0x1, Value: 0x99999}}
	if !containsDrift(Diff(a, b).Drifts, "h2_settings") {
		t.Errorf("expected h2 settings drift")
	}
}

func TestDiffH2PseudoHeaderOrderDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.H2PseudoHeaderOrder = []string{":path", ":method", ":authority", ":scheme"}
	if !containsDrift(Diff(a, b).Drifts, "h2_pseudo_header_order") {
		t.Errorf("expected pseudo-header drift")
	}
}

func TestDiffHeaderOrderDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.HeaderOrder = []string{"accept", "user-agent", "authorization"}
	if !containsDrift(Diff(a, b).Drifts, "header_order") {
		t.Errorf("expected header_order drift")
	}
}

func TestDiffWSExtensionsDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.WSExtensions = "permessage-deflate"
	if !containsDrift(Diff(a, b).Drifts, "ws_extensions") {
		t.Errorf("expected ws_extensions drift")
	}
}

func TestDiffWSSubprotocolDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.WSSubprotocol = "responses_websockets=2025-12-01"
	if !containsDrift(Diff(a, b).Drifts, "ws_subprotocol") {
		t.Errorf("expected ws_subprotocol drift")
	}
}

func TestDiffWSVersionDrift(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.WSVersion = "8"
	if !containsDrift(Diff(a, b).Drifts, "ws_version") {
		t.Errorf("expected ws_version drift")
	}
}

func TestDiffMultipleDrifts(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.JA3 = "X"
	b.SNI = "elsewhere"
	b.WSVersion = "8"
	report := Diff(a, b)
	if len(report.Drifts) != 3 {
		t.Errorf("expected 3 drifts, got %d (%+v)", len(report.Drifts), report.Drifts)
	}
}

func TestDiffSummaryDriftFormat(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.JA3 = "X"
	report := Diff(a, b)
	if !strings.Contains(report.Summary(), "drift in 1") {
		t.Errorf("summary: %q", report.Summary())
	}
}

func TestFingerprintStableAcrossRuns(t *testing.T) {
	c := codexBaseline()
	fp1 := c.Fingerprint()
	fp2 := c.Fingerprint()
	if fp1 != fp2 {
		t.Errorf("fingerprint should be deterministic")
	}
}

func TestFingerprintIgnoresTiming(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	a.TLSHandshakeMs = 1
	b.TLSHandshakeMs = 9999
	if a.Fingerprint() != b.Fingerprint() {
		t.Errorf("timing should not affect fingerprint")
	}
}

func TestFingerprintDifferentForDifferentCaptures(t *testing.T) {
	a := codexBaseline()
	b := codexBaseline()
	b.JA3 = "different"
	if a.Fingerprint() == b.Fingerprint() {
		t.Errorf("different JA3 should yield different fingerprint")
	}
}

func TestValidateMissingJA3(t *testing.T) {
	if err := (Capture{}).Validate(); err == nil {
		t.Errorf("expected error on empty JA3")
	}
}

func TestValidateOK(t *testing.T) {
	if err := codexBaseline().Validate(); err != nil {
		t.Errorf("baseline must validate, got %v", err)
	}
}

func TestCaptureJSONRoundTrip(t *testing.T) {
	c := codexBaseline()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Capture
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Fingerprint() != c.Fingerprint() {
		t.Errorf("fingerprint lost on JSON round-trip")
	}
}

func TestSortedH2SettingsStable(t *testing.T) {
	in := []H2Setting{{ID: 5, Value: 1}, {ID: 1, Value: 2}, {ID: 3, Value: 3}}
	out := SortedH2Settings(in)
	for i := 0; i < len(out)-1; i++ {
		if out[i].ID > out[i+1].ID {
			t.Errorf("not sorted: %+v", out)
		}
	}
	// Original input untouched.
	if in[0].ID != 5 {
		t.Errorf("SortedH2Settings mutated input")
	}
}

func TestSortedH2SettingsEmpty(t *testing.T) {
	if out := SortedH2Settings(nil); len(out) != 0 {
		t.Errorf("got %v", out)
	}
}

func TestFormatUint16sEmpty(t *testing.T) {
	if got := formatUint16s(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestFormatH2SettingsEmpty(t *testing.T) {
	if got := formatH2Settings(nil); got != "" {
		t.Errorf("got %q", got)
	}
}

func TestEqualStringsSliceLengthDifferent(t *testing.T) {
	if equalStrings([]string{"a"}, []string{"a", "b"}) {
		t.Errorf("different lengths should not be equal")
	}
}

func TestEqualUint16SlicesLengthDifferent(t *testing.T) {
	if equalUint16Slices([]uint16{1}, []uint16{1, 2}) {
		t.Errorf("length-diff should be unequal")
	}
}

func TestEqualH2SettingsValueDifferent(t *testing.T) {
	// Same length, same IDs, different values.
	a := []H2Setting{{ID: 1, Value: 100}}
	b := []H2Setting{{ID: 1, Value: 200}}
	if equalH2Settings(a, b) {
		t.Errorf("different values must not compare equal")
	}
}

func TestEqualH2SettingsIDDifferent(t *testing.T) {
	a := []H2Setting{{ID: 1, Value: 100}}
	b := []H2Setting{{ID: 2, Value: 100}}
	if equalH2Settings(a, b) {
		t.Errorf("different IDs must not compare equal")
	}
}

func TestEqualH2SettingsLengthDifferent(t *testing.T) {
	if equalH2Settings([]H2Setting{{1, 1}}, []H2Setting{{1, 1}, {2, 2}}) {
		t.Errorf("length-diff should be unequal")
	}
}

// containsDrift reports whether the slice has a Drift for `field`.
func containsDrift(d []Drift, field string) bool {
	for _, x := range d {
		if x.Field == field {
			return true
		}
	}
	return false
}
