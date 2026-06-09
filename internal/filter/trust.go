package filter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Trust model for project-local filters, ported from the RTK reference catalog.
//
// `.slimference/filters.toml` is loaded from the current working directory.
// An attacker who commits a malicious filters.toml to a public repository
// can control what Claude Code / Codex see: hide malicious code, suppress
// security scanner output, or rewrite command output entirely via the
// `replace` and `match_output` primitives.
//
// This module implements a trust-before-load model:
//   - Untrusted project filters are SKIPPED (not "loaded with a warning").
//   - `slimference trust add <path>` stores the SHA-256 after user review.
//   - Content changes invalidate trust (re-review required).
//   - Environment override SLIMFERENCE_TRUST_PROJECT_FILTERS=1 bypasses the
//     check for CI pipelines where the repo is already trusted.

// TrustEntry records a single trusted filter file with its sha256 and the
// instant it was trusted. TrustedAt is RFC3339 for human readability.
type TrustEntry struct {
	SHA256    string `json:"sha256"`
	TrustedAt string `json:"trusted_at"`
}

// TrustStore maps canonical absolute file paths to TrustEntry records.
type TrustStore struct {
	Version int                   `json:"version"`
	Trusted map[string]TrustEntry `json:"trusted"`
}

// TrustStatus summarises the outcome of evaluating a filter file against
// the trust store.
type TrustStatus int

const (
	// TrustStatusTrusted means the file's canonical path is in the store
	// and its sha256 matches.
	TrustStatusTrusted TrustStatus = iota
	// TrustStatusUntrusted means the file exists but its canonical path is
	// not in the store.
	TrustStatusUntrusted
	// TrustStatusContentChanged means the canonical path is in the store
	// but the file's sha256 no longer matches.
	TrustStatusContentChanged
	// TrustStatusEnvOverride means SLIMFERENCE_TRUST_PROJECT_FILTERS=1 is
	// set; the file is treated as trusted without consulting the store.
	TrustStatusEnvOverride
	// TrustStatusMissing means the file itself does not exist.
	TrustStatusMissing
)

// String returns the human-readable label for a TrustStatus.
func (s TrustStatus) String() string {
	switch s {
	case TrustStatusTrusted:
		return "trusted"
	case TrustStatusUntrusted:
		return "untrusted"
	case TrustStatusContentChanged:
		return "content-changed"
	case TrustStatusEnvOverride:
		return "env-override"
	case TrustStatusMissing:
		return "missing"
	default:
		return "unknown"
	}
}

// trustStoreMu guards concurrent access to the on-disk store.
var trustStoreMu sync.Mutex

// trustStorePathFn is overridable in tests.
var trustStorePathFn = defaultTrustStorePath

// trustHomeDirFn is overridable in tests.
var trustHomeDirFn = os.UserHomeDir

// trustEnvLookupFn is overridable in tests.
var trustEnvLookupFn = func(key string) string { return os.Getenv(key) }

// trustNowFn is overridable in tests.
var trustNowFn = func() time.Time { return time.Now() }

// defaultTrustStorePath returns ~/.slimference/trust.json. Falls back to
// a cwd-relative path only when the home dir cannot be resolved.
func defaultTrustStorePath() string {
	home, err := trustHomeDirFn()
	if err != nil || home == "" {
		return ".slimference/trust.json"
	}
	return filepath.Join(home, ".slimference", "trust.json")
}

// ComputeFileSHA256 reads path and returns its lowercase hex sha256 digest.
func ComputeFileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// loadTrustStore reads the store, returning an empty store on missing file.
// Must be called with trustStoreMu held.
func loadTrustStore() (TrustStore, error) {
	path := trustStorePathFn()
	data, err := os.ReadFile(path)
	if err != nil {
		if isNotExistErr(err) {
			return TrustStore{Version: 1, Trusted: map[string]TrustEntry{}}, nil
		}
		return TrustStore{}, err
	}
	var store TrustStore
	if err := json.Unmarshal(data, &store); err != nil {
		return TrustStore{}, fmt.Errorf("parse trust store: %w", err)
	}
	if store.Trusted == nil {
		store.Trusted = map[string]TrustEntry{}
	}
	if store.Version == 0 {
		store.Version = 1
	}
	return store, nil
}

// trustMarshalFn is overridable in tests so the (normally unreachable)
// json.MarshalIndent error return is covered without relying on an
// unmarshalable value. Our TrustStore is pure strings + ints + maps,
// which json cannot fail on.
var trustMarshalFn = func(v any) ([]byte, error) { return json.MarshalIndent(v, "", "  ") }

