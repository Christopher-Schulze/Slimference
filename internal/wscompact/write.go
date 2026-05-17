package wscompact

import (
	"encoding/binary"
	"errors"
	"io"
)

// WSOpcode is the RFC 6455 frame opcode.
type WSOpcode byte

const (
	OpcodeContinuation WSOpcode = 0x0
	OpcodeText         WSOpcode = 0x1
	OpcodeBinary       WSOpcode = 0x2
	OpcodeClose        WSOpcode = 0x8
	OpcodePing         WSOpcode = 0x9
	OpcodePong         WSOpcode = 0xa
)

// WriteFrame encodes a single RFC 6455 frame and writes it to w.
// `fin` marks the final frame of a message; `opcode` selects the
// frame type. When `maskKey` is non-nil, the payload is XOR-masked
// (RFC requires masking on client→server traffic and forbids it
// on server→client). Returns the number of bytes written so callers
// can update telemetry counters.
//
// payloadLen is naturally bounded by Go's slice length and encoded
// into the WebSocket 64-bit extended length field when needed.
func WriteFrame(w io.Writer, fin bool, opcode WSOpcode, maskKey []byte, payload []byte) (int, error) {
	if maskKey != nil && len(maskKey) != 4 {
		return 0, errInvalidMaskKey
	}

	header := make([]byte, 0, 14)
	first := byte(opcode) & 0x0f
	if fin {
		first |= 0x80
	}
	header = append(header, first)

	masked := maskKey != nil
	lenByte := byte(0)
	if masked {
		lenByte |= 0x80
	}
	switch {
	case len(payload) <= 125:
		header = append(header, lenByte|byte(len(payload)))
	case len(payload) <= 0xffff:
		header = append(header, lenByte|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(len(payload)))
		header = append(header, ext[:]...)
	default:
		header = append(header, lenByte|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(len(payload)))
		header = append(header, ext[:]...)
	}
	if masked {
		header = append(header, maskKey...)
	}

	if masked {
		// Don't mutate the caller's payload buffer; clone first.
		masked := make([]byte, len(payload))
		for i, b := range payload {
			masked[i] = b ^ maskKey[i%4]
		}
		payload = masked
	}

	total := 0
	if n, err := w.Write(header); err != nil {
		return n, err
	} else {
		total += n
	}
	if len(payload) > 0 {
		if n, err := w.Write(payload); err != nil {
			return total + n, err
		} else {
			total += n
		}
	}
	return total, nil
}

var errInvalidMaskKey = errors.New("wscompact: maskKey must be exactly 4 bytes")
