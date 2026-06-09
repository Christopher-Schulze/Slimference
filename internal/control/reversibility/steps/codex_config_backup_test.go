package steps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Christopher-Schulze/Slimference/internal/control/reversibility"
)

// appendMarker is a trivial idempotent patch used in tests: it
// appends `MARKER\n` once and never duplicates.
const testMarker = "SLIMFERENCE-TEST-MARKER"

func appendMarkerPatch(in []byte) []byte {
	if strings.Contains(string(in), testMarker) {
		return in
	}
	return append(append([]byte{}, in...), []byte("\n"+testMarker+"\n")...)
}

func TestFileBackupApplyAddsMarker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("foo = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{
		Path:          path,
		BackupDir:     filepath.Join(t.TempDir(), "backup"),
		Patch:         appendMarkerPatch,
		InspectMarker: testMarker,
		Clock:         func() time.Time { return time.Unix(1, 0) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), testMarker) {
		t.Errorf("marker missing: %q", got)
	}
	if step.Inspect(context.Background()) != reversibility.StatePresent {
		t.Errorf("Inspect should be Present after Apply")
	}
}

func TestFileBackupApplyIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("foo = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{
		Path: path, BackupDir: filepath.Join(t.TempDir(), "backup"),
		Patch: appendMarkerPatch, InspectMarker: testMarker,
		Clock: func() time.Time { return time.Unix(2, 0) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(path)
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Errorf("second Apply mutated content")
	}
}

func TestFileBackupReverseRestoresOriginal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	pre := "original content\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{
		Path: path, BackupDir: filepath.Join(t.TempDir(), "backup"),
		Patch: appendMarkerPatch, InspectMarker: testMarker,
		Clock: func() time.Time { return time.Unix(3, 0) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != pre {
		t.Errorf("not byte-equal restore:\nwant=%q\ngot=%q", pre, got)
	}
}

func TestFileBackupReverseNoBackupNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{
		Path: path, BackupDir: filepath.Join(t.TempDir(), "no-backup"),
		Patch: appendMarkerPatch,
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Errorf("Reverse without backup should no-op, got %v", err)
	}
}

func TestFileBackupApplyMissingFileNotRequired(t *testing.T) {
	step := &FileBackup{
		Path:      filepath.Join(t.TempDir(), "absent"),
		BackupDir: t.TempDir(),
		Patch:     appendMarkerPatch,
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Errorf("not-required + missing should no-op, got %v", err)
	}
}

func TestFileBackupApplyMissingFileRequired(t *testing.T) {
	step := &FileBackup{
		Path:      filepath.Join(t.TempDir(), "absent"),
		BackupDir: t.TempDir(),
		Patch:     appendMarkerPatch,
		Required:  true,
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("required + missing should error")
	}
}

func TestFileBackupApplyValidate(t *testing.T) {
	cases := []*FileBackup{
		{},
		{Path: "/tmp/x"},
		{Patch: appendMarkerPatch},
	}
	for i, s := range cases {
		if err := s.Apply(context.Background()); err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
}

func TestFileBackupApplyContextCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{Path: path, BackupDir: t.TempDir(), Patch: appendMarkerPatch}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := step.Apply(ctx); err == nil {
		t.Errorf("expected ctx error")
	}
}

func TestFileBackupApplyReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	step := &FileBackup{Path: path, BackupDir: t.TempDir(), Patch: appendMarkerPatch}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected read error")
	}
}

func TestFileBackupApplyPatchNoOp(t *testing.T) {
	// Patch that returns input unchanged should skip the write.
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	mtimeBefore, _ := os.Stat(path)
	step := &FileBackup{
		Path: path, BackupDir: t.TempDir(),
		Patch: func(in []byte) []byte { return in },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	mtimeAfter, _ := os.Stat(path)
	if mtimeAfter.ModTime() != mtimeBefore.ModTime() {
		t.Errorf("no-op patch should not rewrite the file (mtimes differ)")
	}
}

func TestFileBackupApplyEmptyPatchTreatedAsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{
		Path: path, BackupDir: t.TempDir(),
		Patch: func(in []byte) []byte { return nil },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original" {
		t.Errorf("empty-return patch should leave file alone, got %q", got)
	}
}

