package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/slimference/slimference/internal/control/reversibility"
)

func newHostsFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "hosts")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHostsPatchApplyAddsBlock(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n::1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com", "api.openai.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(1000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	s := string(got)
	if !strings.Contains(s, hostsBeginMarker) || !strings.Contains(s, hostsEndMarker) {
		t.Errorf("fence markers missing: %q", s)
	}
	if !strings.Contains(s, "127.0.0.1\tchatgpt.com") {
		t.Errorf("chatgpt.com entry missing: %q", s)
	}
	if !strings.Contains(s, "127.0.0.1\tapi.openai.com") {
		t.Errorf("api.openai.com entry missing: %q", s)
	}
	if !strings.HasPrefix(s, pre) {
		t.Errorf("pre-existing lines mutated: %q", s)
	}
}

func TestHostsPatchApplyIdempotent(t *testing.T) {
	path := newHostsFile(t, "127.0.0.1\tlocalhost\n")
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(2000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if !strings.HasSuffix(string(second), "<<< slimference managed <<<\n") {
		t.Errorf("trailer missing: %q", second)
	}
	// Count occurrences of the begin marker; must be exactly one.
	if strings.Count(string(second), hostsBeginMarker) != 1 {
		t.Errorf("idempotency broken: multiple managed blocks")
	}
	// First and second must have only the timestamp differing (we use
	// a fixed clock, so they should be byte-equal).
	if string(first) != string(second) {
		t.Errorf("re-Apply mutated bytes with fixed clock")
	}
}

func TestHostsPatchReverseRestoresFromBackup(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n# original comment\n::1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(3000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != pre {
		t.Errorf("restore from backup not byte-equal:\nwant=%q\ngot =%q", pre, got)
	}
}

func TestHostsPatchReverseStripsBlockWhenBackupMissing(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(4000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Delete the backup so Reverse falls through to strip-marker mode.
	if err := os.Remove(step.backupPath()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if strings.Contains(string(got), hostsBeginMarker) {
		t.Errorf("marker still present: %q", got)
	}
	if !strings.Contains(string(got), "127.0.0.1\tlocalhost") {
		t.Errorf("pre-existing line dropped: %q", got)
	}
}

func TestHostsPatchReverseOnNotExistFileIsNoop(t *testing.T) {
	dir := t.TempDir()
	step := &HostsPatch{
		Path:    filepath.Join(dir, "does-not-exist"),
		Targets: []string{"x"},
		Address: "127.0.0.1",
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("non-existent file Reverse should no-op, got %v", err)
	}
}

func TestHostsPatchInspectStates(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(5000, 0).UTC() },
	}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("pre-apply: got %s want absent", s)
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePresent {
		t.Errorf("post-apply: got %s want present", s)
	}
	// Inject a manual edit inside the fence to drop one target → partial.
	current, _ := os.ReadFile(path)
	mutated := strings.Replace(string(current), "127.0.0.1\tchatgpt.com\n", "", 1)
	if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePartial {
		t.Errorf("after manual mutation: got %s want partial", s)
	}
}

func TestHostsPatchInspectMissingFile(t *testing.T) {
	step := &HostsPatch{
		Path:    filepath.Join(t.TempDir(), "missing"),
		Targets: []string{"x"}, Address: "127.0.0.1",
	}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("missing file: got %s want absent", s)
	}
}

func TestHostsPatchInspectReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	step := &HostsPatch{Path: path, Targets: []string{"x"}, Address: "127.0.0.1"}
	if s := step.Inspect(context.Background()); s != reversibility.StateUnknown {
		t.Errorf("unreadable: got %s want unknown", s)
	}
}

// Note: HostsPatch.path() defaults to /etc/hosts when Path is empty,
// so Inspect with an empty Path Inspect against the real system file.
// That makes a deterministic "empty path → unknown" test impossible
// without faking the filesystem - covered by validate-based tests below.

