package updatecheck_test

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"aead.dev/minisign"

	"github.com/RdHamilton/vault-mtg/services/daemon/internal/updatecheck"
)

// TestVerifySignature_Valid verifies that a SHA256SUMS file signed with a
// known key pair passes the signature check.
func TestVerifySignature_Valid(t *testing.T) {
	// Generate a fresh key pair for the test.
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	// Write a fake SHA256SUMS file.
	sumsContent := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789  vaultmtg-daemon.pkg\n"
	dir := t.TempDir()
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	// Sign it.
	sig := minisign.Sign(priv, []byte(sumsContent))
	sigPath := filepath.Join(dir, "SHA256SUMS.minisig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	// Verify using the matching public key.
	pubKeyStr := pub.String()
	if err := updatecheck.VerifyMinisignature(sumsPath, sigPath, pubKeyStr); err != nil {
		t.Errorf("expected valid signature to pass, got: %v", err)
	}
}

// TestVerifySignature_WrongKey verifies that a SHA256SUMS file signed with
// one key is rejected when verified against a different public key.
func TestVerifySignature_WrongKey(t *testing.T) {
	_, priv1, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key1: %v", err)
	}
	// Use a separate key pair for verification to ensure wrong-key rejection.
	pub2, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key2: %v", err)
	}

	sumsContent := "checksumsdata  vaultmtg-daemon.pkg\n"
	dir := t.TempDir()
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	sig := minisign.Sign(priv1, []byte(sumsContent))
	sigPath := filepath.Join(dir, "SHA256SUMS.minisig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	// Verify with key2 (wrong key) — should fail.
	pubKeyStr := pub2.String()
	err = updatecheck.VerifyMinisignature(sumsPath, sigPath, pubKeyStr)
	if err == nil {
		t.Error("expected signature to be rejected for wrong public key, got nil")
	}
}

// TestVerifySignature_TamperedContent verifies that a tampered SHA256SUMS file
// (modified after signing) is rejected.
func TestVerifySignature_TamperedContent(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	sumsContent := "original content  vaultmtg-daemon.pkg\n"
	dir := t.TempDir()
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	sig := minisign.Sign(priv, []byte(sumsContent))
	sigPath := filepath.Join(dir, "SHA256SUMS.minisig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	// Tamper with the sums file.
	if err := os.WriteFile(sumsPath, []byte("tampered  vaultmtg-daemon.pkg\n"), 0o600); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	pubKeyStr := pub.String()
	err = updatecheck.VerifyMinisignature(sumsPath, sigPath, pubKeyStr)
	if err == nil {
		t.Error("expected tampered content to be rejected, got nil")
	}
}

// TestVerifySignature_MissingSignatureFile verifies that a missing .minisig file
// returns an error rather than silently succeeding.
func TestVerifySignature_MissingSignatureFile(t *testing.T) {
	pub, _, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	dir := t.TempDir()
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte("sums content\n"), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}
	// sigPath does not exist.
	sigPath := filepath.Join(dir, "SHA256SUMS.minisig.notexist")

	err = updatecheck.VerifyMinisignature(sumsPath, sigPath, pub.String())
	if err == nil {
		t.Error("expected error for missing signature file, got nil")
	}
}

// TestVerifyBothBeforeLaunch verifies that VerifyBoth requires BOTH the
// signature AND the checksum to pass before allowing launch. This is the
// gate Sarah PRE-1 specifies: never launch unless both checks clear.
func TestVerifyBothBeforeLaunch(t *testing.T) {
	pub, priv, err := minisign.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	installerContent := []byte("fake installer binary content")
	installerName := "vaultmtg-daemon.pkg"
	sum := sha256.Sum256(installerContent)

	dir := t.TempDir()
	installerPath := filepath.Join(dir, installerName)
	if err := os.WriteFile(installerPath, installerContent, 0o600); err != nil {
		t.Fatalf("write installer: %v", err)
	}

	sumsContent := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), installerName)
	sumsPath := filepath.Join(dir, "SHA256SUMS")
	if err := os.WriteFile(sumsPath, []byte(sumsContent), 0o600); err != nil {
		t.Fatalf("write sums: %v", err)
	}

	sig := minisign.Sign(priv, []byte(sumsContent))
	sigPath := filepath.Join(dir, "SHA256SUMS.minisig")
	if err := os.WriteFile(sigPath, sig, 0o600); err != nil {
		t.Fatalf("write sig: %v", err)
	}

	// Both checks should pass.
	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})
	err = updatecheck.VerifyBoth(d, installerPath, installerName, sumsPath, sigPath, pub.String())
	if err != nil {
		t.Errorf("expected both checks to pass, got: %v", err)
	}

	// Tamper the installer — checksum check should fail even though signature is valid.
	if err := os.WriteFile(installerPath, []byte("tampered installer"), 0o600); err != nil {
		t.Fatalf("tamper installer: %v", err)
	}
	err = updatecheck.VerifyBoth(d, installerPath, installerName, sumsPath, sigPath, pub.String())
	if err == nil {
		t.Error("expected checksum failure after tamper, got nil")
	}
}
