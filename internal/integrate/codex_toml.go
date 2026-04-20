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
func WriteCodexBlock(home, proxyURL string) (WriteEvent, error) {
	path := CodexConfigPath(home)
	dir := filepath.Dir(path)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return WriteEvent{Path: path, Action: "skipped_client_absent"}, nil
		}
		return WriteEvent{}, err
	}

	content, exists, err := ReadRC(path)
	if err != nil {
		return WriteEvent{}, err
	}
	body := codexBlockBody(proxyURL)

	_, existingBlock, _, hasBlock := splitBlock(content)
	if hasBlock && strings.Contains(existingBlock, body) {
		return WriteEvent{Path: path, Action: "skipped_idempotent"}, nil
	}

	if exists {
		if _, err := backupOnce(path); err != nil {
			return WriteEvent{}, err
		}
	}

	newContent := replaceOrAppendBlock(content, body)
	if err := writeAtomic(path, []byte(newContent), 0o644); err != nil {
		return WriteEvent{}, err
	}
	return WriteEvent{Path: path, Action: "wrote_block"}, nil
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
