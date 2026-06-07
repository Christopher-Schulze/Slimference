package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	preserveExplicitEnv := os.Getenv("SLIMFERENCE_TEST_HOME_AUTO") == "1"
	home := os.Getenv("HOME")
	cleanupHome := ""
	if !preserveExplicitEnv || home == "" {
		var err error
		home, err = os.MkdirTemp("", "slimference-proxy-test-home-")
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
