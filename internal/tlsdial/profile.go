// Package tlsdial centralises outbound TLS fingerprint selection.
package tlsdial

import (
	"fmt"
	"sort"
	"strings"

	utls "github.com/refraction-networking/utls"
)

// Profile identifies the concrete TLS client fingerprint used for one
// outbound upstream connection.
type Profile struct {
	Name          string
	ClientHelloID utls.ClientHelloID
	Stdlib        bool
}

var profiles = map[string]Profile{
	"go_stdlib": {
		Name:   "go_stdlib",
		Stdlib: true,
	},
	"chromium_stable": {
		Name:          "chromium_stable",
		ClientHelloID: utls.HelloChrome_133,
	},
	"chrome_133": {
		Name:          "chrome_133",
		ClientHelloID: utls.HelloChrome_133,
	},
	"chrome_131": {
		Name:          "chrome_131",
		ClientHelloID: utls.HelloChrome_131,
	},
	"chrome_120": {
		Name:          "chrome_120",
		ClientHelloID: utls.HelloChrome_120,
	},
	"chrome_120_pq": {
		Name:          "chrome_120_pq",
		ClientHelloID: utls.HelloChrome_120_PQ,
	},
	"ios_12_1": {
		Name:          "ios_12_1",
		ClientHelloID: utls.HelloIOS_12_1,
	},
	"safari_16_0": {
		Name:          "safari_16_0",
		ClientHelloID: utls.HelloSafari_16_0,
	},
}

var profileAliases = map[string]string{
	"":                "chromium_stable",
	"chrome":          "chromium_stable",
	"chromium":        "chromium_stable",
	"node":            "chromium_stable",
	"node_stable":     "chromium_stable",
	"python":          "chromium_stable",
	"python_requests": "chromium_stable",
}

// ResolveProfile returns the concrete profile for name. Aliases deliberately
// map common app-stack labels onto the closest available maintained uTLS
// profile so config can describe intent without hardcoding a stale JA3 string.
func ResolveProfile(name string) (Profile, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if alias, ok := profileAliases[key]; ok {
		key = alias
	}
	if p, ok := profiles[key]; ok {
		return p, nil
	}
	return Profile{}, fmt.Errorf("unknown tls profile %q", name)
}

// ProfileNames returns the stable list of accepted concrete profile names and
// aliases. It is used by diagnostics and tests.
func ProfileNames() []string {
	names := make([]string, 0, len(profiles)+len(profileAliases))
	for name := range profiles {
		names = append(names, name)
	}
	for name := range profileAliases {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
