# ADR-014: Legacy Parser Extraction

**Status**: Accepted
**Date**: 2026-05-07
**Author**: Architect / Product Manager
**Milestone**: v0.3.0

---

## Context

The existing log parsing logic lives in `internal/mtga/logreader/` inside the daemon module. Both the daemon (which reads Player.log directly) and the BFF (which may need to parse payloads forwarded from the daemon) need access to the same parsing primitives.

Keeping the parsers in `internal/` makes them importable only within the daemon module. This means:

1. The BFF cannot import them without copy-paste.
2. Tests for parser correctness live only in the daemon module, making cross-module validation impossible.
3. Future consumers (e.g., a backfill CLI tool) would duplicate the same code.

---

## Decision

### Extract to `pkg/logparse`

The parsing logic in `internal/mtga/logreader/` is extracted to `pkg/logparse/` — a new Go package at the root of the monorepo. `pkg/` is the conventional location for code intended to be imported by multiple modules.

The extraction must preserve all existing unit tests. Tests move from `internal/mtga/logreader/*_test.go` to `pkg/logparse/*_test.go`. CI must pass with no test regressions.

### Daemon update

After extraction, the daemon's `internal/mtga/logreader/` is reduced to a thin adapter that imports `pkg/logparse` and wires it to the daemon's file-watching infrastructure. The adapter may retain daemon-specific concerns (file handle management, tail-follow, OS-specific paths) but must delegate all parsing logic to `pkg/logparse`.

The daemon binary's import paths are updated from `internal/mtga/logreader` to `pkg/logparse` everywhere the parsing structs and functions are referenced.

### Desktop binary update

The desktop binary (if it still imports `internal/mtga/logreader` directly) must have its import paths updated in the same PR as the extraction. Leaving the desktop binary on the old path with the logic removed would break the desktop build. CI must compile all binaries — daemon, BFF, and desktop — after the extraction PR merges.

### CI build steps

The CI pipeline must:
1. Build `pkg/logparse` independently as part of the `go build ./...` step.
2. Run `pkg/logparse` unit tests in the `unit` tier (fast, no external dependencies).
3. Confirm no import cycle violations (`go vet ./...` covers this).

The existing CI job that runs daemon unit tests must be updated to reflect the new package path.

---

## Consequences

**Positive:**
- BFF and future tools can import parsing logic without duplication.
- Parser tests are centralized and run on every CI build regardless of which module triggered the run.
- Extraction creates a clean boundary between "what the log says" (pkg/logparse) and "what to do with it" (daemon classifier, BFF projector).

**Negative:**
- One-time refactor cost — all files importing the old path must be updated atomically.
- The desktop binary import path update is a required step that can be forgotten. It must be included in the same PR as the extraction, not a follow-on.

**Implementation tickets:**
- #1502: Extract `pkg/logparse` and migrate tests
- New ticket required: Update desktop binary import paths from `internal/mtga/logreader` to `pkg/logparse` and verify CI builds all three binaries
- New ticket required: Update CI pipeline — add `pkg/logparse` unit test step, update daemon test job path references

---

## Alternatives Considered

**Keep parsers in `internal/` and copy-paste into BFF**: rejected. Copy-paste creates drift — the BFF parser and daemon parser diverge silently over time.

**Move parsers to a separate Go module (separate `go.mod`)**: rejected for v0.3.0. A separate module requires versioning and `go.work` / `replace` directives. The monorepo's `pkg/` convention is sufficient and simpler for a single-engineer codebase at this scale.

**BFF re-parses Player.log directly**: not applicable. The BFF does not have access to the user's filesystem. Parsing happens only in the daemon.
