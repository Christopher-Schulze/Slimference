package filter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// isStylelintJSONArgv reports stylelint invocations that explicitly request the
// built-in JSON formatter. Custom formatters stay full-pass because their schema
// is caller-defined.
func isStylelintJSONArgv(argv []string) bool {
	tail := stylelintArgvTail(argv)
	if len(tail) == 0 {
		return false
	}
	for i, a := range tail {
		switch {
		case a == "--custom-formatter":
			return false
		case strings.HasPrefix(a, "--custom-formatter="):
			return false
		case a == "--formatter=json", a == "-f=json":
			return true
		case (a == "--formatter" || a == "-f") && i+1 < len(tail) && tail[i+1] == "json":
			return true
		}
	}
	return false
}

func stylelintArgvTail(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	b0 := stylelintArgvBase(argv[0])
	if b0 == "npx" || b0 == "npx.cmd" {
		rest, ok := npxArgvSuffix(argv)
		if !ok {
			return nil
		}
		return stylelintArgvTail(rest)
	}
	if len(argv) >= 3 && (b0 == "pnpm" || b0 == "pnpm.cmd") && argv[1] == "exec" {
		return stylelintArgvTail(argv[2:])
	}
	if len(argv) >= 2 && (b0 == "yarn" || b0 == "yarn.cmd" || b0 == "yarnpkg") {
		return stylelintArgvTail(argv[1:])
	}
	base := stylelintArgvBase(argv[0])
	switch base {
	case "stylelint", "stylelint.js", "stylelint.cmd", "stylelint.exe":
		return argv
	default:
		return nil
	}
}

func stylelintArgvBase(s string) string {
	base := strings.ToLower(s)
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	return base
}

// TryCompactStylelintJSON compacts Stylelint JSON formatter output only when it
// proves a clean no-findings result. Findings, ignored files, autofix facts,
// deprecations, invalid-option warnings, malformed JSON, or schema drift fail
// open to the original output.
func TryCompactStylelintJSON(argv []string, stdout []byte) ([]byte, bool) {
	if !isStylelintJSONArgv(argv) {
		return stdout, false
	}
	trimmed := bytes.TrimSpace(stdout)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return stdout, false
	}
	var rows []map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	if err := dec.Decode(&rows); err != nil {
		return stdout, false
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return stdout, false
	}
	if len(rows) == 0 {
		return stdout, false
	}
	for _, row := range rows {
		if !stylelintJSONRowClean(row) {
			return stdout, false
		}
	}
	out := []byte(fmt.Sprintf("[stylelint] clean (%d file(s))\n", len(rows)))
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return out, true
}

func stylelintJSONRowClean(row map[string]json.RawMessage) bool {
	for key := range row {
		switch key {
		case "source", "errored", "warnings", "deprecations", "invalidOptionWarnings", "ignored", "autofixed":
		default:
			return false
		}
	}
	var source string
	if !decodeStylelintJSONField(row, "source", &source) || strings.TrimSpace(source) == "" {
		return false
	}
	var errored bool
	if !decodeStylelintJSONField(row, "errored", &errored) || errored {
		return false
	}
	if !stylelintJSONArrayFieldEmpty(row, "warnings") ||
		!stylelintJSONArrayFieldEmpty(row, "deprecations") ||
		!stylelintJSONArrayFieldEmpty(row, "invalidOptionWarnings") {
		return false
	}
	if !stylelintJSONOptionalFalse(row, "ignored") || !stylelintJSONOptionalFalse(row, "autofixed") {
		return false
	}
	return true
}

func decodeStylelintJSONField(row map[string]json.RawMessage, key string, dst any) bool {
	raw, ok := row[key]
	if !ok {
		return false
	}
	return json.Unmarshal(raw, dst) == nil
}

func stylelintJSONArrayFieldEmpty(row map[string]json.RawMessage, key string) bool {
	raw, ok := row[key]
	if !ok {
		return false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return false
	}
	return len(values) == 0
}

func stylelintJSONOptionalFalse(row map[string]json.RawMessage, key string) bool {
	raw, ok := row[key]
	if !ok {
		return true
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false
	}
	return !value
}
