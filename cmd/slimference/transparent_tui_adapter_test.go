package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/slimference/slimference/internal/transparent"
)

func withTransparentAdapterStubs(t *testing.T, net *fakeNetworkManager, kc *fakeKeychain, la *fakeLaunchAgent) {
	t.Helper()
	origRun := proxyRunFn
	origNet := newTransparentNetworkFn
	origKeychain := newTransparentKeychainFn
	origLaunch := newTransparentLaunchFn
	origHealth := transparentProxyHealthFn
	origInstall := tuiInstallCmdFn
	origEnable := tuiEnableCmdFn
	origDisable := tuiDisableCmdFn
	origUninstall := tuiUninstallCmdFn
	t.Cleanup(func() {
		proxyRunFn = origRun
		newTransparentNetworkFn = origNet
		newTransparentKeychainFn = origKeychain
		newTransparentLaunchFn = origLaunch
		transparentProxyHealthFn = origHealth
		tuiInstallCmdFn = origInstall
		tuiEnableCmdFn = origEnable
		tuiDisableCmdFn = origDisable
		tuiUninstallCmdFn = origUninstall
	})
	newTransparentNetworkFn = func() proxyNetworkManager { return net }
	newTransparentKeychainFn = func() proxyKeychain { return kc }
	newTransparentLaunchFn = func() proxyLaunchAgent { return la }
	transparentProxyHealthFn = func(string, string) error { return nil }
}

func TestServiceControlAdapterTransparentCommands(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	withTransparentAdapterStubs(t, &fakeNetworkManager{}, &fakeKeychain{}, &fakeLaunchAgent{})

	var calls []string
	stub := func(name string) func([]string, installPrinter) int {
		return func(_ []string, p installPrinter) int {
			calls = append(calls, name)
			if p.Out == nil || p.Err == nil {
				t.Fatal("install lifecycle command missing printer writers")
			}
			return 0
		}
	}
	tuiInstallCmdFn = stub("install")
	tuiEnableCmdFn = stub("enable")
	tuiDisableCmdFn = stub("disable")
	tuiUninstallCmdFn = stub("uninstall")

	adapter := &serviceControlAdapter{}
	for _, run := range []struct {
		name string
		fn   func() error
	}{
		{"install", adapter.InstallTransparent},
		{"enable", adapter.EnableTransparent},
		{"disable", adapter.DisableTransparent},
		{"uninstall", adapter.UninstallTransparent},
	} {
		if err := run.fn(); err != nil {
			t.Fatalf("%s transparent command failed: %v", run.name, err)
		}
	}

	want := []string{"install", "enable", "disable", "uninstall"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("transparent command calls=%v want %v", calls, want)
	}
}

func TestProxyCommandEnvDefaultFactories(t *testing.T) {
	env := proxyCommandEnv(io.Discard, io.Discard, strings.NewReader(""))
	if env.Network == nil || env.Keychain == nil || env.Launch == nil || env.LoadCA == nil || env.HealthCheck == nil {
		t.Fatalf("default proxy command env missing dependencies: %+v", env)
	}
}

func TestServiceControlAdapterTransparentCommandErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withTransparentAdapterStubs(t, &fakeNetworkManager{}, &fakeKeychain{}, &fakeLaunchAgent{})
	adapter := &serviceControlAdapter{}

	tests := []struct {
		name string
		run  func(args []string, p installPrinter) int
		want string
	}{
		{
			name: "stderr",
			run: func(_ []string, p installPrinter) int {
				fmt.Fprint(p.Err, "stderr boom")
				return 1
			},
			want: "stderr boom",
		},
		{
			name: "stdout",
			run: func(_ []string, p installPrinter) int {
				fmt.Fprint(p.Out, "stdout boom")
				return 1
			},
			want: "stdout boom",
		},
		{
			name: "fallback",
			run: func(_ []string, _ installPrinter) int {
				return 7
			},
			want: "install lifecycle command failed with exit 7",
		},
	}

	for _, tc := range tests {
		tuiEnableCmdFn = tc.run
		err := adapter.EnableTransparent()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s error=%v want %q", tc.name, err, tc.want)
		}
	}
}

func TestServiceControlAdapterTransparentStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	certPath := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	net := &fakeNetworkManager{statusSnap: transparent.Snapshot{Services: []transparent.ServiceState{
		{Name: "off", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: false},
		{Name: "other", HTTPSProxy: "example.com", HTTPSPort: "443", HTTPSEnabled: true},
		{Name: "wifi", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: true},
	}}}
	kc := &fakeKeychain{trusted: true}
	la := &fakeLaunchAgent{installed: true}
	withTransparentAdapterStubs(t, net, kc, la)

	status := (&serviceControlAdapter{}).TransparentStatus()
	if !status.Installed() || !status.ProxyArmed || !status.DaemonReachable || status.ActiveServices != 1 {
		t.Fatalf("unexpected transparent status: %+v", status)
	}
}

func TestServiceControlAdapterTransparentStatusEdges(t *testing.T) {
	t.Setenv("HOME", "")
	withTransparentAdapterStubs(t, &fakeNetworkManager{}, &fakeKeychain{}, &fakeLaunchAgent{})
	status := (&serviceControlAdapter{}).TransparentStatus()
	if !strings.Contains(status.Detail, "HOME unresolved") {
		t.Fatalf("home edge status=%+v", status)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	certPath := filepath.Join(home, ".slimference", "ca", "root.crt")
	if err := os.MkdirAll(filepath.Dir(certPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	net := &fakeNetworkManager{statusSnap: transparent.Snapshot{UnreachableErr: errors.New("networksetup down")}}
	kc := &fakeKeychain{verifyErr: errors.New("untrusted")}
	la := &fakeLaunchAgent{}
	withTransparentAdapterStubs(t, net, kc, la)
	status = (&serviceControlAdapter{}).TransparentStatus()
	if !status.CAExists || status.CATrusted || !status.NetworkUnavailable || !strings.Contains(status.Detail, "untrusted") {
		t.Fatalf("unreachable/untrusted status=%+v", status)
	}

	net.statusSnap = transparent.Snapshot{Services: []transparent.ServiceState{
		{Name: "wifi", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: true},
	}}
	kc.verifyErr = nil
	transparentProxyHealthFn = func(string, string) error { return io.ErrUnexpectedEOF }
	status = (&serviceControlAdapter{}).TransparentStatus()
	if !status.ProxyArmed || status.DaemonReachable || !strings.Contains(status.Detail, "unexpected EOF") {
		t.Fatalf("health-failure status=%+v", status)
	}

	net.statusSnap = transparent.Snapshot{UnreachableErr: errors.New("networksetup down")}
	kc.verifyErr = nil
	status = (&serviceControlAdapter{}).TransparentStatus()
	if !status.NetworkUnavailable || !strings.Contains(status.Detail, "networksetup down") {
		t.Fatalf("network-detail status=%+v", status)
	}
}

func TestServiceControlAdapterTransparentStatusMultipleArmedServices(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	net := &fakeNetworkManager{statusSnap: transparent.Snapshot{Services: []transparent.ServiceState{
		{Name: "wifi", HTTPSProxy: "127.0.0.1", HTTPSPort: "8990", HTTPSEnabled: true},
		{Name: "ethernet", HTTPSProxy: "localhost", HTTPSPort: "8990", HTTPSEnabled: true},
	}}}
	withTransparentAdapterStubs(t, net, &fakeKeychain{}, &fakeLaunchAgent{})
	status := (&serviceControlAdapter{}).TransparentStatus()
	if !status.DaemonReachable || status.ActiveServices != 2 {
		t.Fatalf("multi-service status=%+v", status)
	}
}
