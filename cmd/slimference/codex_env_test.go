package main

import (
	"strings"
	"testing"
)

func TestCodexRuntimeEnvDropListPreservesConfigBearingKeys(t *testing.T) {
	for _, key := range []string{
		"CODEX_HOME",
		"CODEX_CONFIG",
		"CODEX_MCP_CONFIG",
		"CODEX_API_KEY",
		"CODEX_MCP_SERVER_HOME",
	} {
		if codexShouldDropInheritedEnvKey(key) {
			t.Fatalf("%s must be preserved so Codex config and MCP servers remain visible", key)
		}
	}
	for _, key := range []string{
		"CODEX_THREAD_ID",
		"CODEX_CI",
		"CODEX_RUN_ID",
		"CODEX_SESSION_ID",
		"CODEX_MANAGED_BY_NPM",
		"CODEX_MANAGED_PACKAGE_ROOT",
	} {
		if !codexShouldDropInheritedEnvKey(key) {
			t.Fatalf("%s must be dropped as inherited runtime/session state", key)
		}
	}
}

func TestCodexRuntimeUnsetShellCommandIsTargeted(t *testing.T) {
	got := codexRuntimeUnsetShellCommand()
	if strings.Contains(got, "CODEX_@") || strings.Contains(got, "CODEX_*") || strings.Contains(got, "CODEX_HOME") {
		t.Fatalf("unset command must be a targeted runtime cleanup, got %q", got)
	}
	for _, want := range []string{"unset ", "CODEX_THREAD_ID", "CODEX_CI", "CODEX_MANAGED_BY_NPM"} {
		if !strings.Contains(got, want) {
			t.Fatalf("unset command missing %q: %q", want, got)
		}
	}
}

func TestAppendCodexRuntimeEnvUnsetsIsTargeted(t *testing.T) {
	got := strings.Join(appendCodexRuntimeEnvUnsets([]string{"env"}), " ")
	if strings.Contains(got, "CODEX_HOME") || strings.Contains(got, "CODEX_MCP_CONFIG") {
		t.Fatalf("env command must preserve config-bearing Codex env: %s", got)
	}
	for _, want := range []string{"env", "-u CODEX_THREAD_ID", "-u CODEX_CI", "-u CODEX_MANAGED_BY_NPM"} {
		if !strings.Contains(got, want) {
			t.Fatalf("env command missing %q: %s", want, got)
		}
	}
}
