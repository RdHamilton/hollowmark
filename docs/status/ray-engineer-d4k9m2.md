# Ray — Task Status (engineer, d4k9m2)
**Updated**: 2026-06-02
**Task**: feat/653-release-channel-wiring: release pipeline channel wiring (ADR-049) + ADR-033 amendment (#658)
**Status**: In Progress

## Progress
- [x] Read ADR-049, issue #653/#651, current .goreleaser.yml, daemon-release.yml, postinstall, installer.nsi
- [x] Branched from origin/main (includes #2891 signs.env fix)
- [ ] Add VAULTMTG_CHANNEL resolution step to daemon-release.yml goreleaser job
- [ ] Add VAULTMTG_CHANNEL env to GoReleaser run step + -X install.Channel ldflag
- [ ] Add VAULTMTG_CHANNEL to build-macos job + ldflags
- [ ] Add __VAULTMTG_CHANNEL__ substitution in Create .pkg installer step
- [ ] Add -DCHANNEL to makensis NSIS hook in .goreleaser.yml
- [ ] Template binary: in .goreleaser.yml on VAULTMTG_CHANNEL
- [ ] Add fail-closed VAULTMTG_CHANNEL validation step
- [ ] Add __VAULTMTG_CHANNEL__ placeholder to postinstall + installer.nsi
- [ ] Write ADR-033 amendment (doc-only, vault-mtg-docs)
- [ ] goreleaser check
- [ ] Open PR

## Blockers
None
