package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Christopher-Schulze/Slimference/internal/tlsdial"
)

func TestParseTLSProbeArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		args          []string
		wantProfile   string
		wantJSON      bool
		wantSave      bool
		wantReflector string
		wantCompare   string
		wantErr       bool
	}{
		{name: "default", wantProfile: tlsProbeDefaultProfile},
		{name: "profile equals", args: []string{"--profile=go_stdlib"}, wantProfile: "go_stdlib"},
		{name: "profile split", args: []string{"--profile", "chrome_131", "--json"}, wantProfile: "chrome_131", wantJSON: true},
		{name: "reflector save compare", args: []string{"--reflector=https://tls.example/probe", "--save", "--compare=/tmp/proofs"}, wantProfile: tlsProbeDefaultProfile, wantReflector: "https://tls.example/probe", wantSave: true, wantCompare: "/tmp/proofs"},
		{name: "unknown arg", args: []string{"--bogus"}, wantErr: true},
		{name: "missing profile value", args: []string{"--profile"}, wantErr: true},
		{name: "missing reflector value", args: []string{"--reflector"}, wantErr: true},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseTLSProbeArgs(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseTLSProbeArgs() error = %v", err)
			}
			if got.profile != tt.wantProfile || got.json != tt.wantJSON || got.save != tt.wantSave ||
				got.reflector != tt.wantReflector || got.comparePath != tt.wantCompare {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestRunTLSProbe_JSONChromiumDiffersFromStdlib(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runTLSProbe([]string{"--profile=chromium_stable", "--json"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d err=%s", code, errOut.String())
	}
	var report tlsProbeReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("json: %v\n%s", err, out.String())
	}
	if report.Profile != "chromium_stable" {
		t.Fatalf("profile = %q", report.Profile)
	}
	if report.RequestedProfile != "chromium_stable" {
		t.Fatalf("requested profile = %q", report.RequestedProfile)
	}
	if report.ClientHelloBytes == 0 || report.CipherSuiteCount == 0 || report.ExtensionCount == 0 {
		t.Fatalf("incomplete report: %+v", report)
	}
	if report.JA3 == "" || report.JA3Hash == "" || report.GoStdlibJA3Hash == "" {
		t.Fatalf("missing JA3 data: %+v", report)
	}
	if !report.DiffersFromGoStdlib {
		t.Fatalf("chromium profile must differ from go stdlib: %+v", report)
	}
	if !report.ExternalProofRequired {
		t.Fatalf("external proof flag must stay explicit")
	}
}

func TestRunTLSProbe_AliasAndCompare(t *testing.T) {
	dir := t.TempDir()
	report, err := buildTLSProbeReport(mustProfile(t, "go_stdlib"))
	if err != nil {
		t.Fatal(err)
	}
	body := `{"profile":"go_stdlib","ja3_hash":"` + report.JA3Hash + `","timestamp":"2026-05-01T00:00:00Z","success":true}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go_stdlib.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := runTLSProbe([]string{"--profile=go_stdlib", "--compare=" + dir, "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit = %d err=%s", code, errOut.String())
	}
	var got tlsProbeReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.CompareStatus != "match" {
		t.Fatalf("report=%+v", got)
	}

	out.Reset()
	errOut.Reset()
	code = runTLSProbe([]string{"--profile=node_stable", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("alias exit = %d err=%s", code, errOut.String())
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.AliasTarget != "chromium_stable" {
		t.Fatalf("report=%+v", got)
	}
}

func TestProbeReflectorRejectsUnsafeURL(t *testing.T) {
	t.Parallel()
	proof := probeReflector(context.Background(), mustProfile(t, "chromium_stable"), "http://example.test")
	if proof.Success || !strings.Contains(proof.Notes, "https URL") {
		t.Fatalf("proof=%+v", proof)
	}
}

func TestRunTLSProbe_TextStdlib(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runTLSProbe([]string{"--profile=go_stdlib"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d err=%s", code, errOut.String())
	}
	text := out.String()
	for _, want := range []string{"TLS Probe", "Profile:", "JA3 hash:", "External proof:"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestRunTLSProbe_InvalidProfile(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := runTLSProbe([]string{"--profile=missing"}, &out, &errOut); code != 2 {
		t.Fatalf("exit = %d out=%s err=%s", code, out.String(), errOut.String())
	}
}

func TestParseTLSClientHello_RejectsBadRecord(t *testing.T) {
	t.Parallel()
	if _, err := parseTLSClientHello([]byte{0x15, 0x03, 0x03, 0x00, 0x00}); err == nil {
		t.Fatal("expected non-clienthello record to fail")
	}
}

func TestParseTLSExtensionHelpers(t *testing.T) {
	t.Parallel()
	if got := parseServerName(mustHex(t, "000b000008746573742e646576")); got != "test.dev" {
		t.Fatalf("sni = %q", got)
	}
	if got := parseALPN(mustHex(t, "000c02683208687474702f312e31")); strings.Join(got, ",") != "h2,http/1.1" {
		t.Fatalf("alpn = %v", got)
	}
	if got := parseSupportedVersions(mustHex(t, "0403040303")); strings.Join(tlsVersionStrings(got), ",") != "TLS1.3,TLS1.2" {
		t.Fatalf("versions = %v", got)
	}
	if !isGREASE(0x0a0a) || isGREASE(0x0304) {
		t.Fatal("GREASE detector regression")
	}
}

func TestTLSProbeReportFromHello(t *testing.T) {
	t.Parallel()
	profile, err := tlsdial.ResolveProfile("go_stdlib")
	if err != nil {
		t.Fatal(err)
	}
	hello := tlsClientHello{
		recordVersion:    0x0303,
		legacyVersion:    0x0303,
		cipherSuites:     []uint16{0x0a0a, 0x1301},
		extensions:       []uint16{0x0a0a, 0, 16, 43},
		supportedGroups:  []uint16{0x0a0a, 29},
		ecPointFormats:   []byte{0},
		alpn:             []string{"h2"},
		supportedVersion: []uint16{0x0304, 0x0303},
	}
	report := tlsProbeReportFromHello(profile, []byte{1, 2, 3}, hello)
	if report.JA3 != "771,4865,0-16-43,29,0" {
		t.Fatalf("ja3 = %q", report.JA3)
	}
	if report.JA3Hash == "" || report.RecordVersion != "TLS1.2" {
		t.Fatalf("bad report: %+v", report)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustProfile(t *testing.T, name string) tlsdial.Profile {
	t.Helper()
	profile, err := tlsdial.ResolveProfile(name)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}
