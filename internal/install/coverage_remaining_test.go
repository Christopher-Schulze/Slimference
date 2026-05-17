package install

import "testing"

func TestResolveHomeEmptyFromOS(t *testing.T) {
	prev := userHomeDirFn
	userHomeDirFn = func() (string, error) { return "", nil }
	t.Cleanup(func() { userHomeDirFn = prev })
	if _, err := resolveHome(""); err == nil {
		t.Fatalf("expected empty HOME error, got %v", err)
	}
}
