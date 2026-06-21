package sessions

import "testing"

func TestFingerprintPaths(t *testing.T) {
	t.Parallel()
	fp1 := FingerprintPaths([]string{"./b.go", "a.go", "a.go"})
	fp2 := FingerprintPaths([]string{"a.go", "b.go"})
	if fp1 == "" || fp1 != fp2 {
		t.Fatalf("fingerprints differ fp1=%q fp2=%q", fp1, fp2)
	}
	if FingerprintPaths(nil) != "" {
		t.Fatal("empty fingerprint should be empty")
	}
}
