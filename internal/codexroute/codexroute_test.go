package codexroute

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCodexConfig(t *testing.T, home, body string) string {
	t.Helper()
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestEnableSkippedWhenCodexConfigAbsent(t *testing.T) {
	home := t.TempDir()
	evt, err := Enable(home, ProxyURL("127.0.0.1", "8990"))
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if evt.Action != "skipped_codex_config_absent" {
		t.Fatalf("event=%+v", evt)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf("Enable must not create ~/.codex, stat err=%v", err)
	}
}

func TestEnableWritesProviderBlockBeforeFirstTable(t *testing.T) {
	home := t.TempDir()
	path := writeCodexConfig(t, home, "model = \"gpt-5\"\n\n[features]\nhooks = true\n")
	evt, err := Enable(home, ProxyURL("127.0.0.1", "8990"))
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if evt.Action != "wrote_block" || evt.Path != path {
		t.Fatalf("event=%+v", evt)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(gotBytes)
	for _, want := range []string{
		markerStart,
		`model_provider = "slimference-codex"`,
		`[model_providers.slimference-codex]`,
		`base_url = "http://127.0.0.1:8990/backend-api/codex"`,
		`supports_websockets = false`,
		`wire_api = "responses"`,
		markerEnd,
		"[features]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("config missing %q:\n%s", want, got)
		}
	}
	if strings.Index(got, markerStart) > strings.Index(got, "[features]") {
		t.Fatalf("route block must stay top-level before first table:\n%s", got)
	}
	matches, err := filepath.Glob(path + ".slimference-codex-route-backup-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("backup matches=%v err=%v", matches, err)
	}
	status, err := Inspect(home, ProxyURL("127.0.0.1", "8990"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if !status.Exists || !status.Enabled || !status.Complete || status.Conflict != "" {
		t.Fatalf("bad status: %+v", status)
	}
}

func TestEnableIsIdempotent(t *testing.T) {
	home := t.TempDir()
	writeCodexConfig(t, home, "suppress_unstable_features_warning = true\n")
	if _, err := Enable(home, ProxyURL("127.0.0.1", "8990")); err != nil {
		t.Fatalf("Enable first: %v", err)
	}
	evt, err := Enable(home, ProxyURL("127.0.0.1", "8990"))
	if err != nil {
		t.Fatalf("Enable second: %v", err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("event=%+v", evt)
	}
}

func TestDisableRemovesOnlyRouteBlock(t *testing.T) {
	home := t.TempDir()
	path := writeCodexConfig(t, home, "model = \"gpt-5\"\n")
	if _, err := Enable(home, ProxyURL("127.0.0.1", "8990")); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	evt, err := Disable(home)
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if evt.Action != "removed_block" {
		t.Fatalf("event=%+v", evt)
	}
	gotBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	got := string(gotBytes)
	if strings.Contains(got, markerStart) || !strings.Contains(got, `model = "gpt-5"`) {
		t.Fatalf("bad disable output:\n%s", got)
	}
	evt, err = Disable(home)
	if err != nil {
		t.Fatalf("Disable second: %v", err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("event=%+v", evt)
	}
}

func TestDisableSkippedWhenCodexConfigAbsent(t *testing.T) {
	home := t.TempDir()
	evt, err := Disable(home)
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if evt.Action != "skipped_idempotent" {
		t.Fatalf("event=%+v", evt)
	}
}

func TestEnableRejectsConflictsAndStatusReportsLegacy(t *testing.T) {
	home := t.TempDir()
	writeCodexConfig(t, home, "model_provider = \"openai\"\nopenai_base_url = \"http://old\"\n")
	if _, err := Enable(home, ProxyURL("127.0.0.1", "8990")); err == nil ||
		!strings.Contains(err.Error(), "top-level model_provider") {
		t.Fatalf("expected model_provider conflict, got %v", err)
	}
	status, err := Inspect(home, ProxyURL("127.0.0.1", "8990"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if status.Conflict == "" || !status.LegacyKeys {
		t.Fatalf("status should report conflict + legacy keys: %+v", status)
	}
}

func TestEnableRejectsUserOwnedProviderTable(t *testing.T) {
	home := t.TempDir()
	writeCodexConfig(t, home, "[model_providers.slimference-codex]\nbase_url = \"http://user\"\n")
	if _, err := Enable(home, ProxyURL("127.0.0.1", "8990")); err == nil ||
		!strings.Contains(err.Error(), "user-owned") {
		t.Fatalf("expected provider table conflict, got %v", err)
	}
}

func TestReadErrorsPropagate(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".codex"), []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write .codex file: %v", err)
	}
	for name, fn := range map[string]func() error{
		"enable": func() error {
			_, err := Enable(home, ProxyURL("127.0.0.1", "8990"))
			return err
		},
		"disable": func() error {
			_, err := Disable(home)
			return err
		},
		"inspect": func() error {
			_, err := Inspect(home, ProxyURL("127.0.0.1", "8990"))
			return err
		},
	} {
		if err := fn(); err == nil {
			t.Fatalf("%s should propagate read error", name)
		}
	}
}

func TestBackupFailuresPropagate(t *testing.T) {
	t.Run("enable", func(t *testing.T) {
		home := t.TempDir()
		writeCodexConfig(t, home, "model = \"gpt-5\"\n")
		codexDir := filepath.Join(home, ".codex")
		if err := os.Chmod(codexDir, 0o500); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(codexDir, 0o700) })
		if _, err := Enable(home, ProxyURL("127.0.0.1", "8990")); err == nil {
			t.Fatal("Enable should fail when backup cannot be written")
		}
	})
	t.Run("disable", func(t *testing.T) {
		home := t.TempDir()
		writeCodexConfig(t, home, renderBlock(blockBody(ProxyURL("127.0.0.1", "8990"))))
		codexDir := filepath.Join(home, ".codex")
		if err := os.Chmod(codexDir, 0o500); err != nil {
			t.Fatalf("chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(codexDir, 0o700) })
		if _, err := Disable(home); err == nil {
			t.Fatal("Disable should fail when backup cannot be written")
		}
	})
}

func TestBackupAndAtomicPrimitiveErrors(t *testing.T) {
	if err := backup(filepath.Join(t.TempDir(), "missing.toml")); err == nil {
		t.Fatal("backup should fail for missing source")
	}
	parentFile := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parentFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("write parent file: %v", err)
	}
	if err := writeAtomic(filepath.Join(parentFile, "config.toml"), []byte("x"), 0o644); err == nil {
		t.Fatal("writeAtomic should fail when parent is not a directory")
	}
}

func TestFenceAndLegacyEdgeHelpers(t *testing.T) {
	t.Run("unterminated fence drops managed tail", func(t *testing.T) {
		got := stripFence("model = \"gpt-5\"\n\n" + markerStart + "\npartial\n")
		if got != "model = \"gpt-5\"\n\n" {
			t.Fatalf("bad strip result %q", got)
		}
	})
	t.Run("top fence keeps later user content", func(t *testing.T) {
		got := stripFence(renderBlock(blockBody(ProxyURL("127.0.0.1", "8990"))) + "\nmodel = \"gpt-5\"\n")
		if got != "model = \"gpt-5\"\n" {
			t.Fatalf("bad strip result %q", got)
		}
	})
	t.Run("legacy ignores commented keys", func(t *testing.T) {
		if hasLegacyKeys("# openai_base_url = \"http://old\"\n") {
			t.Fatal("commented legacy key should be ignored")
		}
		if !hasLegacyKeys("chatgpt_base_url = \"http://old\"\n") {
			t.Fatal("chatgpt_base_url should be detected")
		}
	})
	t.Run("empty newline helper stays empty", func(t *testing.T) {
		if got := ensureTrailingNewline("\n\n"); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestPreviewAndIPv6ProxyURL(t *testing.T) {
	block := PreviewBlock(ProxyURL("::1", "8990"))
	if !strings.Contains(block, "http://[::1]:8990/backend-api/codex") {
		t.Fatalf("IPv6 block not bracketed:\n%s", block)
	}
}
