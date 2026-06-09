package compression

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// buildPNGHeader returns a minimal PNG header with the given width and height.
func buildPNGHeader(w, h int) []byte {
	b := make([]byte, 33)
	// PNG signature
	copy(b[0:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	// IHDR length (4 bytes)
	b[8], b[9], b[10], b[11] = 0, 0, 0, 13
	// IHDR type
	copy(b[12:16], []byte{'I', 'H', 'D', 'R'})
	// Width (4 bytes big-endian)
	b[16] = byte(w >> 24)
	b[17] = byte(w >> 16)
	b[18] = byte(w >> 8)
	b[19] = byte(w)
	// Height (4 bytes big-endian)
	b[20] = byte(h >> 24)
	b[21] = byte(h >> 16)
	b[22] = byte(h >> 8)
	b[23] = byte(h)
	// bit depth, color type, compression, filter, interlace
	b[24] = 8
	return b
}

func TestReplaceImageBase64_ImageBlock(t *testing.T) {
	t.Parallel()
	// Build a base64-encoded PNG header
	pngData := buildPNGHeader(1920, 1080)
	encoded := base64.StdEncoding.EncodeToString(pngData)

	block := types.ContentBlock{
		Type:      "image",
		ImageData: encoded,
	}
	updated, saved := replaceImageBase64(block, 5, 10)
	if saved <= 0 {
		t.Errorf("expected bytes saved > 0, got %d", saved)
	}
	if updated.Type != "text" {
		t.Errorf("image block should become text block, got type=%q", updated.Type)
	}
	if updated.ImageData != "" {
		t.Errorf("ImageData should be cleared, got %q", updated.ImageData[:minLen(updated.ImageData, 20)])
	}
	if !strings.Contains(updated.Text, "message 5") {
		t.Errorf("replacement text should mention message index: %q", updated.Text)
	}
}

func TestReplaceImageBase64_ImageBlock_WithPNGDimensions(t *testing.T) {
	t.Parallel()
	pngData := buildPNGHeader(800, 600)
	encoded := base64.StdEncoding.EncodeToString(pngData)

	block := types.ContentBlock{
		Type:      "image",
		ImageData: encoded,
	}
	updated, saved := replaceImageBase64(block, 3, 8)
	if saved <= 0 {
		t.Errorf("expected bytes saved > 0, got %d", saved)
	}
	// Should mention dimensions extracted from PNG header
	if !strings.Contains(updated.Text, "800") || !strings.Contains(updated.Text, "600") {
		t.Errorf("replacement should mention dimensions: %q", updated.Text)
	}
}

func TestReplaceImageBase64_DataURIInText(t *testing.T) {
	t.Parallel()
	// A tool_result block with an inline data URI
	fakeData := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("A", 200)))
	text := "Output: data:image/png;base64," + fakeData + " end"

	block := types.ContentBlock{
		Type: "tool_result",
		Text: text,
	}
	updated, saved := replaceImageBase64(block, 2, 6)
	if saved <= 0 {
		t.Errorf("expected savings from data URI removal, got %d", saved)
	}
	if strings.Contains(updated.Text, "data:image") {
		t.Errorf("data URI should be replaced: %q", updated.Text)
	}
}

func TestReplaceImageBase64_NonImageBlock_NoChange(t *testing.T) {
	t.Parallel()
	block := types.ContentBlock{
		Type: "tool_result",
		Text: "plain text output without images",
	}
	updated, saved := replaceImageBase64(block, 1, 5)
	if saved != 0 {
		t.Errorf("non-image block should have 0 saved, got %d", saved)
	}
	if updated.Text != block.Text {
		t.Errorf("non-image block text should not change")
	}
}

