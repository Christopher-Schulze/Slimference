package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	codexDesktopMenubarTitle      = "● SF"
	codexDesktopMenubarTooltip    = "Slimference Codex App active"
	codexDesktopMenubarScriptName = "desktop-menubar.jxa"
)

type codexDesktopMenubarConfig struct {
	Title   string
	Tooltip string
}

var (
	codexDesktopMenubarStartFn   = startCodexDesktopMenubarIndicator
	codexDesktopMenubarCommandFn = func(name string, args ...string) *exec.Cmd {
		return exec.Command(name, args...)
	}
	codexDesktopMenubarHomeFn = os.UserHomeDir
)

func startCodexDesktopMenubarIndicator(cfg codexDesktopMenubarConfig) func() {
	if runtime.GOOS != "darwin" || os.Getenv("SLIMFERENCE_CODEX_DESKTOP_MENUBAR") == "0" {
		return func() {}
	}
	title := strings.TrimSpace(cfg.Title)
	if title == "" {
		title = codexDesktopMenubarTitle
	}
	tooltip := strings.TrimSpace(cfg.Tooltip)
	if tooltip == "" {
		tooltip = codexDesktopMenubarTooltip
	}
	scriptPath, err := codexDesktopMenubarScriptPath()
	if err != nil {
		return func() {}
	}
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
		return func() {}
	}
	if err := os.WriteFile(scriptPath, []byte(codexDesktopMenubarScript(title, tooltip)), 0o600); err != nil {
		return func() {}
	}
	cmd := codexDesktopMenubarCommandFn("/usr/bin/osascript", "-l", "JavaScript", scriptPath)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(syscall.SIGTERM)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				if cmd.Process != nil {
					_ = cmd.Process.Kill()
				}
				<-done
			}
		})
	}
}

func codexDesktopMenubarScriptPath() (string, error) {
	home, err := codexDesktopMenubarHomeFn()
	if err != nil || strings.TrimSpace(home) == "" {
		if err != nil {
			return "", err
		}
		return "", os.ErrNotExist
	}
	return filepath.Join(home, ".slimference", "run", codexDesktopMenubarScriptName), nil
}

func codexDesktopMenubarScript(title string, tooltip string) string {
	return "ObjC.import('AppKit');\n" +
		"const app = $.NSApplication.sharedApplication;\n" +
		"app.setActivationPolicy($.NSApplicationActivationPolicyAccessory);\n" +
		"const item = $.NSStatusBar.systemStatusBar.statusItemWithLength($.NSVariableStatusItemLength);\n" +
		"item.button.title = " + strconv.Quote(title) + ";\n" +
		"item.button.toolTip = " + strconv.Quote(tooltip) + ";\n" +
		"const menu = $.NSMenu.alloc.initWithTitle('Slimference');\n" +
		"const active = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent('Slimference active', null, '');\n" +
		"active.setEnabled(false);\n" +
		"menu.addItem(active);\n" +
		"const scoped = $.NSMenuItem.alloc.initWithTitleActionKeyEquivalent('Codex App through Slimference', null, '');\n" +
		"scoped.setEnabled(false);\n" +
		"menu.addItem(scoped);\n" +
		"menu.addItem($.NSMenuItem.separatorItem);\n" +
		"menu.addItemWithTitleActionKeyEquivalent('Hide indicator', 'terminate:', '');\n" +
		"item.menu = menu;\n" +
		"app.run();\n"
}
