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

func isVCSHostJSONExactArgv(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "gh" && b != "gh.exe" && b != "glab" && b != "glab.exe" {
		return false
	}
	return strings.EqualFold(argv[1], "api") || argvHasExplicitJSONOutput(argv[1:])
}

func argvHasExplicitJSONOutput(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		switch {
		case arg == "--json" || strings.HasPrefix(arg, "--json="):
			return true
		case arg == "--format=json" || arg == "--output=json" || arg == "-o=json":
			return true
		case arg == "--format" || arg == "--output" || arg == "-o":
			return i+1 < len(args) && strings.EqualFold(strings.TrimSpace(args[i+1]), "json")
		}
	}
	return false
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

// TryCompactVCSHostJSONExact exact-minifies GitHub/GitLab CLI JSON output and
// otherwise full-passes it so generic reducers cannot schema-summarize API bodies.
func TryCompactVCSHostJSONExact(argv []string, stdout []byte) ([]byte, bool) {
	if !isVCSHostJSONExactArgv(argv) {
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