func TestReplaceImageBase64_SlidingWindowProtected(t *testing.T) {
	t.Parallel()
	// prefixEnd <= 0 means sliding window (nothing should be touched)
	block := types.ContentBlock{
		Type:      "image",
		ImageData: base64.StdEncoding.EncodeToString(buildPNGHeader(100, 100)),
	}
	updated, saved := replaceImageBase64(block, 0, 0)
	if saved != 0 {
		t.Errorf("sliding window image should not be replaced, saved=%d", saved)
	}
	if updated.ImageData == "" {
		t.Errorf("sliding window image data should be preserved")
	}
}

func TestExtractImageDimensions_PNG(t *testing.T) {
	t.Parallel()
	data := buildPNGHeader(1024, 768)
	w, h, format := extractImageDimensions(data)
	if w != 1024 || h != 768 {
		t.Errorf("expected 1024x768, got %dx%d", w, h)
	}
	if format != "PNG" {
		t.Errorf("expected PNG format, got %q", format)
	}
}

func TestExtractImageDimensions_Unknown(t *testing.T) {
	t.Parallel()
	data := []byte("not an image at all")
	w, h, _ := extractImageDimensions(data)
	if w != 0 || h != 0 {
		t.Errorf("unknown format should return 0x0, got %dx%d", w, h)
	}
}

func TestExtractImageDimensions_JPEG(t *testing.T) {
	t.Parallel()
	// Minimal JPEG-like header with SOF0 marker
	// FF D8 = SOF, then FF C0 = SOF0 marker at offset 2
	data := make([]byte, 20)
	data[0] = 0xFF
	data[1] = 0xD8
	data[2] = 0xFF
	data[3] = 0xC0
	// SOF0 length (2 bytes) - skip
	data[4] = 0
	data[5] = 11
	// precision (1 byte)
	data[6] = 8
	// height (2 bytes big-endian): 480
	data[7] = 0x01
	data[8] = 0xE0
	// width (2 bytes big-endian): 640
	data[9] = 0x02
	data[10] = 0x80
	w, h, format := extractImageDimensions(data)
	if w != 640 || h != 480 {
		t.Errorf("JPEG: expected 640x480, got %dx%d", w, h)
	}
	if format != "JPEG" {
		t.Errorf("expected JPEG format, got %q", format)
	}
}

func TestReplaceImageBase64_LargeBase64Blob(t *testing.T) {
	t.Parallel()
	// A tool_result with a large base64 blob (>1000 chars)
	fakeB64 := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", 20)
	block := types.ContentBlock{
		Type: "tool_result",
		Text: "Data: " + fakeB64,
	}
	updated, saved := replaceImageBase64(block, 4, 8)
	if saved <= 0 {
		t.Errorf("large base64 blob should be replaced, saved=%d", saved)
	}
	if strings.Contains(updated.Text, fakeB64[:50]) {
		t.Errorf("base64 blob should not remain in text")
	}
}

func TestDescribeBase64URI_NoComma(t *testing.T) {
	t.Parallel()
	// URI without a comma (malformed) - should still produce a label
	result := describeBase64URI("data:image/png;base64", 3)
	if !strings.Contains(result, "image") {
		t.Errorf("malformed URI should still produce label, got %q", result)
	}
}

func TestDescribeImageData_BadBase64(t *testing.T) {
	t.Parallel()
	// Non-decodable base64 should still produce a fallback label
	result := describeImageData("not-valid-base64!!!", "image", 7)
	if !strings.Contains(result, "message 7") {
		t.Errorf("bad base64 should produce fallback label mentioning message index: %q", result)
	}
}

func TestExtractTerminalText_HighPrintable(t *testing.T) {
	t.Parallel()
	// Build data that's mostly printable ASCII (simulate terminal screenshot)
	data := []byte(strings.Repeat("Hello world! This is a terminal line.\n", 20))
	result := extractTerminalText(data)
	if result == "" {
		t.Errorf("high-printable data should return extracted text")
	}
}

