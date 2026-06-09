// indist_probe is the operator-side tool that snapshots a real
// network capture into an indist.Capture JSON record. It wraps
// tshark, parses its structured output, and emits the JSON the
// internal/indist package expects.
//
// Subcommands:
//
//	indist_probe capture --label=<x> --out=<file> --iface=<lo0> --duration=<5s>
//	indist_probe diff <baseline.json> <proxy.json>
//	indist_probe lock-golden <capture.json> --target=<dir>
//
// Not a runtime component. Run on demand: at first install, on
// Codex version bumps, on Slimference TLS-layer changes.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/indist"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "capture":
		os.Exit(runCapture(os.Args[2:]))
	case "diff":
		os.Exit(runDiff(os.Args[2:]))
	case "lock-golden":
		os.Exit(runLockGolden(os.Args[2:]))
	case "--help", "-h", "help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", os.Args[1])
		printUsage()
		os.Exit(2)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `usage: indist_probe <capture|diff|lock-golden> [flags]

capture       snapshot a real TLS connection into JSON via tshark
diff          compare two capture JSON files; exit 1 on drift
	lock-golden   copy a capture JSON to tests/fixtures/indist/<target>/

Run capture from the operator machine before enabling MITM, then once
after. Diff the two — non-empty drift means the proxy's wire shape is
distinguishable from a vanilla Codex client.`)
}

// runCapture wraps tshark and produces an indist.Capture JSON.
func runCapture(args []string) int {
	fs := flag.NewFlagSet("capture", flag.ContinueOnError)
	label := fs.String("label", "", "tag for this capture (e.g. codex-0.130 or slimference-mitm)")
	out := fs.String("out", "", "output JSON path (default: <label>.json)")
	iface := fs.String("iface", "lo0", "interface to capture on (lo0, en0, ...)")
	duration := fs.Duration("duration", 10*time.Second, "max capture duration")
	host := fs.String("host", "chatgpt.com", "host filter for tshark")
	port := fs.Int("port", 443, "port filter (443 for direct, 8443 for SNI-peek listener)")
	tsharkBin := fs.String("tshark", "tshark", "tshark binary path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *label == "" {
		fmt.Fprintln(os.Stderr, "capture: --label is required")
		return 2
	}
	outPath := *out
	if outPath == "" {
		outPath = *label + ".json"
	}

	// Sanity-check tshark presence.
	if _, err := exec.LookPath(*tsharkBin); err != nil {
		fmt.Fprintf(os.Stderr, "capture: tshark not in PATH: %v\n", err)
		fmt.Fprintln(os.Stderr, "  install via: brew install wireshark")
		return 1
	}

	fmt.Fprintf(os.Stderr, "capture: listening on %s for %s (host=%s port=%d)\n",
		*iface, *duration, *host, *port)
	fmt.Fprintln(os.Stderr, "  → trigger one real conversation now (e.g. run `codex`)")

	cap, err := runTSharkCapture(*tsharkBin, *iface, *host, *port, *duration, *label)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: %v\n", err)
		return 1
	}

	data, err := json.MarshalIndent(cap, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "capture: marshal: %v\n", err)
		return 1
	}
	if dir := filepath.Dir(outPath); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "capture: mkdir %s: %v\n", dir, err)
			return 1
		}
	}
	if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "capture: write %s: %v\n", outPath, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "capture: %d cipher(s), %d extension(s), JA3=%s\n",
		len(cap.CipherIDs), len(cap.ExtensionIDs), cap.JA3)
	fmt.Fprintf(os.Stderr, "capture: wrote %s\n", outPath)
	return 0
}

// runDiff compares two capture files. Exits 0 on indistinguishable,
// 1 on drift.
func runDiff(args []string) int {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "diff: usage: indist_probe diff <baseline.json> <proxy.json>")
		return 2
	}
	baseline, err := loadCapture(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff: baseline: %v\n", err)
		return 2
	}
	proxy, err := loadCapture(args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "diff: proxy: %v\n", err)
		return 2
	}
	report := indist.Diff(baseline, proxy)
	out, _ := json.MarshalIndent(report, "", "  ")
	fmt.Println(string(out))
	if !report.OK() {
		fmt.Fprintln(os.Stderr, report.Summary())
		return 1
	}
	fmt.Fprintln(os.Stderr, report.Summary())
	return 0
}

