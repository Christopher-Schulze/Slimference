package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/slimference/slimference/internal/config"
)

var tomlNewEncoder = func(w *strings.Builder) tomlEncoder {
	return toml.NewEncoder(w)
}

type tomlEncoder interface {
	Encode(v interface{}) error
}

// handleLayer2Cmd implements `slimference layer2 <enable|disable|status>`.
// T121: the only way to enable Layer 2 from the default-off state is
// `slimference layer2 enable --acknowledge-data-policy`.
func handleLayer2Cmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference layer2 <enable|disable|status>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  enable   Enable Layer 2 summarization (requires --acknowledge-data-policy)")
		fmt.Fprintln(os.Stderr, "  disable  Disable Layer 2 summarization")
		fmt.Fprintln(os.Stderr, "  status   Show current Layer 2 configuration")
		exitFn(1)
		return
	}
	switch args[0] {
	case "enable":
		handleLayer2Enable(args[1:])
	case "disable":
		handleLayer2Disable()
	case "status":
		handleLayer2Status()
	default:
		fmt.Fprintf(os.Stderr, "layer2: unknown subcommand %q (enable|disable|status)\n", args[0])
		exitFn(1)
	}
}

const dataPolicyExplanation = `Layer 2 summarization sends compressed conversation prefixes to an
external summarization provider (MiniMax at api.minimax.io). Even with
outbound redaction enabled (default), this means conversation content
including code snippets, file paths, and tool outputs leaves your machine.

Before enabling, review: docs/data-policy.md

To acknowledge and enable, run:
  slimference layer2 enable --acknowledge-data-policy
`

func handleLayer2Enable(args []string) {
	ack := false
	for _, a := range args {
		if a == "--acknowledge-data-policy" {
			ack = true
		}
	}
	if !ack {
		fmt.Print(dataPolicyExplanation)
		exitFn(2)
		return
	}

	cfg, info, err := config.LoadWithOptions(config.LoadOptions{
		ExplicitPath: explicitConfigPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}

	if cfg.Compression.Layer2Enabled {
		fmt.Println("layer2: already enabled")
		return
	}

	cfg.Compression.Layer2Enabled = true

	path := resolvedOrFallback(info)
	if err := writeConfigUpdate(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "layer2 enable: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("layer2: enabled (config written to %s)\n", path)
	fmt.Println("layer2: outbound redaction is ON (default mode). Review: docs/data-policy.md")
}

func handleLayer2Disable() {
	cfg, info, err := config.LoadWithOptions(config.LoadOptions{
		ExplicitPath: explicitConfigPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}

	if !cfg.Compression.Layer2Enabled {
		fmt.Println("layer2: already disabled")
		return
	}

	cfg.Compression.Layer2Enabled = false

	path := resolvedOrFallback(info)
	if err := writeConfigUpdate(path, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "layer2 disable: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("layer2: disabled (config written to %s)\n", path)
}

func handleLayer2Status() {
	cfg, _, err := config.LoadWithOptions(config.LoadOptions{
		ExplicitPath: explicitConfigPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		exitFn(1)
		return
	}

	enabled := cfg.Compression.Layer2Enabled
	redaction := cfg.Compression.Summary.OutboundRedaction
	redaction = effectiveRedaction(redaction)
	apiKeySet := cfg.Compression.MiniMax.APIKey() != ""
	provider := cfg.Compression.MiniMax.BaseURL

	status := "disabled"
	if enabled {
		status = "enabled"
	}

	fmt.Printf("Layer 2 status: %s\n", status)
	fmt.Printf("  Provider:      %s\n", provider)
	fmt.Printf("  Model:         %s\n", cfg.Compression.MiniMax.Model)
	fmt.Printf("  API key:       %s\n", boolStr(apiKeySet, "configured", "not set"))
	fmt.Printf("  Redaction:     %s\n", redaction)
	fmt.Printf("  Min tokens:    %d\n", cfg.Compression.MinTokensForLayer2)

	if enabled {
		fmt.Println()
		fmt.Println("  Data flows to an external third-party provider (MiniMax).")
		fmt.Println("  Review: docs/data-policy.md")
		fmt.Println("  Disable: slimference layer2 disable")
	}
}

func boolStr(cond bool, ifTrue, ifFalse string) string {
	if cond {
		return ifTrue
	}
	return ifFalse
}

func resolvedOrFallback(info config.LoadInfo) string {
	if p := info.ResolvedPath; p != "" {
		return p
	}
	return config.DefaultConfigPath()
}

func effectiveRedaction(mode string) string {
	if mode != "" {
		return mode
	}
	return "default"
}

// writeConfigUpdate re-encodes the config as TOML and writes it to path.
// It preserves the existing file structure by re-encoding from the loaded
// config. For a first pass this is sufficient; a surgical key-level edit
// would require a TOML AST round-trip which the current dependency (BurntSushi/toml)
// does not support for writes.
func writeConfigUpdate(path string, cfg *config.Config) error {
	dir := config.ExpandHomePath(path)
	dir = dir[:strings.LastIndex(dir, "/")]
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var buf strings.Builder
	enc := tomlNewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	fullPath := config.ExpandHomePath(path)
	if err := osWriteFile(fullPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
