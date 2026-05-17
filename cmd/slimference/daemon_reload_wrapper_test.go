package main

import (
	"context"
	"testing"
)

func TestRunDaemonWithSlimferenceReloadWiresReloadCallback(t *testing.T) {
	orig := daemonRunWithReloadFn
	t.Cleanup(func() { daemonRunWithReloadFn = orig })

	called := false
	reloaded := false
	daemonRunWithReloadFn = func(
		start func() (int, func(context.Context) error, error),
		reload func(),
	) error {
		called = true
		port, shutdown, err := start()
		if err != nil {
			t.Fatalf("start error: %v", err)
		}
		if port != 8990 {
			t.Fatalf("port=%d, want 8990", port)
		}
		if shutdown == nil {
			t.Fatal("shutdown must be non-nil")
		}
		if reload == nil {
			t.Fatal("reload callback must be non-nil")
		}
		reload()
		reloaded = true
		return shutdown(context.Background())
	}

	err := runDaemonWithSlimferenceReload(func() (int, func(context.Context) error, error) {
		return 8990, func(context.Context) error { return nil }, nil
	})
	if err != nil {
		t.Fatalf("runDaemonWithSlimferenceReload: %v", err)
	}
	if !called || !reloaded {
		t.Fatalf("called=%v reloaded=%v", called, reloaded)
	}
}
