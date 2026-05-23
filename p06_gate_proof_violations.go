// p06_gate_proof_violations.go — EC-13 fixture file.
// This file intentionally contains violations to demonstrate P-06 CI gates.
// It is NOT real code — the entire file is deleted in the follow-up "clean" commit.
//
// Gate #1 — Secrets scan: fake AWS access key below (AKIA pattern)
// Gate #8 — Stale-repo grep: MTGA-Companion reference below
// Gate #3 — gofumpt: deliberate formatting violation (blank line between func and body)
// Gate #2 — go vet: unused import (fmt below — compiler will catch as build error)
//
// NOTE: go vet / gofumpt only fire when the detect-changes filter marks bff=true
// or logparse=true. This file is at repo root (not in a module), so it will NOT
// be picked up by go vet/gofumpt CI (those jobs run inside module dirs). The
// secrets-scan and stale-repo-grep jobs cover all .go files at repo root.

package main // intentional: no real main package at repo root, but file needs a package clause

// Gate #1 — secrets scan: fake AWS key (AKIAFAKEKEY0123456789 matches AKIA[A-Z0-9]{16})
// FAKE_KEY = "AKIAFAKEKEY0123456789"  <-- grep target

// Gate #8 — stale-repo-grep: old repo name MTGA-Companion appears below
// legacy binary: MTGA-Companion

func unused() {
	// Gate #3 — gofumpt: extra blank line between func declaration and body violates gofumpt
	// (gofumpt only runs on module dirs, so this serves as documentation of the violation type)
	_ = "placeholder"
}
