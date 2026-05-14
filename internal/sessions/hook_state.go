package sessions

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	hookStateMaxTurns       = 16
	hookStateMaxFilesPerSet = 128
	hookStateLockTimeout    = 2 * time.Second
	hookStateStaleLockAge   = 30 * time.Second
)

var hookStateOpenFile = os.OpenFile

type HookState struct {
	SessionID   string          `json:"session_id"`
	Sequence    int             `json:"sequence"`
	CurrentTurn string          `json:"current_turn"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Turns       []HookTurnState `json:"turns"`
}

type HookTurnState struct {
	ID           string                 `json:"id"`
	StartedAt    time.Time              `json:"started_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	Closed       bool                   `json:"closed"`
	Tools        []string               `json:"tools,omitempty"`
	FilesRead    []string               `json:"files_read,omitempty"`
	FilesEdited  []string               `json:"files_edited,omitempty"`
	GitPathLists []HookGitPathListState `json:"git_path_lists,omitempty"`
}

type HookGitPathListState struct {
	Source      string    `json:"source"`
	CWD         string    `json:"cwd,omitempty"`
	Fingerprint string    `json:"fingerprint"`
	Count       int       `json:"count"`
	ObservedAt  time.Time `json:"observed_at"`
}

func DefaultHookStateDir(home string) string {
	return filepath.Join(home, ".slimference", "turn-state")
}

func StartHookSession(dir, sessionID string) error {
	return mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		state.Sequence = 1
		state.CurrentTurn = "turn-1"
		state.Turns = []HookTurnState{newHookTurn("turn-1", now)}
		state.UpdatedAt = now
		return nil
	})
}

func StartHookTurn(dir, sessionID string) error {
	return mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		state.Sequence++
		id := "turn-" + strconvItoa(state.Sequence)
		state.CurrentTurn = id
		state.Turns = append(state.Turns, newHookTurn(id, now))
		trimHookTurns(state)
		state.UpdatedAt = now
		return nil
	})
}

func CloseHookTurn(dir, sessionID string) error {
	return mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		turn := currentHookTurn(state, now)
		turn.Closed = true
		turn.UpdatedAt = now
		state.UpdatedAt = now
		return nil
	})
}

func ObserveHookTool(dir, sessionID, toolName, command string) error {
	return mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		turn := currentHookTurn(state, now)
		label := strings.TrimSpace(toolName)
		if command = strings.TrimSpace(command); command != "" {
			if label == "" {
				label = command
			} else {
				label += ": " + command
			}
		}
		if label != "" {
			turn.Tools = appendUniqueCapped(turn.Tools, label, hookStateMaxFilesPerSet)
		}
		turn.UpdatedAt = now
		state.UpdatedAt = now
		return nil
	})
}

func ObserveHookFile(dir, sessionID, path, operation string) error {
	path = normaliseHookPath(path)
	if path == "" {
		return nil
	}
	return mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		turn := currentHookTurn(state, now)
		switch strings.ToLower(strings.TrimSpace(operation)) {
		case "edit", "write":
			turn.FilesEdited = appendUniqueCapped(turn.FilesEdited, path, hookStateMaxFilesPerSet)
		default:
			turn.FilesRead = appendUniqueCapped(turn.FilesRead, path, hookStateMaxFilesPerSet)
		}
		turn.UpdatedAt = now
		state.UpdatedAt = now
		return nil
	})
}

func ObserveHookGitPathList(dir, sessionID, cwd, source string, paths []string) (HookGitPathListState, bool, error) {
	fp := FingerprintPaths(paths)
	if fp == "" {
		return HookGitPathListState{}, false, nil
	}
	cleanCWD := normaliseHookPath(cwd)
	source = strings.TrimSpace(source)
	var out HookGitPathListState
	var repeated bool
	err := mutateHookState(dir, sessionID, func(state *HookState, now time.Time) error {
		turn := currentHookTurn(state, now)
		for _, existing := range turn.GitPathLists {
			if existing.CWD == cleanCWD && existing.Fingerprint == fp {
				out = existing
				repeated = true
				turn.UpdatedAt = now
				state.UpdatedAt = now
				return nil
			}
		}
		out = HookGitPathListState{
			Source:      source,
			CWD:         cleanCWD,
			Fingerprint: fp,
			Count:       len(sortedUniqueStrings(paths)),
			ObservedAt:  now,
		}
		turn.GitPathLists = append(turn.GitPathLists, out)
		if len(turn.GitPathLists) > hookStateMaxFilesPerSet {
			turn.GitPathLists = turn.GitPathLists[len(turn.GitPathLists)-hookStateMaxFilesPerSet:]
		}
		turn.UpdatedAt = now
		state.UpdatedAt = now
		return nil
	})
	return out, repeated, err
}

