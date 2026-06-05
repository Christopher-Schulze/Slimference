package filter

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
)

func isNetworkResponseArgv(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return b == "curl" || b == "curl.exe" || b == "wget" || b == "wget.exe"
}

// TryCompactNetworkResponse only performs exact network-response reductions.
// It intentionally matches all curl/wget output to stop later lossy generic
// reducers from schema-summarizing or log-windowing API bodies.
func TryCompactNetworkResponse(argv []string, stdout []byte) ([]byte, bool) {
	if !isNetworkResponseArgv(argv) {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return stdout, true
	}
	var buf bytes.Buffer
	buf.Grow(len(trimmed))
	if err := json.Compact(&buf, trimmed); err != nil {
		return stdout, true
	}
	compact := buf.Bytes()
	if len(compact) < len(stdout) {
		return compact, true
	}
	return stdout, true
}
