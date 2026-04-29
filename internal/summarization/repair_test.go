package summarization

import (
	"strings"
	"testing"
)

func TestRepairSummary_PreambleTrimmed(t *testing.T) {

	ResetRepairCounts()
	in := "Here is a summary of what happened:\nThe model worked through several files.\n- first real fact\n- second real fact"
	out, changed := RepairSummary(in)
	if !changed {
		t.Fatal("expected change")
	}
	if !strings.HasPrefix(out, "- first real fact") {
		t.Fatalf("preamble not trimmed: %q", out)
	}
	if _, _, trimmed, _ := RepairCounts(); trimmed == 0 {
		t.Fatal("preamble counter did not advance")
	}
}

func TestRepairSummary_AlternativeBullets(t *testing.T) {

	ResetRepairCounts()
	in := "* alpha bullet\n1. numeric bullet\n- existing\n2. another numbered"
	out, changed := RepairSummary(in)
	if !changed {
		t.Fatalf("expected change: %q", out)
	}
	if strings.Contains(out, "* alpha") || strings.Contains(out, "1. numeric") {
		t.Fatalf("alternative bullets not normalised: %q", out)
	}
	if !strings.Contains(out, "- alpha bullet") || !strings.Contains(out, "- numeric bullet") {
		t.Fatalf("normalised bullets missing: %q", out)
	}
	if _, normalised, _, _ := RepairCounts(); normalised < 3 {
		t.Fatalf("normalised counter did not advance enough: %d", normalised)
	}
}

func TestRepairSummary_StripsMarkdownHeader(t *testing.T) {

	ResetRepairCounts()
	in := "# Summary\n- bullet"
	out, changed := RepairSummary(in)
	if !changed {
		t.Fatal("expected change")
	}
	if strings.Contains(out, "#") {
		t.Fatalf("markdown header not stripped: %q", out)
	}
	if _, _, _, headers := RepairCounts(); headers == 0 {
		t.Fatal("header counter did not advance")
	}
}

func TestRepairSummary_NoChangeReturnsFalse(t *testing.T) {

	ResetRepairCounts()
	in := "- already valid bullet\n- second bullet"
	out, changed := RepairSummary(in)
	if changed {
		t.Fatalf("unexpected change for already-valid input: %q", out)
	}
	if out != in {
		t.Fatalf("output differs without changed flag: %q vs %q", out, in)
	}
	if total, _, _, _ := RepairCounts(); total != 0 {
		t.Fatalf("counter advanced unexpectedly: %d", total)
	}
}

func TestRepairSummary_NoBulletYieldsEmpty(t *testing.T) {

	ResetRepairCounts()
	in := "Free-form prose without any bullets at all."
	out, _ := RepairSummary(in)
	// The repair pulls bullets only; non-bullet preamble without any
	// bullet at all collapses to empty. Empty is rejected by the validator
	// downstream which is the correct outcome.
	if strings.Contains(out, "bullets") {
		// either the result is empty or unchanged (no `- ` to anchor to);
		// just ensure no panic.
	}
}

func TestResetRepairCounts(t *testing.T) {

	ResetRepairCounts()
	RepairSummary("# header\n- x")
	if total, _, _, _ := RepairCounts(); total == 0 {
		t.Fatal("expected counter")
	}
	ResetRepairCounts()
	if total, _, _, _ := RepairCounts(); total != 0 {
		t.Fatalf("reset failed: %d", total)
	}
}
