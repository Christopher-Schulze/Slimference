package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/config"
	"github.com/slimference/slimference/internal/proxy"
	"github.com/slimference/slimference/internal/types"
)

func TestParsePreviewArgs_Default(t *testing.T) {

	f, err := parsePreviewArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.json || f.diff || f.provider != "" || f.input != "" {
		t.Fatalf("default flags wrong: %+v", f)
	}
}

func TestParsePreviewArgs_AllFlags(t *testing.T) {

	f, err := parsePreviewArgs([]string{"--json", "--diff", "--provider", "openai", "--path", "/x", "/tmp/body.json"})
	if err != nil {
		t.Fatal(err)
	}
	if !f.json || !f.diff || f.provider != "openai" || f.path != "/x" || f.input != "/tmp/body.json" {
		t.Fatalf("flags: %+v", f)
	}
}

func TestParsePreviewArgs_Errors(t *testing.T) {

	cases := [][]string{
		{"--unknown"},
		{"--provider"},
		{"--path"},
		{"a.json", "b.json"},
	}
	for _, c := range cases {
		if _, err := parsePreviewArgs(c); err == nil {
			t.Fatalf("expected error on %v", c)
		}
	}
}

func TestParsePreviewArgs_EmptySkipped(t *testing.T) {

	f, err := parsePreviewArgs([]string{"", "/tmp/in.json", ""})
	if err != nil {
		t.Fatal(err)
	}
	if f.input != "/tmp/in.json" {
		t.Fatalf("input: %q", f.input)
	}
}

func TestProviderFromString(t *testing.T) {

	cases := map[string]types.Provider{
		"anthropic":     types.Anthropic,
		"openai":        types.OpenAI,
		"codex_chatgpt": types.CodexChatGPT,
		"codex":         types.CodexChatGPT,
		"chatgpt":       types.CodexChatGPT,
		"":              types.Provider(-1),
		"unknown":       types.Provider(-1),
	}
	for s, want := range cases {
		if got := providerFromString(s); got != want {
			t.Fatalf("providerFromString(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestReadPreviewInput_Stdin(t *testing.T) {
	origStdin := os.Stdin
	defer func() { os.Stdin = origStdin }()
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte("from stdin"))
		_ = w.Close()
	}()
	got, err := readPreviewInput("-")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from stdin" {
		t.Fatalf("got %q", string(got))
	}
}

