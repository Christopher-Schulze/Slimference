package slogutil

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestRotatingWriter_openOrCreate_StatError(t *testing.T) {
	orig := statFileFn
	statFileFn = func(*os.File) (os.FileInfo, error) {
		return nil, errors.New("stat boom")
	}
	defer func() {
		statFileFn = orig
	}()

	rw := &RotatingWriter{path: t.TempDir() + "/app.log"}
	err := rw.openOrCreate()
	if err == nil || !strings.Contains(err.Error(), "stat") {
		t.Fatalf("expected stat error, got %v", err)
	}
}
