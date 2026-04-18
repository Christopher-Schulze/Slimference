package filter

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadTrustStore_nilMapBecomesEmpty covers the `if store.Trusted == nil`
// branch: a trust store JSON that explicitly sets `"trusted": null` parses
// into a nil map, which loadTrustStore must normalise to an empty map so
// downstream lookups do not nil-deref.
func TestLoadTrustStore_nilMapBecomesEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"trusted":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	trustStoreMu.Lock()
	store, err := loadTrustStore()
	trustStoreMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.Trusted == nil {
		t.Fatal("nil map must be replaced with empty map")
	}
}

// TestLoadTrustStore_zeroVersionNormalised covers `if store.Version == 0`.
func TestLoadTrustStore_zeroVersionNormalised(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trust.json")
	if err := os.WriteFile(path, []byte(`{"trusted":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	orig := trustStorePathFn
	trustStorePathFn = func() string { return path }
	t.Cleanup(func() { trustStorePathFn = orig })

	trustStoreMu.Lock()
	store, err := loadTrustStore()
	trustStoreMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if store.Version != 1 {
		t.Fatalf("version must be normalised to 1, got %d", store.Version)
	}
}

// TestRemoveTrust_saveFailureSurfaced drives trustStorePathFn to return a
// valid path for load (so the entry is found) and a blocked path for save,
// so only the save step errors out.
func TestRemoveTrust_saveFailureSurfaced(t *testing.T) {
	goodDir := t.TempDir()
	path := writeFilter(t, "content")

	// Seed a trust store with the entry we plan to remove.
	orig := trustStorePathFn
	trustStorePathFn = func() string { return filepath.Join(goodDir, "trust.json") }
	if _, err := AddTrust(path); err != nil {
		t.Fatal(err)
	}

	// Now force save to fail while load still succeeds.
	blockerDir := t.TempDir()
	blocker := filepath.Join(blockerDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	trustStorePathFn = func() string {
		calls++
		if calls == 1 {
			return filepath.Join(goodDir, "trust.json")
		}
		return filepath.Join(blocker, "nested", "trust.json")
	}
	t.Cleanup(func() { trustStorePathFn = orig })

	if _, err := RemoveTrust(path); err == nil {
		t.Fatal("expected save failure")
	}
}

// TestEvaluateTrust_shaDigestError fires when stat succeeds but read fails.
// We simulate that by pointing at a file descriptor we cannot read: create
// a file and chmod 0000 (on macOS / Linux only).
func TestEvaluateTrust_shaDigestError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.toml")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, _, err := EvaluateTrust(path)
	if err == nil {
		t.Fatal("expected read error after stat-succeeds, read-fails")
	}
}

// TestSaveTrustStore_marshalError covers the normally-unreachable
// json.MarshalIndent error return via trustMarshalFn injection.
func TestSaveTrustStore_marshalError(t *testing.T) {
	dir := t.TempDir()
	orig := trustStorePathFn
	trustStorePathFn = func() string { return filepath.Join(dir, "trust.json") }
	t.Cleanup(func() { trustStorePathFn = orig })

	origMarshal := trustMarshalFn
	trustMarshalFn = func(any) ([]byte, error) { return nil, errFake }
	t.Cleanup(func() { trustMarshalFn = origMarshal })

	trustStoreMu.Lock()
	err := saveTrustStore(TrustStore{Version: 1, Trusted: map[string]TrustEntry{}})
	trustStoreMu.Unlock()
	if err == nil {
		t.Fatal("expected marshal error to surface")
	}
}

// errFake is a sentinel error used by branch tests.
var errFake = fakeErr{}

type fakeErr struct{}

func (fakeErr) Error() string { return "fake" }

// TestCanonicalFilterPath_absError covers the filepath.Abs error path via
// filepathAbsFn injection.
func TestCanonicalFilterPath_absError(t *testing.T) {
	orig := filepathAbsFn
	filepathAbsFn = func(string) (string, error) { return "", errFake }
	t.Cleanup(func() { filepathAbsFn = orig })

	in := "some-relative-path"
	if got := canonicalFilterPath(in); got != in {
		t.Fatalf("Abs-error must return original path, got %q", got)
	}
}

// TestSetTrustStorePathForTest / ResetTrustStorePathForTest exposes the
// test-only setters so unit tests cannot drift from the real resolver.
func TestSetResetTrustStorePathForTest(t *testing.T) {
	orig := trustStorePathFn
	t.Cleanup(func() { trustStorePathFn = orig })

	SetTrustStorePathForTest("/tmp/anything.json")
	if got := trustStorePathFn(); got != "/tmp/anything.json" {
		t.Fatalf("set failed: %s", got)
	}
	ResetTrustStorePathForTest()
	if trustStorePathFn == nil {
		t.Fatal("reset must restore a non-nil resolver")
	}
	// The reset resolver must produce something path-shaped.
	if got := trustStorePathFn(); got == "" {
		t.Fatal("reset resolver returned empty path")
	}
}
