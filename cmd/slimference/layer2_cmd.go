package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/slimference/slimference/internal/config"
)

var tomlNewEncoder = func(w *strings.Builder) tomlEncoder {
	return toml.NewEncoder(w)
}

type tomlEncoder interface {
	Encode(v interface{}) error
}

// handleLayer2Cmd implements `slimference layer2 <enable|disable|status|acknowledge>`.
// T121: the only way to enable Layer 2 from the default-off state is
// `slimference layer2 enable --acknowledge-data-policy`.
func handleLayer2Cmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference layer2 <enable|disable|status|acknowledge>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  enable       Enable Layer 2 summarization (requires --acknowledge-data-policy)")
		fmt.Fprintln(os.Stderr, "  disable      Disable Layer 2 summarization")
		fmt.Fprintln(os.Stderr, "  status       Show current Layer 2 configuration")
		fmt.Fprintln(os.Stderr, "  acknowledge  Record the Layer 2 data-policy acknowledgement")
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
	case "acknowledge", "ack":
		handleLayer2Acknowledge()
	default:
		fmt.Fprintf(os.Stderr, "layer2: unknown subcommand %q (enable|disable|status|acknowledge)\n", args[0])
		exitFn(1)
	}
}

const dataPolicyExplanation = `Layer 2 background summarization can prepare compact
conversation summaries. The default engine is local and deterministic; if you
configure an OpenAI-compatible summarization provider, redacted conversation
content can leave your machine.

Model-facing summary replacement, including mid-exchange summaries, stays blocked unless
[compression.summary].allow_model_facing_replacement is explicitly true. The
product direction is the deterministic context ledger, not summary-as-truth.

Before enabling, review: docs/data-policy.md

To acknowledge and enable, run:
  slimference layer2 enable --acknowledge-data-policy
`

const layer2DefaultOnAckVersion = "t129-layer2-default-on-v1"

type layer2PolicyAck struct {
	Version string `json:"version"`
	Unix    int64  `json:"unix"`
}

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
	fmt.Println("layer2: model-facing summary replacement, including mid-exchange summaries, remains blocked unless allow_model_facing_replacement=true.")
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

	status := "disabled"
	if enabled {
		status = "enabled"
	}

	fmt.Printf("Layer 2 status: %s\n", status)
	fmt.Printf("  Engine:        in-process deterministic extractive compactor\n")
	fmt.Printf("  Model-facing:  %s\n", boolStr(cfg.Compression.Summary.AllowModelFacingReplacement, "legacy summary replacement enabled", "summary replacement blocked"))
	fmt.Printf("  Redaction:     %s\n", redaction)
	fmt.Printf("  Min tokens:    %d\n", cfg.Compression.MinTokensForLayer2)
	fmt.Printf("  Policy ack:    %s\n", boolStr(layer2PolicyAcknowledged(), "recorded", "missing"))

	if enabled {
		fmt.Println()
		fmt.Println("  Model-facing summaries, including mid-exchange summaries, stay shadow-only unless allow_model_facing_replacement=true.")
		fmt.Println("  Runs locally unless an explicit OpenAI-compatible summarization provider is configured.")
		fmt.Println("  Disable: slimference layer2 disable")
	}
}

func handleLayer2Acknowledge() {
	path, err := writeLayer2PolicyAck(time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "layer2 acknowledge: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("layer2: data-policy acknowledgement recorded at %s\n", path)
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

func layer2PolicyAckPath() string {
	home, err := osUserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".slimference", "policy", "layer2-default-on-ack.json")
}

func layer2PolicyAcknowledged() bool {
	path := layer2PolicyAckPath()
	if path == "" {
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var ack layer2PolicyAck
	if err := json.Unmarshal(data, &ack); err != nil {
		return false
	}
	return ack.Version == layer2DefaultOnAckVersion && ack.Unix > 0
}

func writeLayer2PolicyAck(now time.Time) (string, error) {
	path := layer2PolicyAckPath()
	if path == "" {
		return "", fmt.Errorf("home directory unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", fmt.Errorf("create policy dir: %w", err)
	}
	data, _ := json.MarshalIndent(layer2PolicyAck{
		Version: layer2DefaultOnAckVersion,
		Unix:    now.Unix(),
	}, "", "  ")
	if err := osWriteFile(path, append(data, '\n'), 0600); err != nil {
		return "", fmt.Errorf("write ack: %w", err)
	}
	return path, nil
}

func ensureLayer2PolicyAcknowledged(cfg *config.Config, interactive bool, stdin io.Reader, stdout, stderr io.Writer) error {
	if cfg == nil || !cfg.Compression.Layer2Enabled || layer2PolicyAcknowledged() {
		return nil
	}
	msg := "Layer 2 is enabled in this config. Model-facing summary replacement, including mid-exchange summaries, stays blocked unless allow_model_facing_replacement=true; configured external summarization providers may receive redacted conversation content."
	if !interactive {
		fmt.Fprintf(stderr, "[WARN] %s Run `slimference layer2 acknowledge` after reviewing docs/data-policy.md, or `slimference layer2 disable`.\n", msg)
		return nil
	}
	fmt.Fprintln(stdout, msg)
	fmt.Fprintln(stdout, "Review docs/data-policy.md. Press Enter to acknowledge, or press Ctrl-C and run `slimference layer2 disable`.")
	if _, err := bufio.NewReader(stdin).ReadString('\n'); err != nil && err != io.EOF {
		return fmt.Errorf("read acknowledgement: %w", err)
	}
	if _, err := writeLayer2PolicyAck(time.Now()); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "layer2: data-policy acknowledgement recorded")
	return nil
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
