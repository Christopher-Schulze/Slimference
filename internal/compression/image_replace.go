package compression

import (
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// replaceImageBase64 replaces base64 image data in old messages with a compact placeholder.
// msgAge 0 means the message is in the sliding window - never touch it.
// Returns the modified block and bytes saved.
func replaceImageBase64(block types.ContentBlock, msgIdx, prefixEnd int) (types.ContentBlock, int) {
	// Sliding window messages (msgIdx >= prefixEnd) are never passed here,
	// but guard defensively.
	if prefixEnd <= 0 {
		return block, 0
	}

	// Handle explicit image-type blocks (Anthropic vision API format)
	if block.Type == "image" && block.ImageData != "" {
		orig := block.ImageData
		label := describeImageData(block.ImageData, "image", msgIdx)
		block.ImageData = ""
		block.Type = "text"
		block.Text = label
		return block, len(orig)
	}

	// Handle data URIs embedded in text (e.g., tool_result with inline images)
	if block.Text != "" && reBase64DataURI.MatchString(block.Text) {
		orig := block.Text
		replaced := reBase64DataURI.ReplaceAllStringFunc(block.Text, func(match string) string {
			return describeBase64URI(match, msgIdx)
		})
		if len(replaced) < len(orig) {
			block.Text = replaced
			return block, len(orig) - len(replaced)
		}
	}

	// Handle large standalone base64 blobs (>1000 chars of base64 alphabet)
	if block.Text != "" && reBase64Blob.MatchString(block.Text) {
		orig := block.Text
		replaced := reBase64Blob.ReplaceAllStringFunc(block.Text, func(match string) string {
			if len(match) > 1000 {
				return fmt.Sprintf("[base64 data removed from old message %d, %d bytes]",
					msgIdx, len(match)*3/4)
			}
			return match
		})
		if len(replaced) < len(orig) {
			block.Text = replaced
			return block, len(orig) - len(replaced)
		}
	}

	return block, 0
}

// describeImageData produces a human-readable label for base64 image data.
func describeImageData(data, mediaType string, msgIdx int) string {
	// Try to extract image dimensions from PNG/JPEG headers
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		// Try URL-safe base64
		raw, err = base64.URLEncoding.DecodeString(data)
	}
	if err == nil {
		if w, h, format := extractImageDimensions(raw); w > 0 {
			// Check if it looks like a terminal screenshot (monospace pixel patterns)
			if extracted := extractTerminalText(raw); extracted != "" {
				return fmt.Sprintf("[Terminal screenshot from message %d (%dx%d %s)]\n%s",
					msgIdx, w, h, format, extracted)
			}
			return fmt.Sprintf("[Image from message %d: %dx%d %s]", msgIdx, w, h, format)
		}
	}
	return fmt.Sprintf("[Image data removed from old message %d]", msgIdx)
}

// describeBase64URI produces a compact label for a data URI.
func describeBase64URI(uri string, msgIdx int) string {
	// Extract media type from data:image/png;base64,...
	var format string
	if m := reMediaType.FindStringSubmatch(uri); m != nil {
		format = m[1]
	}
	if format == "" {
		format = "image"
	}
	// Extract the base64 payload
	_, after, ok := strings.Cut(uri, ",")
	if ok {
		payload := after
		size := len(payload) * 3 / 4 // approximate decoded bytes
		return fmt.Sprintf("[%s data removed from old message %d, ~%d bytes]", format, msgIdx, size)
	}
	return fmt.Sprintf("[image data removed from old message %d]", msgIdx)
}

// extractImageDimensions extracts width, height from PNG IHDR or JPEG SOF0.
func extractImageDimensions(data []byte) (w, h int, format string) {
	if len(data) >= 24 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		// PNG: IHDR chunk at offset 8, width at 16, height at 20
		w = int(data[16])<<24 | int(data[17])<<16 | int(data[18])<<8 | int(data[19])
		h = int(data[20])<<24 | int(data[21])<<16 | int(data[22])<<8 | int(data[23])
		return w, h, "PNG"
	}
	if len(data) >= 4 && data[0] == 0xFF && data[1] == 0xD8 {
		// JPEG: scan for SOF0 marker
		for i := 2; i < len(data)-8; i++ {
			if data[i] == 0xFF && (data[i+1] == 0xC0 || data[i+1] == 0xC2) {
				h = int(data[i+5])<<8 | int(data[i+6])
				w = int(data[i+7])<<8 | int(data[i+8])
				return w, h, "JPEG"
			}
		}
	}
	return 0, 0, ""
}

// extractTerminalText attempts to find printable ASCII runs in image data.
// Returns non-empty string if the image appears to be a terminal screenshot.
func extractTerminalText(data []byte) string {
	// Simple heuristic: look for long runs of printable ASCII (suggests terminal screenshot)
	var runs []string
	var current strings.Builder
	printableCount := 0
	totalCount := 0

	for _, b := range data {
		totalCount++
		if b >= 0x20 && b <= 0x7E {
			printableCount++
			current.WriteByte(b)
		} else {
			if current.Len() >= 10 { // only keep meaningful runs
				runs = append(runs, current.String())
			}
			current.Reset()
		}
	}
	if current.Len() >= 10 {
		runs = append(runs, current.String())
	}

	// Only claim terminal screenshot if >30% printable bytes (real images are mostly binary)
	if totalCount == 0 || float64(printableCount)/float64(totalCount) < 0.30 {
		return ""
	}

	return strings.Join(runs, "\n")
}

var (
	reBase64DataURI = regexp.MustCompile(
		`data:(?:image|application)/[a-zA-Z0-9.+-]+;base64,[A-Za-z0-9+/=]{100,}`)
	reBase64Blob = regexp.MustCompile(
		`[A-Za-z0-9+/]{100,}={0,2}`)
	reMediaType = regexp.MustCompile(
		`data:(image/[a-zA-Z0-9.+-]+);base64,`)
)
