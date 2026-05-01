package hooks

import (
	"context"
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
)

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
// 2026-05-01).
//
// Current state (2026-05-01): the only capability advertised across
// every supported version is decision_block. transparent_rewrite and
// permission_decision are listed in Codex's hook contract but parsed-
// only (fail open), so Slimference does not rely on them yet.
var codexCapabilityMatrix = []CodexCapabilityRange{
	{
		Min:          "0.117.0",
		Max:          "",
		Capabilities: []CodexCapability{CodexCapDecisionBlock},
		Notes:        "decision/block + reason supported; updatedInput parsed-only (fail open)",
	},
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
	for _, c := range CapabilitiesFor(version) {
		if c == cap {
			return true
		}
	}
	return false
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
		snap.Notes = "transparent rewrite (updatedInput) not honoured by upstream Codex; legacy block+rerun script emitted"
	default:
		snap.Notes = "transparent rewrite available; modern hook payload eligible"
	}
	return snap
}
