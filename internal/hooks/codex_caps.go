package hooks

import (
	"context"
	"slices"
	"strings"
	"time"
)

// CodexCapability names a single feature of the Codex PreToolUse / PostToolUse
// hook contract that Slimference's emitted scripts can rely on. The set is
// open-ended: capabilities are added as Codex evolves. A version's capability
// list is derived purely from the parsed semver triple.
type CodexCapability string

const (
	// CodexCapPermissionDecision means Codex honours the
	// {"permissionDecision": "allow"|"ask"|"deny"} hook output. As of
	// 2026-05-01 the official Codex hooks doc states this field is
	// "parsed but not supported yet, so they fail open" - meaning the
	// JSON is accepted without error but no actual permission gating
	// takes place. Slimference therefore does NOT advertise this
	// capability for any current version.
	CodexCapPermissionDecision CodexCapability = "permission_decision"

	// CodexCapTransparentRewrite means Codex honours the
	// {"hookSpecificOutput": {"updatedInput": {...}}} payload to
	// substitute the actual command being executed without the model
	// having to retry. As of 2026-05-01 the upstream Codex hooks doc
	// states updatedInput is "parsed but not supported yet, so they
	// fail open". Until that changes, this capability is reported for
	// no version, and the hook script keeps the legacy block-rerun
	// path. See docs/todo/t113-codex-transparent-rewrite.md for the
	// re-activation criteria.
	CodexCapTransparentRewrite CodexCapability = "transparent_rewrite"

	// CodexCapDecisionBlock means Codex honours the legacy
	// {"decision": "block", "reason": "..."} output. Every Codex
	// version Slimference targets supports this, including the
	// current 0.x line.
	CodexCapDecisionBlock CodexCapability = "decision_block"

	CodexCapPermissionRequestDecision CodexCapability = "permission_request_decision"
	CodexCapPostToolReplaceResult     CodexCapability = "posttool_replace_result"
	CodexCapLifecycleContext          CodexCapability = "lifecycle_additional_context"

	// CodexCapCompactionHooks means Codex emits PreCompact + PostCompact
	// hook events with full JSON-schema-validated input/output. Verified
	// via direct binary inspection of codex 0.130 on 2026-05-15.
	// PreCompact fires before Codex's own compaction (trigger=manual|auto);
	// PostCompact fires after. Both let Slimference signal its proxy to
	// escalate aggressive compaction in parallel.
	CodexCapCompactionHooks CodexCapability = "compaction_hooks"
)

type CodexFeatureStatus string

const (
	CodexFeatureSupported      CodexFeatureStatus = "supported"
	CodexFeatureParsedFailOpen CodexFeatureStatus = "parsed_fail_open"
	CodexFeatureFailClosed     CodexFeatureStatus = "fail_closed"
	CodexFeatureUnprobed       CodexFeatureStatus = "unprobed"
)

type CodexHookFeature struct {
	Event  string
	Name   string
	Status CodexFeatureStatus
	Notes  string
}

// CodexCapabilityRange maps a closed semver interval [Min, Max) to the set
// of capabilities valid in that range. The ranges must be non-overlapping
// and ordered ascending; CapabilitiesFor walks the slice and returns the
// first match. Empty Min means "from the beginning of the line"; empty Max
// means "open-ended".
type CodexCapabilityRange struct {
	Min          string
	Max          string
	Capabilities []CodexCapability
	Notes        string
}

// codexCapabilityMatrix is the single source of truth for which Codex
// version supports which Slimference-relevant hook feature. It must be
// updated when Codex ships changes to its hook contract; the upstream
// reference is https://developers.openai.com/codex/hooks (probed
// 2026-05-01) plus direct binary schema dumps for live verification.
//
// 2026-05-15 update: codex 0.130 binary inspection confirmed
// PreCompact + PostCompact events ship with full JSON schemas
// (pre-compact.command.{input,output}, post-compact.command.{input,output}).
// These were previously listed as "unprobed".
var codexCapabilityMatrix = []CodexCapabilityRange{
	{
		Min:          "0.117.0",
		Max:          "0.130.0",
		Capabilities: []CodexCapability{CodexCapDecisionBlock, CodexCapPermissionRequestDecision, CodexCapPostToolReplaceResult, CodexCapLifecycleContext},
		Notes:        "official hooks contract supports lifecycle context, PermissionRequest allow/deny, PostToolUse result replacement, and decision/block; updatedInput remains parsed-only",
	},
	{
		Min:          "0.130.0",
		Max:          "",
		Capabilities: []CodexCapability{CodexCapDecisionBlock, CodexCapPermissionRequestDecision, CodexCapPostToolReplaceResult, CodexCapLifecycleContext, CodexCapCompactionHooks},
		Notes:        "0.130 adds PreCompact and PostCompact hooks verified via binary schema dump on 2026-05-15",
	},
}

