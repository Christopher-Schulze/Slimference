package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	codexTerminalTitleReset = "Codex CLI"
)

var terminalTitleWriteFn = func(title string) {
	if !termIsTerminalFn(int(os.Stderr.Fd())) {
		return
	}
	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
}

var terminalTitleKeepaliveInterval = 750 * time.Millisecond
var terminalTitleWorkingDirFn = os.Getwd
var terminalTitleHomeDirFn = os.UserHomeDir
var terminalTitleReadFileFn = os.ReadFile

func setScopedCodexTerminalTitle(codexArgs []string) func() {
	return startTerminalTitleKeepalive(scopedCodexTerminalTitle(codexArgs), codexTerminalTitleReset)
}

func scopedCodexTerminalTitle(codexArgs []string) string {
	cwd := compactTerminalTitleCWD()
	model := terminalTitleModel(codexArgs)
	if model == "" {
		model = "Codex CLI"
	}
	if cwd == "" {
		return "[SF] " + model
	}
	return "[SF] " + cwd + " | " + model
}

func compactTerminalTitleCWD() string {
	cwd, err := terminalTitleWorkingDirFn()
	if err != nil {
		return ""
	}
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "." || cwd == "" {
		return ""
	}
	home, err := terminalTitleHomeDirFn()
	if err == nil {
		home = filepath.Clean(strings.TrimSpace(home))
		if home != "" && (cwd == home || strings.HasPrefix(cwd, home+string(os.PathSeparator))) {
			cwd = "~" + strings.TrimPrefix(cwd, home)
		}
	}
	return sanitizeTerminalTitlePart(cwd)
}

func terminalTitleModel(codexArgs []string) string {
	if model := terminalTitleModelFromArgs(codexArgs); model != "" {
		return model
	}
	if model := sanitizeTerminalTitlePart(os.Getenv("CODEX_MODEL")); model != "" {
		return model
	}
	if model := terminalTitleModelFromConfig(); model != "" {
		return model
	}
	return ""
}

func terminalTitleModelFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		switch {
		case arg == "--model" || arg == "-m":
			if i+1 < len(args) {
				return sanitizeTerminalTitlePart(args[i+1])
			}
		case strings.HasPrefix(arg, "--model="):
			return sanitizeTerminalTitlePart(strings.TrimPrefix(arg, "--model="))
		case strings.HasPrefix(arg, "-m="):
			return sanitizeTerminalTitlePart(strings.TrimPrefix(arg, "-m="))
		}
	}
	return ""
}

func terminalTitleModelFromConfig() string {
	home, err := terminalTitleHomeDirFn()
	if err != nil || strings.TrimSpace(home) == "" {
		return ""
	}
	data, err := terminalTitleReadFileFn(filepath.Join(home, ".codex", "config.toml"))
	if err != nil {
		return ""
	}
	return parseTerminalTitleModelConfig(data)
}

func parseTerminalTitleModelConfig(data []byte) string {
	var model, effort string
	section := ""
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.Contains(line, "]") {
			section = strings.TrimSpace(line)
			continue
		}
		if section != "" {
			continue
		}
		key, raw, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value := parseTerminalTitleTomlString(raw)
		switch strings.TrimSpace(key) {
		case "model":
			model = value
		case "model_reasoning_effort", "reasoning_effort":
			effort = value
		}
	}
	model = sanitizeTerminalTitlePart(model)
	effort = sanitizeTerminalTitlePart(effort)
	switch {
	case model != "" && effort != "":
		return model + " " + effort
	case model != "":
		return model
	default:
		return ""
	}
}

func parseTerminalTitleTomlString(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "#"); idx >= 0 {
		raw = strings.TrimSpace(raw[:idx])
	}
	if raw == "" {
		return ""
	}
	if unquoted, err := strconv.Unquote(raw); err == nil {
		return unquoted
	}
	return strings.Trim(raw, `"'`)
}

func sanitizeTerminalTitlePart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("\x1b", "", "\a", "", "\n", " ", "\r", " ", "\t", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func startTerminalTitleKeepalive(active string, reset string) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	terminalTitleWriteFn(active)
	go func() {
		ticker := time.NewTicker(terminalTitleKeepaliveInterval)
		defer func() {
			ticker.Stop()
			close(stopped)
		}()
		for {
			select {
			case <-ticker.C:
				terminalTitleWriteFn(active)
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			close(done)
			<-stopped
			terminalTitleWriteFn(reset)
		})
	}
}
