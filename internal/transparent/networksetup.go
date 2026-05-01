// Package transparent contains the macOS-specific glue that makes
// Slimference's transparent-mode work end-to-end: System-HTTPS-Proxy
// flipping (`networksetup`), CA-cert installation in the keychain
// (`security`), and an optional launchd plist for daemon auto-start.
//
// The package is deliberately macOS-only. Linux and Windows
// equivalents live behind future build tags.
package transparent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Manager wraps `networksetup` invocations. SOCKS proxy is
// intentionally NOT touched: WebRTC's UDP path bypasses
// `setsecurewebproxy` / `setwebproxy` by design, and a SOCKS hook
// would route audio traffic through Slimference too. The audio bypass
// guarantee is a property of NOT touching SOCKS.
type Manager struct {
	exec    func(ctx context.Context, name string, args ...string) ([]byte, error)
	listFn  func(ctx context.Context) ([]string, error)
	timeout time.Duration
}

// NewManager returns a manager wired to the production `networksetup`
// binary with a 5-second per-command timeout.
func NewManager() *Manager {
	m := &Manager{timeout: 5 * time.Second}
	m.exec = m.defaultExec
	m.listFn = m.defaultListServices
	return m
}

// SetExec overrides the command runner; tests pin this to a stub so
// no real networksetup invocations happen during go test.
func (m *Manager) SetExec(fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	if fn != nil {
		m.exec = fn
	}
}

// SetServiceLister overrides the service-enumeration call so tests
// can provide a deterministic service list.
func (m *Manager) SetServiceLister(fn func(ctx context.Context) ([]string, error)) {
	if fn != nil {
		m.listFn = fn
	}
}

// Snapshot is the per-call observation `slimference proxy status`
// prints so the operator sees exactly which network services are
// currently routed through Slimference.
type Snapshot struct {
	Services       []ServiceState
	UnreachableErr error
}

// ServiceState holds one service's HTTPS-proxy configuration.
type ServiceState struct {
	Name         string
	HTTPSProxy   string
	HTTPSPort    string
	HTTPSEnabled bool
}

// EnableHTTPS sets `host:port` as the HTTPS (and HTTP, for plain
// upgrade-style traffic) proxy on every active network service.
// Returns the first failure it encountered alongside a list of
// services that did flip successfully so the operator can fix the
// failing service and retry.
func (m *Manager) EnableHTTPS(host, port string) (succeeded []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	services, err := m.listFn(ctx)
	if err != nil {
		return nil, fmt.Errorf("transparent: list services: %w", err)
	}
	var firstErr error
	for _, svc := range services {
		if e := m.enableOnService(ctx, svc, host, port); e != nil {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		succeeded = append(succeeded, svc)
	}
	if firstErr != nil {
		return succeeded, firstErr
	}
	return succeeded, nil
}

// Disable clears the HTTPS / HTTP proxy on every active service. The
// per-service state machine: setsecurewebproxystate <svc> off, same
// for setwebproxystate. Operator usage: `slimference proxy disable`.
func (m *Manager) Disable() (cleared []string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	services, err := m.listFn(ctx)
	if err != nil {
		return nil, fmt.Errorf("transparent: list services: %w", err)
	}
	var firstErr error
	for _, svc := range services {
		if e := m.disableOnService(ctx, svc); e != nil {
			if firstErr == nil {
				firstErr = e
			}
			continue
		}
		cleared = append(cleared, svc)
	}
	if firstErr != nil {
		return cleared, firstErr
	}
	return cleared, nil
}

// Status enumerates per-service proxy state.
func (m *Manager) Status() Snapshot {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	services, err := m.listFn(ctx)
	if err != nil {
		return Snapshot{UnreachableErr: err}
	}
	snap := Snapshot{Services: make([]ServiceState, 0, len(services))}
	for _, svc := range services {
		state := m.statusForService(ctx, svc)
		state.Name = svc
		snap.Services = append(snap.Services, state)
	}
	return snap
}

func (m *Manager) enableOnService(ctx context.Context, svc, host, port string) error {
	if _, err := m.exec(ctx, "networksetup", "-setsecurewebproxy", svc, host, port); err != nil {
		return fmt.Errorf("setsecurewebproxy %s: %w", svc, err)
	}
	if _, err := m.exec(ctx, "networksetup", "-setwebproxy", svc, host, port); err != nil {
		return fmt.Errorf("setwebproxy %s: %w", svc, err)
	}
	return nil
}

func (m *Manager) disableOnService(ctx context.Context, svc string) error {
	if _, err := m.exec(ctx, "networksetup", "-setsecurewebproxystate", svc, "off"); err != nil {
		return fmt.Errorf("setsecurewebproxystate %s: %w", svc, err)
	}
	if _, err := m.exec(ctx, "networksetup", "-setwebproxystate", svc, "off"); err != nil {
		return fmt.Errorf("setwebproxystate %s: %w", svc, err)
	}
	return nil
}

func (m *Manager) statusForService(ctx context.Context, svc string) ServiceState {
	state := ServiceState{Name: svc}
	out, err := m.exec(ctx, "networksetup", "-getsecurewebproxy", svc)
	if err != nil {
		return state
	}
	enabled, server, port := parseGetWebProxy(string(out))
	state.HTTPSEnabled = enabled
	state.HTTPSProxy = server
	state.HTTPSPort = port
	return state
}

// defaultListServices runs `networksetup -listallnetworkservices` and
// returns each non-disabled service name. macOS prefixes disabled
// services with an asterisk and emits a header line we strip.
func (m *Manager) defaultListServices(ctx context.Context) ([]string, error) {
	out, err := m.exec(ctx, "networksetup", "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	return parseServiceList(string(out)), nil
}

func parseServiceList(out string) []string {
	var services []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(strings.ToLower(line), "asterisk") {
			// header line: "An asterisk (*) denotes that a network service is disabled."
			continue
		}
		if strings.HasPrefix(line, "*") {
			// disabled
			continue
		}
		services = append(services, line)
	}
	return services
}

// parseGetWebProxy reads a `networksetup -getsecurewebproxy <svc>`
// payload and extracts the Enabled / Server / Port fields. Format:
//
//	Enabled: Yes
//	Server: 127.0.0.1
//	Port: 8990
//	Authenticated Proxy Enabled: 0
func parseGetWebProxy(out string) (enabled bool, server, port string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Enabled:"):
			val := strings.TrimSpace(strings.TrimPrefix(line, "Enabled:"))
			enabled = strings.EqualFold(val, "Yes")
		case strings.HasPrefix(line, "Server:"):
			server = strings.TrimSpace(strings.TrimPrefix(line, "Server:"))
		case strings.HasPrefix(line, "Port:"):
			port = strings.TrimSpace(strings.TrimPrefix(line, "Port:"))
		}
	}
	return enabled, server, port
}

// ErrNoServices is the sentinel returned when networksetup yielded no
// active services. Surfaced so a caller can distinguish "no services
// flipped because there are none" from "every service failed".
var ErrNoServices = errors.New("transparent: no active network services found")

// defaultExec runs the command via os/exec. Indirected through the
// exec hook so tests do NOT spawn real networksetup.
func (m *Manager) defaultExec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return runCommand(ctx, name, args...)
}