var codexHookFeatureMatrix = []CodexHookFeature{
	{Event: "Config", Name: "hooks feature flag", Status: CodexFeatureSupported, Notes: "required in config.toml for hook loading"},
	{Event: "SessionStart", Name: "additionalContext", Status: CodexFeatureSupported, Notes: "developer context injection for startup/resume/clear boundaries"},
	{Event: "SessionStart", Name: "matcher", Status: CodexFeatureSupported, Notes: "matches source: startup, resume, clear"},
	{Event: "PreToolUse", Name: "decision:block", Status: CodexFeatureSupported, Notes: "legacy block reason is honoured and remains Slimference's safe rewrite fallback"},
	{Event: "PreToolUse", Name: "permissionDecision:deny", Status: CodexFeatureSupported, Notes: "hook-specific deny shape is documented as supported"},
	{Event: "PreToolUse", Name: "updatedInput", Status: CodexFeatureParsedFailOpen, Notes: "parsed but not honoured; never use for transparent command rewrite"},
	{Event: "PreToolUse", Name: "additionalContext", Status: CodexFeatureParsedFailOpen, Notes: "parsed-only for this event"},
	{Event: "PreToolUse", Name: "unified_exec", Status: CodexFeatureUnprobed, Notes: "official docs say interception is incomplete"},
	{Event: "PreToolUse", Name: "WebSearch", Status: CodexFeatureUnprobed, Notes: "not intercepted by current hook runtime"},
	{Event: "PermissionRequest", Name: "decision.allow", Status: CodexFeatureSupported, Notes: "can bypass approval prompt when local policy allows"},
	{Event: "PermissionRequest", Name: "decision.deny", Status: CodexFeatureSupported, Notes: "any deny wins across matching hooks"},
	{Event: "PermissionRequest", Name: "updatedInput", Status: CodexFeatureFailClosed, Notes: "reserved future field; do not emit"},
	{Event: "PermissionRequest", Name: "updatedPermissions", Status: CodexFeatureFailClosed, Notes: "reserved future field; do not emit"},
	{Event: "PermissionRequest", Name: "interrupt", Status: CodexFeatureFailClosed, Notes: "reserved future field; do not emit"},
	{Event: "PostToolUse", Name: "additionalContext", Status: CodexFeatureSupported, Notes: "developer context feedback after tool result"},
	{Event: "PostToolUse", Name: "decision:block", Status: CodexFeatureSupported, Notes: "replaces completed tool result with feedback and continues"},
	{Event: "PostToolUse", Name: "continue:false", Status: CodexFeatureSupported, Notes: "stops normal original-result processing and continues from feedback"},
	{Event: "PostToolUse", Name: "updatedMCPToolOutput", Status: CodexFeatureParsedFailOpen, Notes: "parsed-only; do not rely on it"},
	{Event: "UserPromptSubmit", Name: "additionalContext", Status: CodexFeatureSupported, Notes: "developer context injection before model turn"},
	{Event: "UserPromptSubmit", Name: "matcher", Status: CodexFeatureParsedFailOpen, Notes: "ignored for this event"},
	{Event: "UserPromptSubmit", Name: "decision:block", Status: CodexFeatureSupported, Notes: "can block prompt submission"},
	{Event: "Stop", Name: "decision:block", Status: CodexFeatureSupported, Notes: "continues the turn with the reason as the continuation prompt"},
	{Event: "Stop", Name: "continue:false", Status: CodexFeatureSupported, Notes: "takes precedence over continuation decisions"},
	{Event: "Stop", Name: "matcher", Status: CodexFeatureParsedFailOpen, Notes: "ignored for this event"},
	{Event: "PreCompact", Name: "event", Status: CodexFeatureSupported, Notes: "verified via codex 0.130 binary schema dump (pre-compact.command.{input,output}); fires with trigger=manual|auto before Codex's own compaction"},
	{Event: "PreCompact", Name: "input.trigger", Status: CodexFeatureSupported, Notes: "values: manual, auto — auto fires when context approaches auto_compact_token_limit"},
	{Event: "PreCompact", Name: "input.session_id", Status: CodexFeatureSupported, Notes: "required field for correlation"},
	{Event: "PreCompact", Name: "input.turn_id", Status: CodexFeatureSupported, Notes: "required field; Codex extension"},
	{Event: "PreCompact", Name: "output.continue:false", Status: CodexFeatureSupported, Notes: "blocks Codex's own compaction; Slimference proxy escalates aggressive compaction on next request instead"},
	{Event: "PreCompact", Name: "output.systemMessage", Status: CodexFeatureSupported, Notes: "schema-supported; injects a system message"},
	{Event: "PostCompact", Name: "event", Status: CodexFeatureSupported, Notes: "verified via codex 0.130 binary schema dump (post-compact.command.{input,output}); fires after Codex compaction completes"},
	{Event: "PostCompact", Name: "input.trigger", Status: CodexFeatureSupported, Notes: "values: manual, auto — same semantics as PreCompact"},
	{Event: "PostCompact", Name: "output.continue:false", Status: CodexFeatureSupported, Notes: "schema-supported; can abort the post-compaction flow"},
}