func TestHostsPatchValidate(t *testing.T) {
	cases := []*HostsPatch{
		{},
		{Path: "/tmp/x"},
		{Path: "/tmp/x", Targets: []string{"a"}},
	}
	for i, s := range cases {
		if err := s.Apply(context.Background()); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestHostsPatchPathDefault(t *testing.T) {
	step := &HostsPatch{Targets: []string{"x"}, Address: "127.0.0.1"}
	if step.path() != "/etc/hosts" {
		t.Errorf("default path: got %s", step.path())
	}
}

func TestHostsPatchBackupDirRespected(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	backupDir := t.TempDir()
	step := &HostsPatch{
		Path:      path,
		Targets:   []string{"chatgpt.com"},
		Address:   "127.0.0.1",
		BackupDir: backupDir,
		Clock:     func() time.Time { return time.Unix(6000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "hosts.slimference.bak")); err != nil {
		t.Errorf("backup not in BackupDir: %v", err)
	}
}

func TestHostsPatchBackupKeepsOldestOnSecondApply(t *testing.T) {
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path:    path,
		Targets: []string{"chatgpt.com"},
		Address: "127.0.0.1",
		Clock:   func() time.Time { return time.Unix(7000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(step.backupPath())
	// Pretend the hosts file got mutated between turns.
	if err := os.WriteFile(path, []byte("# mutated\n"+string(original)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	current, _ := os.ReadFile(step.backupPath())
	if string(current) != string(original) {
		t.Errorf("backup overwritten on second Apply; should preserve oldest pre-install state")
	}
}

func TestHostsPatchApplyReadError(t *testing.T) {
	// File doesn't exist → read fails.
	dir := t.TempDir()
	step := &HostsPatch{
		Path:    filepath.Join(dir, "does-not-exist"),
		Targets: []string{"x"}, Address: "127.0.0.1",
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected read error")
	}
}

func TestHostsPatchApplyContextCancelled(t *testing.T) {
	path := newHostsFile(t, "")
	step := &HostsPatch{Path: path, Targets: []string{"x"}, Address: "127.0.0.1"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Errorf("expected context error")
	}
}

func TestHostsPatchReverseValidateError(t *testing.T) {
	step := &HostsPatch{}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected validation err on Reverse")
	}
}

func TestHostsPatchReverseBackupReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path: path, Targets: []string{"chatgpt.com"}, Address: "127.0.0.1",
		Clock: func() time.Time { return time.Unix(8000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(step.backupPath(), 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(step.backupPath(), 0o644) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected backup-read error to surface")
	}
}

func TestStripManagedBlockSimple(t *testing.T) {
	in := `127.0.0.1 localhost
` + hostsBeginMarker + `
127.0.0.1 chatgpt.com
` + hostsEndMarker + `
::1 localhost
`
	want := "127.0.0.1 localhost\n::1 localhost\n"
	if got := stripManagedBlock(in); got != want {
		t.Errorf("strip:\ngot =%q\nwant=%q", got, want)
	}
}

func TestStripManagedBlockMultipleBlocks(t *testing.T) {
	// Should strip every Slimference block, leaving everything else.
	in := hostsBeginMarker + "\n1\n" + hostsEndMarker + "\nKEEP\n" +
		hostsBeginMarker + "\n2\n" + hostsEndMarker + "\n"
	want := "KEEP\n"
	if got := stripManagedBlock(in); got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestStripManagedBlockNoMarkers(t *testing.T) {
	in := "abc\ndef\n"
	if got := stripManagedBlock(in); got != in {
		t.Errorf("unchanged content should pass through: got %q", got)
	}
}

func TestWriteAtomicCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	if err := writeAtomic(path, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "hello" {
		t.Errorf("content: %q", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode: %o", info.Mode().Perm())
	}
}

func TestWriteAtomicMkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := writeAtomic(filepath.Join(dir, "sub", "f"), []byte("x"), 0o644); err == nil {
		t.Errorf("expected mkdir error")
	}
}

func TestHostsPatchApplyAppendsNewlineWhenMissing(t *testing.T) {
	// Pre-existing content has no trailing newline. Apply must add
	// one so the fenced block lands on its own line.
	pre := "127.0.0.1\tlocalhost" // no trailing newline
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path: path, Targets: []string{"chatgpt.com"}, Address: "127.0.0.1",
		Clock: func() time.Time { return time.Unix(11000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(got), pre+"\n") {
		t.Errorf("Apply did not append newline before block: %q", got)
	}
}

func TestHostsPatchWriteBackupMkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	step := &HostsPatch{
		Path: path, Targets: []string{"x"}, Address: "127.0.0.1",
		BackupDir: filepath.Join(parent, "no-write-here"),
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected backup mkdir error")
	}
}

func TestHostsPatchWriteBackupWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	backupDir := t.TempDir()
	if err := os.Chmod(backupDir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(backupDir, 0o700) })
	step := &HostsPatch{
		Path: path, Targets: []string{"x"}, Address: "127.0.0.1",
		BackupDir: backupDir,
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected backup write error")
	}
}

func TestHostsPatchApplyWriteAtomicError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Make the parent directory read-only so writeAtomic cannot
	// create the temp file inside it.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	step := &HostsPatch{
		Path: path, Targets: []string{"x"}, Address: "127.0.0.1",
		BackupDir: t.TempDir(),
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected writeAtomic error")
	}
}

func TestHostsPatchReverseFallbackWriteAtomicError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path: path, Targets: []string{"x"}, Address: "127.0.0.1",
		Clock: func() time.Time { return time.Unix(12000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(step.backupPath()); err != nil {
		t.Fatal(err)
	}
	// Make the file's directory read-only so the strip-marker write fails.
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected reverse writeAtomic error")
	}
}

func TestHostsPatchReverseRestoreWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	pre := "127.0.0.1\tlocalhost\n"
	path := newHostsFile(t, pre)
	step := &HostsPatch{
		Path: path, Targets: []string{"x"}, Address: "127.0.0.1",
		Clock: func() time.Time { return time.Unix(13000, 0).UTC() },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Backup exists but the hosts directory is locked → restore-write fails.
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected restore writeAtomic error")
	}
}

func TestHostsPatchReverseReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// No backup exists; make hosts file unreadable so the fallback
	// read errors.
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	step := &HostsPatch{Path: path, Targets: []string{"x"}, Address: "127.0.0.1"}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected reverse read error")
	}
}

func TestWriteAtomicCreateTempError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	// Directory exists but has no write permission → CreateTemp fails.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := writeAtomic(filepath.Join(dir, "out"), []byte("x"), 0o644); err == nil {
		t.Errorf("expected CreateTemp error")
	}
}

