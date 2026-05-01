package crosstool

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type State struct {
	mu       sync.RWMutex
	sessions map[string]*sessionState
}

type sessionState struct {
	lists      map[string]gitPathList
	lastUpdate time.Time
}

type gitPathList struct {
	Source string
	Paths  []string
}

type Result struct {
	Output      []byte
	Elided      bool
	ElidedPaths int
	Source      string
	PathCount   int
}

func NewState() *State {
	return &State{sessions: map[string]*sessionState{}}
}

func (s *State) ResetSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, sessionID)
}

func (s *State) ObserveGitStatus(sessionID string, output []byte) int {
	paths := ExtractGitStatusPaths(output)
	return s.observe(sessionID, "git status", paths)
}

func (s *State) ApplyGitNameOnly(sessionID string, output []byte) Result {
	paths := ExtractGitNameOnlyPaths(output)
	return s.apply(sessionID, "git name-only", output, paths)
}

func (s *State) apply(sessionID, source string, output []byte, paths []string) Result {
	result := Result{Output: output, PathCount: len(paths)}
	if len(paths) == 0 {
		return result
	}

	fp := fingerprint(paths)
	s.mu.RLock()
	session := s.sessions[sessionID]
	var previous gitPathList
	ok := false
	if session != nil {
		previous, ok = session.lists[fp]
	}
	s.mu.RUnlock()
	if ok {
		result.Output = []byte("[Slimference: " + intString(len(paths)) + " git paths already shown by previous `" + previous.Source + "`]\n")
		result.Elided = true
		result.ElidedPaths = len(paths)
		result.Source = previous.Source
		return result
	}
	s.observe(sessionID, source, paths)
	return result
}

func (s *State) observe(sessionID, source string, paths []string) int {
	if len(paths) == 0 {
		return 0
	}
	fp := fingerprint(paths)
	s.mu.Lock()
	defer s.mu.Unlock()
	session := s.sessions[sessionID]
	if session == nil {
		session = &sessionState{lists: map[string]gitPathList{}}
		s.sessions[sessionID] = session
	}
	session.lists[fp] = gitPathList{Source: source, Paths: append([]string(nil), paths...)}
	session.lastUpdate = time.Now()
	return len(paths)
}

func ExtractGitStatusPaths(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	paths := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "##") || strings.HasPrefix(trimmed, "!!") {
			continue
		}
		if len(line) < 4 {
			return nil
		}
		status := line[:2]
		if !isGitStatusCode(status) || line[2] != ' ' {
			return nil
		}
		path := strings.TrimSpace(line[3:])
		if path == "" {
			return nil
		}
		if arrow := strings.LastIndex(path, " -> "); arrow >= 0 {
			path = path[arrow+4:]
		}
		paths = append(paths, normalizeGitPath(path))
	}
	return sortedUnique(paths)
}

func ExtractGitNameOnlyPaths(output []byte) []string {
	lines := strings.Split(string(output), "\n")
	paths := make([]string, 0, len(lines))
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "\t\r") || strings.Contains(line, " ") {
			return nil
		}
		paths = append(paths, normalizeGitPath(line))
	}
	return sortedUnique(paths)
}

func isGitStatusCode(code string) bool {
	for _, r := range code {
		if !strings.ContainsRune(" MADRCU?!", r) {
			return false
		}
	}
	return code != "  "
}

func normalizeGitPath(path string) string {
	path = strings.TrimPrefix(path, "./")
	path = filepath.ToSlash(path)
	return path
}

func sortedUnique(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	out := paths[:0]
	for _, path := range paths {
		if path == "" {
			continue
		}
		if len(out) > 0 && out[len(out)-1] == path {
			continue
		}
		out = append(out, path)
	}
	return out
}

func fingerprint(paths []string) string {
	normalised := make([]string, 0, len(paths))
	for _, path := range paths {
		normalised = append(normalised, normalizeGitPath(path))
	}
	normalised = sortedUnique(normalised)
	sum := sha256.Sum256([]byte(strings.Join(normalised, "\x00")))
	return hex.EncodeToString(sum[:])
}

func intString(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
