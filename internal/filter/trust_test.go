package filter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withTempTrustStore redirects the trust store to a tempdir and pins the
// clock so assertions about TrustedAt are deterministic.
func withTempTrustStore(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origPathFn := trustStorePathFn
	origNow := trustNowFn
	origEnv := trustEnvLookupFn
	trustStorePathFn = func() string { return filepath.Join(dir, "trust.json") }
	trustNowFn = func() time.Time { return time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC) }
	trustEnvLookupFn = func(key string) string { return "" }
	t.Cleanup(func() {
		trustStorePathFn = origPathFn
		trustNowFn = origNow
		trustEnvLookupFn = origEnv
	})
}

// writeFilter creates a small filter file in a tempdir and returns its
// absolute path.
func writeFilter(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "filters.toml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTrustStatus_String covers every label branch.
func TestTrustStatus_String(t *testing.T) {
	t.Parallel()
	cases := map[TrustStatus]string{
		TrustStatusTrusted:        "trusted",
		TrustStatusUntrusted:      "untrusted",
		TrustStatusContentChanged: "content-changed",
		TrustStatusEnvOverride:    "env-override",
		TrustStatusMissing:        "missing",
		TrustStatus(99):           "unknown",
	}
	for s, want := range cases {
		if got := s.String(); got != want {
			t.Errorf("%d: got %q, want %q", s, got, want)
		}
	}
}

// TestDefaultTrustStorePath returns a sensible default, including the home
// fallback.
func TestDefaultTrustStorePath(t *testing.T) {
	orig := trustHomeDirFn
	t.Cleanup(func() { trustHomeDirFn = orig })
	trustHomeDirFn = func() (string, error) { return "", errors.New("no home") }
	if got := defaultTrustStorePath(); got != ".slimference/trust.json" {
		t.Fatalf("fallback path: %s", got)
	}
	trustHomeDirFn = func() (string, error) { return "/Users/alice", nil }
	if got := defaultTrustStorePath(); got != "/Users/alice/.slimference/trust.json" {
		t.Fatalf("home path: %s", got)
	}
}

// TestComputeFileSHA256 verifies the digest and the missing-file error.
func TestComputeFileSHA256(t *testing.T) {
	t.Parallel()
	path := writeFilter(t, "deny_patterns = []\n")
	digest, err := ComputeFileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(digest) != 64 {
		t.Fatalf("digest length: %d", len(digest))
	}
	if _, err := ComputeFileSHA256(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("missing file must error")
	}
}

// TestAddTrust_Roundtrip writes an entry, reads the store and verifies.
func TestAddTrust_Roundtrip(t *testing.T) {
	withTempTrustStore(t)
	path := writeFilter(t, "schema_version = 1\n")
	entry, err := AddTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	if entry.SHA256 == "" || entry.TrustedAt == "" {
		t.Fatalf("entry incomplete: %+v", entry)
	}
	status, got, err := EvaluateTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	if status != TrustStatusTrusted || got.SHA256 != entry.SHA256 {
		t.Fatalf("status=%s got=%+v", status, got)
	}
}

// TestAddTrust_OverwritesExisting lets operators re-trust after intentional edits.
func TestAddTrust_OverwritesExisting(t *testing.T) {
	withTempTrustStore(t)
	path := writeFilter(t, "original\n")
	if _, err := AddTrust(path); err != nil {
		t.Fatal(err)
	}
	// Mutate file, re-trust.
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entry, err := AddTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	status, got, err := EvaluateTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	if status != TrustStatusTrusted || got.SHA256 != entry.SHA256 {
		t.Fatalf("re-trust did not take: status=%s", status)
	}
}

// TestAddTrust_MissingFile errors out.
func TestAddTrust_MissingFile(t *testing.T) {
	withTempTrustStore(t)
	if _, err := AddTrust(filepath.Join(t.TempDir(), "nope.toml")); err == nil {
		t.Fatal("missing file must error")
	}
}

// TestRemoveTrust_ExistingAndMissing returns true/false correctly.
func TestRemoveTrust_ExistingAndMissing(t *testing.T) {
	withTempTrustStore(t)
	path := writeFilter(t, "x\n")
	if _, err := AddTrust(path); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveTrust(path)
	if err != nil || !removed {
		t.Fatalf("removed=%v err=%v", removed, err)
	}
	// Second removal is a no-op.
	removed, err = RemoveTrust(path)
	if err != nil || removed {
		t.Fatalf("second remove must return false, got removed=%v err=%v", removed, err)
	}
}

// TestListTrusted returns entries in stable order.
func TestListTrusted(t *testing.T) {
	withTempTrustStore(t)
	a := writeFilter(t, "a\n")
	b := writeFilter(t, "b\n")
	if _, err := AddTrust(a); err != nil {
		t.Fatal(err)
	}
	if _, err := AddTrust(b); err != nil {
		t.Fatal(err)
	}
	entries, err := ListTrusted()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Path >= entries[1].Path {
		t.Fatalf("entries not sorted: %+v", entries)
	}
}

// TestEvaluateTrust_missingFile reports the missing status.
func TestEvaluateTrust_missingFile(t *testing.T) {
	withTempTrustStore(t)
	status, _, err := EvaluateTrust(filepath.Join(t.TempDir(), "nope"))
	if err != nil || status != TrustStatusMissing {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

// TestEvaluateTrust_contentChanged fires when the file sha256 drifts.
func TestEvaluateTrust_contentChanged(t *testing.T) {
	withTempTrustStore(t)
	path := writeFilter(t, "original\n")
	if _, err := AddTrust(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, _, err := EvaluateTrust(path)
	if err != nil {
		t.Fatal(err)
	}
	if status != TrustStatusContentChanged {
		t.Fatalf("status=%s", status)
	}
}

// TestEvaluateTrust_envOverride is unconditional.
func TestEvaluateTrust_envOverride(t *testing.T) {
	withTempTrustStore(t)
	trustEnvLookupFn = func(key string) string {
		if key == "SLIMFERENCE_TRUST_PROJECT_FILTERS" {
			return "1"
		}
		return ""
	}
	status, _, err := EvaluateTrust("/bogus/nonexistent")
	if err != nil || status != TrustStatusEnvOverride {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

// TestEvaluateTrust_untrusted reports untrusted for existing but
// non-recorded files.
func TestEvaluateTrust_untrusted(t *testing.T) {
	withTempTrustStore(t)
	path := writeFilter(t, "new file\n")
	status, _, err := EvaluateTrust(path)
	if err != nil || status != TrustStatusUntrusted {
		t.Fatalf("status=%s err=%v", status, err)
	}
}

// TestProjectFilterAllowed_matrix covers every status branch.
func TestProjectFilterAllowed_matrix(t *testing.T) {
	origEval := evaluateTrustFn
	t.Cleanup(func() { evaluateTrustFn = origEval })
	cases := []struct {
		status TrustStatus
		err    error
		want   bool
	}{
		{TrustStatusTrusted, nil, true},
		{TrustStatusEnvOverride, nil, true},
		{TrustStatusMissing, nil, true},
		{TrustStatusUntrusted, nil, false},
		{TrustStatusContentChanged, nil, false},
		{TrustStatusTrusted, errors.New("io"), false},
	}
	for _, tc := range cases {
		evaluateTrustFn = func(string) (TrustStatus, TrustEntry, error) { return tc.status, TrustEntry{}, tc.err }
		if got := projectFilterAllowed("/whatever"); got != tc.want {
			t.Errorf("status=%v err=%v: got %v want %v", tc.status, tc.err, got, tc.want)
		}
	}
}

// TestLoadTrustStore_corruptJSON surfaces the parse error.
func TestLoadTrustStore_corruptJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	trustStoreMu.Lock()
	_, err := loadTrustStore()
	trustStoreMu.Unlock()
	if err == nil || !strings.Contains(err.Error(), "parse trust store") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// TestSaveTrustStore_MkdirFailure errors when the parent cannot be created.
func TestSaveTrustStore_MkdirFailure(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return filepath.Join(blocker, "nested", "trust.json") }
	t.Cleanup(func() { trustStorePathFn = orig })

	trustStoreMu.Lock()
	err := saveTrustStore(TrustStore{Version: 1, Trusted: map[string]TrustEntry{}})
	trustStoreMu.Unlock()
	if err == nil {
		t.Fatal("expected mkdir error")
	}
}

// TestCanonicalFilterPath handles both absolute and relative inputs.
func TestCanonicalFilterPath(t *testing.T) {
	t.Parallel()
	p := writeFilter(t, "x")
	got := canonicalFilterPath(p)
	if !filepath.IsAbs(got) {
		t.Fatalf("canonical path must be absolute: %s", got)
	}
	// Relative path resolves to something absolute-looking.
	rel := "./doesnt-exist.toml"
	if got := canonicalFilterPath(rel); got == "" || got == rel {
		t.Fatalf("rel canonicalisation: got=%s", got)
	}
}

// TestLoadTrustStore_readErrorNonNotExist surfaces generic IO errors.
func TestLoadTrustStore_readErrorNonNotExist(t *testing.T) {
	dir := t.TempDir()
	// Point store path at a directory so ReadFile returns EISDIR, not NotExist.
	orig := trustStorePathFn
	trustStorePathFn = func() string { return dir }
	t.Cleanup(func() { trustStorePathFn = orig })

	trustStoreMu.Lock()
	_, err := loadTrustStore()
	trustStoreMu.Unlock()
	if err == nil {
		t.Fatal("expected read error")
	}
}

// TestAddTrust_saveFailureSurfaced when the parent directory cannot be
// created because a regular file blocks the path. Drives trustStorePathFn
// to return a valid path on load and a blocked path on save so only the
// save step errors out.
func TestAddTrust_saveFailureSurfaced(t *testing.T) {
	goodDir := t.TempDir()
	blockerDir := t.TempDir()
	blocker := filepath.Join(blockerDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	calls := 0
	trustStorePathFn = func() string {
		calls++
		if calls == 1 {
			return filepath.Join(goodDir, "trust.json")
		}
		return filepath.Join(blocker, "nested", "trust.json")
	}
	t.Cleanup(func() { trustStorePathFn = orig })

	path := writeFilter(t, "x")
	if _, err := AddTrust(path); err == nil {
		t.Fatal("expected save failure")
	}
}

// The canonical RemoveTrust save-failure test lives in
// trust_branches_test.go (uses the counter-based trustStorePathFn swap).

// TestListTrusted_readError surfaces a corrupt store.
func TestListTrusted_readError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	if _, err := ListTrusted(); err == nil {
		t.Fatal("expected read error")
	}
}

// TestEvaluateTrust_corruptStore surfaces load errors cleanly.
func TestEvaluateTrust_corruptStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	file := writeFilter(t, "x")
	_, _, err := EvaluateTrust(file)
	if err == nil {
		t.Fatal("expected load error surfaced")
	}
}

// TestRemoveTrust_corruptStore surfaces load errors.
func TestRemoveTrust_corruptStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	if _, err := RemoveTrust("/some/path"); err == nil {
		t.Fatal("expected load error")
	}
}

// TestAddTrust_corruptStore surfaces load errors.
func TestAddTrust_corruptStore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	file := writeFilter(t, "x")
	if _, err := AddTrust(file); err == nil {
		t.Fatal("expected load error")
	}
}

// TestEvaluateTrust_statError triggers when path sits under a file used as
// a directory component.
func TestEvaluateTrust_statError(t *testing.T) {
	withTempTrustStore(t)
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Path lookup traverses blocker as if it were a directory.
	status, _, err := EvaluateTrust(filepath.Join(blocker, "under"))
	if err != nil {
		t.Logf("surfaced err (acceptable): %v", err)
	}
	_ = status
}

// TestErrorsIs exercises the defensive wrapper.
func TestErrorsIs(t *testing.T) {
	t.Parallel()
	if errorsIs(nil, os.ErrNotExist) {
		t.Fatal("nil must not match")
	}
	if !errorsIs(os.ErrNotExist, os.ErrNotExist) {
		t.Fatal("identical must match")
	}
}
