package codexroute

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveCertification_MkdirAllError(t *testing.T) {
	home := t.TempDir()
	slimDir := filepath.Join(home, ".slimference")
	if err := os.WriteFile(slimDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveCertification(home, CertificationState{Passed: true}); err == nil {
		t.Fatal("SaveCertification should fail when .slimference is a file")
	}
}

func TestSaveBridgeProof_MkdirAllError(t *testing.T) {
	home := t.TempDir()
	slimDir := filepath.Join(home, ".slimference")
	if err := os.WriteFile(slimDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveBridgeProof(home, BridgeProofState{Passed: true}); err == nil {
		t.Fatal("SaveBridgeProof should fail when .slimference is a file")
	}
}

func TestSaveRecertState_MkdirAllError(t *testing.T) {
	home := t.TempDir()
	slimDir := filepath.Join(home, ".slimference")
	if err := os.WriteFile(slimDir, []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveRecertState(home, RecertState{Status: "running"}); err == nil {
		t.Fatal("SaveRecertState should fail when .slimference is a file")
	}
}

func TestLoadCertification_ReadFilePermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission test not meaningful as root")
	}
	home := t.TempDir()
	path := CertificationPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, _, err := LoadCertification(home); err == nil {
		t.Fatal("LoadCertification should fail with permission error")
	}
}

func TestLoadBridgeProof_ReadFilePermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission test not meaningful as root")
	}
	home := t.TempDir()
	path := BridgeProofPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, _, err := LoadBridgeProof(home); err == nil {
		t.Fatal("LoadBridgeProof should fail with permission error")
	}
}

func TestLoadRecertState_ReadFilePermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission test not meaningful as root")
	}
	home := t.TempDir()
	path := RecertStatePath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })
	if _, _, err := LoadRecertState(home); err == nil {
		t.Fatal("LoadRecertState should fail with permission error")
	}
}
