// Package main - scripts/release cross-builds the slimference binary for
// every supported OS/arch target and assembles a release bundle under dist/.
//
// Usage:
//
//	go run ./scripts/release --version v0.6.0
//	go run ./scripts/release --version v0.6.0 --dry-run
//
// For each target the tool runs:
//
//	CGO_ENABLED=0 GOOS=<os> GOARCH=<arch> go build \
//	    -trimpath -ldflags "-s -w -X main.version=<v> -X main.commit=<sha>" \
//	    -o dist/slimference_<version>_<os>_<arch>/slimference ./cmd/slimference
//
// then packages the binary, LICENSE, README.md, install.sh, docs, and service
// helpers into a
// .tar.gz and emits a SHA256SUMS file next to the archives.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type target struct {
	os   string
	arch string
}

// allTargets is the full matrix of platforms the release script CAN build.
// Kept as a complete list so cross-platform support stays available; the
// default --targets selection ships only the primary target (macOS on
// M-series Apple silicon).
var allTargets = []target{
	{"darwin", "arm64"}, // primary, default build target
	{"darwin", "amd64"},
	{"linux", "arm64"},
	{"linux", "amd64"},
}

// defaultTargetSelector names the default build matrix. "primary" emits only
// darwin_arm64 (the supported macOS-on-M-series build). "all" emits every
// target in allTargets. Any comma-separated list of `os/arch` pairs is also
// accepted, e.g. "darwin/arm64,linux/amd64".
const defaultTargetSelector = "primary"

func resolveTargets(selector string) ([]target, error) {
	switch selector {
	case "primary", "":
		return []target{{"darwin", "arm64"}}, nil
	case "all":
		return allTargets, nil
	}
	var out []target
	for _, spec := range strings.Split(selector, ",") {
		spec = strings.TrimSpace(spec)
		if spec == "" {
			continue
		}
		parts := strings.Split(spec, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid target %q (want os/arch)", spec)
		}
		out = append(out, target{parts[0], parts[1]})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty --targets list")
	}
	return out, nil
}

func main() {
	version := flag.String("version", "", "release tag, e.g. v0.6.0 (required)")
	dryRun := flag.Bool("dry-run", false, "print commands instead of executing them")
	outDir := flag.String("out", "dist", "output directory")
	targetsFlag := flag.String("targets", defaultTargetSelector,
		`target selector: "primary" (darwin/arm64 only, default), "all" (every supported target), or a comma-separated list like "darwin/arm64,linux/amd64"`)
	flag.Parse()

	targets, err := resolveTargets(*targetsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "targets: %v\n", err)
		os.Exit(2)
	}

	if *version == "" {
		fmt.Fprintln(os.Stderr, "--version is required (e.g. v0.6.0)")
		os.Exit(2)
	}
	ver := strings.TrimPrefix(*version, "v")

	commit, cerr := gitCommit()
	if cerr != nil {
		fmt.Fprintf(os.Stderr, "git commit lookup failed: %v\n", cerr)
		os.Exit(1)
	}

	if !*dryRun {
		if err := os.RemoveAll(*outDir); err != nil {
			fmt.Fprintf(os.Stderr, "clean %s: %v\n", *outDir, err)
			os.Exit(1)
		}
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", *outDir, err)
			os.Exit(1)
		}
	}

	var archives []string
	for _, t := range targets {
		name := fmt.Sprintf("slimference_%s_%s_%s", ver, t.os, t.arch)
		tgtDir := filepath.Join(*outDir, name)
		binPath := filepath.Join(tgtDir, "slimference")
		if t.os == "windows" {
			binPath += ".exe"
		}

		env := append(os.Environ(),
			"CGO_ENABLED=0",
			"GOOS="+t.os,
			"GOARCH="+t.arch,
		)
		// Inject both the buildinfo.Version package-level var (the real
		// source of truth read by `slimference --version` / doctor) and a
		// main.commit symbol for diagnostic builds. main.version is also
		// set for backwards compat with older code that read it directly.
		ldflags := fmt.Sprintf(
			"-s -w -X github.com/Christopher-Schulze/Slimference/internal/buildinfo.Version=%s -X main.version=%s -X main.commit=%s",
			ver, ver, commit)
		args := []string{"build", "-trimpath",
			"-ldflags", ldflags,
			"-o", binPath,
			"./cmd/slimference",
		}
		cmd := exec.Command("go", args...)
		cmd.Env = env
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		fmt.Printf(">>> GOOS=%s GOARCH=%s go %s\n", t.os, t.arch, strings.Join(args, " "))
		if *dryRun {
			continue
		}
		if err := os.MkdirAll(tgtDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir %s: %v\n", tgtDir, err)
			os.Exit(1)
		}
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "build %s/%s: %v\n", t.os, t.arch, err)
			os.Exit(1)
		}

		// Copy auxiliary docs and helpers into the bundle directory.
		for _, extra := range []string{"README.md", "LICENSE", "install.sh", "docs/layer0-exit-codes.md"} {
			if _, err := os.Stat(extra); err != nil {
				continue
			}
			dst := filepath.Join(tgtDir, filepath.Base(extra))
			if err := copyFile(extra, dst); err != nil {
				fmt.Fprintf(os.Stderr, "copy %s: %v\n", extra, err)
			}
		}

		// Linux bundles get the systemd unit template.
		if t.os == "linux" {
			unit := "scripts/service/linux/slimference.service"
			if _, err := os.Stat(unit); err == nil {
				dst := filepath.Join(tgtDir, "slimference.service")
				_ = copyFile(unit, dst)
			}
		}

		archive := filepath.Join(*outDir, name+".tar.gz")
		if err := tarGzDir(tgtDir, archive); err != nil {
			fmt.Fprintf(os.Stderr, "tar.gz %s: %v\n", tgtDir, err)
			os.Exit(1)
		}
		archives = append(archives, archive)
		fmt.Printf(">>> wrote %s\n", archive)
	}

	if *dryRun {
		return
	}

	sumsPath := filepath.Join(*outDir, "SHA256SUMS")
	if err := writeSHA256Sums(archives, sumsPath); err != nil {
		fmt.Fprintf(os.Stderr, "sha256sums: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(">>> wrote %s\n", sumsPath)
}

func gitCommit() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()
	_, err = io.Copy(d, s)
	if err != nil {
		return err
	}
	return d.Chmod(info.Mode().Perm())
}

func tarGzDir(dir, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	gz := gzip.NewWriter(f)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Dir(dir), path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = io.Copy(tw, in)
		return err
	})
}

func writeSHA256Sums(paths []string, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		if _, err := fmt.Fprintf(f, "%s  %s\n", hex.EncodeToString(sum[:]),
			filepath.Base(p)); err != nil {
			return err
		}
	}
	return nil
}
