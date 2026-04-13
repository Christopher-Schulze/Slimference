package filter

import (
	"os"
	"regexp"
	"strings"
	"sync"
)

var denyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|\s)rm\s+(-[rf]+\s+)*--?\s*no-preserve-root`),
	regexp.MustCompile(`(?i)(?:^|\s)rm\s+(-[rf]+\s+)*/\s*(?:\s|$)`),
	regexp.MustCompile(`(?i)(?:^|\s)mkfs\.`),
	regexp.MustCompile(`(?i)(?:^|\s)dd\s+if=/dev/(zero|random|urandom)`),
	regexp.MustCompile(`\(\)\s*\{\s*:\|:&\s*\};:`),
}

var (
	extraDenyMu sync.RWMutex
	extraDeny   []*regexp.Regexp
)

// SetExtraDenyPatterns replaces config-driven deny regexes (compiled from RE2 strings). Invalid patterns are skipped.
func SetExtraDenyPatterns(patterns []string) {
	var out []*regexp.Regexp
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		out = append(out, re)
	}
	extraDenyMu.Lock()
	extraDeny = out
	extraDenyMu.Unlock()
}

// DeniedShellCommand returns true if the command line matches a hard deny rule (destructive / fork bomb).
func DeniedShellCommand(cmdLine string) (denied bool, reason string) {
	s := strings.TrimSpace(cmdLine)
	if s == "" {
		return false, ""
	}
	for _, re := range denyPatterns {
		if re.MatchString(s) {
			return true, "slimference: denied (destructive pattern)"
		}
	}
	extraDenyMu.RLock()
	defer extraDenyMu.RUnlock()
	for _, re := range extraDeny {
		if re.MatchString(s) {
			return true, "slimference: denied (config pattern)"
		}
	}
	return false, ""
}

// AskRequired returns true if the command should be confirmed (sudo) before running.
// Set SLIMFERENCE_CONFIRM_SUDO=1 to allow without prompting at the hook layer.
func AskRequired(cmdLine string) bool {
	if !strings.Contains(cmdLine, "sudo") {
		return false
	}
	return os.Getenv("SLIMFERENCE_CONFIRM_SUDO") != "1"
}
