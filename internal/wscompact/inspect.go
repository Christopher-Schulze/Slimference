package wscompact

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

type Direction string

const (
	DirectionClientToServer Direction = "client_to_server"
	DirectionServerToClient Direction = "server_to_client"
)

type FrameSummary struct {
	Direction    Direction      `json:"direction"`
	Opcode       string         `json:"opcode"`
	Fin          bool           `json:"fin"`
	Masked       bool           `json:"masked"`
	PayloadBytes int64          `json:"payload_bytes"`
	Fragmented   bool           `json:"fragmented,omitempty"`
	JSON         bool           `json:"json,omitempty"`
	JSONTopLevel string         `json:"json_top_level,omitempty"`
	JSONKeys     []string       `json:"json_keys,omitempty"`
	MessageType  string         `json:"message_type,omitempty"`
	InspectNote  string         `json:"inspect_note,omitempty"`
	Shadow       *ShadowSummary `json:"shadow,omitempty"`
}

type ShadowSummary struct {
	Eligible         bool     `json:"eligible"`
	OriginalBytes    int64    `json:"original_bytes,omitempty"`
	CompressedBytes  int64    `json:"compressed_bytes,omitempty"`
	SavedBytes       int64    `json:"saved_bytes,omitempty"`
	OriginalTokens   int      `json:"original_tokens,omitempty"`
	CompressedTokens int      `json:"compressed_tokens,omitempty"`
	SavedTokens      int      `json:"saved_tokens,omitempty"`
	AppliedLayers    []string `json:"applied_layers,omitempty"`
	Blocker          string   `json:"blocker,omitempty"`
}

type Inspector interface {
	Observe(FrameSummary)
}

type InspectorFunc func(FrameSummary)

func (fn InspectorFunc) Observe(summary FrameSummary) {
	if fn != nil {
		fn(summary)
	}
}

type Frame struct {
	Raw     []byte
	Payload []byte
	Fin     bool
	RSV     bool
	Opcode  byte
	Masked  bool
}

func InspectStream(dst io.Writer, src io.Reader, direction Direction, inspector Inspector) (int64, error) {
	var written int64
	var fragments []byte
	fragmented := false
	for {
		frame, err := ReadFrame(src)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return written, nil
			}
			return written, err
		}
		n, err := dst.Write(frame.Raw)
		written += int64(n)
		if err != nil {
			return written, err
		}
		if n != len(frame.Raw) {
			return written, io.ErrShortWrite
		}
		if inspector == nil {
			continue
		}
		payload := frame.Payload
		emitPayload := payload
		emitFragmented := fragmented || !frame.Fin || frame.Opcode == 0
		if frame.Opcode == 1 && !frame.Fin {
			fragments = append(fragments[:0], payload...)
			fragmented = true
			emitPayload = nil
		} else if frame.Opcode == 0 && fragmented {
			fragments = append(fragments, payload...)
			if frame.Fin {
				emitPayload = fragments
				fragmented = false
			} else {
				emitPayload = nil
			}
		}
		inspector.Observe(SummarizeFrame(frame, direction, emitPayload, emitFragmented))
	}
}

func ReadFrame(r io.Reader) (Frame, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return Frame{}, err
	}
	raw := []byte{header[0], header[1]}
	lengthCode := header[1] & 0x7f
	payloadLen := uint64(lengthCode)
	if lengthCode == 126 {
		ext, err := readExact(r, 2)
		if err != nil {
			return Frame{}, err
		}
		raw = append(raw, ext...)
		payloadLen = uint64(binary.BigEndian.Uint16(ext))
	} else if lengthCode == 127 {
		ext, err := readExact(r, 8)
		if err != nil {
			return Frame{}, err
		}
		raw = append(raw, ext...)
		payloadLen = binary.BigEndian.Uint64(ext)
	}
	var maskKey []byte
	masked := header[1]&0x80 != 0
	if masked {
		key, err := readExact(r, 4)
		if err != nil {
			return Frame{}, err
		}
		raw = append(raw, key...)
		maskKey = key
	}
	if payloadLen > uint64(int(^uint(0)>>1)) {
		return Frame{}, fmt.Errorf("websocket frame too large: %d", payloadLen)
	}
	payload, err := readExact(r, int(payloadLen))
	if err != nil {
		return Frame{}, err
	}
	raw = append(raw, payload...)
	decoded := append([]byte(nil), payload...)
	if masked {
		for i := range decoded {
			decoded[i] ^= maskKey[i%4]
		}
	}
	return Frame{
		Raw:     raw,
		Payload: decoded,
		Fin:     header[0]&0x80 != 0,
		RSV:     header[0]&0x70 != 0,
		Opcode:  header[0] & 0x0f,
		Masked:  masked,
	}, nil
}

