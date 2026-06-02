package contextledger

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type CapsuleKind string

const (
	CapsuleCommand CapsuleKind = "command"
	CapsuleFile    CapsuleKind = "file"
	CapsuleSearch  CapsuleKind = "search"
	CapsuleFailure CapsuleKind = "failure"
)

type Capsule struct {
	Kind       CapsuleKind       `json:"kind"`
	Provenance Provenance        `json:"provenance"`
	Facts      map[string]string `json:"facts"`
	Hashes     map[string]string `json:"hashes,omitempty"`
	Archives   []string          `json:"archives,omitempty"`
}

type Provenance struct {
	SessionID string `json:"session_id,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	Source    string `json:"source"`
}

type CommandObservation struct {
	SessionID   string
	TurnID      string
	CommandLine string
	CWD         string
	ExitCode    int
	Stdout      []byte
	Stderr      []byte
	ArchiveIDs  []string
	Mechanisms  []string
}

type FileObservation struct {
	SessionID    string
	TurnID       string
	Path         string
	RepoRoot     string
	Range        string
	Content      []byte
	ArchiveID    string
	FullPassTurn string
}

type SearchObservation struct {
	SessionID    string
	TurnID       string
	CommandLine  string
	RepoRoot     string
	PatternHash  string
	FilesMatched []string
	OmittedCount int
	Output       []byte
	ArchiveID    string
}

type FailureObservation struct {
	SessionID string
	TurnID    string
	Tool      string
	File      string
	Line      string
	Column    string
	Message   string
	ExitCode  int
	ArchiveID string
}

func BuildCommandCapsule(obs CommandObservation) (Capsule, error) {
	if strings.TrimSpace(obs.CommandLine) == "" {
		return Capsule{}, errors.New("command line is required")
	}
	facts := map[string]string{
		"command":   strings.TrimSpace(obs.CommandLine),
		"cwd":       cleanPathFact(obs.CWD),
		"exit_code": intFact(obs.ExitCode),
	}
	if len(obs.Mechanisms) > 0 {
		facts["mechanisms"] = strings.Join(sortedStrings(obs.Mechanisms), ",")
	}
	hashes := map[string]string{}
	if len(obs.Stdout) > 0 {
		hashes["stdout_sha256"] = contentHash(obs.Stdout)
	}
	if len(obs.Stderr) > 0 {
		hashes["stderr_sha256"] = contentHash(obs.Stderr)
	}
	return newCapsule(CapsuleCommand, obs.SessionID, obs.TurnID, "command", facts, hashes, obs.ArchiveIDs), nil
}

func BuildFileCapsule(obs FileObservation) (Capsule, error) {
	if strings.TrimSpace(obs.Path) == "" {
		return Capsule{}, errors.New("file path is required")
	}
	if len(obs.Content) > 0 && strings.TrimSpace(obs.ArchiveID) == "" {
		return Capsule{}, errors.New("archive id is required for omitted file content")
	}
	if strings.TrimSpace(obs.ArchiveID) != "" && strings.TrimSpace(obs.FullPassTurn) == "" {
		return Capsule{}, errors.New("full-pass turn is required for archived file content")
	}
	facts := map[string]string{
		"path":           cleanPathFact(obs.Path),
		"repo_root":      cleanPathFact(obs.RepoRoot),
		"range":          strings.TrimSpace(obs.Range),
		"full_pass_turn": strings.TrimSpace(obs.FullPassTurn),
	}
	hashes := map[string]string{}
	if len(obs.Content) > 0 {
		hashes["content_sha256"] = contentHash(obs.Content)
	}
	return newCapsule(CapsuleFile, obs.SessionID, obs.TurnID, "file", facts, hashes, singleton(obs.ArchiveID)), nil
}

func BuildSearchCapsule(obs SearchObservation) (Capsule, error) {
	if strings.TrimSpace(obs.CommandLine) == "" {
		return Capsule{}, errors.New("search command is required")
	}
	if len(obs.Output) > 0 && strings.TrimSpace(obs.ArchiveID) == "" {
		return Capsule{}, errors.New("archive id is required for omitted search output")
	}
	files := sortedStrings(obs.FilesMatched)
	facts := map[string]string{
		"command":       strings.TrimSpace(obs.CommandLine),
		"repo_root":     cleanPathFact(obs.RepoRoot),
		"pattern_hash":  strings.TrimSpace(obs.PatternHash),
		"files_matched": strings.Join(files, ","),
		"omitted_count": intFact(obs.OmittedCount),
	}
	hashes := map[string]string{}
	if len(obs.Output) > 0 {
		hashes["output_sha256"] = contentHash(obs.Output)
	}
	return newCapsule(CapsuleSearch, obs.SessionID, obs.TurnID, "search", facts, hashes, singleton(obs.ArchiveID)), nil
}

func BuildFailureCapsule(obs FailureObservation) (Capsule, error) {
	if strings.TrimSpace(obs.Message) == "" {
		return Capsule{}, errors.New("failure message is required")
	}
	facts := map[string]string{
		"tool":      strings.TrimSpace(obs.Tool),
		"file":      cleanPathFact(obs.File),
		"line":      strings.TrimSpace(obs.Line),
		"column":    strings.TrimSpace(obs.Column),
		"message":   strings.TrimSpace(obs.Message),
		"exit_code": intFact(obs.ExitCode),
	}
	return newCapsule(CapsuleFailure, obs.SessionID, obs.TurnID, "failure", facts, nil, singleton(obs.ArchiveID)), nil
}

func newCapsule(kind CapsuleKind, sessionID, turnID, source string, facts, hashes map[string]string, archives []string) Capsule {
	return Capsule{
		Kind: kind,
		Provenance: Provenance{
			SessionID: strings.TrimSpace(sessionID),
			TurnID:    strings.TrimSpace(turnID),
			Source:    source,
		},
		Facts:    compactStringMap(facts),
		Hashes:   compactStringMap(hashes),
		Archives: sortedStrings(archives),
	}
}

func contentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func cleanPathFact(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return ""
	}
	return filepath.Clean(trimmed)
}

func compactStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func singleton(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return []string{value}
}

func intFact(value int) string {
	return strconv.Itoa(value)
}
