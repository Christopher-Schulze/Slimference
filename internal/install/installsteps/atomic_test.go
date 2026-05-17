package installsteps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicWritesContentAndMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")
	if err := writeAtomic(path, []byte("hello"), 0o640); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello" {
		t.Fatalf("data=%q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
}

func TestWriteAtomicMissingParentErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "file.txt")
	if err := writeAtomic(path, []byte("hello"), 0o600); err == nil {
		t.Fatal("expected error for missing parent")
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
			if err := writeAtomic(filepath.Join(t.TempDir(), "file.txt"), []byte("hello"), 0o600); err == nil {
				t.Fatal("expected injected error")
			}
			if len(removed) != 1 || removed[0] != tc.file.name {
				t.Fatalf("removed=%v want %q", removed, tc.file.name)
			}
		})
	}
}
