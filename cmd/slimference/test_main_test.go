package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

func TestMain(m *testing.M) {
	raiseFDLimit()
	preserveExplicitEnv := os.Getenv("SLIMFERENCE_TEST_HOME_AUTO") == "1"
	home := os.Getenv("HOME")
	cleanupHome := ""
	if !preserveExplicitEnv || home == "" {
		var err error
		home, err = os.MkdirTemp("", "slimference-cmd-test-home-")
		if err != nil {
			panic(err)
		}
		cleanupHome = home
	}
	setTestHome(home, preserveExplicitEnv)
	_ = os.Setenv("SLIMFERENCE_TEST_HOME_AUTO", "1")
	code := m.Run()
	if cleanupHome != "" {
		_ = os.RemoveAll(cleanupHome)
	}
	os.Exit(code)
}

// raiseFDLimit raises the process file-descriptor limit so the test suite
// does not hit "too many open files" on platforms with a low default soft
// limit. The cmd/slimference test binary opens many sockets, pipes, and
// temp files across hundreds of tests; the Go runtime's default raise to
// 10240 is insufficient on large test suites.
func raiseFDLimit() {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return
	}
	var rlim syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlim); err != nil {
		return
	}
	const want = 65536
	if rlim.Cur >= want {
		return
	}
	rlim.Cur = want
	if rlim.Max > 0 && rlim.Cur > rlim.Max {
		rlim.Cur = rlim.Max
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rlim)
}

func setTestHome(home string, preserveExplicitEnv bool) {
	if !preserveExplicitEnv || os.Getenv("HOME") == "" {
		_ = os.Setenv("HOME", home)
	}
	setTestPathEnv("XDG_CONFIG_HOME", filepath.Join(home, ".config"), preserveExplicitEnv)
	setTestPathEnv("XDG_CACHE_HOME", filepath.Join(home, ".cache"), preserveExplicitEnv)
	setTestPathEnv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"), preserveExplicitEnv)
	setTestPathEnv("XDG_STATE_HOME", filepath.Join(home, ".local", "state"), preserveExplicitEnv)
}

func setTestPathEnv(key string, value string, preserveExplicitEnv bool) {
	if preserveExplicitEnv && os.Getenv(key) != "" {
		return
	}
	_ = os.Setenv(key, value)
}