func readExact(r io.Reader, n int) ([]byte, error) {
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func SummarizeFrame(frame Frame, direction Direction, payload []byte, fragmented bool) FrameSummary {
	summary := FrameSummary{
		Direction:    direction,
		Opcode:       OpcodeName(frame.Opcode),
		Fin:          frame.Fin,
		Masked:       frame.Masked,
		PayloadBytes: int64(len(frame.Payload)),
		Fragmented:   fragmented,
	}
	if frame.RSV {
		summary.InspectNote = "reserved_bits_or_compressed_extension"
		summary.Shadow = &ShadowSummary{Blocker: summary.InspectNote}
		return summary
	}
	if frame.Opcode != 1 && frame.Opcode != 0 {
		return summary
	}
	if len(payload) == 0 {
		return summary
	}
	applyJSONShape(&summary, payload)
	applyShadowEstimate(&summary, payload)
	return summary
}

func OpcodeName(op byte) string {
	switch op {
	case 0:
		return "continuation"
	case 1:
		return "text"
	case 2:
		return "binary"
	case 8:
		return "close"
	case 9:
		return "ping"
	case 10:
		return "pong"
	default:
		return fmt.Sprintf("unknown_%d", op)
	}
}

func applyJSONShape(summary *FrameSummary, payload []byte) {
	var value any
	if err := json.Unmarshal(payload, &value); err != nil {
		summary.InspectNote = "non_json_text"
		return
	}
	summary.JSON = true
	switch typed := value.(type) {
	case map[string]any:
		summary.JSONTopLevel = "object"
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		summary.JSONKeys = keys
		summary.MessageType = firstStringField(typed, "type", "event", "method")
	case []any:
		summary.JSONTopLevel = "array"
	default:
		summary.JSONTopLevel = "scalar"
	}
}

func applyShadowEstimate(summary *FrameSummary, payload []byte) {
	if !summary.JSON {
		if summary.InspectNote != "" {
			summary.Shadow = &ShadowSummary{Blocker: summary.InspectNote}
		}
		return
	}
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, payload); err != nil {
		summary.Shadow = &ShadowSummary{Blocker: "json_compact_failed"}
		return
	}
	originalBytes := int64(len(payload))
	compressedBytes := int64(compacted.Len())
	shadow := &ShadowSummary{
		OriginalBytes:    originalBytes,
		CompressedBytes:  compressedBytes,
		OriginalTokens:   shadowEstimateTokens(originalBytes),
		CompressedTokens: shadowEstimateTokens(compressedBytes),
	}
	if compressedBytes < originalBytes {
		shadow.Eligible = true
		shadow.SavedBytes = originalBytes - compressedBytes
		shadow.SavedTokens = shadow.OriginalTokens - shadow.CompressedTokens
		shadow.AppliedLayers = []string{"json_compact"}
	} else {
		shadow.Blocker = "no_savings"
	}
	summary.Shadow = shadow
}

func shadowEstimateTokens(bytes int64) int {
	if bytes <= 0 {
		return 0
	}
	tokens := int((bytes + 3) / 4)
	return tokens
}

func firstStringField(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := obj[key].(string); ok {
			return value
		}
	}
	return ""
}
