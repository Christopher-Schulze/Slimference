package wscompact

import (
	"strconv"
	"strings"
)

// WSExtensionProfile describes the negotiated WebSocket extension
// subset Slimference can use for mutation.
type WSExtensionProfile struct {
	PermessageDeflate       bool
	ClientNoContextTakeover bool
	ServerNoContextTakeover bool
	ClientMaxWindowBits     int
	ServerMaxWindowBits     int
	RawOffer                string
	RawAccept               string
	Supported               bool
	UnsupportedReason       string
}

// ExtensionToken is one parsed Sec-WebSocket-Extensions token.
type ExtensionToken struct {
	Name   string
	Params map[string]string
	Raw    string
}

// ParseExtensionsHeader parses a Sec-WebSocket-Extensions header value.
func ParseExtensionsHeader(header string) []ExtensionToken {
	parts := splitExtensionHeader(header)
	out := make([]ExtensionToken, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		segments := splitExtensionParams(part)
		if len(segments) == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(segments[0]))
		if name == "" {
			continue
		}
		token := ExtensionToken{
			Name:   name,
			Params: make(map[string]string),
			Raw:    part,
		}
		for _, segment := range segments[1:] {
			key, value, _ := strings.Cut(segment, "=")
			key = strings.ToLower(strings.TrimSpace(key))
			if key == "" {
				continue
			}
			token.Params[key] = strings.Trim(strings.TrimSpace(value), `"`)
		}
		out = append(out, token)
	}
	return out
}

// NegotiatePermessageDeflate returns the accepted permessage-deflate profile.
func NegotiatePermessageDeflate(offer, accept string) WSExtensionProfile {
	profile := WSExtensionProfile{
		RawOffer:  offer,
		RawAccept: accept,
	}
	if strings.TrimSpace(accept) == "" {
		profile.UnsupportedReason = "no_extensions_accepted"
		return profile
	}
	accepted := ParseExtensionsHeader(accept)
	var pm *ExtensionToken
	for i := range accepted {
		if accepted[i].Name == "permessage-deflate" {
			if pm != nil {
				profile.UnsupportedReason = "duplicate_permessage_deflate"
				return profile
			}
			pm = &accepted[i]
			continue
		}
		profile.UnsupportedReason = "unknown_extension_accepted"
		return profile
	}
	if pm == nil {
		profile.UnsupportedReason = "permessage_deflate_not_accepted"
		return profile
	}
	profile.PermessageDeflate = true
	for key, value := range pm.Params {
		switch key {
		case "client_no_context_takeover":
			if value != "" {
				profile.UnsupportedReason = "invalid_client_no_context_takeover"
				return profile
			}
			profile.ClientNoContextTakeover = true
		case "server_no_context_takeover":
			if value != "" {
				profile.UnsupportedReason = "invalid_server_no_context_takeover"
				return profile
			}
			profile.ServerNoContextTakeover = true
		case "client_max_window_bits":
			bits, ok := parseWindowBits(value)
			if !ok || bits != 15 {
				profile.UnsupportedReason = "unsupported_client_max_window_bits"
				return profile
			}
			profile.ClientMaxWindowBits = bits
		case "server_max_window_bits":
			bits, ok := parseWindowBits(value)
			if !ok || bits != 15 {
				profile.UnsupportedReason = "unsupported_server_max_window_bits"
				return profile
			}
			profile.ServerMaxWindowBits = bits
		default:
			profile.UnsupportedReason = "unknown_permessage_deflate_parameter"
			return profile
		}
	}
	profile.Supported = true
	return profile
}

func parseWindowBits(value string) (int, bool) {
	if value == "" {
		return 15, true
	}
	bits, err := strconv.Atoi(value)
	if err != nil || bits < 8 || bits > 15 {
		return 0, false
	}
	return bits, true
}

func splitExtensionHeader(header string) []string {
	return splitRespectingQuotes(header, ',')
}

func splitExtensionParams(token string) []string {
	return splitRespectingQuotes(token, ';')
}

func splitRespectingQuotes(s string, sep rune) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\' && inQuote:
			b.WriteRune(r)
			escaped = true
		case r == '"':
			b.WriteRune(r)
			inQuote = !inQuote
		case r == sep && !inQuote:
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	out = append(out, b.String())
	return out
}
