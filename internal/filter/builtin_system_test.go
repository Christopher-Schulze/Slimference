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

// --- TryCompactDocker tests ---

func TestTryCompactDocker_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "container_%d  image_%d  status_%d\n", i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactDocker([]string{"docker"}, input)
	if !ok {
		t.Fatalf("TryCompactDocker returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if !strings.Contains(s, "[docker] 100 lines") {
		t.Fatalf("compacted missing summary: %s", s[:200])
	}
	if !strings.Contains(s, "container_1") {
		t.Fatalf("compacted missing first entry")
	}
	if !strings.Contains(s, "container_100") {
		t.Fatalf("compacted missing last entry")
	}
}

func TestTryCompactDocker_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("container_1  image_1  Up\ncontainer_2  image_2  Up\n")
	_, ok := TryCompactDocker([]string{"docker"}, input)
	if ok {
		t.Fatalf("TryCompactDocker should return ok=false for small output")
	}
}

func TestTryCompactDocker_Empty(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactDocker([]string{"docker"}, []byte(""))
	if !ok {
		t.Fatalf("TryCompactDocker should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[docker] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

func TestTryCompactDocker_WrongCommand(t *testing.T) {
	t.Parallel()
	input := []byte("some output\n")
	_, ok := TryCompactDocker([]string{"ls"}, input)
	if ok {
		t.Fatalf("TryCompactDocker should return ok=false for wrong command")
	}
}

// --- TryCompactKubectl tests ---

func TestTryCompactKubectl_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "pod-%d  Running  10.0.0.%d  node-%d\n", i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactKubectl([]string{"kubectl"}, input)
	if !ok {
		t.Fatalf("TryCompactKubectl returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	if !strings.Contains(string(compacted), "[kubectl] 100 lines") {
		t.Fatalf("compacted missing summary")
	}
}

func TestTryCompactKubectl_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("pod-1  Running  10.0.0.1\n")
	_, ok := TryCompactKubectl([]string{"kubectl"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

func TestTryCompactKubectl_WrongCommand(t *testing.T) {
	t.Parallel()
	_, ok := TryCompactKubectl([]string{"docker"}, []byte("output\n"))
	if ok {
		t.Fatalf("should return ok=false for wrong command")
	}
}

// --- TryCompactHelm tests ---

func TestTryCompactHelm_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 50; i++ {
		fmt.Fprintf(&sb, "release-%d  chart-%d  deployed  namespace-%d\n", i, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactHelm([]string{"helm"}, input)
	if !ok {
		t.Fatalf("TryCompactHelm returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	if !strings.Contains(string(compacted), "[helm] 50 lines") {
		t.Fatalf("compacted missing summary")
	}
}

func TestTryCompactHelm_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("release-1  chart-1  deployed\n")
	_, ok := TryCompactHelm([]string{"helm"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

// --- TryCompactSystemctl tests ---

func TestTryCompactSystemctl_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "service-%d.service  loaded  active  running\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactSystemctl([]string{"systemctl"}, input)
	if !ok {
		t.Fatalf("TryCompactSystemctl returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
}

func TestTryCompactSystemctl_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("service-1.service  loaded  active  running\n")
	_, ok := TryCompactSystemctl([]string{"systemctl"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

func TestTryCompactSystemctl_WrongCommand(t *testing.T) {
	t.Parallel()
	_, ok := TryCompactSystemctl([]string{"ls"}, []byte("output\n"))
	if ok {
		t.Fatalf("should return ok=false for wrong command")
	}
}

// --- TryCompactJournalctl tests ---

func TestTryCompactJournalctl_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&sb, "Jun 22 10:00:%d host service[%d]: log message %d\n", i%60, i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactJournalctl([]string{"journalctl"}, input)
	if !ok {
		t.Fatalf("TryCompactJournalctl returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	if !strings.Contains(string(compacted), "[journalctl] 200 lines") {
		t.Fatalf("compacted missing summary")
	}
}

func TestTryCompactJournalctl_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("Jun 22 10:00:00 host service[1]: log\n")
	_, ok := TryCompactJournalctl([]string{"journalctl"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

func TestTryCompactJournalctl_Empty(t *testing.T) {
	t.Parallel()
	compacted, ok := TryCompactJournalctl([]string{"journalctl"}, []byte(""))
	if !ok {
		t.Fatalf("should return true for empty output")
	}
	if !strings.Contains(string(compacted), "[journalctl] empty") {
		t.Fatalf("compacted should contain empty marker")
	}
}

// --- TryCompactCargo tests ---

func TestTryCompactCargo_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "Compiling crate-%d v0.1.%d\n", i, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactCargo([]string{"cargo"}, input)
	if !ok {
		t.Fatalf("TryCompactCargo returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	if !strings.Contains(string(compacted), "[cargo] 100 lines") {
		t.Fatalf("compacted missing summary")
	}
}

func TestTryCompactCargo_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("Compiling crate-1 v0.1.0\n")
	_, ok := TryCompactCargo([]string{"cargo"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

// --- TryCompactRustc tests ---

func TestTryCompactRustc_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "warning: unused variable `x` at line %d\n", i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactRustc([]string{"rustc"}, input)
	if !ok {
		t.Fatalf("TryCompactRustc returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
}

func TestTryCompactRustc_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("warning: unused variable\n")
	_, ok := TryCompactRustc([]string{"rustc"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

// --- TryCompactTcpdump tests ---

func TestTryCompactTcpdump_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "10:00:00.%06d IP 10.0.0.%d > 10.0.0.%d: packet %d\n", i, i, i+1, i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactTcpdump([]string{"tcpdump"}, input)
	if !ok {
		t.Fatalf("TryCompactTcpdump returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
}

func TestTryCompactTcpdump_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("10:00:00 IP 10.0.0.1 > 10.0.0.2: packet\n")
	_, ok := TryCompactTcpdump([]string{"tcpdump"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

// --- TryCompactPerf tests ---

func TestTryCompactPerf_BasicCap(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := 1; i <= 100; i++ {
		fmt.Fprintf(&sb, "%8.2f%%  function_%d  /usr/lib/lib.so\n", float64(i), i)
	}
	input := []byte(sb.String())
	compacted, ok := TryCompactPerf([]string{"perf"}, input)
	if !ok {
		t.Fatalf("TryCompactPerf returned ok=false")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
}

func TestTryCompactPerf_BelowThreshold(t *testing.T) {
	t.Parallel()
	input := []byte("  1.23%  function_1\n")
	_, ok := TryCompactPerf([]string{"perf"}, input)
	if ok {
		t.Fatalf("should return ok=false for small output")
	}
}

// --- Edge-case tests: exact threshold (cap+lastN lines → unchanged)
// and threshold+1 (cap+lastN+1 lines → compacted) ---
//
// These verify the boundary behavior required by AGENTS.md §3.5:
// at exactly the threshold the compactor must NOT fire (no data loss),
// and at threshold+1 it must fire (savings begin). Long lines are used
// to ensure the fail-open size guard does not mask the boundary.

func makeLongLines(n int, prefix string) []byte {
	var sb strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&sb, "%s_line_%04d_%s\n", prefix, i, strings.Repeat("x", 80))
	}
	return []byte(sb.String())
}

func TestTryCompactDocker_ExactThreshold(t *testing.T) {
	t.Parallel()
	// cap=50, lastN=3 → threshold=53
	input := makeLongLines(53, "docker")
	_, ok := TryCompactDocker([]string{"docker"}, input)
	if ok {
		t.Fatalf("TryCompactDocker should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactDocker_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	// cap=50, lastN=3 → threshold=53, threshold+1=54
	input := makeLongLines(54, "docker")
	compacted, ok := TryCompactDocker([]string{"docker"}, input)
	if !ok {
		t.Fatalf("TryCompactDocker should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted (%d) >= input (%d)", len(compacted), len(input))
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("compacted contains negative omission count: %s", s[:200])
	}
	// Verify no duplicated lines (first-N and last-N must not overlap)
	if strings.Count(s, "docker_line_0050") > 1 {
		t.Fatalf("line 50 duplicated (first-N/last-N overlap)")
	}
	if strings.Count(s, "docker_line_0051") > 1 {
		t.Fatalf("line 51 duplicated (first-N/last-N overlap)")
	}
}

func TestTryCompactKubectl_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "kubectl")
	_, ok := TryCompactKubectl([]string{"kubectl"}, input)
	if ok {
		t.Fatalf("TryCompactKubectl should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactKubectl_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "kubectl")
	compacted, ok := TryCompactKubectl([]string{"kubectl"}, input)
	if !ok {
		t.Fatalf("TryCompactKubectl should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "kubectl_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}

func TestTryCompactHelm_ExactThreshold(t *testing.T) {
	t.Parallel()
	// cap=30, lastN=3 → threshold=33
	input := makeLongLines(33, "helm")
	_, ok := TryCompactHelm([]string{"helm"}, input)
	if ok {
		t.Fatalf("TryCompactHelm should return ok=false at exact threshold (33 lines)")
	}
}

func TestTryCompactHelm_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(34, "helm")
	compacted, ok := TryCompactHelm([]string{"helm"}, input)
	if !ok {
		t.Fatalf("TryCompactHelm should return ok=true at threshold+1 (34 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "helm_line_0030") > 1 {
		t.Fatalf("line 30 duplicated")
	}
}

func TestTryCompactSystemctl_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "systemctl")
	_, ok := TryCompactSystemctl([]string{"systemctl"}, input)
	if ok {
		t.Fatalf("TryCompactSystemctl should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactSystemctl_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "systemctl")
	compacted, ok := TryCompactSystemctl([]string{"systemctl"}, input)
	if !ok {
		t.Fatalf("TryCompactSystemctl should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "systemctl_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}

func TestTryCompactJournalctl_ExactThreshold(t *testing.T) {
	t.Parallel()
	// Journalctl uses its own implementation (not capWithLastN), cap=50, no lastN
	// threshold is 50 lines (len(lines) <= 50 → unchanged via `if len(lines) < 30` + maxLines=50)
	// Actually journalctl: `if len(lines) < 30 { return false }` then caps at 50.
	// At exactly 50 lines: start=0, shows all 50, out >= stdout → fail-open → unchanged.
	input := makeLongLines(50, "journalctl")
	_, ok := TryCompactJournalctl([]string{"journalctl"}, input)
	if ok {
		t.Fatalf("TryCompactJournalctl should return ok=false at exact threshold (50 lines)")
	}
}

func TestTryCompactJournalctl_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(51, "journalctl")
	compacted, ok := TryCompactJournalctl([]string{"journalctl"}, input)
	if !ok {
		t.Fatalf("TryCompactJournalctl should return ok=true at threshold+1 (51 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
}

func TestTryCompactCargo_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "cargo")
	_, ok := TryCompactCargo([]string{"cargo"}, input)
	if ok {
		t.Fatalf("TryCompactCargo should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactCargo_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "cargo")
	compacted, ok := TryCompactCargo([]string{"cargo"}, input)
	if !ok {
		t.Fatalf("TryCompactCargo should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "cargo_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}

func TestTryCompactRustc_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "rustc")
	_, ok := TryCompactRustc([]string{"rustc"}, input)
	if ok {
		t.Fatalf("TryCompactRustc should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactRustc_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "rustc")
	compacted, ok := TryCompactRustc([]string{"rustc"}, input)
	if !ok {
		t.Fatalf("TryCompactRustc should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "rustc_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}

func TestTryCompactTcpdump_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "tcpdump")
	_, ok := TryCompactTcpdump([]string{"tcpdump"}, input)
	if ok {
		t.Fatalf("TryCompactTcpdump should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactTcpdump_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "tcpdump")
	compacted, ok := TryCompactTcpdump([]string{"tcpdump"}, input)
	if !ok {
		t.Fatalf("TryCompactTcpdump should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "tcpdump_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}

func TestTryCompactPerf_ExactThreshold(t *testing.T) {
	t.Parallel()
	input := makeLongLines(53, "perf")
	_, ok := TryCompactPerf([]string{"perf"}, input)
	if ok {
		t.Fatalf("TryCompactPerf should return ok=false at exact threshold (53 lines)")
	}
}

func TestTryCompactPerf_ThresholdPlusOne(t *testing.T) {
	t.Parallel()
	input := makeLongLines(54, "perf")
	compacted, ok := TryCompactPerf([]string{"perf"}, input)
	if !ok {
		t.Fatalf("TryCompactPerf should return ok=true at threshold+1 (54 lines)")
	}
	if len(compacted) >= len(input) {
		t.Fatalf("compacted >= input")
	}
	s := string(compacted)
	if strings.Contains(s, "[-") {
		t.Fatalf("negative omission count")
	}
	if strings.Count(s, "perf_line_0050") > 1 {
		t.Fatalf("line 50 duplicated")
	}
}
