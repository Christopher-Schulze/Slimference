package installsteps

import (
	"os"
	"path/filepath"
)

// writeAtomic writes data to path via a temp file + rename so a
// partial write is impossible. Shared by Step implementations in this
// package. Mirrored from the reversibility/steps package which has
// the same helper for /etc/hosts.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := createAtomicTempFileFn(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = removeAtomicFileFn(tmp.Name())
		return err
	}
	return renameAtomicFileFn(tmp.Name(), path)
}

type atomicTempFile interface {
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
	Name() string
}

var (
	createAtomicTempFileFn = func(dir, pattern string) (atomicTempFile, error) {
		return os.CreateTemp(dir, pattern)
	}
	removeAtomicFileFn = os.Remove
	renameAtomicFileFn = os.Rename
)
