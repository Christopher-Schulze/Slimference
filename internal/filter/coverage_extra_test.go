package filter

import "testing"

func TestRewriteCommand_BlankAndEmptySegments(t *testing.T) {
	if got, changed := RewriteCommand("   ", nil); changed || got != "   " {
		t.Fatalf("blank command should stay unchanged, got %q changed=%v", got, changed)
	}
	if got, changed := rewriteCompoundSeg(nil, map[string]bool{}); changed || got != "" {
		t.Fatalf("empty segment should stay unchanged, got %q changed=%v", got, changed)
	}
}

func TestTokenize_SpacesOnlyAndEmbeddedRedirect(t *testing.T) {
	if toks := tokenize("   \t"); len(toks) != 0 {
		t.Fatalf("spaces-only tokenize should be empty, got %v", toks)
	}

	toks := tokenize("echo out2>&1")
	if len(toks) < 3 {
		t.Fatalf("unexpected token count: %v", toks)
	}
	if toks[1].Kind != TokenArg || toks[1].Value != "out" {
		t.Fatalf("expected flushed word before redirect, got %#v", toks[1])
	}
	if toks[2].Kind != TokenRedirect || toks[2].Value != "2>&1" {
		t.Fatalf("expected embedded redirect token, got %#v", toks[2])
	}
}

func TestAdditionalPackageAndPythonBranches(t *testing.T) {
	if out, ok := TryCompactNpmInstall([]string{"npm", "install"}, []byte("")); !ok || string(out) != "[npm install] ok\n" {
		t.Fatalf("npm install compact = %q ok=%v", out, ok)
	}
	if out, ok := TryCompactPnpmInstall([]string{"pnpm", "ci"}, []byte("")); !ok || string(out) != "[pnpm ci] ok\n" {
		t.Fatalf("pnpm ci compact = %q ok=%v", out, ok)
	}
	if !isPythonUnittestArgv([]string{"python3.exe", "-m", "unittest"}) {
		t.Fatal("python3.exe unittest should be detected")
	}
}

func TestCapturedOutputAndRewriteEdgeBranches(t *testing.T) {
	compacted, changed := CompactCapturedOutput("", "echo hello", "hello\n", 2000)
	if changed || string(compacted) != "hello\n" {
		t.Fatalf("expected unchanged captured output, got %q changed=%v", compacted, changed)
	}
	if argv := primaryArgvForCapturedOutput("FOO=1 BAR=2"); argv != nil {
		t.Fatalf("expected nil argv for env-only command, got %#v", argv)
	}
	if got, changed := RewriteCommand("''", nil); changed || got != "''" {
		t.Fatalf("expected quote-only command passthrough, got %q changed=%v", got, changed)
	}
	if got, changed := RewriteCommand("&&", nil); changed || got != "&&" {
		t.Fatalf("expected operator-only command passthrough, got %q changed=%v", got, changed)
	}
}
