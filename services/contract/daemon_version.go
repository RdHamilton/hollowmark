package contract

// DaemonVersionResponse is the wire type returned by GET /api/v1/daemon/version.
// The endpoint requires no authentication; version metadata is public.
type DaemonVersionResponse struct {
	Latest      string `json:"latest"`
	ReleasedAt  string `json:"released_at"`
	DownloadURL string `json:"download_url"`
	// Sha256SumsURL is the download URL for the SHA256SUMS file for this release.
	// Empty when not yet available (BFF pre-update or release missing the asset).
	Sha256SumsURL string `json:"sha256sums_url"`
	// AttestationURL is the download URL for the SHA256SUMS.minisig detached
	// minisign signature file. Empty when not yet available.
	AttestationURL string `json:"attestation_url"`
	// MacOSInstallerURL is the download URL for the macOS universal .pkg installer
	// asset (vaultmtg-daemon-darwin-universal.pkg). Empty when not available.
	MacOSInstallerURL string `json:"macos_installer_url,omitempty"`
	// WindowsInstallerURL is the download URL for the Windows amd64 .exe installer
	// asset (vaultmtg-daemon-windows-amd64.exe). Empty when not available.
	WindowsInstallerURL string `json:"windows_installer_url,omitempty"`
	Changelog           string `json:"changelog"`
}
