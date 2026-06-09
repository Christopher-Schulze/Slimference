package main

import (
	"fmt"
	"os"

	"github.com/Christopher-Schulze/Slimference/internal/filter"
)

// handleTrustCmd implements `slimference trust [add|list|remove|status] [path]`.
// Ported from the RTK reference catalog. Project-local filters are a prompt-injection
// vector when committed to repositories; this command manages the trust
// store that gates their loading.
func handleTrustCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: slimference trust [add|list|remove|status] [path]")
		exitFn(1)
		return
	}
	switch args[0] {
	case "add":
		handleTrustAdd(args[1:])
	case "remove", "rm":
		handleTrustRemove(args[1:])
	case "list":
		handleTrustList()
	case "status":
		handleTrustStatus(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown trust subcommand: %s\n", args[0])
		fmt.Fprintln(os.Stderr, "Usage: slimference trust [add|list|remove|status] [path]")
		exitFn(1)
	}
}

func handleTrustAdd(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: slimference trust add <path>")
		exitFn(1)
		return
	}
	entry, err := filter.AddTrust(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust add: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("Trusted %s\n  sha256:     %s\n  trusted_at: %s\n", args[0], entry.SHA256, entry.TrustedAt)
}

func handleTrustRemove(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: slimference trust remove <path>")
		exitFn(1)
		return
	}
	removed, err := filter.RemoveTrust(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust remove: %v\n", err)
		exitFn(1)
		return
	}
	if !removed {
		fmt.Printf("No trust record for %s\n", args[0])
		return
	}
	fmt.Printf("Removed trust for %s\n", args[0])
}

func handleTrustList() {
	entries, err := filter.ListTrusted()
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust list: %v\n", err)
		exitFn(1)
		return
	}
	if len(entries) == 0 {
		fmt.Println("No trusted filters.")
		return
	}
	fmt.Println("Trusted filters:")
	for _, e := range entries {
		fmt.Printf("  %s\n    sha256:     %s\n    trusted_at: %s\n", e.Path, e.Entry.SHA256, e.Entry.TrustedAt)
	}
}

func handleTrustStatus(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: slimference trust status <path>")
		exitFn(1)
		return
	}
	status, entry, err := filter.EvaluateTrust(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust status: %v\n", err)
		exitFn(1)
		return
	}
	fmt.Printf("%s: %s\n", args[0], status)
	if entry.SHA256 != "" {
		fmt.Printf("  trusted sha256: %s\n", entry.SHA256)
		fmt.Printf("  trusted_at:     %s\n", entry.TrustedAt)
	}
}