func CodexHookFeatureMatrix() []CodexHookFeature {
	out := make([]CodexHookFeature, len(codexHookFeatureMatrix))
	copy(out, codexHookFeatureMatrix)
	return out
}

// CapabilitiesFor returns the capability set advertised for a parsed
// semver triple (as produced by parseCLIVersion). Versions outside any
// declared range yield an empty slice; callers must treat that as
// "unknown - assume the legacy path".
func CapabilitiesFor(version string) []CodexCapability {
	tri, ok := splitSemver(version)
	if !ok {
		return nil
	}
	for _, r := range codexCapabilityMatrix {
		if r.Min != "" {
			lo, lok := splitSemver(r.Min)
			if !lok || compareSemver(tri, lo) < 0 {
				continue
			}
		}
		if r.Max != "" {
			hi, hok := splitSemver(r.Max)
			if !hok || compareSemver(tri, hi) >= 0 {
				continue
			}
		}
		out := make([]CodexCapability, len(r.Capabilities))
		copy(out, r.Capabilities)
		return out
	}
	return nil
}

// HasCodexCapability reports whether the parsed version advertises the
// given capability. Returns false for unknown / unparseable versions so
// callers default to the conservative legacy path.
func HasCodexCapability(version string, cap CodexCapability) bool {
	return slices.Contains(CapabilitiesFor(version), cap)
}

// SupportsTransparentRewrite is the canonical guard the script generator
// consults before emitting the modern updatedInput payload. As long as
// Codex keeps updatedInput parsed-only, this returns false for every
// version and the legacy block-rerun script is the only path emitted.
func SupportsTransparentRewrite(version string) bool {
	return HasCodexCapability(version, CodexCapTransparentRewrite)
}

// DetectCodexVersion runs `codex --version` (subject to the same
// override hook the drift detector uses) and returns the parsed semver
// triple plus the raw line. An empty version means the binary was not
// found, did not respond, or printed an unrecognised string.
func DetectCodexVersion(ctx context.Context) (parsed string, raw string) {
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := cliVersionCmdFn(probeCtx, "codex")
	if err != nil {
		return "", ""
	}
	raw = strings.TrimSpace(string(out))
	parsed = parseCLIVersion(raw)
	return parsed, raw
}

// CodexCapabilitySnapshot is the user-visible bundle returned by
// `slimference doctor` and the drift report so an operator can see, at
// a glance, what Codex hook features Slimference will rely on for the
// installed CLI.
type CodexCapabilitySnapshot struct {
	VersionRaw            string
	VersionParsed         string
	Capabilities          []CodexCapability
	TransparentRewrite    bool
	DecisionBlock         bool
	UpstreamUnsupportedOK bool
	Notes                 string
}

// SnapshotCodexCapabilities probes the installed Codex CLI and produces
// a snapshot suitable for human display. It never returns an error; an
// uninstalled / unparseable CLI yields a snapshot with empty version
// fields and Capabilities set to nil so the caller can render
// "not installed" without branching.
func SnapshotCodexCapabilities(ctx context.Context) CodexCapabilitySnapshot {
	parsed, raw := DetectCodexVersion(ctx)
	caps := CapabilitiesFor(parsed)
	snap := CodexCapabilitySnapshot{
		VersionRaw:            raw,
		VersionParsed:         parsed,
		Capabilities:          caps,
		TransparentRewrite:    HasCodexCapability(parsed, CodexCapTransparentRewrite),
		DecisionBlock:         HasCodexCapability(parsed, CodexCapDecisionBlock),
		UpstreamUnsupportedOK: true,
	}
	switch {
	case parsed == "":
		snap.Notes = "codex CLI not installed or version unrecognised; emit legacy block+rerun script"
	case !snap.TransparentRewrite:
		snap.Notes = "official hooks support PermissionRequest and PostToolUse replacement; transparent rewrite (updatedInput) is not honoured, so legacy block+rerun stays"
	default:
		snap.Notes = "transparent rewrite available; modern hook payload eligible"
	}
	return snap
}