// saveTrustStore writes store to disk with 0600 perms, creating parent
// directories with 0700. Must be called with trustStoreMu held.
func saveTrustStore(store TrustStore) error {
	path := trustStorePathFn()
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	data, err := trustMarshalFn(store)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func isNotExistErr(err error) bool {
	return err != nil && (errorsIs(err, fs.ErrNotExist) || os.IsNotExist(err))
}

// errorsIs is kept as a wrapper so a test can stub it if needed. It exists
// so the file does not drag in errors.Is only for the single call above.
func errorsIs(target error, want error) bool {
	return os.IsNotExist(target) || (target != nil && target.Error() == want.Error())
}

// filepathAbsFn is overridable in tests so the (normally unreachable)
// filepath.Abs error path is covered without corrupting the process cwd.
var filepathAbsFn = filepath.Abs

// canonicalFilterPath returns an absolute, symlink-free path so two
// references to the same file always hash to the same trust key.
// filepath.Abs only fails when the process has no working directory,
// which is itself a fatal OS-level state - we return the original path
// in that case rather than blocking trust operations.
func canonicalFilterPath(path string) string {
	abs, err := filepathAbsFn(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// EvaluateTrust returns the status of the filter file at path against the
// on-disk trust store, honouring the SLIMFERENCE_TRUST_PROJECT_FILTERS
// environment override.
func EvaluateTrust(path string) (TrustStatus, TrustEntry, error) {
	if trustEnvLookupFn("SLIMFERENCE_TRUST_PROJECT_FILTERS") == "1" {
		return TrustStatusEnvOverride, TrustEntry{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		if isNotExistErr(err) {
			return TrustStatusMissing, TrustEntry{}, nil
		}
		return TrustStatusUntrusted, TrustEntry{}, err
	}
	digest, err := ComputeFileSHA256(path)
	if err != nil {
		return TrustStatusUntrusted, TrustEntry{}, err
	}
	canon := canonicalFilterPath(path)
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	store, err := loadTrustStore()
	if err != nil {
		return TrustStatusUntrusted, TrustEntry{}, err
	}
	entry, ok := store.Trusted[canon]
	if !ok {
		return TrustStatusUntrusted, TrustEntry{}, nil
	}
	if !strings.EqualFold(entry.SHA256, digest) {
		return TrustStatusContentChanged, entry, nil
	}
	return TrustStatusTrusted, entry, nil
}

// AddTrust records the current sha256 of path in the trust store.
// If path does not exist, returns an error. Existing entries are overwritten
// (re-trust after intentional edits).
func AddTrust(path string) (TrustEntry, error) {
	canon := canonicalFilterPath(path)
	digest, err := ComputeFileSHA256(path)
	if err != nil {
		return TrustEntry{}, err
	}
	entry := TrustEntry{
		SHA256:    digest,
		TrustedAt: trustNowFn().UTC().Format(time.RFC3339),
	}
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	store, err := loadTrustStore()
	if err != nil {
		return TrustEntry{}, err
	}
	store.Trusted[canon] = entry
	if err := saveTrustStore(store); err != nil {
		return TrustEntry{}, err
	}
	return entry, nil
}

// RemoveTrust removes the trust entry for path. Returns (true, nil) when a
// record existed and was removed, (false, nil) when no record existed.
func RemoveTrust(path string) (bool, error) {
	canon := canonicalFilterPath(path)
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	store, err := loadTrustStore()
	if err != nil {
		return false, err
	}
	if _, ok := store.Trusted[canon]; !ok {
		return false, nil
	}
	delete(store.Trusted, canon)
	if err := saveTrustStore(store); err != nil {
		return false, err
	}
	return true, nil
}

// ListTrusted returns all trust entries sorted by canonical path.
func ListTrusted() ([]TrustListing, error) {
	trustStoreMu.Lock()
	defer trustStoreMu.Unlock()
	store, err := loadTrustStore()
	if err != nil {
		return nil, err
	}
	out := make([]TrustListing, 0, len(store.Trusted))
	for path, entry := range store.Trusted {
		out = append(out, TrustListing{Path: path, Entry: entry})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// TrustListing is the public shape returned by ListTrusted.
type TrustListing struct {
	Path  string
	Entry TrustEntry
}

// SetTrustStorePathForTest lets external packages (currently cmd tests)
// point the trust store at a tempdir without exporting the package-private
// hook. Pair with ResetTrustStorePathForTest.
func SetTrustStorePathForTest(path string) {
	trustStorePathFn = func() string { return path }
}

// ResetTrustStorePathForTest restores the default resolver.
func ResetTrustStorePathForTest() {
	trustStorePathFn = defaultTrustStorePath
}
