package integrate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CodexConfigPath returns ~/.codex/config.toml inside the given home.
func CodexConfigPath(home string) string {
	return filepath.Join(home, ".codex", "config.toml")
}

// codexBlockBody returns the TOML body we insert into Codex config.toml.
// We set both openai_base_url (the model-responses endpoint, matches
// ANTHROPIC_BASE_URL semantically) and chatgpt_base_url (the backend-api
// routes that Codex uses for session + rate-limit metadata). Pointing both
// at Slimference means the proxy sees 100% of Codex's request volume.
func codexBlockBody(proxyURL string) string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		proxyURL = ProxyURL
	}
	return fmt.Sprintf(
		`openai_base_url = "%s"
chatgpt_base_url = "%s"`,
		proxyURL, proxyURL,
	)
}

// WriteCodexBlock upserts the Slimference block in Codex's config.toml.
// The file is treated as a string (not TOML-round-tripped) so comments and
// key ordering stay byte-equal outside our fenced block.
//
// If ~/.codex/ does not exist yet (Codex not installed) the function returns
// an event indicating skip - no directories are created.
//
// TOML scoping: our block must land at the top level of the document.
// TOML spec: once a `[table]` header is encountered, all subsequent
// key=value lines belong to that table until either the next `[header]`
// or EOF. Appending our block at the end of a file that contains tables
// would silently nest our keys inside the last table, and Codex would
// never see them at the root. WriteCodexBlock therefore inserts the
// fence BEFORE the first `[table]` header (or at EOF when no tables
// exist), guaranteeing top-level scope.
//
// Duplicate-key safety: TOML forbids a key appearing twice at the same
// table level. If the user has a manual `openai_base_url` or
// `chatgpt_base_url` line outside our fence AND at a scope that would
// conflict with ours, we strip it first.
func WriteCodexBlock(home, proxyURL string) (WriteEvent, error) {
	path := CodexConfigPath(home)
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return WriteEvent{Path: path, Action: "skipped_client_absent"}, nil
		}
	}

	content, exists, err := ReadRC(path)
	if err != nil {
		return WriteEvent{}, err
	}
	body := codexBlockBody(proxyURL)

	// Detect the current fence and decide whether the file already matches
	// what we would write. Idempotence requires BOTH (a) the fence body
	// matches our target AND (b) the fence sits at top-level scope AND
	// (c) no conflicting user line outside the fence.
	existingFenceOK := fenceIsTopLevelWithBody(content, body)
	outsideClean := !hasConflictingKeyOutsideFence(content)
	if existingFenceOK && outsideClean {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}

	if exists {
		if _, err := backupOnce(path); err != nil {
			return WriteEvent{}, err
		}
	}

	// 1. Remove any existing fence so we can re-insert at the right scope.
	stripped := stripFenceBlock(content)
	// 2. Strip conflicting user-owned top-level lines from the non-fence
	//    content (conservative: only strips lines that are unambiguously
	//    at top level).
	sanitised := stripConflictingTopLevelKeys(stripped)
	// 3. Insert the fence block BEFORE the first `[table]` header. If no
	//    tables exist, append at EOF.
	newContent := insertBeforeFirstTable(sanitised, renderBlock(body))

	if err := writeAtomic(path, []byte(newContent), 0o644); err != nil {
		return WriteEvent{}, err
	}
	return WriteEvent{Path: path, Action: "wrote_block"}, nil
}

// stripFenceBlock removes the Slimference fence (and any content inside it)
// from the given content. Comments + surrounding content preserved.
func stripFenceBlock(content string) string {
	_, _, _, exists := splitBlock(content)
	if !exists {
		return content
	}
	return replaceOrAppendBlock(content, "")
}

// insertBeforeFirstTable places block right before the first line that
// starts a `[table]` at column 0. If no such line exists (or the content
// is empty) the block is appended at the end.
func insertBeforeFirstTable(content, block string) string {
	lines := strings.Split(content, "\n")
	insertAt := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			insertAt = i
			break
		}
	}
	if insertAt < 0 {
		// No tables - append at EOF. replaceOrAppendBlock semantics.
		return replaceOrAppendBlock(content, strings.TrimSuffix(
			strings.TrimPrefix(block, markerStart+"\n"), markerEnd+"\n"))
	}
	before := strings.Join(lines[:insertAt], "\n")
	after := strings.Join(lines[insertAt:], "\n")
	before = strings.TrimRight(before, "\n")
	if before != "" {
		before += "\n\n"
	}
	result := before + block
	if !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	if !strings.HasPrefix(after, "\n") {
		result += "\n"
	}
	result += after
	// Normalise trailing newlines.
	result = strings.TrimRight(result, "\n") + "\n"
	return result
}

