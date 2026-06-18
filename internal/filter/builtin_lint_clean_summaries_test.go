package filter

import (
	"strings"
	"testing"
)

func TestTryCompactLintCleanSummaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		argv   []string
		stdout string
		want   string
	}{
		{
			name:   "direct ruff",
			argv:   []string{"ruff", "check", "."},
			stdout: "All checks passed!\n",
			want:   "[ruff check] ok\n",
		},
		{
			name:   "python module ruff",
			argv:   []string{"python", "-m", "ruff", "check", "src"},
			stdout: "All checks passed!\n",
			want:   "[ruff check] ok\n",
		},
		{
			name:   "package script ruff",
			argv:   []string{"pnpm", "run", "lint"},
			stdout: "> app@1.0.0 lint /repo\n> ruff check .\nAll checks passed!\n",
			want:   "[ruff check] ok\n",
		},
		{
			name:   "direct biome plural",
			argv:   []string{"biome", "check", "."},
			stdout: "Checked 196 files in 24ms. No fixes applied.\n",
			want:   "[biome check] ok (196 files checked)\n",
		},
		{
			name:   "direct biome singular",
			argv:   []string{"biome", "check", "src/app.ts"},
			stdout: "Checked 1 file in 9ms. No fixes applied.\n",
			want:   "[biome check] ok (1 file checked)\n",
		},
		{
			name:   "package script biome",
			argv:   []string{"bun", "run", "lint"},
			stdout: "bun run lint\n$ biome check .\nChecked 196 files in 24ms. No fixes applied.\n",
			want:   "[biome check] ok (196 files checked)\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := TryCompactLintOutput(tt.argv, []byte(tt.stdout))
			if !ok || string(out) != tt.want {
				t.Fatalf("TryCompactLintOutput ok=%v out=%q want=%q", ok, out, tt.want)
			}
		})
	}
}

func TestTryCompactLintCleanSummariesFailOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		argv        []string
		stdout      string
		forbiddenOK string
		mustContain string
		strictOpen  bool
	}{
		{
			name:        "ruff warning after clean line",
			argv:        []string{"ruff", "check", "."},
			stdout:      "All checks passed!\nwarning: no files included\n",
			forbiddenOK: "[ruff check] ok",
			mustContain: "warning: no files included",
		},
		{
			name:        "ruff finding",
			argv:        []string{"ruff", "check", "."},
			stdout:      "src/app.py:1:1: F401 imported but unused\nFound 1 error.\n",
			forbiddenOK: "[ruff check] ok",
			mustContain: "F401",
		},
		{
			name:        "biome zero files",
			argv:        []string{"biome", "check", "."},
			stdout:      "Checked 0 files in 65ms. No fixes applied.\n",
			forbiddenOK: "[biome check] ok",
			strictOpen:  true,
		},
		{
			name:        "biome warning line",
			argv:        []string{"biome", "check", "."},
			stdout:      "Checked 2 files in 245ms. No fixes applied.\nFound 1 warning.\n",
			forbiddenOK: "[biome check] ok",
			mustContain: "Found 1 warning",
		},
		{
			name:        "biome singular mismatch",
			argv:        []string{"biome", "check", "."},
			stdout:      "Checked 1 files in 9ms. No fixes applied.\n",
			forbiddenOK: "[biome check] ok",
			strictOpen:  true,
		},
		{
			name:        "biome fixed output",
			argv:        []string{"biome", "check", "."},
			stdout:      "Checked 2 files in 9ms. Fixed 1 file.\n",
			forbiddenOK: "[biome check] ok",
			strictOpen:  true,
		},
		{
			name:        "package script biome warning",
			argv:        []string{"pnpm", "run", "lint"},
			stdout:      "> app@1.0.0 lint /repo\n> biome check .\nChecked 2 files in 245ms. No fixes applied.\nFound 1 warning.\n",
			forbiddenOK: "[biome check] ok",
			mustContain: "Found 1 warning",
		},
		{
			name:        "non biome argv",
			argv:        []string{"node", "scripts/lint.js", "check"},
			stdout:      "Checked 196 files in 24ms. No fixes applied.\n",
			forbiddenOK: "[biome check] ok",
			strictOpen:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, ok := TryCompactLintOutput(tt.argv, []byte(tt.stdout))
			if strings.Contains(string(out), tt.forbiddenOK) {
				t.Fatalf("unsafe output became clean OK: ok=%v out=%q", ok, out)
			}
			if tt.strictOpen && ok {
				t.Fatalf("expected strict fail-open, got ok out=%q", out)
			}
			if ok && tt.mustContain != "" && !strings.Contains(string(out), tt.mustContain) {
				t.Fatalf("diagnostic compaction lost required signal %q: out=%q", tt.mustContain, out)
			}
		})
	}
}
