// Package main builds the single Slimference binary with the default
// local release flags. This is intentionally not a split build: the
// product stays one self-contained binary.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const defaultOutput = "./slimference"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", defaultOutput, "output binary path")
	install := fs.Bool("install", false, "copy built binary to ~/.local/bin/slimference")
	version := fs.String("version", "", "optional version to inject into buildinfo.Version")
	dryRun := fs.Bool("dry-run", false, "print commands without executing them")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}

	buildArgs := buildCommandArgs(*out, *version)
	fmt.Fprintf(stdout, "go %s\n", strings.Join(buildArgs, " "))
	if !*dryRun {
		cmd := exec.Command("go", buildArgs...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("go build: %w", err)
		}
	}

	if !*install {
		return nil
	}
	dst, err := installPath()
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "install %s -> %s\n", *out, dst)
	if *dryRun {
		return nil
	}
	if err := copyFile(*out, dst); err != nil {
		return fmt.Errorf("install binary: %w", err)
	}
	return nil
}

func buildCommandArgs(out, version string) []string {
	ldflags := "-s -w"
	if strings.TrimSpace(version) != "" {
		ldflags += " -X github.com/slimference/slimference/internal/buildinfo.Version=" + strings.TrimSpace(version)
	}
	return []string{
		"build",
		"-trimpath",
		"-ldflags", ldflags,
		"-o", out,
		"./cmd/slimference",
	}
}

func installPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".local", "bin", "slimference"), nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	dstDir := filepath.Dir(dst)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	out, err := os.CreateTemp(dstDir, "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := out.Name()
	installed := false
	defer func() {
		if !installed {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Chmod(0o755); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	installed = true
	return nil
}
