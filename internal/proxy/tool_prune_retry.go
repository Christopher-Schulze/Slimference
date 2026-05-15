package proxy

import (
	"bytes"
	"io"
	"net/http"

	"github.com/slimference/slimference/internal/toolprune"
)

func peekMissingToolDefinitionError(resp *http.Response) bool {
	if resp == nil || resp.StatusCode < 400 || resp.StatusCode >= 500 {
		return false
	}
	const maxPeek = 64 * 1024
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, maxPeek))
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(buf))
	return toolprune.LooksLikeMissingToolError(resp.StatusCode, buf)
}
