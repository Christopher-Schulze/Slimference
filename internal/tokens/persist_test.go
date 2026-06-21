package tokens

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

func TestModelFamily(t *testing.T) {
	t.Parallel()
	tests := []struct {
		model string
		want  string
	}{
		{"claude-3-opus-20240229", "opus"},
		{"claude-opus-4-20250514", "opus"},
		{"claude-3-5-sonnet-20241022", "sonnet"},
		{"claude-sonnet-4-20250514", "sonnet"},
		{"claude-3-haiku-20240307", "haiku"},
		{"claude-haiku-3-5-20241022", "haiku"},
		{"gpt-4", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := modelFamily(tt.model)
		if got != tt.want {
			t.Errorf("modelFamily(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestPerModelIsolation(t *testing.T) {
	defer resetForTest()
	for range 50 {
		ObserveUpstreamUsage(types.Anthropic, "claude-opus", 80, 100)
	}
	for range 50 {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 120, 100)
	}
	opusRatio := anthropic.BytesPerTokenX1000ForModel("claude-opus")
	sonnetRatio := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")
	if opusRatio <= sonnetRatio {
		t.Fatalf("opus should have higher ratio (was overestimated), got opus=%d sonnet=%d", opusRatio, sonnetRatio)
	}
}

func TestEmptyModelUsesGlobal(t *testing.T) {
	defer resetForTest()
	anthropic.bytesPerTokenX1000.Store(4000)
	r := anthropic.BytesPerTokenX1000ForModel("")
	if r != 4000 {
		t.Fatalf("empty model should use global: %d", r)
	}
}

func TestUnknownModelUsesGlobal(t *testing.T) {
	defer resetForTest()
	anthropic.bytesPerTokenX1000.Store(4200)
	r := anthropic.BytesPerTokenX1000ForModel("gpt-4")
	if r != 4200 {
		t.Fatalf("unknown model should use global: %d", r)
	}
}

func TestCalibrationRoundtrip(t *testing.T) {
	defer resetForTest()
	dir := t.TempDir()
	ResetCalibration()
	LoadCalibrationFromDir(dir)

	for range 5 {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 90, 100)
	}
	ratioAfter := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")

	ResetCalibration()
	anthropic.perModel.Range(func(key, _ any) bool {
		anthropic.perModel.Delete(key)
		return true
	})
	LoadCalibrationFromDir(dir)

	ratioReplay := anthropic.BytesPerTokenX1000ForModel("claude-sonnet")
	if ratioReplay != ratioAfter {
		t.Fatalf("replay mismatch: after=%d replay=%d", ratioAfter, ratioReplay)
	}
}

func TestCalibrationCap(t *testing.T) {
	defer resetForTest()
	dir := t.TempDir()
	ResetCalibration()
	LoadCalibrationFromDir(dir)

	for range 1100 {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 90, 100)
	}

	data, err := os.ReadFile(filepath.Join(dir, "anthropic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := splitLines(string(data))
	if len(lines) != 1100 {
		t.Fatalf("file should have 1100 lines (append-only), got %d", len(lines))
	}
}

func TestCalibrationCapOnLoad(t *testing.T) {
	defer resetForTest()
	dir := t.TempDir()
	ResetCalibration()
	LoadCalibrationFromDir(dir)
	for range 1100 {
		ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 90, 100)
	}

	ResetCalibration()
	anthropic.perModel.Range(func(key, _ any) bool {
		anthropic.perModel.Delete(key)
		return true
	})
	LoadCalibrationFromDir(dir)
	if len(calBuf) > calibrationCap {
		t.Fatalf("in-memory buffer should be capped at %d, got %d", calibrationCap, len(calBuf))
	}
}

func TestCalibrationCorruptFile(t *testing.T) {
	defer resetForTest()
	dir := t.TempDir()
	f, _ := os.Create(filepath.Join(dir, "anthropic.jsonl"))
	f.WriteString("not json\n")
	f.WriteString("\n")
	f.WriteString("also not json\n")
	f.WriteString(`{"model":"gpt-4","observed":100,"estimated":100,"ratio":3500}` + "\n")
	f.Close()

	ResetCalibration()
	LoadCalibrationFromDir(dir)
	if anthropic.BytesPerTokenX1000ForModel("claude-sonnet") <= 0 {
		t.Fatal("should default to global ratio on corrupt file")
	}
}

func TestCalibrationMissingDir(t *testing.T) {
	defer resetForTest()
	ResetCalibration()
	LoadCalibrationFromDir("/nonexistent/path")
	if anthropic.BytesPerTokenX1000() != 3500 {
		t.Fatal("should stay at default")
	}
}

func TestLoadCalibrationEmptyCalFile(t *testing.T) {
	defer resetForTest()
	calMu.Lock()
	calFile = ""
	calMu.Unlock()
	loadCalibrationLocked()
}

func TestCalibrationFileWriteErrors(t *testing.T) {
	defer resetForTest()
	dir := t.TempDir()
	ResetCalibration()
	LoadCalibrationFromDir(dir)
	roDir := filepath.Join(dir, "readonly")
	os.MkdirAll(roDir, 0555)
	calMu.Lock()
	calFile = filepath.Join(roDir, "nested/deep/anthropic.jsonl")
	calMu.Unlock()
	ObserveUpstreamUsage(types.Anthropic, "claude-sonnet", 90, 100)
}

func TestSplitLines(t *testing.T) {
	t.Parallel()
	if got := splitLines(""); len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}
	if got := splitLines("a\nb\nc"); len(got) != 3 {
		t.Fatalf("3 lines: %v", got)
	}
	if got := splitLines("a\r\nb"); len(got) != 2 || got[0] != "a" {
		t.Fatalf("CRLF: %v", got)
	}
}
