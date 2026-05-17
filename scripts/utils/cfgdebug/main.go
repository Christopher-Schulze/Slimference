// cfgdebug prints how Slimference's config loader sees the current user config.
//
// T210 classification: remove-after-certification. Keep this debug-only helper
// out of default docs/help; it exists only to diagnose config path drift until
// live Codex certification proves the Phase H path stable.
package main

import (
	"fmt"

	"github.com/slimference/slimference/internal/config"
)

func main() {
	cfg, info, err := config.LoadWithOptions(config.LoadOptions{})
	if err != nil {
		fmt.Println("err:", err)
		return
	}
	fmt.Println("source:", info.Source, "path:", info.ResolvedPath)
	fmt.Println("Transparent.SNIPeekMode:", cfg.Transparent.SNIPeekMode)
	fmt.Println("Transparent.SNIPeekPort:", cfg.Transparent.SNIPeekPort)
	fmt.Println("Transparent.Enabled:", cfg.Transparent.Enabled)
	fmt.Println("Transparent.CADir:", cfg.Transparent.CADir)
	fmt.Println("Proxy.ListenPort:", cfg.Proxy.ListenPort)
}
