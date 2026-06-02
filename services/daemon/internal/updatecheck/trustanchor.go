package updatecheck

import (
	"fmt"
	"os"

	"aead.dev/minisign"
)

// EmbeddedPublicKey is the minisign public key hard-coded into the daemon binary.
// This is the trust anchor: the GoReleaser `signs:` step signs SHA256SUMS with
// the private key whose matching public key is embedded here. The daemon verifies
// every downloaded SHA256SUMS.minisig against this key before executing any
// installer.
//
// TRUST ANCHOR CHOICE (Sarah PRE-1):
//
// Option (a) — minisign detached signature of SHA256SUMS verified against an
// embedded public key — was chosen over option (b) (GitHub artifact attestation
// via Sigstore) because:
//
//  1. The end-user machine has no `gh` CLI (spec constraint).
//  2. The Sigstore/cosign verification library adds ~30MB to the binary.
//     minisign verification is 100% in-process with a ~40kB dep (aead.dev/minisign).
//  3. The trust root is the VaultMTG release key, not a third-party OIDC issuer.
//  4. minisign public keys are 52 bytes (base64) — safe to embed as a constant.
//
// To rotate the signing key: generate a new keypair with `minisign -G`,
// update this constant to the new public key, cut a new daemon/v* release.
// Old daemon binaries will refuse installers signed with the new key — this is
// intentional. Users must upgrade through the BFF-served update path or via
// direct download.
//
// IMPORTANT: This is a placeholder value. The build pipeline must inject the real
// production public key via ldflags:
//
//	-X github.com/RdHamilton/vault-mtg/services/daemon/internal/updatecheck.EmbeddedPublicKey=<key>
//
// until the key is finalized and baked in as a literal.
var EmbeddedPublicKey = "PLACEHOLDER_REPLACE_WITH_REAL_MINISIGN_PUBKEY"

// VerifyMinisignature verifies that sumsFile was signed by the private key
// corresponding to the embedded public key. sigFile is the .minisig file.
// The public key to verify against can be overridden via pubKeyOverride (used
// in tests; pass "" to use EmbeddedPublicKey).
func VerifyMinisignature(sumsFile, sigFile, pubKeyOverride string) error {
	pubKeyStr := EmbeddedPublicKey
	if pubKeyOverride != "" {
		pubKeyStr = pubKeyOverride
	}

	var pub minisign.PublicKey
	if err := pub.UnmarshalText([]byte(pubKeyStr)); err != nil {
		return fmt.Errorf("parse embedded public key: %w", err)
	}

	message, err := os.ReadFile(sumsFile)
	if err != nil {
		return fmt.Errorf("read sums file for signature verification: %w", err)
	}

	sig, err := os.ReadFile(sigFile)
	if err != nil {
		return fmt.Errorf("read signature file: %w", err)
	}

	if !minisign.Verify(pub, message, sig) {
		return fmt.Errorf("signature verification failed: SHA256SUMS signature is invalid")
	}

	return nil
}

// VerifyBoth runs both the minisign signature check and the SHA-256 checksum
// check against the downloaded installer. BOTH must pass before the caller
// may invoke LaunchInstaller. This is the enforcement of ADR-036 I-10.
//
// Parameters:
//   - d: the Downloader (for VerifyChecksum)
//   - installerPath: path to the downloaded installer binary
//   - installerFilename: the base filename as it appears in SHA256SUMS
//   - sumsPath: path to the downloaded SHA256SUMS file
//   - sigPath: path to the downloaded SHA256SUMS.minisig file
//   - pubKeyOverride: override the embedded key (tests only; pass "" for production)
func VerifyBoth(d *Downloader, installerPath, installerFilename, sumsPath, sigPath, pubKeyOverride string) error {
	// Step 1: verify minisign signature on SHA256SUMS.
	// This authenticates the entire checksum file — an attacker who can swap the
	// installer must also forge the signature, which requires the release private key.
	if err := VerifyMinisignature(sumsPath, sigPath, pubKeyOverride); err != nil {
		return fmt.Errorf("trust anchor: %w", err)
	}

	// Step 2: verify SHA-256 of the installer against the authenticated sums file.
	// Doing this AFTER the signature check means we only trust hashes that were
	// attested by the VaultMTG release key.
	if err := d.VerifyChecksum(installerPath, installerFilename, sumsPath); err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	return nil
}