func RecentlyEditedHookFile(dir, sessionID, path string, previousTurns int) (bool, error) {
	path = normaliseHookPath(path)
	if path == "" {
		return false, nil
	}
	var hit bool
	err := inspectHookState(dir, sessionID, func(state *HookState) error {
		start := len(state.Turns) - 1 - previousTurns
		if start < 0 {
			start = 0
		}
		for _, turn := range state.Turns[start:] {
			for _, edited := range turn.FilesEdited {
				if edited == path {
					hit = true
					return nil
				}
			}
		}
		return nil
	})
	return hit, err
}

func CurrentHookTurnID(dir, sessionID string) (string, error) {
	var out string
	err := inspectHookState(dir, sessionID, func(state *HookState) error {
		out = SafeOptionalTurnID(state.CurrentTurn)
		return nil
	})
	return out, err
}

func LoadHookState(dir, sessionID string) (HookState, error) {
	path := hookStatePath(dir, sessionID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			now := time.Now().UTC()
			return HookState{
				SessionID:   safeHookSessionID(sessionID),
				Sequence:    1,
				CurrentTurn: "turn-1",
				UpdatedAt:   now,
				Turns:       []HookTurnState{newHookTurn("turn-1", now)},
			}, nil
		}
		return HookState{}, err
	}
	var state HookState
	if err := json.Unmarshal(data, &state); err != nil {
		return HookState{}, err
	}
	normaliseHookState(&state, sessionID, time.Now().UTC())
	return state, nil
}

func mutateHookState(dir, sessionID string, fn func(*HookState, time.Time) error) error {
	return withHookStateLock(dir, sessionID, func() error {
		now := time.Now().UTC()
		state, err := LoadHookState(dir, sessionID)
		if err != nil {
			return err
		}
		if err := fn(&state, now); err != nil {
			return err
		}
		return saveHookState(dir, &state)
	})
}

func inspectHookState(dir, sessionID string, fn func(*HookState) error) error {
	return withHookStateLock(dir, sessionID, func() error {
		state, err := LoadHookState(dir, sessionID)
		if err != nil {
			return err
		}
		return fn(&state)
	})
}

func saveHookState(dir string, state *HookState) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	normaliseHookState(state, state.SessionID, time.Now().UTC())
	data, _ := json.MarshalIndent(state, "", "  ")
	path := hookStatePath(dir, state.SessionID)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func withHookStateLock(dir, sessionID string, fn func() error) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	lock := hookStatePath(dir, sessionID) + ".lock"
	deadline := time.Now().Add(hookStateLockTimeout)
	for {
		f, err := hookStateOpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = f.WriteString(strconvItoa(os.Getpid()))
			_ = f.Close()
			defer os.Remove(lock)
			return fn()
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > hookStateStaleLockAge {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func currentHookTurn(state *HookState, now time.Time) *HookTurnState {
	normaliseHookState(state, state.SessionID, now)
	for i := range state.Turns {
		if state.Turns[i].ID == state.CurrentTurn {
			return &state.Turns[i]
		}
	}
	state.Sequence++
	id := "turn-" + strconvItoa(state.Sequence)
	state.CurrentTurn = id
	state.Turns = append(state.Turns, newHookTurn(id, now))
	trimHookTurns(state)
	return &state.Turns[len(state.Turns)-1]
}

func normaliseHookState(state *HookState, sessionID string, now time.Time) {
	state.SessionID = safeHookSessionID(state.SessionID)
	if state.SessionID == "anonymous" && strings.TrimSpace(sessionID) != "" {
		state.SessionID = safeHookSessionID(sessionID)
	}
	if state.Sequence <= 0 {
		state.Sequence = len(state.Turns)
	}
	if len(state.Turns) == 0 {
		state.Sequence = 1
		state.CurrentTurn = "turn-1"
		state.Turns = []HookTurnState{newHookTurn("turn-1", now)}
	}
	if state.CurrentTurn == "" {
		state.CurrentTurn = state.Turns[len(state.Turns)-1].ID
	}
	trimHookTurns(state)
}

func trimHookTurns(state *HookState) {
	if len(state.Turns) > hookStateMaxTurns {
		state.Turns = append([]HookTurnState(nil), state.Turns[len(state.Turns)-hookStateMaxTurns:]...)
	}
}

func newHookTurn(id string, now time.Time) HookTurnState {
	return HookTurnState{ID: id, StartedAt: now, UpdatedAt: now}
}

func hookStatePath(dir, sessionID string) string {
	return filepath.Join(dir, safeHookSessionID(sessionID)+".json")
}

func safeHookSessionID(sessionID string) string {
	return SafeSessionID(sessionID)
}

func normaliseHookPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

func appendUniqueCapped(values []string, value string, capSize int) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	values = append(values, value)
	if capSize > 0 && len(values) > capSize {
		values = values[len(values)-capSize:]
	}
	return values
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	sign := ""
	if n < 0 {
		sign = "-"
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return sign + string(buf[i:])
}
