package summarization

import (
	"strings"
	"sync"
	"testing"
)

func TestPickExampleLang_Default(t *testing.T) {
	if got := pickExampleLang(""); got != "go" {
		t.Fatalf("empty input must default to go, got %q", got)
	}
	if got := pickExampleLang("just plain prose without signals"); got != "go" {
		t.Fatalf("no-signal input must default to go, got %q", got)
	}
}

func TestPickExampleLang_Go(t *testing.T) {
	in := "ran go test ./internal/auth/handler.go and saw package main\nfunc HandleX()"
	if got := pickExampleLang(in); got != "go" {
		t.Fatalf("expected go, got %q", got)
	}
}

func TestPickExampleLang_Python(t *testing.T) {
	in := "edited app/auth/handler.py with def handle_login() then ran pytest app/auth/"
	if got := pickExampleLang(in); got != "python" {
		t.Fatalf("expected python, got %q", got)
	}
}

func TestPickExampleLang_TS(t *testing.T) {
	in := "edited src/auth/handler.ts with function handleLogin() then ran npm test"
	if got := pickExampleLang(in); got != "ts" {
		t.Fatalf("expected ts, got %q", got)
	}
}

func TestPickExampleLang_TSXJSX(t *testing.T) {
	if got := pickExampleLang("touched src/Page.tsx and src/App.jsx"); got != "ts" {
		t.Fatalf("expected ts for tsx/jsx, got %q", got)
	}
}

func TestPickExampleLang_Rust(t *testing.T) {
	in := "edited src/auth/handler.rs with pub async fn handle_login then ran cargo test -p auth"
	if got := pickExampleLang(in); got != "rust" {
		t.Fatalf("expected rust, got %q", got)
	}
}

func TestBuildSystemPrompt_PythonExample(t *testing.T) {
	ResetExamplePromptCounts()
	prompt := buildSystemPrompt("edited app/auth/handler.py and ran pytest app/")
	if !strings.Contains(prompt, "handle_login()") {
		t.Fatalf("python example missing in prompt:\n%s", prompt[:200])
	}
	if !strings.Contains(prompt, "MANDATORY OUTPUT FORMAT") {
		t.Fatal("prompt header missing")
	}
	if !strings.Contains(prompt, "First character must be dash-space") {
		t.Fatal("prompt footer missing")
	}
	if ExamplePromptCount("python") != 1 {
		t.Fatalf("python counter not advanced: %d", ExamplePromptCount("python"))
	}
}

func TestBuildSystemPrompt_TSExample(t *testing.T) {
	ResetExamplePromptCounts()
	prompt := buildSystemPrompt("touched src/auth/handler.ts and ran npm test")
	if !strings.Contains(prompt, "handleLogin(req: Request") {
		t.Fatalf("ts example missing in prompt:\n%s", prompt[:200])
	}
	if ExamplePromptCount("ts") != 1 {
		t.Fatalf("ts counter not advanced: %d", ExamplePromptCount("ts"))
	}
}

func TestBuildSystemPrompt_DefaultsToGo(t *testing.T) {
	ResetExamplePromptCounts()
	prompt := buildSystemPrompt("ambiguous input")
	if !strings.Contains(prompt, "src/auth/handler.go") {
		t.Fatal("go example missing in default prompt")
	}
	if ExamplePromptCount("go") != 1 {
		t.Fatalf("go counter not advanced: %d", ExamplePromptCount("go"))
	}
}

func TestBuildSystemPrompt_RustExample(t *testing.T) {
	ResetExamplePromptCounts()
	prompt := buildSystemPrompt("touched src/auth/handler.rs and ran cargo test -p auth")
	if !strings.Contains(prompt, "handle_login()") || !strings.Contains(prompt, "cargo test -p auth") {
		t.Fatalf("rust example missing in prompt:\n%s", prompt[:200])
	}
	if ExamplePromptCount("rust") != 1 {
		t.Fatalf("rust counter not advanced: %d", ExamplePromptCount("rust"))
	}
}

func TestExamplePromptCounts_ReturnsCopy(t *testing.T) {
	ResetExamplePromptCounts()
	buildSystemPrompt("ambiguous input")
	snap := ExamplePromptCounts()
	snap["go"] = 999
	if ExamplePromptCount("go") == 999 {
		t.Fatal("ExamplePromptCounts must return a copy")
	}
}

func TestExamplePromptCount_UnknownLang(t *testing.T) {
	ResetExamplePromptCounts()
	if got := ExamplePromptCount("rust"); got != 0 {
		t.Fatalf("unknown lang must return 0, got %d", got)
	}
}

func TestExamplePromptCounts_ConcurrentAccess(t *testing.T) {
	ResetExamplePromptCounts()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = buildSystemPrompt("edited src/auth/handler.ts and ran npm test")
		}()
		go func() {
			defer wg.Done()
			_ = ExamplePromptCounts()
			_ = ExamplePromptCount("ts")
		}()
	}
	wg.Wait()
	if got := ExamplePromptCount("ts"); got != 32 {
		t.Fatalf("ts counter = %d, want 32", got)
	}
}
