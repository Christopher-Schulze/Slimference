package compression

import (
	"strings"
	"testing"
)

func TestIsWhitelistedComment_AllPatterns(t *testing.T) {
	t.Parallel()
	want := []string{
		"// SAFETY: must hold the mutex before write",
		"// INVARIANT: pool size is power of two",
		"// TODO(critical): retry must use jitter",
		"// FIXME(critical): file descriptor leak under load",
		"// HACK(critical): work around upstream bug",
		"// Copyright 2026 Slimference",
		"// SPDX-License-Identifier: MIT",
		"// All rights reserved.",
		"// Licensed under the Apache 2.0 License",
	}
	for _, line := range want {
		if !isWhitelistedComment(line) {
			t.Fatalf("expected whitelist hit on %q", line)
		}
	}
}

func TestIsWhitelistedComment_NonMatching(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"// next line is the real code",
		"// TODO: someday",
		"// FIXME: low priority",
		"# regular comment",
		"actual code line",
		"",
	} {
		if isWhitelistedComment(line) {
			t.Fatalf("must NOT match: %q", line)
		}
	}
}

func TestStripCStyleComments_PreservesWhitelistedLine(t *testing.T) {
	t.Parallel()
	input := `// Copyright 2026 Slimference
// regular comment
package main

func F() {
	// SAFETY: must lock first
	x := 1
	_ = x
}
`
	got := stripCStyleComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("license header dropped:\n%s", got)
	}
	if !strings.Contains(got, "SAFETY: must lock first") {
		t.Fatalf("SAFETY note dropped:\n%s", got)
	}
	if strings.Contains(got, "regular comment") {
		t.Fatalf("regular comment must still be stripped:\n%s", got)
	}
}

func TestStripCStyleComments_PreservesLicenseClosingOnSameLine(t *testing.T) {
	t.Parallel()
	// Block opens, whitelist line carries the closing terminator, must
	// flip inBlockComment back to false and keep the line.
	input := `/*
 * non-license filler line
 * Copyright 2026 Slimference */
package main
// regular comment
func F() {}
`
	got := stripCStyleComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("license-with-closing not preserved:\n%s", got)
	}
	if strings.Contains(got, "regular comment") {
		t.Fatalf("downstream comment must still strip:\n%s", got)
	}
}

func TestStripCStyleComments_PreservesUnterminatedLicenseBlock(t *testing.T) {
	t.Parallel()
	// A license block that runs past the input boundary; the closing */
	// is never seen so the inBlockComment flag stays true and the
	// whitelist must keep these lines around without panicking.
	input := `/*
 * Copyright 2026 Slimference
 * Licensed under MIT
`
	got := stripCStyleComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("unterminated license header lost:\n%s", got)
	}
}

func TestStripCStyleComments_PreservesMultiLineLicenseBlock(t *testing.T) {
	t.Parallel()
	input := `/*
 * Copyright 2026 Slimference
 * Licensed under MIT
 */
package main
// regular comment
func F() {}
`
	got := stripCStyleComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("multi-line license header lost:\n%s", got)
	}
	if !strings.Contains(got, "Licensed under MIT") {
		t.Fatalf("license body lost:\n%s", got)
	}
	if strings.Contains(got, "regular comment") {
		t.Fatalf("non-whitelisted comment must still be stripped:\n%s", got)
	}
}

func TestStripPythonComments_PreservesWhitelistedLine(t *testing.T) {
	t.Parallel()
	input := `# Copyright 2026 Slimference
# regular comment
import os
# SAFETY: must check before chmod
os.chmod('x', 0o600)
`
	got := stripPythonComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("license stripped:\n%s", got)
	}
	if !strings.Contains(got, "SAFETY: must check before chmod") {
		t.Fatalf("safety note stripped:\n%s", got)
	}
	if strings.Contains(got, "regular comment") {
		t.Fatalf("non-whitelisted comment must be stripped:\n%s", got)
	}
}

func TestStripHashComments_PreservesWhitelistedLine(t *testing.T) {
	t.Parallel()
	input := `# Copyright 2026 Slimference
# regular comment
echo hi
`
	got := stripHashComments(input)
	if !strings.Contains(got, "Copyright 2026 Slimference") {
		t.Fatalf("license stripped:\n%s", got)
	}
	if strings.Contains(got, "regular comment") {
		t.Fatalf("non-whitelisted stripped failed:\n%s", got)
	}
}
