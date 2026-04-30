package summarization

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptVersion_DefaultWhenUnset(t *testing.T) {
	SetPromptOverride("", "")
	if got := PromptVersion(); got != "default" {
		t.Fatalf("default version expected, got %q", got)
	}
}

func TestPromptVersion_ReturnsConfigured(t *testing.T) {
	SetPromptOverride("override body", "v2.1")
	t.Cleanup(func() { SetPromptOverride("", "") })
	if got := PromptVersion(); got != "v2.1" {
		t.Fatalf("expected v2.1, got %q", got)
	}
}

func TestSetPromptOverride_AffectsBuildSystemPrompt(t *testing.T) {
	SetPromptOverride("CUSTOM HEADER REPLACE\n", "v9")
	t.Cleanup(func() { SetPromptOverride("", "") })
	got := buildSystemPrompt("ambiguous")
	if !strings.Contains(got, "CUSTOM HEADER REPLACE") {
		t.Fatalf("override not applied: %s", got[:200])
	}
	if strings.Contains(got, "deterministic information extractor") {
		t.Fatalf("default header should be replaced: %s", got[:200])
	}
}

func TestParsePromptDocument_VersionExtracted(t *testing.T) {
	body, version := parsePromptDocument("# version: v3.2\n\nMain body line\nSecond line\n")
	if version != "v3.2" {
		t.Fatalf("version: %q", version)
	}
	if !strings.Contains(body, "Main body line") {
		t.Fatalf("body wrong: %q", body)
	}
}

func TestParsePromptDocument_NoVersion(t *testing.T) {
	body, version := parsePromptDocument("Plain body\nLine 2\n")
	if version != "" {
		t.Fatalf("expected empty version, got %q", version)
	}
	if !strings.HasPrefix(body, "Plain body") {
		t.Fatalf("body wrong: %q", body)
	}
}

func TestParsePromptDocument_OnlyHeader(t *testing.T) {
	body, version := parsePromptDocument("# version: v1.0\n")
	if version != "v1.0" || body != "" {
		t.Fatalf("got body=%q version=%q", body, version)
	}
}

func TestLoadPromptOverrideFromPath_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(path, []byte("# version: v9\n\nCustom prompt body content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { SetPromptOverride("", "") })
	version, err := LoadPromptOverrideFromPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if version != "v9" {
		t.Fatalf("version: %q", version)
	}
	if PromptVersion() != "v9" {
		t.Fatalf("version not applied")
	}
}

func TestLoadPromptOverrideFromPath_MissingFile(t *testing.T) {
	if _, err := LoadPromptOverrideFromPath(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("expected read error")
	}
}
