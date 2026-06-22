package filter

import (
	"fmt"
	"strings"
	"testing"
)

// --- TryCompactHistory tests ---

func TestTryCompactHistory_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&sb, "%5d  command_%d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactHistory([]string{"history"}, input)
	if !ok {
		t.Fatalf("TryCompactHistory returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[history] 200 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "command_200") {
		t.Fatalf("compacted missing most recent entry")
	}
	if !strings.Contains(s, "command_151") {
		t.Fatalf("compacted missing first kept entry (151)")
	}
}

func TestTryCompactHistory_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("    1  ls\n    2  cd\n    3  git status\n")
	_, ok := TryCompactHistory([]string{"history"}, input)
	if ok {
		t.Fatalf("TryCompactHistory should return false for small output")
	}
}

func TestTryCompactHistory_NotHistory(t *testing.T) {
	t.Parallel()
	input := []byte("1  ls\n2  cd\n")
	_, ok := TryCompactHistory([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactHistory should return false for non-history argv")
	}
}

func TestTryCompactHistory_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactHistory([]string{"history"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactHistory should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[history] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactDmesg tests ---

func TestTryCompactDmesg_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "[   %d.000000] kernel message %d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactDmesg([]string{"dmesg"}, input)
	if !ok {
		t.Fatalf("TryCompactDmesg returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[dmesg] 200 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "kernel message 199") {
		t.Fatalf("compacted missing most recent entry")
	}
}

func TestTryCompactDmesg_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("[ 1.0] msg1\n[ 2.0] msg2\n")
	_, ok := TryCompactDmesg([]string{"dmesg"}, input)
	if ok {
		t.Fatalf("TryCompactDmesg should return false for small output")
	}
}

func TestTryCompactDmesg_NotDmesg(t *testing.T) {
	t.Parallel()
	input := []byte("[ 1.0] msg\n")
	_, ok := TryCompactDmesg([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactDmesg should return false for non-dmesg argv")
	}
}

func TestTryCompactDmesg_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactDmesg([]string{"dmesg"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactDmesg should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[dmesg] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactMount tests ---

func TestTryCompactMount_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "/dev/sda%d on /mnt/disk%d type ext4 (rw)\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactMount([]string{"mount"}, input)
	if !ok {
		t.Fatalf("TryCompactMount returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[mount] 100 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "[+60 more entries]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactMount_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("/dev/sda1 on / type ext4 (rw)\n")
	_, ok := TryCompactMount([]string{"mount"}, input)
	if ok {
		t.Fatalf("TryCompactMount should return false for small output")
	}
}

func TestTryCompactMount_NotMount(t *testing.T) {
	t.Parallel()
	input := []byte("/dev/sda1 on / type ext4 (rw)\n")
	_, ok := TryCompactMount([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactMount should return false for non-mount argv")
	}
}

func TestTryCompactMount_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactMount([]string{"mount"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactMount should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[mount] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactBase64 tests ---

func TestTryCompactBase64_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "SGVsbG8gV29ybGQgVGhpcyBJcyBBIFRlc3QgTGluZSAlZAAK\n")
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactBase64([]string{"base64"}, input)
	if !ok {
		t.Fatalf("TryCompactBase64 returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[base64] 100 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
}

func TestTryCompactBase64_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("SGVsbG8gV29ybGQK\n")
	_, ok := TryCompactBase64([]string{"base64"}, input)
	if ok {
		t.Fatalf("TryCompactBase64 should return false for small output")
	}
}

func TestTryCompactBase64_NotBase64(t *testing.T) {
	t.Parallel()
	input := []byte("SGVsbG8=\n")
	_, ok := TryCompactBase64([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactBase64 should return false for non-base64 argv")
	}
}

func TestTryCompactBase64_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactBase64([]string{"base64"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactBase64 should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[base64] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactHashSum tests ---

func TestTryCompactHashSum_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "abcdef0123456789  file_%d.txt\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactHashSum([]string{"sha256sum"}, input)
	if !ok {
		t.Fatalf("TryCompactHashSum returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[sha256sum] 200 entries") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "[+150 more entries]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactHashSum_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("abcdef0123456789  file.txt\n")
	_, ok := TryCompactHashSum([]string{"sha256sum"}, input)
	if ok {
		t.Fatalf("TryCompactHashSum should return false for small output")
	}
}

func TestTryCompactHashSum_NotHashSum(t *testing.T) {
	t.Parallel()
	input := []byte("abcdef  file.txt\n")
	_, ok := TryCompactHashSum([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactHashSum should return false for non-hashsum argv")
	}
}

func TestTryCompactHashSum_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactHashSum([]string{"md5sum"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactHashSum should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[md5sum] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactObjdump tests ---

func TestTryCompactObjdump_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("file format elf64-x86-64\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "  %x:  48 89 e5  mov    rbp,rsp\n", i*4)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactObjdump([]string{"objdump"}, input)
	if !ok {
		t.Fatalf("TryCompactObjdump returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[objdump] 201 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
}

func TestTryCompactObjdump_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("file format elf64-x86-64\n  0:  48 89 e5  mov rbp,rsp\n")
	_, ok := TryCompactObjdump([]string{"objdump"}, input)
	if ok {
		t.Fatalf("TryCompactObjdump should return false for small output")
	}
}

func TestTryCompactObjdump_NotObjdump(t *testing.T) {
	t.Parallel()
	input := []byte("disassembly\n")
	_, ok := TryCompactObjdump([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactObjdump should return false for non-objdump argv")
	}
}

func TestTryCompactObjdump_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactObjdump([]string{"nm"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactObjdump should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[nm] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactStrace tests ---

func TestTryCompactStrace_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, "read(3, \"data\", 1024) = %d\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactStrace([]string{"strace"}, input)
	if !ok {
		t.Fatalf("TryCompactStrace returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[strace] 200 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
}

func TestTryCompactStrace_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("read(3, \"data\", 1024) = 5\n")
	_, ok := TryCompactStrace([]string{"strace"}, input)
	if ok {
		t.Fatalf("TryCompactStrace should return false for small output")
	}
}

func TestTryCompactStrace_NotStrace(t *testing.T) {
	t.Parallel()
	input := []byte("read(3) = 5\n")
	_, ok := TryCompactStrace([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactStrace should return false for non-strace argv")
	}
}

func TestTryCompactStrace_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactStrace([]string{"strace"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactStrace should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[strace] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactVmstat tests ---

func TestTryCompactVmstat_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("procs -----------memory---------- ---cpu--\n")
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&sb, " 1  0  0  100  200  300  0  0  10  90  0  0\n")
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactVmstat([]string{"vmstat"}, input)
	if !ok {
		t.Fatalf("TryCompactVmstat returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[vmstat] 200 data lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "procs") {
		t.Fatalf("compacted missing header")
	}
	if !strings.Contains(s, "omitted") {
		t.Fatalf("compacted missing omission marker")
	}
}

func TestTryCompactVmstat_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("procs ---cpu--\n 1  0  0  10  90\n")
	_, ok := TryCompactVmstat([]string{"vmstat"}, input)
	if ok {
		t.Fatalf("TryCompactVmstat should return false for small output")
	}
}

func TestTryCompactVmstat_NotVmstat(t *testing.T) {
	t.Parallel()
	input := []byte("procs\n 1  0\n")
	_, ok := TryCompactVmstat([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactVmstat should return false for non-vmstat argv")
	}
}

func TestTryCompactVmstat_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactVmstat([]string{"iostat"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactVmstat should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[iostat] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactIpAddr tests ---

func TestTryCompactIpAddr_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&sb, "eth%d: flags=4163<UP,BROADCAST,RUNNING,MULTICAST>  mtu 1500\n", i)
		fmt.Fprintf(&sb, "    inet 10.0.%d.%d  netmask 255.255.255.0  broadcast 10.0.%d.255\n", i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactIpAddr([]string{"ip"}, input)
	if !ok {
		t.Fatalf("TryCompactIpAddr returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[ip] 200 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "[+150 more lines]") {
		t.Fatalf("compacted missing truncation marker")
	}
}

func TestTryCompactIpAddr_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("eth0: flags=4163  mtu 1500\n    inet 10.0.0.1\n")
	_, ok := TryCompactIpAddr([]string{"ip"}, input)
	if ok {
		t.Fatalf("TryCompactIpAddr should return false for small output")
	}
}

func TestTryCompactIpAddr_NotIp(t *testing.T) {
	t.Parallel()
	input := []byte("eth0: flags=4163\n")
	_, ok := TryCompactIpAddr([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactIpAddr should return false for non-ip argv")
	}
}

func TestTryCompactIpAddr_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactIpAddr([]string{"ifconfig"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactIpAddr should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[ifconfig] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactCloc tests ---

func TestTryCompactCloc_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("Language                     files         blank       comment           code\n")
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&sb, "Language_%d                      10          100            50           500\n", i)
	}
	sb.WriteString("SUM                          500         5000           2500         25000\n")
	input := []byte(sb.String())
	compacted, ok := TryCompactCloc([]string{"cloc"}, input)
	if !ok {
		t.Fatalf("TryCompactCloc returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[cloc] 52 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "SUM") {
		t.Fatalf("compacted missing SUM line")
	}
}

func TestTryCompactCloc_SmallOutput(t *testing.T) {
	t.Parallel()
	input := []byte("Language  files  code\nGo          10   500\nSUM         10   500\n")
	_, ok := TryCompactCloc([]string{"cloc"}, input)
	if ok {
		t.Fatalf("TryCompactCloc should return false for small output")
	}
}

func TestTryCompactCloc_NotCloc(t *testing.T) {
	t.Parallel()
	input := []byte("Language  files  code\n")
	_, ok := TryCompactCloc([]string{"cat"}, input)
	if ok {
		t.Fatalf("TryCompactCloc should return false for non-cloc argv")
	}
}

func TestTryCompactCloc_EmptyOutput(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactCloc([]string{"tokei"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactCloc should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[tokei] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}
