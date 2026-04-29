package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/slimference/slimference/internal/checkpoints"
	"github.com/slimference/slimference/internal/toolarchive"
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
	_, body, err := toolarchive.Expand(toolarchive.DefaultDir(home), args[0])
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
