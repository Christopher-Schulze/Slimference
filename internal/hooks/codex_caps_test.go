package hooks

import (
	"context"
	"errors"
	"testing"
)

func TestCapabilitiesFor_KnownRange_AdvertisesDecisionBlockOnly(t *testing.T) {
	t.Parallel()
	caps := CapabilitiesFor("0.125.0")
	want := []CodexCapability{
		CodexCapDecisionBlock,
		CodexCapPermissionRequestDecision,
		CodexCapPostToolReplaceResult,
		CodexCapLifecycleContext,
	}
	if len(caps) != len(want) {
		t.Fatalf("expected %v, got %v", want, caps)
	}
	for i := range want {
		if caps[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, caps)
		}
	}
}

func TestCapabilitiesFor_BelowMin_ReturnsNil(t *testing.T) {
	t.Parallel()
	if got := CapabilitiesFor("0.116.9"); got != nil {
		t.Fatalf("expected nil for below-min version, got %v", got)
	}
}

func TestCapabilitiesFor_UnparseableTriple_ReturnsNil(t *testing.T) {
	t.Parallel()
	if got := CapabilitiesFor("not-a-semver"); got != nil {
		t.Fatalf("expected nil for unparseable input, got %v", got)
	}
}

func TestCapabilitiesFor_EmptyMaxIsOpenEnded(t *testing.T) {
	t.Parallel()
	caps := CapabilitiesFor("9.9.9")
	if len(caps) == 0 || caps[0] != CodexCapDecisionBlock {
		t.Fatalf("expected open-ended match to advertise decision_block, got %v", caps)
	}
}

func TestCapabilitiesFor_BoundaryAtMin_Inclusive(t *testing.T) {
	t.Parallel()
	caps := CapabilitiesFor("0.117.0")
	if len(caps) != 4 {
		t.Fatalf("expected min boundary to be inclusive; got %v", caps)
	}
}

func TestCapabilitiesFor_ReturnsCopy_NoSharedBackingArray(t *testing.T) {
	t.Parallel()
	a := CapabilitiesFor("0.125.0")
	b := CapabilitiesFor("0.125.0")
	if &a[0] == &b[0] {
		t.Fatal("expected independent backing arrays per call")
	}
	a[0] = "tampered"
	if HasCodexCapability("0.125.0", "tampered") {
		t.Fatal("matrix mutation leaked through CapabilitiesFor")
	}
}

func TestHasCodexCapability_KnownPositive(t *testing.T) {
	t.Parallel()
	if !HasCodexCapability("0.125.0", CodexCapDecisionBlock) {
		t.Fatal("expected 0.125.0 to advertise decision_block")
	}
	if !HasCodexCapability("0.125.0", CodexCapPermissionRequestDecision) {
		t.Fatal("expected 0.125.0 to advertise permission_request_decision")
	}
	if !HasCodexCapability("0.125.0", CodexCapPostToolReplaceResult) {
		t.Fatal("expected 0.125.0 to advertise posttool_replace_result")
	}
}

func TestHasCodexCapability_UpstreamFailOpenIsFalse(t *testing.T) {
	t.Parallel()
	if HasCodexCapability("0.125.0", CodexCapTransparentRewrite) {
		t.Fatal("transparent_rewrite must remain false until Codex honours updatedInput")
	}
	if HasCodexCapability("0.125.0", CodexCapPermissionDecision) {
		t.Fatal("pretool permission_decision must remain false until Codex honours allow/ask for PreToolUse")
	}
}

func TestCodexHookFeatureMatrix_ReturnsCopyAndCurrentReality(t *testing.T) {
	t.Parallel()
	a := CodexHookFeatureMatrix()
	b := CodexHookFeatureMatrix()
	if len(a) == 0 || len(b) == 0 {
		t.Fatal("expected feature matrix entries")
	}
	if &a[0] == &b[0] {
		t.Fatal("expected copied backing array")
	}
	var sawPostReplace, sawUpdatedInput bool
	for _, f := range a {
		if f.Event == "PostToolUse" && f.Name == "continue:false" && f.Status == CodexFeatureSupported {
			sawPostReplace = true
		}
		if f.Event == "PreToolUse" && f.Name == "updatedInput" && f.Status == CodexFeatureParsedFailOpen {
			sawUpdatedInput = true
		}
	}
	if !sawPostReplace || !sawUpdatedInput {
		t.Fatalf("feature matrix missing required current facts: post=%v updatedInput=%v", sawPostReplace, sawUpdatedInput)
	}
}

func TestSupportsTransparentRewrite_MirrorsHasCapability(t *testing.T) {
	t.Parallel()
	if SupportsTransparentRewrite("0.125.0") {
		t.Fatal("SupportsTransparentRewrite must report false until upstream support lands")
	}
	if SupportsTransparentRewrite("not-a-semver") {
		t.Fatal("unparseable input must yield false")
	}
}

func TestCapabilitiesFor_BelowOpenLowerBound_ButInsideUpper(t *testing.T) {
	// Inject a transient range with empty Min and a closed Max so we
	// exercise the empty-Min code path. Restore on exit. Not parallel
	// because the matrix is package-global.
	original := codexCapabilityMatrix
	defer func() { codexCapabilityMatrix = original }()
	codexCapabilityMatrix = []CodexCapabilityRange{
		{Min: "", Max: "0.50.0", Capabilities: []CodexCapability{CodexCapDecisionBlock}},
		{Min: "0.50.0", Max: "", Capabilities: []CodexCapability{CodexCapDecisionBlock}},
	}
	caps := CapabilitiesFor("0.10.0")
	if len(caps) != 1 {
		t.Fatalf("expected the open-Min range to match, got %v", caps)
	}
}

