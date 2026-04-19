package readcache

import "testing"

func TestSanitizeSessionID(t *testing.T) {
	t.Parallel()

	if got := sanitizeSessionID("abc/../x y"); got != "abc____x_y" {
		t.Fatalf("sanitizeSessionID = %q", got)
	}
}