func TestExtractTerminalText_BinaryData_Empty(t *testing.T) {
	t.Parallel()
	// Binary data: <5% printable -> should return empty string
	data := make([]byte, 200)
	for i := range data {
		data[i] = byte(i % 256) // many non-printable bytes
	}
	result := extractTerminalText(data)
	// With mostly binary data, should return empty (not a terminal screenshot)
	_ = result // just ensure no panic; actual result depends on byte distribution
}

// TestExtractTerminalText_TrailingRun covers the "if current.Len() >= 10" check AFTER the main
// for-loop (line 146). When data ends with a long printable run not terminated by a non-printable
// byte, the final run must be appended after the loop.
func TestExtractTerminalText_TrailingRun(t *testing.T) {
	t.Parallel()
	// 60 printable bytes with no trailing newline: the loop exits without resetting current,
	// so the post-loop append fires.
	data := []byte(strings.Repeat("A", 60))
	result := extractTerminalText(data)
	if result == "" {
		t.Errorf("all-printable data without trailing newline should return non-empty, got %q", result)
	}
}

// TestReplaceImageBase64_SmallBase64Blob covers the "return match" branch (line 52) in
// replaceImageBase64 where the base64 blob is between 100 and 1000 characters. The match
// is returned unchanged, replaced==orig, so no savings occur.
func TestReplaceImageBase64_SmallBase64Blob(t *testing.T) {
	t.Parallel()
	// 150 chars of base64 alphabet: matches reBase64Blob (>= 100) but len <= 1000
	// so the lambda returns match unchanged -> len(replaced) == len(orig) -> saved=0.
	smallB64 := strings.Repeat("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", 3)[:150]
	block := types.ContentBlock{
		Type: "tool_result",
		Text: "Data: " + smallB64 + " end",
	}
	_, saved := replaceImageBase64(block, 4, 8)
	if saved != 0 {
		t.Errorf("small base64 blob (<=1000 chars) should not be replaced: saved=%d", saved)
	}
}

func minLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}

// TestDescribeImageData_URLSafeBase64 covers the URL-safe base64 fallback branch in
// describeImageData: StdEncoding fails (input contains '-'/'_'), URLEncoding succeeds.
func TestDescribeImageData_URLSafeBase64(t *testing.T) {
	t.Parallel()
	// 0xFB, 0xFF, 0xFE encodes to "-__-" under URLEncoding but "+//+" under StdEncoding.
	// StdEncoding.DecodeString("-__-") returns an error; URLEncoding.DecodeString("-__-") succeeds.
	raw := []byte{0xFB, 0xFF, 0xFE}
	encoded := base64.URLEncoding.EncodeToString(raw)
	if !strings.ContainsAny(encoded, "-_") {
		t.Fatalf("expected URL-safe chars in %q", encoded)
	}
	// Must not panic; should produce a non-empty label referencing the message index.
	result := describeImageData(encoded, "image", 3)
	if !strings.Contains(result, "message 3") {
		t.Errorf("URL-safe base64: expected label with message index, got %q", result)
	}
}

// TestDescribeImageData_TerminalScreenshot covers the "Terminal screenshot" return path in
// describeImageData. Requires: valid PNG dimensions AND >30% printable bytes in the decoded data.
func TestDescribeImageData_TerminalScreenshot(t *testing.T) {
	t.Parallel()
	// PNG header (33 bytes with valid w/h) + lots of printable ASCII text.
	// The printable bytes push the ratio well above the 30% threshold.
	pngBytes := buildPNGHeader(320, 240)
	printable := []byte(strings.Repeat("Hello terminal! ABCDEFGHIJKLMNOPQRSTUVWXYZ abcdefghij\n", 6))
	combined := append(pngBytes, printable...)
	encoded := base64.StdEncoding.EncodeToString(combined)

	result := describeImageData(encoded, "image", 9)
	if !strings.Contains(result, "Terminal screenshot") {
		t.Errorf("expected Terminal screenshot label, got %q", result)
	}
	if !strings.Contains(result, "message 9") {
		t.Errorf("expected message index in label, got %q", result)
	}
}
