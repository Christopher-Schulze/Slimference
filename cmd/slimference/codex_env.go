package main

import "slices"

import "strings"

// Codex sets process-local runtime variables when an agent launches tools.
// Slimference must not leak those into a new Codex session, but config-bearing
// values such as CODEX_HOME must survive so MCP servers and auth config remain visible.
var codexInheritedRuntimeEnvKeys = []string{
	"CODEX_THREAD_ID",
	"CODEX_CI",
	"CODEX_RUN_ID",
	"CODEX_SESSION_ID",
	"CODEX_ACTIVE_RUN_ID",
	"CODEX_PARENT_RUN_ID",
	"CODEX_RESUME_ID",
	"CODEX_TASK_ID",
	"CODEX_TRACE_ID",
	"CODEX_MANAGED_BY_NPM",
	"CODEX_MANAGED_PACKAGE_ROOT",
}

func codexShouldDropInheritedEnvKey(key string) bool {
	return slices.Contains(codexInheritedRuntimeEnvKeys, key)
}

func codexRuntimeUnsetShellCommand() string {
	return "unset " + strings.Join(codexInheritedRuntimeEnvKeys, " ")
}

func appendCodexRuntimeEnvUnsets(args []string) []string {
	for _, key := range codexInheritedRuntimeEnvKeys {
		args = append(args, "-u", key)
	}
	return args
}