func TestFileBackupSnapshotPreservesOldest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	pre := "pre-state\n"
	if err := os.WriteFile(path, []byte(pre), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backup")
	step := &FileBackup{
		Path: path, BackupDir: backupDir,
		Patch: appendMarkerPatch, InspectMarker: testMarker,
		Clock: func() time.Time { return time.Unix(0, 100) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Modify the file externally; second Apply must NOT overwrite
	// the backup we already saved.
	if err := os.WriteFile(path, []byte("post-mutation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	step.Clock = func() time.Time { return time.Unix(0, 200) }
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := step.Reverse(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != pre {
		t.Errorf("oldest backup not preserved: want=%q got=%q", pre, got)
	}
}

func TestFileBackupSnapshotMkdirError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
	step := &FileBackup{
		Path:      path,
		BackupDir: filepath.Join(parent, "no-write"),
		Patch:     appendMarkerPatch,
	}
	if err := step.Apply(context.Background()); err == nil {
		t.Errorf("expected mkdir error")
	}
}

func TestFileBackupReverseReadBackupError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backup")
	step := &FileBackup{
		Path: path, BackupDir: backupDir,
		Patch: appendMarkerPatch,
		Clock: func() time.Time { return time.Unix(0, 1) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	backup, _ := step.oldestBackup()
	if err := os.Chmod(backup, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(backup, 0o644) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected backup-read error")
	}
}

func TestFileBackupReverseRestoreWriteError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backup")
	step := &FileBackup{
		Path: path, BackupDir: backupDir,
		Patch: appendMarkerPatch,
		Clock: func() time.Time { return time.Unix(0, 2) },
	}
	if err := step.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected restore write error")
	}
}

func TestFileBackupReverseValidate(t *testing.T) {
	step := &FileBackup{}
	if err := step.Reverse(context.Background()); err == nil {
		t.Errorf("expected validate err")
	}
}

func TestFileBackupInspectStates(t *testing.T) {
	step := &FileBackup{
		Path:          filepath.Join(t.TempDir(), "absent"),
		Patch:         appendMarkerPatch,
		InspectMarker: testMarker,
	}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("absent file: got %s", s)
	}
	// Create file without marker → absent
	step.Path = filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(step.Path, []byte("no marker"), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StateAbsent {
		t.Errorf("file without marker: got %s", s)
	}
	// Add marker → present
	if err := os.WriteFile(step.Path, []byte(testMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := step.Inspect(context.Background()); s != reversibility.StatePresent {
		t.Errorf("file with marker: got %s", s)
	}
}

func TestFileBackupInspectWithoutMarkerPresent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("any"), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{Path: path, Patch: appendMarkerPatch} // no InspectMarker
	if s := step.Inspect(context.Background()); s != reversibility.StatePresent {
		t.Errorf("no marker configured + file exists → present, got %s", s)
	}
}

func TestFileBackupInspectEmptyPath(t *testing.T) {
	step := &FileBackup{}
	if s := step.Inspect(context.Background()); s != reversibility.StateUnknown {
		t.Errorf("got %s", s)
	}
}

func TestFileBackupInspectReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "x")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	step := &FileBackup{Path: path, Patch: appendMarkerPatch, InspectMarker: testMarker}
	if s := step.Inspect(context.Background()); s != reversibility.StateUnknown {
		t.Errorf("locked file: got %s want unknown", s)
	}
}

func TestFileBackupNameOverride(t *testing.T) {
	step := &FileBackup{StepName: "custom"}
	if step.Name() != "custom" {
		t.Errorf("got %s", step.Name())
	}
}

func TestFileBackupNameDefault(t *testing.T) {
	step := &FileBackup{Path: "/usr/local/etc/myfile.toml"}
	if !strings.Contains(step.Name(), "myfile.toml") {
		t.Errorf("got %s", step.Name())
	}
}

func TestFindBackupsMissingDir(t *testing.T) {
	step := &FileBackup{Path: "/x"}
	got, err := step.findBackups(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Errorf("missing dir should be nil err, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

func TestFindBackupsSortAscending(t *testing.T) {
	dir := t.TempDir()
	names := []string{
		"config.toml.slimference.300.bak",
		"config.toml.slimference.100.bak",
		"config.toml.slimference.200.bak",
		"unrelated.txt",
	}
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	step := &FileBackup{Path: "/x/config.toml"}
	got, _ := step.findBackups(dir)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	// Filename sort puts "1" < "2" < "3" alphabetically.
	if !strings.Contains(got[0], "100") {
		t.Errorf("expected oldest first; got %v", got)
	}
}

func TestFindBackupsReadError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypass")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Skip("chmod unsupported")
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	step := &FileBackup{Path: "/x"}
	if _, err := step.findBackups(dir); err == nil {
		t.Errorf("expected read err")
	}
}

func TestFindBackupsSkipsDirectories(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "config.toml.slimference.100.bak"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml.slimference.200.bak"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	step := &FileBackup{Path: "/x/config.toml"}
	got, _ := step.findBackups(dir)
	if len(got) != 1 || !strings.Contains(got[0], "200") {
		t.Errorf("dir should be skipped, got %v", got)
	}
}

func TestFileBackupDefaultBackupDirAndSnapshotErrors(t *testing.T) {
	step := &FileBackup{Path: filepath.Join(t.TempDir(), "config.toml"), Patch: appendMarkerPatch}
	dir, err := step.backupDir()
	if err != nil {
		t.Fatalf("backupDir: %v", err)
	}
	if !strings.HasSuffix(dir, filepath.Join(".slimference", "backups")) {
		t.Fatalf("default backup dir=%q", dir)
	}

	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	step.BackupDir = filepath.Join(blocker, "child")
	if err := step.maybeSnapshot([]byte("original")); err == nil {
		t.Fatal("expected mkdir error below file blocker")
	}

	step.BackupDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(step.BackupDir, filepath.Base(step.Path)+".slimference.1.bak"), []byte("old"), 0o600); err != nil {
		t.Fatalf("write existing backup: %v", err)
	}
	if err := step.maybeSnapshot([]byte("new")); err != nil {
		t.Fatalf("existing backup should make snapshot a no-op: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(step.BackupDir, filepath.Base(step.Path)+".slimference.1.bak"))
	if err != nil || string(got) != "old" {
		t.Fatalf("existing backup changed: %q err=%v", got, err)
	}

	if os.Getuid() != 0 {
		step.BackupDir = t.TempDir()
		if err := os.Chmod(step.BackupDir, 0o000); err != nil {
			t.Skip("chmod unsupported")
		}
		t.Cleanup(func() { _ = os.Chmod(step.BackupDir, 0o700) })
		if err := step.maybeSnapshot([]byte("new")); err == nil {
			t.Fatal("expected read-dir error from unreadable backup dir")
		}
	}
}

func TestFileBackupDefaultBackupDirHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	step := &FileBackup{Path: filepath.Join(t.TempDir(), "config.toml"), Patch: appendMarkerPatch}
	if _, err := step.backupDir(); err == nil {
		t.Fatal("expected home-dir error with empty HOME")
	}
	if err := step.maybeSnapshot([]byte("original")); err == nil {
		t.Fatal("maybeSnapshot should surface backupDir error")
	}
	if _, err := step.oldestBackup(); err == nil {
		t.Fatal("oldestBackup should surface backupDir error")
	}
}

func TestFileBackupInjectedWriteErrors(t *testing.T) {
	prevCreate := createAtomicTempFileFn
	prevRemove := removeAtomicFileFn
	prevRename := renameAtomicFileFn
	t.Cleanup(func() {
		createAtomicTempFileFn = prevCreate
		removeAtomicFileFn = prevRemove
		renameAtomicFileFn = prevRename
	})
	removeAtomicFileFn = func(string) error { return nil }
	renameAtomicFileFn = func(string, string) error {
		t.Fatal("rename should not run after injected write failure")
		return nil
	}
	createAtomicTempFileFn = func(string, string) (atomicTempFile, error) {
		return &fakeAtomicTempFile{name: "broken.tmp", writeErr: errors.New("write failed")}, nil
	}

	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("foo = 1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	step := &FileBackup{Path: path, BackupDir: t.TempDir(), Patch: appendMarkerPatch}
	if err := step.Apply(context.Background()); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("apply err=%v want injected write failure", err)
	}

	backupDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(backupDir, "config.toml.slimference.1.bak"), []byte("original"), 0o600); err != nil {
		t.Fatalf("write backup: %v", err)
	}
	step.BackupDir = backupDir
	if err := step.Reverse(context.Background()); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("reverse err=%v want injected write failure", err)
	}
}

func TestFileBackupOldestBackupErrorFromFileBackupDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "backup-file")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	step := &FileBackup{Path: filepath.Join(t.TempDir(), "config.toml"), BackupDir: blocker, Patch: appendMarkerPatch}
	if _, err := step.oldestBackup(); err == nil {
		t.Fatal("expected ReadDir error when backup dir is a file")
	}
	if err := step.Reverse(context.Background()); err == nil {
		t.Fatal("Reverse should surface oldestBackup error")
	}
}
