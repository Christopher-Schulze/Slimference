package sessions

import "strings"

const AnonymousSessionID = "anonymous"

func SafeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AnonymousSessionID
	}
	var b strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func SafeOptionalSessionID(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return SafeSessionID(sessionID)
}
