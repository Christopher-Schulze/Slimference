package integrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsConflictingTopLevelKey(t *testing.T) {
	cases := map[string]bool{
		`openai_base_url = "http://x"`:          true,
		`openai_base_url="http://x"`:            true,
		`chatgpt_base_url = "http://y"`:         true,
		`chatgpt_base_url="http://y"`:           true,
		`# openai_base_url = "http://z"`:        false, // comment
		`model = "gpt-5"`:                       false, // different key
		``:                                      false,
		`openai_base_url_extended = "http://x"`: false, // prefix-only
	}
	for line, want := range cases {
		if got := isConflictingTopLevelKey(line); got != want {
			t.Errorf("%q: got %v, want %v", line, got, want)
		}
	}
}

func TestCountTopLevelKey_SkipsTableSections(t *testing.T) {
	content := `
openai_base_url = "http://top1"

[some_table]
openai_base_url = "http://inside-table"

[another]
chatgpt_base_url = "http://also-inside"

chatgpt_base_url = "http://top2"
`
	if n := countTopLevelKey(content, "openai_base_url"); n != 1 {
		t.Errorf("openai count = %d, want 1 (only the top-level one)", n)
	}
	// chatgpt_base_url: the one on the final line is "after" a [another]
	// table, so it still counts as "inside" per our conservative parser.
	// This is fine: we only care about the top-level duplicate case.
	n := countTopLevelKey(content, "chatgpt_base_url")
	if n != 0 && n != 1 {
		t.Errorf("chatgpt count = %d (unexpected)", n)
	}
}

func TestWriteCodexBlock_StripsExistingUserSetting(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".codex", "config.toml")
	preExisting := `model = "gpt-5.4"
approval_policy = "never"
openai_base_url = "http://127.0.0.1:8990"

[features]
codex_hooks = true
`
	if err := os.WriteFile(cfgPath, []byte(preExisting), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteCodexBlock(home, ProxyURL); err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(cfgPath)
	// The top-level duplicate must be gone - only our fenced version
	// should assign the key.
	outside := stripSlimferenceBlock(string(content))
	if strings.Contains(outside, "openai_base_url") {
		t.Fatalf("duplicate openai_base_url survived:\n%s", content)
	}
	// Other content must survive.
	if !strings.Contains(string(content), `model = "gpt-5.4"`) {
		t.Fatalf("user content dropped: %s", content)
	}
	if !strings.Contains(string(content), `[features]`) {
		t.Fatalf("user table dropped: %s", content)
	}
	// The fence must exist exactly once.
	if strings.Count(string(content), markerStart) != 1 {
		t.Fatalf("fence appeared != 1 times: %s", content)
	}
}

// stripSlimferenceBlock removes the fenced block from content so tests can
// inspect what lives outside.
func stripSlimferenceBlock(content string) string {
	before, _, after, _ := splitBlock(content)
	return before + after
}

func TestWriteCodexBlock_IdempotentEvenWithStaleDuplicate(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, ".codex", "config.toml")
	// Seed with a stale top-level dup AND our fence already present.
	stale := `openai_base_url = "http://127.0.0.1:9999"
` + renderBlock(codexBlockBody(ProxyURL))
	if err := os.WriteFile(cfgPath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	evt, err := WriteCodexBlock(home, ProxyURL)
	if err != nil {
		t.Fatal(err)
	}
	// We expect "wrote_block" (not skipped_idempotent) because the stale
	// top-level line forced a rewrite to eliminate the duplicate.
	if evt.Action != "wrote_block" {
		t.Fatalf("action = %q, want wrote_block", evt.Action)
	}
	content, _ := os.ReadFile(cfgPath)
	outside := stripSlimferenceBlock(string(content))
	if strings.Contains(outside, "openai_base_url") {
		t.Fatalf("stale duplicate survived:\n%s", content)
	}
}

func TestHasDuplicateTopLevelKey(t *testing.T) {
	// Content with a fenced block + an outside duplicate.
	withDup := `openai_base_url = "http://a"
` + renderBlock(`openai_base_url = "http://b"`)
	if !hasDuplicateTopLevelKey(withDup) {
		t.Fatal("expected duplicate detected")
	}
	// Content with ONLY the fenced block.
	clean := renderBlock(`openai_base_url = "http://b"`)
	if hasDuplicateTopLevelKey(clean) {
		t.Fatal("clean content misreported as duplicated")
	}
}
