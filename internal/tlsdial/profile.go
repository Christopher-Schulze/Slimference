// Package tlsdial centralises outbound TLS fingerprint selection.
package tlsdial

import (
	"fmt"
	"sort"
	"strings"
	"time"

	utls "github.com/refraction-networking/utls"
)

const (
	// ProfileCatalogVersion marks the pinned uTLS profile catalogue shipped by
	// this Slimference build. It is operator-facing metadata, not a JA3 claim.
	ProfileCatalogVersion = "utls-chrome-133-2026-05-02"
	// ProfileCatalogGenerated is the UTC date the profile catalogue was last
	// reviewed against the pinned uTLS dependency.
	ProfileCatalogGenerated = "2026-05-02"
	// ProfileCatalogMaxAgeDays is the doctor warning threshold. Browser TLS
	// profiles drift; stale metadata should prompt a review, not fail startup.
	ProfileCatalogMaxAgeDays = 180
)

// Profile identifies the concrete TLS client fingerprint used for one
// outbound upstream connection.
type Profile struct {
	Name          string
	ClientHelloID utls.ClientHelloID
	Stdlib        bool
}

// CatalogInfo describes the shipped profile catalogue.
type CatalogInfo struct {
	Version    string
	Generated  time.Time
	MaxAgeDays int
	Concrete   []string
	Aliases    []string
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

// Catalog returns operator-facing metadata for the profile catalogue.
func Catalog() CatalogInfo {
	generated, _ := time.Parse("2006-01-02", ProfileCatalogGenerated)
	concrete := make([]string, 0, len(profiles))
	for name := range profiles {
		concrete = append(concrete, name)
	}
	sort.Strings(concrete)
	aliases := make([]string, 0, len(profileAliases))
	for name := range profileAliases {
		if name != "" {
			aliases = append(aliases, name)
		}
	}
	sort.Strings(aliases)
	return CatalogInfo{
		Version:    ProfileCatalogVersion,
		Generated:  generated,
		MaxAgeDays: ProfileCatalogMaxAgeDays,
		Concrete:   concrete,
		Aliases:    aliases,
	}
}

// CatalogAge reports how long the shipped profile catalogue has aged.
func CatalogAge(now time.Time) time.Duration {
	info := Catalog()
	return now.Sub(info.Generated)
}

// CatalogStale reports whether the profile catalogue is older than the review
// threshold. Future-dated build clocks are treated as fresh.
func CatalogStale(now time.Time) bool {
	age := CatalogAge(now)
	if age <= 0 {
		return false
	}
	return age > time.Duration(ProfileCatalogMaxAgeDays)*24*time.Hour
}
