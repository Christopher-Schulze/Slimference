package tui

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	orig := tuiStatePathFn
	tuiStatePathFn = func() string { return "" }
	code := m.Run()
	tuiStatePathFn = orig
	os.Exit(code)
}