// runLockGolden copies a capture JSON into tests/fixtures/indist/<target>/.
func runLockGolden(args []string) int {
	fs := flag.NewFlagSet("lock-golden", flag.ContinueOnError)
	target := fs.String("target", "", "subdirectory under tests/fixtures/indist/")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *target == "" || fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "lock-golden: usage: indist_probe lock-golden --target=<dir> <capture.json>")
		return 2
	}
	srcPath := fs.Arg(0)
	src, err := loadCapture(srcPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lock-golden: load: %v\n", err)
		return 1
	}
	dstDir := filepath.Join("tests", "fixtures", "indist", *target)
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "lock-golden: mkdir: %v\n", err)
		return 1
	}
	dstFile := filepath.Join(dstDir, "baseline.json")
	data, _ := json.MarshalIndent(src, "", "  ")
	if err := os.WriteFile(dstFile, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "lock-golden: write: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "lock-golden: locked %s (label=%s, JA3=%s)\n",
		dstFile, src.Label, src.JA3)
	return 0
}

// loadCapture reads + decodes a Capture JSON file.
func loadCapture(path string) (indist.Capture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return indist.Capture{}, err
	}
	var c indist.Capture
	if err := json.Unmarshal(data, &c); err != nil {
		return indist.Capture{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return c, nil
}

// runTSharkCapture invokes tshark with the right filters and parses
// its JSON-array output into an indist.Capture. Best-effort: missing
// fields are left zero-valued so the diff still functions even if
// tshark omits some dissection.
func runTSharkCapture(bin, iface, host string, port int, duration time.Duration, label string) (indist.Capture, error) {
	filter := fmt.Sprintf("host %s and tcp port %d", host, port)
	args := []string{
		"-i", iface,
		"-f", filter,
		"-Y", "tls.handshake.type==1 or tls.handshake.type==2 or http2 or websocket",
		"-T", "json",
		"-a", fmt.Sprintf("duration:%d", int(duration.Seconds())),
		"-J", "tls http2 websocket http",
		"-l",
	}
	cmd := exec.Command(bin, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return indist.Capture{}, fmt.Errorf("tshark exited %d: %s", ee.ExitCode(), string(ee.Stderr))
		}
		return indist.Capture{}, err
	}
	return parseTSharkJSON(out, label)
}

// parseTSharkJSON walks tshark's JSON-array dissection output and
// fills an indist.Capture. tshark's output is verbose; this code is
// intentionally tolerant of missing fields — operator runs it on
// real-world captures where dissection is partial.
func parseTSharkJSON(data []byte, label string) (indist.Capture, error) {
	if len(data) == 0 || strings.TrimSpace(string(data)) == "[]" {
		return indist.Capture{}, errors.New("tshark returned empty array (no matching packets)")
	}
	var pkts []tsharkPacket
	if err := json.Unmarshal(data, &pkts); err != nil {
		return indist.Capture{}, fmt.Errorf("parse tshark json: %w", err)
	}
	cap := indist.Capture{Label: label}
	for _, p := range pkts {
		layers := p.Source.Layers

		if tls := layers.TLS; tls != nil {
			if ch := tls.Record.HandshakeExtensions(); ch != nil {
				if cap.SNI == "" {
					cap.SNI = ch.SNI
				}
				if len(cap.ALPN) == 0 && len(ch.ALPN) > 0 {
					cap.ALPN = ch.ALPN
				}
				if len(cap.CipherIDs) == 0 && len(ch.Ciphers) > 0 {
					cap.CipherIDs = ch.Ciphers
				}
				if len(cap.ExtensionIDs) == 0 && len(ch.Extensions) > 0 {
					cap.ExtensionIDs = ch.Extensions
				}
				if len(cap.CurveIDs) == 0 && len(ch.Curves) > 0 {
					cap.CurveIDs = ch.Curves
				}
				if !cap.GREASE && ch.HasGREASE {
					cap.GREASE = true
				}
				if cap.JA3 == "" && ch.JA3 != "" {
					cap.JA3 = ch.JA3
					cap.JA3Hash = ch.JA3Hash
				}
				if cap.JA4 == "" && ch.JA4 != "" {
					cap.JA4 = ch.JA4
				}
			}
		}
		if http2 := layers.HTTP2; http2 != nil {
			if len(cap.H2Settings) == 0 {
				cap.H2Settings = http2.Settings()
			}
			if len(cap.H2PseudoHeaderOrder) == 0 {
				cap.H2PseudoHeaderOrder = http2.PseudoHeaderOrder()
			}
			if len(cap.HeaderOrder) == 0 {
				cap.HeaderOrder = http2.HeaderOrder()
			}
		}
		if ws := layers.WebSocket; ws != nil {
			if cap.WSExtensions == "" {
				cap.WSExtensions = ws.Extensions
			}
			if cap.WSSubprotocol == "" {
				cap.WSSubprotocol = ws.Subprotocol
			}
			if cap.WSVersion == "" {
				cap.WSVersion = ws.Version
			}
		}
	}
	if cap.JA3 == "" && len(cap.CipherIDs) == 0 {
		return cap, errors.New("no TLS ClientHello observed in capture window")
	}
	return cap, nil
}
