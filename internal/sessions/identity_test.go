package sessions

import "testing"

func TestSafeSessionID(t *testing.T) {
	if got := SafeSessionID(""); got != AnonymousSessionID {
		t.Fatalf("blank session id = %q", got)
	}
	if got := SafeSessionID(" A-a_1/ b "); got != "A-a_1__b" {
		t.Fatalf("safe session id = %q", got)
	}
	if got := SafeOptionalSessionID(" \n "); got != "" {
		t.Fatalf("optional blank session id = %q", got)
	}
	if got := SafeOptionalSessionID("sess/1"); got != "sess_1" {
		t.Fatalf("optional session id = %q", got)
	}
	if got := SafeTurnID(""); got != AnonymousTurnID {
		t.Fatalf("blank turn id = %q", got)
	}
	if got := SafeTurnID(" turn/1:x "); got != "turn_1_x" {
		t.Fatalf("safe turn id = %q", got)
	}
	if got := SafeOptionalTurnID(" \n "); got != "" {
		t.Fatalf("optional blank turn id = %q", got)
	}
	if got := SafeOptionalTurnID("turn/2"); got != "turn_2" {
		t.Fatalf("optional turn id = %q", got)
	}
}
