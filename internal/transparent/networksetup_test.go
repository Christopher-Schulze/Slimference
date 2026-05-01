package transparent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type mockExec struct {
	mu    sync.Mutex
	calls [][]string
	out   map[string][]byte
	errs  map[string]error
}

func newMockExec() *mockExec {
	return &mockExec{
		out:  map[string][]byte{},
		errs: map[string]error{},
	}
}

func (m *mockExec) run(_ context.Context, name string, args ...string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	full := append([]string{name}, args...)
	m.calls = append(m.calls, full)
	key := strings.Join(full, " ")
	if e, ok := m.errs[key]; ok {
		return m.out[key], e
	}
	return m.out[key], nil
}

func (m *mockExec) callsFor(prefix string) [][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out [][]string
	for _, c := range m.calls {
		if strings.Join(c, " ") == prefix || strings.HasPrefix(strings.Join(c, " "), prefix+" ") {
			out = append(out, c)
		}
	}
	return out
}

func TestNetworkSetup_EnableHTTPSCallsBoth(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi", "Ethernet"}, nil
	})
	got, err := m.EnableHTTPS("127.0.0.1", "8990")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 succeeded services, got %v", got)
	}
	if len(mock.calls) != 4 {
		t.Fatalf("expected 4 networksetup calls (2 services x 2 calls), got %d", len(mock.calls))
	}
	// Assert SOCKS is never touched.
	for _, c := range mock.calls {
		joined := strings.Join(c, " ")
		if strings.Contains(strings.ToLower(joined), "socks") {
			t.Fatalf("SOCKS proxy must not be touched, got call %v", c)
		}
	}
}

func TestNetworkSetup_DisableCallsBoth(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi"}, nil
	})
	got, err := m.Disable()
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 cleared, got %v", got)
	}
}

func TestNetworkSetup_PartialFailureSurfaced(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -setsecurewebproxy Ethernet 127.0.0.1 8990"] = errors.New("nope")
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi", "Ethernet"}, nil
	})
	succ, err := m.EnableHTTPS("127.0.0.1", "8990")
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	if len(succ) != 1 || succ[0] != "Wi-Fi" {
		t.Fatalf("expected Wi-Fi in succeeded, got %v", succ)
	}
}

func TestNetworkSetup_EnableSecondaryWebProxyFailure(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -setwebproxy Wi-Fi 127.0.0.1 8990"] = errors.New("nope2")
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi"}, nil
	})
	if _, err := m.EnableHTTPS("127.0.0.1", "8990"); err == nil {
		t.Fatal("expected secondary setwebproxy error to surface")
	}
}

func TestNetworkSetup_DisableSecondaryWebProxyFailure(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -setwebproxystate Wi-Fi off"] = errors.New("nope2")
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi"}, nil
	})
	if _, err := m.Disable(); err == nil {
		t.Fatal("expected secondary setwebproxystate error to surface")
	}
}

func TestNetworkSetup_DisablePartialFailure(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -setsecurewebproxystate Ethernet off"] = errors.New("locked")
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi", "Ethernet"}, nil
	})
	cleared, err := m.Disable()
	if err == nil {
		t.Fatal("expected aggregate error")
	}
	if len(cleared) != 1 {
		t.Fatalf("expected single cleared service, got %v", cleared)
	}
}

func TestNetworkSetup_EnableServicesFailure(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return nil, errors.New("listallnetworkservices failed")
	})
	if _, err := m.EnableHTTPS("h", "p"); err == nil {
		t.Fatal("expected error from list-services")
	}
	if _, err := m.Disable(); err == nil {
		t.Fatal("expected error from list-services on disable")
	}
}

func TestNetworkSetup_StatusReports(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.out["networksetup -getsecurewebproxy Wi-Fi"] = []byte(
		"Enabled: Yes\nServer: 127.0.0.1\nPort: 8990\nAuthenticated Proxy Enabled: 0\n",
	)
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi"}, nil
	})
	snap := m.Status()
	if snap.UnreachableErr != nil {
		t.Fatalf("status: %v", snap.UnreachableErr)
	}
	if len(snap.Services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(snap.Services))
	}
	s := snap.Services[0]
	if s.Name != "Wi-Fi" || s.HTTPSProxy != "127.0.0.1" || s.HTTPSPort != "8990" || !s.HTTPSEnabled {
		t.Fatalf("unexpected snapshot: %+v", s)
	}
}

