package sessions

import "strings"

const AnonymousSessionID = "anonymous"
const AnonymousTurnID = "turn-unknown"

func SafeSessionID(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return AnonymousSessionID
	}
	return safeToken(sessionID)
}

func SafeOptionalSessionID(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return ""
	}
	return SafeSessionID(sessionID)
}

func SafeTurnID(turnID string) string {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return AnonymousTurnID
	}
	return safeToken(turnID)
}

func SafeOptionalTurnID(turnID string) string {
	if strings.TrimSpace(turnID) == "" {
		return ""
	}
	return SafeTurnID(turnID)
}

func safeToken(value string) string {
	var b strings.Builder
	for _, r := range value {
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
