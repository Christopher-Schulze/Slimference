package filter

import (
	"fmt"
	"path/filepath"
	"strings"
)

// TryCompactHistory compacts shell history output (`history`, `fc -l`) by
// capping the number of entries. Shell history can contain thousands of lines;
// the model typically only needs the most recent entries for context.
//
// Drawdown vector: the model loses individual history entries beyond the cap.
// The most recent entries (which are most relevant for agent context) are
// preserved. Fail-open on non-history output or small output.
func TryCompactHistory(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "history" && b != "history.exe" && b != "fc" && b != "fc.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[history] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxEntries = 50
	// Keep the most recent entries (last N lines) — they are most relevant.
	start := len(lines) - maxEntries
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[history] %d entries (showing last %d)\n", len(lines), len(lines)-start))
	for _, line := range lines[start:] {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactDmesg compacts kernel ring buffer output (`dmesg`) by capping the
// number of lines. dmesg can produce thousands of lines on a system with
// extensive boot or hardware messages; the model typically needs the most
// recent messages and any error/warning lines.
//
// Drawdown vector: the model loses individual log entries beyond the cap.
// The most recent entries are preserved. Fail-open on non-dmesg output or
// small output.
func TryCompactDmesg(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "dmesg" && b != "dmesg.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[dmesg] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLines = 50
	// Keep the most recent entries (last N lines) — they are most relevant
	// for diagnosing current issues.
	start := len(lines) - maxLines
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[dmesg] %d lines (showing last %d)\n", len(lines), len(lines)-start))
	for _, line := range lines[start:] {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactMount compacts `mount` output by capping the number of mount
// entries. On containers or systems with many bind mounts, mount output can
// be dozens of lines.
//
// Drawdown vector: the model loses individual mount entries beyond the cap.
// Fail-open on non-mount output or small output.
func TryCompactMount(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "mount" && b != "mount.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[mount] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 15 {
		return stdout, false
	}
	const maxEntries = 40
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[mount] %d entries (showing first %d)\n", len(lines), maxEntries))
	for i, line := range lines {
		if i >= maxEntries {
			sb.WriteString(fmt.Sprintf("  [+%d more entries]\n", len(lines)-maxEntries))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactBase64 compacts `base64`/`base32` output by capping the number of
// lines. Base64-encoded output is extremely low information density for
// language models — the model rarely needs more than the first/last few lines
// to identify the content type or verify encoding.
//
// Drawdown vector: the model loses encoded bytes beyond the cap. The first
// and last lines are always preserved (content signature + end marker).
// Fail-open on non-base64 output or small output.
func TryCompactBase64(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "base64" && b != "base64.exe" && b != "base32" && b != "base32.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[base64] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 20 {
		return stdout, false
	}
	const maxLines = 10
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[base64] %d lines (showing first %d + last 3)\n", len(lines), maxLines))
	lastLines := lines[len(lines)-3:]
	for i, line := range lines {
		if i >= maxLines && i < len(lines)-3 {
			sb.WriteString(fmt.Sprintf("  [+%d more lines]\n", len(lines)-maxLines-3))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	// Ensure last 3 lines are always included.
	written := strings.TrimRight(sb.String(), "\n")
	writtenLines := strings.Split(written, "\n")
	needLast := true
	if len(writtenLines) >= 3 {
		match := true
		for j := 0; j < 3; j++ {
			if writtenLines[len(writtenLines)-3+j] != lastLines[j] {
				match = false
				break
			}
		}
		if match {
			needLast = false
		}
	}
	if needLast {
		for _, line := range lastLines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactHashSum compacts `md5sum`/`sha256sum`/`shasum`/`b2sum` output by
// capping the number of hash entries. Hash listings for large directory trees
// can produce hundreds or thousands of lines.
//
// Drawdown vector: the model loses individual hash entries beyond the cap.
// Fail-open on non-hashsum output or small output.
func TryCompactHashSum(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "md5sum" && b != "md5sum.exe" &&
		b != "sha256sum" && b != "sha256sum.exe" &&
		b != "sha1sum" && b != "sha1sum.exe" &&
		b != "sha512sum" && b != "sha512sum.exe" &&
		b != "shasum" && b != "shasum.exe" &&
		b != "b2sum" && b != "b2sum.exe" &&
		b != "cksum" && b != "cksum.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxEntries = 50
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d entries (showing first %d)\n", b, len(lines), maxEntries))
	for i, line := range lines {
		if i >= maxEntries {
			sb.WriteString(fmt.Sprintf("  [+%d more entries]\n", len(lines)-maxEntries))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactObjdump compacts `objdump`/`readelf`/`nm`/`strings` output by
// capping the number of lines. Binary disassembly and symbol tables can
// produce thousands of lines that are extremely low information density for
// language models.
//
// Drawdown vector: the model loses disassembly/symbol entries beyond the cap.
// The first and last lines are preserved (file header + end marker).
// Fail-open on non-objdump output or small output.
func TryCompactObjdump(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "objdump" && b != "objdump.exe" &&
		b != "readelf" && b != "readelf.exe" &&
		b != "nm" && b != "nm.exe" &&
		b != "strings" && b != "strings.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLines = 50
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d lines (showing first %d + last 3)\n", b, len(lines), maxLines))
	lastLines := lines[len(lines)-3:]
	for i, line := range lines {
		if i >= maxLines && i < len(lines)-3 {
			sb.WriteString(fmt.Sprintf("  [+%d more lines]\n", len(lines)-maxLines-3))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	// Ensure last 3 lines are always included.
	written := strings.TrimRight(sb.String(), "\n")
	writtenLines := strings.Split(written, "\n")
	needLast := true
	if len(writtenLines) >= 3 {
		match := true
		for j := 0; j < 3; j++ {
			if writtenLines[len(writtenLines)-3+j] != lastLines[j] {
				match = false
				break
			}
		}
		if match {
			needLast = false
		}
	}
	if needLast {
		for _, line := range lastLines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactStrace compacts `strace`/`ltrace` output by capping the number of
// lines. Syscall/library call traces can produce thousands of lines that are
// extremely low information density for language models.
//
// Drawdown vector: the model loses individual syscall entries beyond the cap.
// The first and last lines are preserved (initial syscalls + exit status).
// Fail-open on non-strace output or small output.
func TryCompactStrace(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "strace" && b != "strace.exe" && b != "ltrace" && b != "ltrace.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLines = 50
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d lines (showing first %d + last 3)\n", b, len(lines), maxLines))
	lastLines := lines[len(lines)-3:]
	for i, line := range lines {
		if i >= maxLines && i < len(lines)-3 {
			sb.WriteString(fmt.Sprintf("  [+%d more lines]\n", len(lines)-maxLines-3))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	// Ensure last 3 lines are always included.
	written := strings.TrimRight(sb.String(), "\n")
	writtenLines := strings.Split(written, "\n")
	needLast := true
	if len(writtenLines) >= 3 {
		match := true
		for j := 0; j < 3; j++ {
			if writtenLines[len(writtenLines)-3+j] != lastLines[j] {
				match = false
				break
			}
		}
		if match {
			needLast = false
		}
	}
	if needLast {
		for _, line := range lastLines {
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactVmstat compacts `vmstat`/`iostat`/`mpstat`/`sar` output by capping
// the number of lines. These tools produce repeated snapshots over time that
// can generate hundreds of lines; the model typically needs the header and
// the most recent samples.
//
// Drawdown vector: the model loses individual stat samples beyond the cap.
// The header and most recent entries are preserved. Fail-open on non-vmstat
// output or small output.
func TryCompactVmstat(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "vmstat" && b != "vmstat.exe" &&
		b != "iostat" && b != "iostat.exe" &&
		b != "mpstat" && b != "mpstat.exe" &&
		b != "sar" && b != "sar.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 20 {
		return stdout, false
	}
	const maxLines = 30
	// Keep header + first few + last few (most recent samples).
	header := lines[0]
	dataLines := lines[1:]
	var sb strings.Builder
	sb.Grow(len(stdout))
	if len(dataLines) <= maxLines {
		return stdout, false
	}
	keepFirst := 10
	keepLast := 15
	if keepFirst+keepLast > len(dataLines) {
		keepFirst = len(dataLines) / 2
		keepLast = len(dataLines) - keepFirst
	}
	sb.WriteString(fmt.Sprintf("[%s] %d data lines (header + first %d + last %d)\n", b, len(dataLines), keepFirst, keepLast))
	sb.WriteString(header)
	sb.WriteByte('\n')
	for i := 0; i < keepFirst; i++ {
		sb.WriteString(dataLines[i])
		sb.WriteByte('\n')
	}
	sb.WriteString(fmt.Sprintf("  [+%d more samples omitted]\n", len(dataLines)-keepFirst-keepLast))
	for i := len(dataLines) - keepLast; i < len(dataLines); i++ {
		sb.WriteString(dataLines[i])
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactIpAddr compacts `ip`/`ifconfig` output by capping the number of
// interface sections. On systems with many network interfaces or VLANs, this
// output can be lengthy.
//
// Drawdown vector: the model loses individual interface details beyond the cap.
// Fail-open on non-ip/ifconfig output or small output.
func TryCompactIpAddr(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "ip" && b != "ip.exe" && b != "ifconfig" && b != "ifconfig.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLines = 50
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d lines (showing first %d)\n", b, len(lines), maxLines))
	for i, line := range lines {
		if i >= maxLines {
			sb.WriteString(fmt.Sprintf("  [+%d more lines]\n", len(lines)-maxLines))
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactCloc compacts `cloc`/`scc`/`tokei`/`loc` output by extracting the
// summary table and capping per-language entries. These tools produce a
// language-by-language breakdown followed by a SUM line; the model typically
// only needs the summary.
//
// Drawdown vector: the model loses individual per-language breakdown entries
// beyond the cap. The SUM/total line is always preserved. Fail-open on
// non-cloc output or small output.
func TryCompactCloc(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "cloc" && b != "cloc.exe" &&
		b != "scc" && b != "scc.exe" &&
		b != "tokei" && b != "tokei.exe" &&
		b != "loc" && b != "loc.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", b)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLangEntries = 20
	// Find the SUM/total line — it's usually the last meaningful line.
	var sumLine string
	sumIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		upper := strings.ToUpper(strings.TrimSpace(lines[i]))
		if strings.HasPrefix(upper, "SUM") || strings.HasPrefix(upper, "TOTAL") {
			sumLine = lines[i]
			sumIdx = i
			break
		}
	}
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d lines (showing first %d + summary)\n", b, len(lines), maxLangEntries))
	for i, line := range lines {
		if i >= maxLangEntries {
			break
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	if sumIdx >= 0 && sumIdx >= maxLangEntries {
		sb.WriteString("  [...middle entries omitted...]\n")
		sb.WriteString(sumLine)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactDocker compacts docker/podman output by capping lines.
func TryCompactDocker(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if !strings.HasPrefix(b, "docker") && b != "podman" && b != "nerdctl" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactKubectl compacts kubectl/oc output by capping lines.
func TryCompactKubectl(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "kubectl" && b != "oc" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactHelm compacts helm output by capping lines.
func TryCompactHelm(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "helm" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 30, 3)
}

// TryCompactSystemctl compacts systemctl output by capping lines.
func TryCompactSystemctl(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "systemctl" && b != "systemctl.exe" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactJournalctl compacts journalctl output by capping to most-recent lines.
func TryCompactJournalctl(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "journalctl" && b != "journalctl.exe" {
		return stdout, false
	}
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte("[journalctl] empty\n"), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) < 30 {
		return stdout, false
	}
	const maxLines = 50
	start := len(lines) - maxLines
	if start < 0 {
		start = 0
	}
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[journalctl] %d lines (showing last %d)\n", len(lines), len(lines)-start))
	for _, line := range lines[start:] {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}

// TryCompactCargo compacts cargo output by capping lines.
func TryCompactCargo(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "cargo" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactRustc compacts rustc output by capping lines.
func TryCompactRustc(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "rustc" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactTcpdump compacts tcpdump/tshark output by capping lines.
func TryCompactTcpdump(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "tcpdump" && b != "tshark" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// TryCompactPerf compacts perf output by capping lines.
func TryCompactPerf(argv []string, stdout []byte) ([]byte, bool) {
	if len(argv) < 1 {
		return stdout, false
	}
	b := strings.ToLower(filepath.Base(argv[0]))
	if b != "perf" {
		return stdout, false
	}
	return capWithLastN(argv, stdout, b, 50, 3)
}

// capWithLastN is the shared helper for first-N + last-N + omission marker compaction.
// Returns the compacted output and true if compaction happened, stdout and false otherwise.
func capWithLastN(argv []string, stdout []byte, name string, cap, lastN int) ([]byte, bool) {
	s := strings.TrimSpace(string(stdout))
	if s == "" {
		return []byte(fmt.Sprintf("[%s] empty\n", name)), true
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= cap {
		return stdout, false
	}
	var sb strings.Builder
	sb.Grow(len(stdout))
	sb.WriteString(fmt.Sprintf("[%s] %d lines (showing first %d + last %d)\n", name, len(lines), cap, lastN))
	for i := 0; i < cap && i < len(lines); i++ {
		sb.WriteString(lines[i])
		sb.WriteByte('\n')
	}
	sb.WriteString(fmt.Sprintf("  [...%d lines omitted...]\n", len(lines)-cap-lastN))
	for i := len(lines) - lastN; i < len(lines); i++ {
		if i >= 0 {
			sb.WriteString(lines[i])
			sb.WriteByte('\n')
		}
	}
	out := strings.TrimRight(sb.String(), "\n")
	if len(out) >= len(stdout) {
		return stdout, false
	}
	return []byte(out + "\n"), true
}