func TestNetworkSetup_StatusListFailureSurfaced(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return nil, errors.New("listservices fail")
	})
	snap := m.Status()
	if snap.UnreachableErr == nil {
		t.Fatal("expected UnreachableErr to surface")
	}
}

func TestNetworkSetup_StatusServiceCommandFailureLeavesEmpty(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -getsecurewebproxy Wi-Fi"] = errors.New("locked")
	m := NewManager()
	m.SetExec(mock.run)
	m.SetServiceLister(func(ctx context.Context) ([]string, error) {
		return []string{"Wi-Fi"}, nil
	})
	snap := m.Status()
	if len(snap.Services) != 1 {
		t.Fatalf("expected 1 service even on failure, got %d", len(snap.Services))
	}
	s := snap.Services[0]
	if s.HTTPSEnabled || s.HTTPSPort != "" {
		t.Fatalf("expected empty state on exec failure, got %+v", s)
	}
}

func TestNetworkSetup_DefaultListServicesParsesOutput(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.out["networksetup -listallnetworkservices"] = []byte(
		"An asterisk (*) denotes that a network service is disabled.\n" +
			"Wi-Fi\n" +
			"Ethernet\n" +
			"*Bluetooth PAN\n",
	)
	m := NewManager()
	m.SetExec(mock.run)
	got, err := m.defaultListServices(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 || got[0] != "Wi-Fi" || got[1] != "Ethernet" {
		t.Fatalf("unexpected services: %v", got)
	}
}

func TestNetworkSetup_DefaultListServicesError(t *testing.T) {
	t.Parallel()
	mock := newMockExec()
	mock.errs["networksetup -listallnetworkservices"] = errors.New("nope")
	m := NewManager()
	m.SetExec(mock.run)
	if _, err := m.defaultListServices(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseGetWebProxy_HandlesAllFields(t *testing.T) {
	t.Parallel()
	enabled, server, port := parseGetWebProxy(
		"Enabled: Yes\nServer: 1.2.3.4\nPort: 9090\nAuthenticated Proxy Enabled: 0\n",
	)
	if !enabled || server != "1.2.3.4" || port != "9090" {
		t.Fatalf("got enabled=%v server=%q port=%q", enabled, server, port)
	}
}

func TestParseGetWebProxy_EnabledNoMatchesFalse(t *testing.T) {
	t.Parallel()
	enabled, _, _ := parseGetWebProxy("Enabled: No\nServer: x\nPort: 1\n")
	if enabled {
		t.Fatal("expected enabled=false")
	}
}

func TestParseServiceList_BlanksAndDisabled(t *testing.T) {
	t.Parallel()
	got := parseServiceList("\n\nFoo\n*Disabled\nBar\n")
	if len(got) != 2 || got[0] != "Foo" || got[1] != "Bar" {
		t.Fatalf("got %v", got)
	}
}

func TestErrNoServices_Sentinel(t *testing.T) {
	t.Parallel()
	if ErrNoServices == nil || !strings.Contains(ErrNoServices.Error(), "no active") {
		t.Fatal("ErrNoServices must be a non-nil sentinel with explanatory text")
	}
}

func TestSetExec_NilNoOp(t *testing.T) {
	t.Parallel()
	m := NewManager()
	original := m.exec
	m.SetExec(nil)
	// Cannot compare funcs directly; just verify no panic and no
	// nilification.
	_ = original
	if m.exec == nil {
		t.Fatal("nil arg must NOT clear the exec hook")
	}
}

func TestSetServiceLister_NilNoOp(t *testing.T) {
	t.Parallel()
	m := NewManager()
	m.SetServiceLister(nil)
	if m.listFn == nil {
		t.Fatal("nil arg must NOT clear the listFn hook")
	}
}

func TestDefaultExec_ProxiesToRunCommand(t *testing.T) {
	t.Parallel()
	m := NewManager()
	// Run a benign command that exists on macOS / Linux.
	out, err := m.defaultExec(context.Background(), "true")
	if err != nil {
		t.Fatalf("true should succeed: %v", err)
	}
	_ = out
}
