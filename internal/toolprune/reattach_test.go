package toolprune

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/types"
)

func TestMentionedTools(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		text       string
		candidates []string
		want       []string
	}{
		{"single hit", "please use Bash to list", []string{"Bash"}, []string{"Bash"}},
		{"word boundary respected", "do not call Bashful", []string{"Bash"}, nil},
		{"multiple candidates", "Read first then Write", []string{"Bash", "Read", "Write"}, []string{"Read", "Write"}},
		{"no hits", "no tools at all", []string{"Bash", "Read"}, nil},
		{"empty text", "", []string{"Bash"}, nil},
		{"empty candidates", "Bash", nil, nil},
		{"empty candidate string", "Read", []string{""}, nil},
		{"end-of-string boundary", "use Bash", []string{"Bash"}, []string{"Bash"}},
		{"start-of-string boundary", "Bash run", []string{"Bash"}, []string{"Bash"}},
		{"mid-word match rejected", "BashCalculator", []string{"Bash"}, nil},
		{"case-insensitive hit", "please use bash to list", []string{"Bash"}, []string{"Bash"}},
		{"camel tail intent", "check the weather before replying", []string{"GetWeather"}, []string{"GetWeather"}},
		{"snake alias intent", "send an email update", []string{"send_email"}, []string{"send_email"}},
		{"command family intent", "run a shell command", []string{"BashTool"}, []string{"BashTool"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := MentionedTools(tc.text, tc.candidates)
			if (len(got) == 0) != (len(tc.want) == 0) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			gs := strings.Join(got, ",")
			ws := strings.Join(tc.want, ",")
			if gs != ws {
				t.Fatalf("got %q want %q", gs, ws)
			}
		})
	}
}

func TestIsWordBoundary(t *testing.T) {
	t.Parallel()
	if !isWordBoundary("abc", -1) {
		t.Fatal("negative index must be a boundary")
	}
	if !isWordBoundary("abc", 99) {
		t.Fatal("out-of-range index must be a boundary")
	}
	if isWordBoundary("a", 0) {
		t.Fatal("'a' is not a boundary")
	}
	if isWordBoundary("9", 0) {
		t.Fatal("'9' is not a boundary")
	}
	if isWordBoundary("_", 0) {
		t.Fatal("'_' is not a boundary")
	}
	if !isWordBoundary(" ", 0) {
		t.Fatal("space is a boundary")
	}
}

func TestReattachToolDefinitions_Anthropic(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"claude-3","tools":[{"name":"Read","description":"r"}]}`)
	defs := map[string]json.RawMessage{
		"Bash": json.RawMessage(`{"name":"Bash","description":"b"}`),
	}
	out, n, err := ReattachToolDefinitions(body, types.Anthropic, defs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("added: %d", n)
	}
	if !strings.Contains(string(out), `"Bash"`) || !strings.Contains(string(out), `"Read"`) {
		t.Fatalf("body: %s", out)
	}
}

func TestReattachToolDefinitions_OpenAIFunctionShape(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"type":"function","function":{"name":"Read"}}]}`)
	defs := map[string]json.RawMessage{
		"Bash": json.RawMessage(`{"type":"function","function":{"name":"Bash"}}`),
	}
	out, n, err := ReattachToolDefinitions(body, types.OpenAI, defs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("added: %d", n)
	}
	if !strings.Contains(string(out), "Bash") {
		t.Fatalf("body: %s", out)
	}
}

func TestReattachToolDefinitions_DeterministicOrder(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"name":"Read"}]}`)
	defs := map[string]json.RawMessage{
		"Zulu":  json.RawMessage(`{"name":"Zulu"}`),
		"Alpha": json.RawMessage(`{"name":"Alpha"}`),
	}
	out, n, err := ReattachToolDefinitions(body, types.Anthropic, defs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("added: %d", n)
	}
	text := string(out)
	alpha := strings.Index(text, `"Alpha"`)
	zulu := strings.Index(text, `"Zulu"`)
	if alpha < 0 || zulu < 0 || alpha > zulu {
		t.Fatalf("reattach order must be deterministic by tool name: %s", out)
	}
}

func TestReattachToolDefinitions_DuplicateSkipped(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"name":"Bash"}]}`)
	defs := map[string]json.RawMessage{
		"Bash": json.RawMessage(`{"name":"Bash"}`),
	}
	out, n, err := ReattachToolDefinitions(body, types.Anthropic, defs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("dup must not re-add: %d", n)
	}
	if string(out) != string(body) {
		t.Fatalf("body must be unchanged on no-op: %s vs %s", out, body)
	}
}

