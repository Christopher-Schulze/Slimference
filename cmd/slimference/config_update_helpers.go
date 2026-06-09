package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/Christopher-Schulze/Slimference/internal/config"
)

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

func writeConfigUpdate(path string, cfg *config.Config) error {
	dir := config.ExpandHomePath(path)
	dir = dir[:strings.LastIndex(dir, "/")]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	var buf strings.Builder
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(cfg); err != nil {
		return fmt.Errorf("encode config: %w", err)
	}

	fullPath := config.ExpandHomePath(path)
	if err := osWriteFile(fullPath, []byte(buf.String()), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
