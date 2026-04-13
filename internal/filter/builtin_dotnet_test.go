package filter

import (
	"strings"
	"testing"
)

func TestTryCompactDotnet(t *testing.T) {
	t.Parallel()
	out, ok := TryCompactDotnet([]string{"dotnet", "build", "-c", "Release"}, []byte(""))
	if !ok || string(out) != "[dotnet build] ok\n" {
		t.Fatalf("build: ok=%v %q", ok, out)
	}
	out2, ok := TryCompactDotnet([]string{"dotnet.exe", "test"}, []byte("\n"))
	if !ok || string(out2) != "[dotnet test] ok\n" {
		t.Fatalf("test: %q", out2)
	}
	pub, ok := TryCompactDotnet([]string{"dotnet", "publish", "-c", "Release"}, []byte(""))
	if !ok || string(pub) != "[dotnet publish] ok\n" {
		t.Fatalf("publish: %q", pub)
	}
	pk, ok := TryCompactDotnet([]string{"dotnet", "pack"}, []byte("\n"))
	if !ok || string(pk) != "[dotnet pack] ok\n" {
		t.Fatalf("pack: %q", pk)
	}
	if _, ok := TryCompactDotnet([]string{"dotnet", "restore"}, []byte("")); ok {
		t.Fatal("restore not compacted")
	}
	dnNpx, ok := TryCompactDotnet([]string{"npx", "dotnet", "build"}, []byte(""))
	if !ok || string(dnNpx) != "[dotnet build] ok\n" {
		t.Fatalf("npx dotnet build: %q", dnNpx)
	}
	dnPnpm, ok := TryCompactDotnet([]string{"pnpm", "exec", "dotnet", "test"}, []byte("\n"))
	if !ok || string(dnPnpm) != "[dotnet test] ok\n" {
		t.Fatalf("pnpm dotnet test: %q", dnPnpm)
	}
	dnYarn, ok := TryCompactDotnet([]string{"yarn", "dotnet", "pack"}, []byte(""))
	if !ok || string(dnYarn) != "[dotnet pack] ok\n" {
		t.Fatalf("yarn dotnet pack: %q", dnYarn)
	}
}

func TestTryCompactDotnet_nonEmptySuccess(t *testing.T) {
	t.Parallel()
	// Typical successful dotnet build output with warnings.
	input := `Microsoft (R) Build Engine version 17.8.0
Copyright (C) Microsoft Corporation. All rights reserved.

  Determining projects to restore...
  All projects are up-to-date for restore.
  MyApp -> /home/user/MyApp/bin/Debug/net8.0/MyApp.dll

Build succeeded.
    0 Warning(s)
    0 Error(s)

Time Elapsed 00:00:03.21
`
	out, ok := TryCompactDotnet([]string{"dotnet", "build"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact success output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[dotnet build] ok") {
		t.Errorf("want ok summary, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact output should be shorter: got %d vs %d", len(s), len(input))
	}
}

func TestTryCompactDotnet_nonEmptyFailure(t *testing.T) {
	t.Parallel()
	// Failed dotnet build output with error lines.
	input := `Microsoft (R) Build Engine version 17.8.0
Copyright (C) Microsoft Corporation. All rights reserved.

  Determining projects to restore...
  All projects are up-to-date for restore.
src/MyApp/Program.cs(12,5): error CS0246: The type or namespace name 'Foo' could not be found [/home/user/MyApp/MyApp.csproj]
src/MyApp/Program.cs(15,9): error CS1002: ; expected [/home/user/MyApp/MyApp.csproj]

Build FAILED.

Error(s)
    2 Error(s)
    0 Warning(s)

Time Elapsed 00:00:01.55
`
	out, ok := TryCompactDotnet([]string{"dotnet", "build"}, []byte(input))
	if !ok {
		t.Fatalf("expected compact failure output, got pass-through")
	}
	s := string(out)
	if !strings.Contains(s, "[dotnet build] FAILED") {
		t.Errorf("want FAILED header, got: %q", s)
	}
	if !strings.Contains(s, "CS0246") {
		t.Errorf("want error line with CS0246, got: %q", s)
	}
	if len(s) >= len(input) {
		t.Errorf("compact output should be shorter: got %d vs %d", len(s), len(input))
	}
}

func TestTryCompactDotnet_nonEmptyUnknownSubcmd(t *testing.T) {
	t.Parallel()
	// dotnet restore: not compacted, should pass through.
	input := "Restoring packages for /home/user/MyApp/MyApp.csproj...\n  Packages restored.\n"
	_, ok := TryCompactDotnet([]string{"dotnet", "restore"}, []byte(input))
	if ok {
		t.Fatal("restore with non-empty output should not be compacted")
	}
}

func TestTryCompactDotnet_nonDotnetCmd(t *testing.T) {
	t.Parallel()
	// Non-dotnet binary with non-empty output should pass through.
	_, ok := TryCompactDotnet([]string{"msbuild", "MyApp.sln"}, []byte("build output\n"))
	if ok {
		t.Fatal("msbuild not handled")
	}
}

// TestTryCompactDotnet_npxNonEmpty covers the npxArgvSuffix path (lines 65-68) with non-empty
// stdout so the early empty-return is skipped and the npx branch executes.
func TestTryCompactDotnet_npxNonEmpty(t *testing.T) {
	t.Parallel()
	input := "Build succeeded.\n    0 Warning(s)\n    0 Error(s)\n"
	out, ok := TryCompactDotnet([]string{"npx", "dotnet", "build"}, []byte(input))
	if !ok {
		t.Fatalf("npx dotnet build non-empty: expected compact, got pass-through")
	}
	if !strings.Contains(string(out), "[dotnet build] ok") {
		t.Errorf("want ok summary, got: %q", out)
	}
}

// TestTryCompactDotnet_noMatchingContent covers the compact=="" guard (line 78-80):
// non-empty dotnet output with no success/error/warning lines → extractDotnetErrors returns "".
func TestTryCompactDotnet_noMatchingContent(t *testing.T) {
	t.Parallel()
	// Generic dotnet output with no build succeeded, no errors, no warnings.
	input := "Restoring packages...\nAll packages restored.\nDetermining NuGet source.\n"
	_, ok := TryCompactDotnet([]string{"dotnet", "build"}, []byte(input))
	if ok {
		t.Fatal("no-match content: expected pass-through, got compact")
	}
}

// TestExtractDotnetErrors_withWarnings covers the warnings>0 branch (line 104-106).
func TestExtractDotnetErrors_withWarnings(t *testing.T) {
	t.Parallel()
	input := "Build succeeded.\n    1 Warning(s)\n    0 Error(s)\n"
	got := extractDotnetErrors(input, "build")
	if !strings.Contains(got, "1 warning") {
		t.Errorf("with warnings: want warning count in output, got %q", got)
	}
}

// TestExtractDotnetErrors_noErrLines covers the len(errLines)==0 return "" (line 123-125):
// content has no success marker AND no error/failed/warning lines → returns "".
func TestExtractDotnetErrors_noErrLines(t *testing.T) {
	t.Parallel()
	input := "Restoring packages for /path/to/project.csproj...\n  Packages restored in 0.5 sec.\n"
	got := extractDotnetErrors(input, "build")
	if got != "" {
		t.Errorf("no error lines: want empty string, got %q", got)
	}
}
