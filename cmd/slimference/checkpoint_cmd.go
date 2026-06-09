package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Christopher-Schulze/Slimference/internal/checkpoints"
	"github.com/Christopher-Schulze/Slimference/internal/codecompact"
	"github.com/Christopher-Schulze/Slimference/internal/contentarchive"
	"github.com/Christopher-Schulze/Slimference/internal/filter"
	"github.com/Christopher-Schulze/Slimference/internal/toolarchive"
)

func handleCheckpointCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: slimference checkpoint <capture|list|restore|stats>")
		exitFn(1)
	}
	home, err := osUserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "home: %v\n", err)
		exitFn(1)
	}
	dir := checkpoints.DefaultDir(home)
	switch args[0] {
	case "capture":
		cp, err := checkpoints.Capture(dir, checkpoints.CaptureInput{Trigger: checkpoints.TriggerManual})
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkpoint capture: %v\n", err)
			exitFn(1)
		}
		fmt.Printf("%s\n", cp.ID)
	case "list":
		items, err := checkpoints.List(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkpoint list: %v\n", err)
			exitFn(1)
		}
		for _, item := range items {
			fmt.Printf("%s  %s  %s  score=%d\n", item.CreatedAt.Format("2006-01-02 15:04:05"), item.ID, item.Trigger, item.Score)
		}
	case "restore":
		var cp *checkpoints.Checkpoint
		var err error
		if len(args) > 1 {
			cp, err = checkpoints.RestoreByID(dir, args[1])
		} else {
			cp, err = checkpoints.RestoreBest(dir)
		}
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Fprintln(os.Stderr, "no checkpoint available")
				exitFn(1)
			}
			fmt.Fprintf(os.Stderr, "checkpoint restore: %v\n", err)
			exitFn(1)
		}
		fmt.Println(cp.Body)
	case "stats":
		stats, err := checkpoints.Snapshot(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "checkpoint stats: %v\n", err)
			exitFn(1)
		}
		data, _ := json.MarshalIndent(stats, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Fprintf(os.Stderr, "unknown checkpoint subcommand: %s\n", args[0])
		exitFn(1)
	}
}

func handleExpandCmd(args []string) {
	if len(args) != 1 || strings.TrimSpace(args[0]) == "" {
		fmt.Fprintln(os.Stderr, "usage: slimference expand <archive-id|local-archive://<id>>")
		exitFn(1)
	}
	home, err := osUserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "home: %v\n", err)
		exitFn(1)
	}
	// T76: archive ids may belong to either toolarchive (large tool
	// outputs) or contentarchive (lossy Layer 1 mutations). Try both.
	// Tool archive is checked first for backward compatibility.
	if _, body, err := toolarchive.Expand(toolarchive.DefaultDir(home), args[0]); err == nil {
		if _, werr := os.Stdout.Write(body); werr != nil {
			fmt.Fprintf(os.Stderr, "expand write: %v\n", werr)
			exitFn(1)
		}
		return
	}
	_, body, err := contentarchive.Get(contentarchive.DefaultDir(home), args[0])
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "archive entry not found")
			exitFn(1)
		}
		fmt.Fprintf(os.Stderr, "expand: %v\n", err)
		exitFn(1)
	}
	if _, err := os.Stdout.Write(body); err != nil {
		fmt.Fprintf(os.Stderr, "expand write: %v\n", err)
		exitFn(1)
	}
}

func handleExpandBodyCmd(args []string) {
	if len(args) != 2 || strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
		fmt.Fprintln(os.Stderr, "usage: slimference expand-body <archive-id|local-archive://<id>> <go-symbol>")
		exitFn(1)
		return
	}
	home, err := osUserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "home: %v\n", err)
		exitFn(1)
		return
	}
	var body []byte
	path := "archive.go"
	if meta, raw, err := toolarchive.Expand(toolarchive.DefaultDir(home), args[0]); err == nil {
		body = raw
		if p := filterReadPath(meta.Command); p != "" {
			path = p
		}
	} else if _, raw, err := contentarchive.Get(contentarchive.DefaultDir(home), args[0]); err == nil {
		body = raw
	} else {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintln(os.Stderr, "archive entry not found")
			exitFn(1)
			return
		}
		fmt.Fprintf(os.Stderr, "expand-body: %v\n", err)
		exitFn(1)
		return
	}
	out, ok, err := codecompact.ExtractGoSymbolBody(path, body, args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "expand-body: %v\n", err)
		exitFn(1)
		return
	}
	if !ok {
		fmt.Fprintf(os.Stderr, "symbol body not found: %s\n", strings.TrimSpace(args[1]))
		exitFn(1)
		return
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "expand-body write: %v\n", err)
		exitFn(1)
		return
	}
}

func filterReadPath(command string) string {
	return filter.ReadPathFromCommandLine(command)
}