func TestReattachToolDefinitions_NoTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{"model":"x"}`)
	defs := map[string]json.RawMessage{
		"Bash": json.RawMessage(`{"name":"Bash"}`),
	}
	out, n, err := ReattachToolDefinitions(body, types.Anthropic, defs)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected to add to empty tools[]: %d", n)
	}
	if !strings.Contains(string(out), `"Bash"`) {
		t.Fatalf("body: %s", out)
	}
}

func TestReattachToolDefinitions_Edges(t *testing.T) {
	t.Parallel()
	if _, n, _ := ReattachToolDefinitions(nil, types.Anthropic, map[string]json.RawMessage{"x": []byte(`{}`)}); n != 0 {
		t.Fatal("nil body must short-circuit")
	}
	if _, n, _ := ReattachToolDefinitions([]byte(`{}`), types.Anthropic, nil); n != 0 {
		t.Fatal("nil defs must short-circuit")
	}
	if _, n, _ := ReattachToolDefinitions([]byte(`{not-json`), types.Anthropic, map[string]json.RawMessage{"x": []byte(`{}`)}); n != 0 {
		t.Fatal("malformed body must fail-open")
	}
	if _, n, _ := ReattachToolDefinitions([]byte(`{"tools":"oops"}`), types.Anthropic, map[string]json.RawMessage{"x": []byte(`{"name":"x"}`)}); n != 1 {
		t.Fatal("non-array tools field becomes empty entries; new def must still be added")
	}
}

func TestUsageTracker_PrunedDefRoundTrip(t *testing.T) {
	t.Parallel()
	u := NewUsageTracker(20)
	u.RememberPrunedDef("sess-1", "Bash", json.RawMessage(`{"name":"Bash"}`))
	u.RememberPrunedDef("sess-1", "Read", json.RawMessage(`{"name":"Read"}`))

	names := u.PrunedToolNames("sess-1")
	if len(names) != 2 {
		t.Fatalf("names: %v", names)
	}

	defs := u.LookupPrunedDefs("sess-1", []string{"Bash"})
	if len(defs) != 1 || string(defs["Bash"]) != `{"name":"Bash"}` {
		t.Fatalf("lookup: %v", defs)
	}
	// Lookup consumes the entry; second call must return empty.
	if defs2 := u.LookupPrunedDefs("sess-1", []string{"Bash"}); defs2 != nil {
		t.Fatalf("entry must be removed after lookup: %v", defs2)
	}
	// Read is still cached.
	remaining := u.PrunedToolNames("sess-1")
	if len(remaining) != 1 || remaining[0] != "Read" {
		t.Fatalf("remaining: %v", remaining)
	}
}

func TestUsageTracker_PrunedDef_EdgeCases(t *testing.T) {
	t.Parallel()
	u := NewUsageTracker(20)
	u.RememberPrunedDef("", "Bash", []byte(`{}`)) // empty session -> no-op
	u.RememberPrunedDef("s", "", []byte(`{}`))    // empty name -> no-op
	u.RememberPrunedDef("s", "Bash", nil)         // empty def -> no-op
	if u.PrunedToolNames("") != nil {
		t.Fatal("empty session returns nil")
	}
	if u.PrunedToolNames("nope") != nil {
		t.Fatal("unknown session returns nil")
	}
	if u.LookupPrunedDefs("", []string{"x"}) != nil {
		t.Fatal("empty session returns nil")
	}
	if u.LookupPrunedDefs("s", nil) != nil {
		t.Fatal("nil names returns nil")
	}
	if u.LookupPrunedDefs("nope", []string{"x"}) != nil {
		t.Fatal("unknown session returns nil")
	}
	u.RememberPrunedDef("s", "Bash", []byte(`{"name":"Bash"}`))
	if defs := u.LookupPrunedDefs("s", []string{"missing"}); defs != nil {
		t.Fatalf("non-matching names returns nil: %v", defs)
	}
}

// TestUsageTracker_PrunedDef_OnExistingSession covers the path where
// the session was created via ObserveTurn (without prunedDefs map)
// and RememberPrunedDef has to lazily allocate it.
func TestUsageTracker_PrunedDef_OnExistingSession(t *testing.T) {
	t.Parallel()
	u := NewUsageTracker(20)
	u.ObserveTurn("s", []string{"Read"})
	u.RememberPrunedDef("s", "Bash", []byte(`{"name":"Bash"}`))
	if got := u.PrunedToolNames("s"); len(got) != 1 || got[0] != "Bash" {
		t.Fatalf("got %v", got)
	}
}

// TestUsageTracker_PrunedDef_NewSession_HitsMaxSessions covers the
// branch where RememberPrunedDef must evict the oldest session before
// creating a new one.
func TestUsageTracker_PrunedDef_EvictionUnderPressure(t *testing.T) {
	t.Parallel()
	u := NewUsageTracker(20)
	u.maxSessions = 2
	u.RememberPrunedDef("a", "T1", []byte(`{"name":"T1"}`))
	u.RememberPrunedDef("b", "T2", []byte(`{"name":"T2"}`))
	u.RememberPrunedDef("c", "T3", []byte(`{"name":"T3"}`))
	if got := u.PrunedToolNames("a"); got != nil {
		t.Fatalf("oldest session must be evicted: %v", got)
	}
	if got := u.PrunedToolNames("c"); len(got) != 1 || got[0] != "T3" {
		t.Fatalf("newest session must survive: %v", got)
	}
}