type fakeAtomicTempFile struct {
	name     string
	writeErr error
	chmodErr error
	closeErr error
}

func (f *fakeAtomicTempFile) Write([]byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return 1, nil
}

func (f *fakeAtomicTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *fakeAtomicTempFile) Close() error {
	return f.closeErr
}

func (f *fakeAtomicTempFile) Name() string {
	return f.name
}

func TestWriteAtomicInjectedFileErrors(t *testing.T) {
	prevCreate := createAtomicTempFileFn
	prevRemove := removeAtomicFileFn
	prevRename := renameAtomicFileFn
	t.Cleanup(func() {
		createAtomicTempFileFn = prevCreate
		removeAtomicFileFn = prevRemove
		renameAtomicFileFn = prevRename
	})
	var removed []string
	removeAtomicFileFn = func(path string) error {
		removed = append(removed, path)
		return nil
	}
	renameAtomicFileFn = func(string, string) error {
		t.Fatal("rename should not run after temp-file failure")
		return nil
	}
	for _, tc := range []struct {
		name string
		file *fakeAtomicTempFile
	}{
		{name: "write", file: &fakeAtomicTempFile{name: "write.tmp", writeErr: errors.New("write failed")}},
		{name: "chmod", file: &fakeAtomicTempFile{name: "chmod.tmp", chmodErr: errors.New("chmod failed")}},
		{name: "close", file: &fakeAtomicTempFile{name: "close.tmp", closeErr: errors.New("close failed")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removed = nil
			createAtomicTempFileFn = func(string, string) (atomicTempFile, error) {
				return tc.file, nil
			}
			if err := writeAtomic(filepath.Join(t.TempDir(), "hosts"), []byte("hello"), 0o600); err == nil {
				t.Fatal("expected injected error")
			}
			if len(removed) != 1 || removed[0] != tc.file.name {
				t.Fatalf("removed=%v want %q", removed, tc.file.name)
			}
		})
	}
}

func TestHostsPatchNameStable(t *testing.T) {
	step := &HostsPatch{}
	if !strings.Contains(step.Name(), "hosts") {
		t.Errorf("step name: %q", step.Name())
	}
}
