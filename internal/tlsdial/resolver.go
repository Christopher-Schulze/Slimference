package tlsdial

import (
	"net"
	"strings"
)

// Resolver selects a TLS profile by upstream hostname.
type Resolver struct {
	defaultProfile Profile
	byHost         map[string]Profile
}

// NewResolver validates and normalises the configured host profile map.
func NewResolver(defaultName string, hostProfiles map[string]string) (Resolver, error) {
	defaultProfile, err := ResolveProfile(defaultName)
	if err != nil {
		return Resolver{}, err
	}
	byHost := make(map[string]Profile, len(hostProfiles))
	for host, name := range hostProfiles {
		cleanHost := normalizeHost(host)
		if cleanHost == "" {
			continue
		}
		profile, err := ResolveProfile(name)
		if err != nil {
			return Resolver{}, err
		}
		byHost[cleanHost] = profile
	}
	return Resolver{defaultProfile: defaultProfile, byHost: byHost}, nil
}

// Resolve returns the configured profile for host, falling back to the
// resolver default. Host may include a port.
func (r Resolver) Resolve(host string) Profile {
	cleanHost := normalizeHost(host)
	if profile, ok := r.byHost[cleanHost]; ok {
		return profile
	}
	return r.defaultProfile
}

func normalizeHost(host string) string {
	clean := strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(clean); err == nil {
		clean = h
	}
	return strings.Trim(clean, "[]")
}
