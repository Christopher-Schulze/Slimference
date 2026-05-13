package filter

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

type PostToolPayload struct {
	CommandLine  string
	ToolResponse string
	ToolName     string
	ToolUseID    string
	SessionID    string
	CWD          string
	FilePaths    []string
}

func ExtractPostToolDetailsFromHookJSON(b []byte) (PostToolPayload, error) {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return PostToolPayload{}, fmt.Errorf("filter: JSON: %w", err)
	}
	toolResponse, _ := findStringForKey(v, "tool_response")
	if toolResponse == "" {
		return PostToolPayload{}, fmt.Errorf("filter: no string field %q in JSON", "tool_response")
	}
	command, _ := findStringForKey(v, "command")
	toolName, ok := findStringForKey(v, "tool_name")
	if !ok {
		toolName, _ = findStringForKey(v, "toolName")
	}
	toolUseID, ok := findStringForKey(v, "tool_use_id")
	if !ok {
		toolUseID, _ = findStringForKey(v, "toolUseID")
	}
	sessionID, ok := findStringForKey(v, "session_id")
	if !ok {
		sessionID, _ = findStringForKey(v, "conversation_id")
	}
	cwd, _ := findStringForKey(v, "cwd")
	return PostToolPayload{
		CommandLine:  command,
		ToolResponse: toolResponse,
		ToolName:     toolName,
		ToolUseID:    toolUseID,
		SessionID:    sessionID,
		CWD:          cwd,
		FilePaths:    collectPostToolFilePaths(v, command),
	}, nil
}

func collectPostToolFilePaths(v interface{}, command string) []string {
	seen := map[string]struct{}{}
	add := func(path string) {
		path = strings.TrimSpace(path)
		if path == "" || strings.Contains(path, "\n") {
			return
		}
		path = filepath.Clean(path)
		if path == "." {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
	}
	for _, key := range []string{"file_path", "filePath", "filepath", "path"} {
		for _, path := range findStringsForKey(v, key) {
			add(path)
		}
	}
	for _, path := range patchPathsFromText(command) {
		add(path)
	}
	out := make([]string, 0, len(seen))
	for path := range seen {
		out = append(out, path)
	}
	sortStrings(out)
	return out
}

func findStringsForKey(v interface{}, key string) []string {
	var out []string
	var walk func(interface{})
	walk = func(cur interface{}) {
		switch t := cur.(type) {
		case map[string]interface{}:
			if s, ok := t[key].(string); ok {
				out = append(out, s)
			}
			for _, child := range t {
				walk(child)
			}
		case []interface{}:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func patchPathsFromText(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"*** Add File:", "*** Update File:", "*** Delete File:"} {
			if strings.HasPrefix(line, prefix) {
				out = append(out, strings.TrimSpace(strings.TrimPrefix(line, prefix)))
			}
		}
	}
	return out
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
