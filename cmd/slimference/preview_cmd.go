package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/proxy"
	"github.com/Christopher-Schulze/Slimference/internal/types"
)

// previewFlags captures parsed `slimference compress-preview` flags. T82.
type previewFlags struct {
	provider string
	path     string
	json     bool
	diff     bool
	input    string // positional input path; "-" or empty for stdin
}

func parsePreviewArgs(args []string) (previewFlags, error) {
	var f previewFlags
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch a {
		case "--json":
			f.json = true
		case "--diff":
			f.diff = true
		case "--provider":
			i++
			if i >= len(args) || args[i] == "" {
				return f, fmt.Errorf("--provider requires a value (anthropic|openai|codex_chatgpt)")
			}
			f.provider = args[i]
		case "--path":
			i++
			if i >= len(args) || args[i] == "" {
				return f, fmt.Errorf("--path requires a value")
			}
			f.path = args[i]
		default:
			if a == "" {
				continue
			}
			if strings.HasPrefix(a, "--") {
				return f, fmt.Errorf("unknown flag: %s", a)
			}
			if f.input != "" {
				return f, fmt.Errorf("unexpected extra argument: %s", a)
			}
			f.input = a
		}
	}
	return f, nil
}

func providerFromString(s string) types.Provider {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "anthropic":
		return types.Anthropic
	case "openai":
		return types.OpenAI
	case "codex_chatgpt", "codex", "chatgpt":
		return types.CodexChatGPT
	}
	// Negative sentinel = auto-detect.
	return types.Provider(-1)
}

// readPreviewInput returns the body from a path or stdin. Empty/`-`
// means stdin.
func readPreviewInput(path string) ([]byte, error) {
	if path == "" || path == "-" {
		return io.ReadAll(os.Stdin)
	}
	return os.ReadFile(path)
}

// renderPreviewDiff returns a unified-style line-by-line diff between
// the original body bytes and the rewritten body bytes. Tiny custom
// implementation to avoid pulling a diff library; output is best-effort
// for human review, not a strict patch.
func renderPreviewDiff(original, rewritten []byte) string {
	origLines := strings.Split(string(original), "\n")
	newLines := strings.Split(string(rewritten), "\n")
	var sb strings.Builder
	sb.WriteString("--- original\n+++ rewritten\n")
	max := len(origLines)
	if len(newLines) > max {
		max = len(newLines)
	}
	for i := 0; i < max; i++ {
		var o, n string
		if i < len(origLines) {
			o = origLines[i]
		}
		if i < len(newLines) {
			n = newLines[i]
		}
		if o == n {
			sb.WriteString("  ")
			sb.WriteString(o)
			sb.WriteString("\n")
			continue
		}
		if o != "" {
			sb.WriteString("- ")
			sb.WriteString(o)
			sb.WriteString("\n")
		}
		if n != "" {
			sb.WriteString("+ ")
			sb.WriteString(n)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// renderPreviewText is the default human-readable output for
// `slimference compress-preview`. Shows token counts and per-sub-layer
// attribution; bodies are shown only when --diff is requested.
func renderPreviewText(res proxy.PreviewResult, includeDiff bool) string {
	var sb strings.Builder
	sb.WriteString("Slimference compress-preview\n")
	sb.WriteString(strings.Repeat("=", 60) + "\n")
	sb.WriteString(fmt.Sprintf("Provider:           %s\n", res.ProviderString))
	sb.WriteString(fmt.Sprintf("Original tokens:    %d\n", res.OrigTokens))
	sb.WriteString(fmt.Sprintf("Compressed tokens:  %d\n", res.CompressedTokens))
	sb.WriteString(fmt.Sprintf("Saved tokens:       %d\n", res.SavedTokens))
	sb.WriteString(fmt.Sprintf("Savings ratio:      %.2f%%\n", res.SavingsRatio*100))
	if len(res.Layer1Breakdown) > 0 {
		sb.WriteString("Layer 1 breakdown:\n")
		for _, k := range []string{"ansi", "json", "comment", "dedup", "structure", "delta", "tool_compressor", "image", "success_short", "repeated_collapse", "graph_pruning", "preview", "loop_nudge"} {
			if v, ok := res.Layer1Breakdown[k]; ok && v != 0 {
				sb.WriteString(fmt.Sprintf("  %-20s %d\n", k, v))
			}
		}
	}
	if includeDiff {
		sb.WriteString(strings.Repeat("-", 60) + "\n")
		sb.WriteString(renderPreviewDiff(res.OriginalBody, res.RewrittenBody))
	}
	return sb.String()
}

// handleCompressPreviewCmd is the `slimference compress-preview` entry
// point. T82.
func handleCompressPreviewCmd(args []string) {
	flags, err := parsePreviewArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitFn(1)
		return
	}
	cfg, err := configLoadFn()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}
	body, err := readPreviewInput(flags.input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		exitFn(1)
		return
	}
	provider := providerFromString(flags.provider)
	includeBodies := flags.diff || flags.json
	res, err := proxy.PreviewCompress(cfg, flags.path, body, provider, includeBodies)
	if err != nil {
		fmt.Fprintf(os.Stderr, "preview: %v\n", err)
		exitFn(1)
		return
	}
	if flags.json {
		out, _ := json.MarshalIndent(&res, "", "  ")
		fmt.Println(string(out))
		return
	}
	fmt.Print(renderPreviewText(res, flags.diff))
}
