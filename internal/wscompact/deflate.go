package wscompact

import (
	"bytes"
	"compress/flate"
	"errors"
	"io"
)

const maxDeflateDict = 32 * 1024

var syncFlushTail = []byte{0x00, 0x00, 0xff, 0xff}

// InflateContext inflates permessage-deflate messages with optional
// context takeover.
type InflateContext struct {
	noContextTakeover bool
	dict              []byte
	blocked           bool
}

// DeflateContext deflates permessage-deflate messages with optional
// context takeover.
type DeflateContext struct {
	noContextTakeover bool
	dict              []byte
	blocked           bool
}

func NewInflateContext(noContextTakeover bool) *InflateContext {
	return &InflateContext{noContextTakeover: noContextTakeover}
}

func NewDeflateContext(noContextTakeover bool) *DeflateContext {
	return &DeflateContext{noContextTakeover: noContextTakeover}
}

func (c *InflateContext) Blocked() bool {
	return c == nil || c.blocked
}

func (c *DeflateContext) Blocked() bool {
	return c == nil || c.blocked
}

func (c *InflateContext) Inflate(payload []byte) ([]byte, error) {
	return c.InflateWithLimit(payload, 0)
}

func (c *InflateContext) InflateWithLimit(payload []byte, maxPlaintextBytes int) ([]byte, error) {
	if c == nil || c.blocked {
		return nil, errDeflateContextBlocked
	}
	input := make([]byte, 0, len(payload)+len(syncFlushTail))
	input = append(input, payload...)
	input = append(input, syncFlushTail...)
	reader := flate.NewReaderDict(bytes.NewReader(input), c.dict)
	var out []byte
	var err error
	if maxPlaintextBytes > 0 {
		out, err = io.ReadAll(io.LimitReader(reader, int64(maxPlaintextBytes)+1))
	} else {
		out, err = io.ReadAll(reader)
	}
	closeErr := reader.Close()
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		c.blocked = true
		return nil, err
	}
	if closeErr != nil && !errors.Is(closeErr, io.ErrUnexpectedEOF) {
		c.blocked = true
		return nil, closeErr
	}
	if maxPlaintextBytes > 0 && len(out) > maxPlaintextBytes {
		c.blocked = true
		return nil, errInflateOutputTooLarge
	}
	c.observe(out)
	return out, nil
}

func (c *DeflateContext) Deflate(payload []byte) ([]byte, error) {
	if c == nil || c.blocked {
		return nil, errDeflateContextBlocked
	}
	var out bytes.Buffer
	writer, err := flate.NewWriterDict(&out, flate.DefaultCompression, c.dict)
	if err != nil {
		c.blocked = true
		return nil, err
	}
	if _, err := writer.Write(payload); err != nil {
		c.blocked = true
		return nil, err
	}
	if err := writer.Flush(); err != nil {
		c.blocked = true
		return nil, err
	}
	compressed := append([]byte(nil), out.Bytes()...)
	if !bytes.HasSuffix(compressed, syncFlushTail) {
		c.blocked = true
		return nil, errMissingSyncFlushTail
	}
	compressed = compressed[:len(compressed)-len(syncFlushTail)]
	c.observe(payload)
	return compressed, nil
}

// Observe advances the destination-side dictionary when Slimference
// forwards an existing compressed message byte-equal.
func (c *DeflateContext) Observe(payload []byte) error {
	if c == nil || c.blocked {
		return errDeflateContextBlocked
	}
	c.observe(payload)
	return nil
}

func (c *InflateContext) observe(payload []byte) {
	if c.noContextTakeover {
		c.dict = nil
		return
	}
	c.dict = appendDict(c.dict, payload)
}

func (c *DeflateContext) observe(payload []byte) {
	if c.noContextTakeover {
		c.dict = nil
		return
	}
	c.dict = appendDict(c.dict, payload)
}

func appendDict(dict, payload []byte) []byte {
	if len(payload) >= maxDeflateDict {
		return append([]byte(nil), payload[len(payload)-maxDeflateDict:]...)
	}
	out := make([]byte, 0, len(dict)+len(payload))
	out = append(out, dict...)
	out = append(out, payload...)
	if len(out) > maxDeflateDict {
		out = out[len(out)-maxDeflateDict:]
	}
	return out
}

var (
	errDeflateContextBlocked = errors.New("wscompact: deflate context blocked")
	errMissingSyncFlushTail  = errors.New("wscompact: missing sync-flush tail")
	errInflateOutputTooLarge = errors.New("wscompact: inflate output too large")
)