// fenceIsTopLevelWithBody reports whether the Slimference fence (a) exists,
// (b) sits before the first `[table]`, and (c) contains exactly the target
// body text.
func fenceIsTopLevelWithBody(content, body string) bool {
	_, existingBlock, _, has := splitBlock(content)
	if !has {
		return false
	}
	if !strings.Contains(existingBlock, strings.TrimSpace(body)) {
		return false
	}
	// Scope check: find first [table] before the fence.
	fenceIdx := strings.Index(content, markerStart)
	preFence := content[:fenceIdx]
	for _, line := range strings.Split(preFence, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			return false // fence sits inside a table scope
		}
	}
	return true
}

// hasConflictingKeyOutsideFence reports whether any line outside our fence
// declares a conflicting top-level key. Conservative: only counts lines
// that are unambiguously at top-level scope (not inside any table).
func hasConflictingKeyOutsideFence(content string) bool {
	before, _, after, _ := splitBlock(content)
	return countTopLevelKey(before+"\n"+after, "openai_base_url") > 0 ||
		countTopLevelKey(before+"\n"+after, "chatgpt_base_url") > 0
}

// stripConflictingTopLevelKeys removes top-level conflicting lines from
// content (no fence present). Keys inside a `[table]` are preserved.
func stripConflictingTopLevelKeys(content string) string {
	var out []string
	inTable := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTable = true
			out = append(out, line)
			continue
		}
		if inTable {
			out = append(out, line)
			continue
		}
		if isConflictingTopLevelKey(trimmed) {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// isConflictingTopLevelKey returns true when the given line is a TOML
// top-level assignment to openai_base_url or chatgpt_base_url. Matches
// `key = value` with optional whitespace; ignores comments.
func isConflictingTopLevelKey(line string) bool {
	if strings.HasPrefix(line, "#") {
		return false
	}
	for _, key := range []string{"openai_base_url", "chatgpt_base_url"} {
		prefix := key
		if strings.HasPrefix(line, prefix+" =") ||
			strings.HasPrefix(line, prefix+"=") {
			return true
		}
	}
	return false
}

// hasDuplicateTopLevelKey reports whether the file would end up with more
// than one top-level `openai_base_url` or `chatgpt_base_url` after the
// Slimference fence is applied. Used to force a rewrite when the user
// has a stale manual entry outside our fence.
func hasDuplicateTopLevelKey(content string) bool {
	before, _, after, _ := splitBlock(content)
	outside := before + after
	return countTopLevelKey(outside, "openai_base_url") > 0 ||
		countTopLevelKey(outside, "chatgpt_base_url") > 0
}

// countTopLevelKey counts top-level assignments of `key` in text, ignoring
// anything inside a [table] or [table.sub] section.
func countTopLevelKey(text, key string) int {
	n := 0
	inTable := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inTable = true
			continue
		}
		if inTable {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+" =") ||
			strings.HasPrefix(trimmed, key+"=") {
			n++
		}
	}
	return n
}

// RemoveCodexBlock strips the Slimference block from config.toml. Safe to
// run even if the file does not exist.
func RemoveCodexBlock(home string) (WriteEvent, error) {
	path := CodexConfigPath(home)
	content, exists, err := ReadRC(path)
	if err != nil {
		return WriteEvent{}, err
	}
	if !exists {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}
	_, _, _, hasBlock := splitBlock(content)
	if !hasBlock {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}
	if _, err := backupOnce(path); err != nil {
		return WriteEvent{}, err
	}
	newContent := replaceOrAppendBlock(content, "")
	if err := writeAtomic(path, []byte(newContent), 0o644); err != nil {
		return WriteEvent{}, err
	}
	return WriteEvent{Path: path, Action: "removed_block"}, nil
}

// HasCodexBlock returns whether the Slimference block is present in the file.
// Used by the status detector.
func HasCodexBlock(home string) bool {
	content, exists, err := ReadRC(CodexConfigPath(home))
	if !exists || err != nil {
		return false
	}
	_, _, _, has := splitBlock(content)
	return has
}

// HasCompleteCodexBlock reports whether config.toml contains the current
// Slimference-owned Codex block with both required base URLs.
func HasCompleteCodexBlock(home, proxyURL string) bool {
	content, exists, err := ReadRC(CodexConfigPath(home))
	if !exists || err != nil {
		return false
	}
	return fenceIsTopLevelWithBody(content, codexBlockBody(proxyURL)) &&
		!hasConflictingKeyOutsideFence(content)
}