func TestCapabilitiesFor_UnparseableMaxIsSkipped(t *testing.T) {
	original := codexCapabilityMatrix
	defer func() { codexCapabilityMatrix = original }()
	codexCapabilityMatrix = []CodexCapabilityRange{
		{Min: "0.0.0", Max: "garbage", Capabilities: []CodexCapability{CodexCapDecisionBlock}},
	}
	if got := CapabilitiesFor("0.125.0"); got != nil {
		t.Fatalf("expected nil when range Max is unparseable, got %v", got)
	}
}

func TestCapabilitiesFor_UnparseableMinIsSkipped(t *testing.T) {
	original := codexCapabilityMatrix
	defer func() { codexCapabilityMatrix = original }()
	codexCapabilityMatrix = []CodexCapabilityRange{
		{Min: "garbage", Max: "1.0.0", Capabilities: []CodexCapability{CodexCapDecisionBlock}},
	}
	if got := CapabilitiesFor("0.125.0"); got != nil {
		t.Fatalf("expected nil when range Min is unparseable, got %v", got)
	}
}

func TestDetectCodexVersion_Stubbed(t *testing.T) {
	original := cliVersionCmdFn
	defer func() { cliVersionCmdFn = original }()
	cliVersionCmdFn = func(_ context.Context, name string) ([]byte, error) {
		if name != "codex" {
			t.Fatalf("expected probe of 'codex', got %q", name)
		}
		return []byte("codex-cli 0.125.0\n"), nil
	}
	parsed, raw := DetectCodexVersion(context.Background())
	if parsed != "0.125.0" {
		t.Fatalf("expected parsed=0.125.0, got %q", parsed)
	}
	if raw != "codex-cli 0.125.0" {
		t.Fatalf("expected raw to retain the trimmed line, got %q", raw)
	}
}

func TestDetectCodexVersion_NotInstalled(t *testing.T) {
	original := cliVersionCmdFn
	defer func() { cliVersionCmdFn = original }()
	cliVersionCmdFn = func(_ context.Context, _ string) ([]byte, error) {
		return nil, errors.New("not installed")
	}
	parsed, raw := DetectCodexVersion(context.Background())
	if parsed != "" || raw != "" {
		t.Fatalf("expected empty fields when binary missing, got parsed=%q raw=%q", parsed, raw)
	}
}

func TestDetectCodexVersion_UnparseableOutput(t *testing.T) {
	original := cliVersionCmdFn
	defer func() { cliVersionCmdFn = original }()
	cliVersionCmdFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("garbage line\n"), nil
	}
	parsed, raw := DetectCodexVersion(context.Background())
	if parsed != "" {
		t.Fatalf("expected empty parsed for unrecognised output, got %q", parsed)
	}
	if raw != "garbage line" {
		t.Fatalf("expected raw line to round-trip, got %q", raw)
	}
}

func TestSnapshotCodexCapabilities_NotInstalled_NotesPath(t *testing.T) {
	original := cliVersionCmdFn
	defer func() { cliVersionCmdFn = original }()
	cliVersionCmdFn = func(_ context.Context, _ string) ([]byte, error) {
		return nil, errors.New("not installed")
	}
	snap := SnapshotCodexCapabilities(context.Background())
	if snap.VersionParsed != "" || snap.TransparentRewrite || snap.DecisionBlock {
		t.Fatalf("expected empty snapshot for missing CLI, got %+v", snap)
	}
	if snap.Notes == "" || snap.Capabilities != nil {
		t.Fatalf("expected explanatory note + nil capabilities, got %+v", snap)
	}
}

func TestSnapshotCodexCapabilities_LegacyBlockNote(t *testing.T) {
	original := cliVersionCmdFn
	defer func() { cliVersionCmdFn = original }()
	cliVersionCmdFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("codex-cli 0.125.0\n"), nil
	}
	snap := SnapshotCodexCapabilities(context.Background())
	if !snap.DecisionBlock {
		t.Fatal("decision_block must be advertised for current Codex line")
	}
	if snap.TransparentRewrite {
		t.Fatal("transparent_rewrite must remain off until upstream support lands")
	}
	if snap.Notes == "" {
		t.Fatal("expected explanatory note for legacy path")
	}
}

func TestSnapshotCodexCapabilities_TransparentNote(t *testing.T) {
	originalMatrix := codexCapabilityMatrix
	defer func() { codexCapabilityMatrix = originalMatrix }()
	codexCapabilityMatrix = []CodexCapabilityRange{
		{
			Min:          "0.117.0",
			Capabilities: []CodexCapability{CodexCapDecisionBlock, CodexCapTransparentRewrite},
		},
	}
	originalCmd := cliVersionCmdFn
	defer func() { cliVersionCmdFn = originalCmd }()
	cliVersionCmdFn = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("codex-cli 1.0.0\n"), nil
	}
	snap := SnapshotCodexCapabilities(context.Background())
	if !snap.TransparentRewrite {
		t.Fatal("expected synthetic future range to flip transparent_rewrite on")
	}
	if snap.Notes == "" {
		t.Fatal("expected note even on the modern path")
	}
}
