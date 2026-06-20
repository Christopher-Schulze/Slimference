package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseRecoveryContractMatrixFlags(t *testing.T) {
	t.Parallel()
	flags, err := parseRecoveryContractMatrixFlags([]string{"--json", "--fail-on-product-gaps"})
	if err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	if flags.outputFormat != outputJSON || !flags.failOnProductGaps {
		t.Fatalf("unexpected flags: %+v", flags)
	}
	if _, err := parseRecoveryContractMatrixFlags([]string{"--json", "--json"}); err == nil {
		t.Fatal("expected duplicate output flag error")
	}
	if _, err := parseRecoveryContractMatrixFlags([]string{"--bad"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
	if _, err := parseRecoveryContractMatrixFlags([]string{"extra"}); err == nil {
		t.Fatal("expected positional argument error")
	}
}

func TestBuildRecoveryContractMatrixReport(t *testing.T) {
	t.Parallel()
	report := buildRecoveryContractMatrixReport()
	if report.Summary.Rows < 40 {
		t.Fatalf("expected registry-backed matrix rows, got %d", report.Summary.Rows)
	}
	if report.Summary.ProductGaps != 0 {
		t.Fatalf("default product rows must not have gaps: %+v", report.ProductGapRows)
	}
	if report.Summary.BlockedRows == 0 {
		t.Fatal("expected non-default research/candidate blocked rows")
	}
	if report.Summary.CommandOutputRows != 3 {
		t.Fatalf("command-output-first rows=%d, want 3", report.Summary.CommandOutputRows)
	}
	if report.Summary.Layer0RegistryRows == 0 {
		t.Fatal("expected Layer-0 registry rows")
	}
	assertRecoveryRow(t, report.Rows, "t418_command_output_first_archive_stdout", true, nil)
	assertRecoveryRow(t, report.Rows, "t417_class_b_server_state_recovery_gate", false, []string{"not_default_eligible"})
	assertRecoveryRow(t, report.Rows, "t408_backend_honored_reference_lane3", false, []string{"not_default_eligible", "missing_mechanical_recovery"})
}

func TestRunRecoveryContractMatrixJSONAndGate(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runRecoveryContractMatrix([]string{"--json", "--fail-on-product-gaps"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRecoveryContractMatrix exit=%d stderr=%q", code, stderr.String())
	}
	var report recoveryContractMatrixReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, stdout.String())
	}
	if report.Summary.ProductGaps != 0 || report.Summary.BlockedRows == 0 {
		t.Fatalf("unexpected summary: %+v", report.Summary)
	}
}

func TestRunRecoveryContractMatrixText(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runRecoveryContractMatrix(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runRecoveryContractMatrix exit=%d stderr=%q", code, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"T419 recovery-contract matrix",
		"High-impact next:",
		"T417: unlock Class-B/server-state continuation",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("text output missing %q:\n%s", want, out)
		}
	}
}

func assertRecoveryRow(t *testing.T, rows []recoveryContractRow, id string, wantReady bool, wantBlockers []string) {
	t.Helper()
	for _, row := range rows {
		if row.ID != id {
			continue
		}
		if row.ProductReady != wantReady {
			t.Fatalf("%s ProductReady=%v want %v blockers=%v", id, row.ProductReady, wantReady, row.Blockers)
		}
		for _, blocker := range wantBlockers {
			if !stringSliceContains(row.Blockers, blocker) {
				t.Fatalf("%s missing blocker %q in %v", id, blocker, row.Blockers)
			}
		}
		return
	}
	t.Fatalf("row %s not found", id)
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