func TestReadPreviewInput_File(t *testing.T) {

	dir := t.TempDir()
	path := dir + "/body.json"
	if err := os.WriteFile(path, []byte("file body"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readPreviewInput(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "file body" {
		t.Fatalf("got %q", string(got))
	}
}

func TestRenderPreviewDiff(t *testing.T) {

	got := renderPreviewDiff([]byte("a\nshared\nold"), []byte("a\nshared\nnew"))
	if !strings.Contains(got, "- old") || !strings.Contains(got, "+ new") {
		t.Fatalf("diff missing markers:\n%s", got)
	}
	if !strings.Contains(got, "  shared") {
		t.Fatalf("shared line missing context marker:\n%s", got)
	}
}

func TestRenderPreviewText(t *testing.T) {

	res := proxy.PreviewResult{
		ProviderString:    "anthropic",
		OrigTokens:        100,
		CompressedTokens:  60,
		SavedTokens:       40,
		SavingsRatio:      0.4,
		Layer1Breakdown:   map[string]int{"json": 10, "ansi": 5},
	}
	out := renderPreviewText(res, false)
	for _, want := range []string{"compress-preview", "anthropic", "Original tokens", "json", "ansi"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestRenderPreviewText_WithDiff(t *testing.T) {

	res := proxy.PreviewResult{
		ProviderString:  "openai",
		OriginalBody:    []byte("orig"),
		RewrittenBody:   []byte("new"),
		Layer1Breakdown: map[string]int{},
	}
	out := renderPreviewText(res, true)
	if !strings.Contains(out, "- orig") {
		t.Fatalf("diff section missing:\n%s", out)
	}
}

func TestHandleSubcommand_CompressPreviewDispatch(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
		_ = w.Close()
	}()
	outR, outW, _ := os.Pipe()
	os.Stdout = outW
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	handleSubcommand([]string{"compress-preview", "--provider", "anthropic"})
	_ = outW.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	if !strings.Contains(buf.String(), "compress-preview") {
		t.Fatalf("dispatcher output: %q", buf.String())
	}
}

func TestHandleCompressPreviewCmd_BadFlag(t *testing.T) {
	origExit := exitFn
	defer func() { exitFn = origExit }()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	pr, w, _ := os.Pipe()
	_ = pr
	origStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = origStderr
		_ = w.Close()
	}()
	handleCompressPreviewCmd([]string{"--xxx"})
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleCompressPreviewCmd_ConfigError(t *testing.T) {
	origExit := exitFn
	origCfg := configLoadFn
	defer func() {
		exitFn = origExit
		configLoadFn = origCfg
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	configLoadFn = func() (*config.Config, error) { return nil, io.ErrUnexpectedEOF }
	handleCompressPreviewCmd([]string{"-"})
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleCompressPreviewCmd_StdinHappy(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
		_ = w.Close()
	}()
	out, outW, _ := os.Pipe()
	os.Stdout = outW
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	handleCompressPreviewCmd([]string{"-", "--provider", "anthropic"})
	_ = outW.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, out)
	if !strings.Contains(buf.String(), "compress-preview") {
		t.Fatalf("output: %q", buf.String())
	}
}

func TestHandleCompressPreviewCmd_JSON(t *testing.T) {
	origStdin := os.Stdin
	origStdout := os.Stdout
	origCfg := configLoadFn
	defer func() {
		os.Stdin = origStdin
		os.Stdout = origStdout
		configLoadFn = origCfg
	}()
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte(`{"model":"claude","messages":[{"role":"user","content":"hi"}]}`))
		_ = w.Close()
	}()
	outR, outW, _ := os.Pipe()
	os.Stdout = outW
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	handleCompressPreviewCmd([]string{"--provider", "anthropic", "--json"})
	_ = outW.Close()
	os.Stdout = origStdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)
	if !strings.Contains(buf.String(), `"provider_string"`) {
		t.Fatalf("json output: %q", buf.String())
	}
}

func TestHandleCompressPreviewCmd_PreviewError(t *testing.T) {
	origExit := exitFn
	origStdin := os.Stdin
	origCfg := configLoadFn
	origStderr := os.Stderr
	defer func() {
		exitFn = origExit
		os.Stdin = origStdin
		configLoadFn = origCfg
		os.Stderr = origStderr
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		_, _ = w.Write([]byte("not json"))
		_ = w.Close()
	}()
	er, ew, _ := os.Pipe()
	os.Stderr = ew
	handleCompressPreviewCmd([]string{"--provider", "anthropic"})
	_ = ew.Close()
	os.Stderr = origStderr
	_, _ = io.Copy(io.Discard, er)
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}

func TestHandleCompressPreviewCmd_ReadInputError(t *testing.T) {
	origExit := exitFn
	origCfg := configLoadFn
	defer func() {
		exitFn = origExit
		configLoadFn = origCfg
	}()
	exits := []int{}
	exitFn = func(code int) { exits = append(exits, code) }
	configLoadFn = func() (*config.Config, error) { return config.Defaults(), nil }
	r, w, _ := os.Pipe()
	origStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = origStderr
		_ = w.Close()
		_, _ = io.Copy(io.Discard, r)
	}()
	handleCompressPreviewCmd([]string{"/no/such/file/path/to/body.json"})
	if len(exits) == 0 || exits[0] != 1 {
		t.Fatalf("expected exit 1, got %v", exits)
	}
}
