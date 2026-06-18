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
	return b == "curl" || b == "curl.exe" ||
		b == "wget" || b == "wget.exe" ||
		b == "http" || b == "http.exe" ||
		b == "https" || b == "https.exe"
}

func isAPIJSONExactArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	return (b == "gh" || b == "gh.exe" || b == "glab" || b == "glab.exe") && strings.EqualFold(argv[1], "api")
}

// TryCompactNetworkResponse only performs exact network-response reductions.
// It intentionally matches common HTTP client output to stop later lossy generic
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

// TryCompactAPIJSONExact exact-minifies API CLI JSON output and otherwise
// full-passes it so generic reducers cannot schema-summarize API bodies.
func TryCompactAPIJSONExact(argv []string, stdout []byte) ([]byte, bool) {
	if !isAPIJSONExactArgv(argv) {
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
