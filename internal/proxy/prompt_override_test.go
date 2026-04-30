package proxy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/summarization"
)

func TestNew_LoadsPromptOverride(t *testing.T) {
	t.Cleanup(func() { summarization.SetPromptOverride("", "") })

	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("# version: vX-test\n\noverride header body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Compression.PromptOverridePath = path
	_ = New(cfg)
	if got := summarization.PromptVersion(); got != "vX-test" {
		t.Fatalf("active prompt version: %q", got)
	}
}

func TestNew_PromptOverrideMissingPathLogged(t *testing.T) {
	t.Cleanup(func() { summarization.SetPromptOverride("", "") })

	cfg := config.Defaults()
	cfg.Compression.PromptOverridePath = filepath.Join(t.TempDir(), "missing.txt")
	// Should not panic; the missing file is best-effort.
	_ = New(cfg)
	if got := summarization.PromptVersion(); got != "default" {
		t.Fatalf("missing file should keep default, got %q", got)
	}
}
