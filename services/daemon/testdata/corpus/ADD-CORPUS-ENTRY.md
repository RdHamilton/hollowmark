# Add-Corpus-Entry Runbook

**Ticket**: vmt-t#697
**ADR**: ADR-052 (Layer 5 Golden-Corpus Replay-and-Reconcile Harness)
**Owner**: Tim (test infrastructure)
**Bob review required**: Yes (replay injector owner)
**Ray review required**: Yes (ADR-052 author)

This runbook documents how to add a new recorded game session to the Layer 5
golden corpus. Follow it exactly. Do not fabricate fixture data.

---

## Prerequisites

- Go toolchain installed, `GOPRIVATE=github.com/RdHamilton/vault-mtg` set
- Local Postgres running with the vault-mtg schema (`DATABASE_URL` set)
- BFF running locally (`BFF_URL` set, `BFF_TOKEN` set to a valid test token)
- `jq` installed (used by `regenerate.sh`)

---

## Step 1 — Capture a live session

Play at least one match in MTGA. For draft data, play a full draft on the
**fixed daemon** (built from source — it emits `session_id` on draft events;
historical pre-session_id logs cannot project draft sessions).

After the session, copy the log:

```bash
# macOS
SNAPSHOT_DIR="$HOME/mtga-log-backups/corpus-snapshot-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$SNAPSHOT_DIR/daemon-archives"
cp ~/Library/Logs/Wizards\ Of\ The\ Coast/MTGA/Player.log \
   "$SNAPSHOT_DIR/daemon-archives/Player_$(date -u +%Y%m%dT%H%M%SZ).log"
```

**What projects and what does not:**

| Event class | Projects? | Notes |
|---|---|---|
| `match.completed` | Yes | Requires non-empty `format` and `match_id` |
| `quest.progress` | Yes | Upserts by `quest_id` (higher progress wins) |
| `deck.updated` | Yes | Upserts by `deck_id` |
| `draft.pack` / `draft.pick` | Yes | Only on fixed daemon that emits `session_id` |
| `player.authenticated` | No | Skipped by the injector — used for clientId extraction only |
| `greToClientEvent` | No | GRE pipeline not yet built |

---

## Step 2 — Run the replay injector

```bash
export DATABASE_URL="postgres://ramonehamilton@localhost:5432/vault_test?sslmode=disable"
export LAYER5_CORPUS_SNAPSHOT_DIR="$SNAPSHOT_DIR/daemon-archives"
GOPRIVATE=github.com/RdHamilton/vault-mtg \
go test -v -tags layer5 -run TestLayer5ReplayInjector_Reconstruct ./services/bff/
```

Expected output (example):

```
[layer5] corpus scan complete:
  files scanned:     N
  matches parsed:    N
  quests parsed:     N
  decks parsed:      N
  draft packs:       N
  draft picks:       N
  events inserted:   N
  insert errors:     0
[layer5] projected rows:
  matches:           N
  quests:            N
  decks:             N
  projection_errors: 0
--- PASS
```

**AC: `projection_errors: 0` is required.** A non-zero count means the projection
worker sent events to the DLQ — investigate before proceeding.

---

## Step 3 — Verify idempotency

Run the determinism test. The second replay must insert 0 new events and produce
identical row counts:

```bash
GOPRIVATE=github.com/RdHamilton/vault-mtg \
go test -v -tags layer5 -run TestLayer5ReplayDeterminism ./services/bff/
```

**AC: `run 2 complete: 0 events inserted` — this proves the ON CONFLICT
deduplication is working and manifests will be stable.**

If the test fails with non-zero events on run-2, check that `event_id` derivation
in `replayCorpusIntoAccount` uses the fixed `clientID + filename` seed (not
wall-clock time). Any randomness in the event_id breaks idempotency.

---

## Step 4 — Regenerate the expected-truth manifests

```bash
export BFF_URL="http://localhost:8080"
export BFF_TOKEN="<your-test-clerk-token>"
./tools/layer5-manifest-gen/regenerate.sh
```

The script updates `services/daemon/testdata/corpus/layer5-expected/*.json`.
Review the diff carefully:

- `match-list.json`: `corpus_match_count` and `min_row_count` must reflect the
  new total match count.
- `quest-list.json`: `corpus_quest_count` and `min_quest_count` must reflect the
  new total quest count.
- `draft-surface.json`: if new draft sessions projected, update
  `expected_empty: false` and add assertion fields.
- `match-detail-timeline.json`: if GRE write path now built, update
  `expected_empty: false`.

**Do NOT commit manifests with wall-clock timestamps.** The `corpus_promotion`
block uses the PR date (`YYYY-MM-DD`), not a timestamp.

---

## Step 5 — Verify Mode B locally

Run the Layer 5 Playwright spec against the seeded BFF:

```bash
cd frontend
npx playwright test tests/e2e/layer5-render-reconcile.spec.ts --project=smoke
```

All `@smoke` tests must pass. Any failure in the six regression surfaces is a
blocking defect (not a known-yellow).

---

## Step 6 — Sanitise and commit

**The raw log file is NEVER committed.** Only the manifests in
`services/daemon/testdata/corpus/layer5-expected/` are committed.

Update `MANIFEST` if you are also promoting new `player-log/` or `daemon-emit/`
fixtures. Update `mtga-version.txt` if the MTGA client version changed.

Stage only:
```bash
git add services/daemon/testdata/corpus/layer5-expected/*.json
git add services/daemon/testdata/corpus/MANIFEST      # if updated
git add services/daemon/testdata/corpus/mtga-version.txt  # if updated
```

---

## Step 7 — Open a PR

Follow the standard PR template. Include:

- The injector output (Steps 2–3) as the Local Verification transcript.
- The manifest diff as evidence of the corpus state change.
- `**Agent**: tim` in the PR body.

The PR triggers the S-07 security review gate if any fixture files are included.
Bob and Ray must review and comment sign-off per #697 AC5.

---

## Handling partial captures

If your session lacks some event types, use `expected_empty: true` with a
comment explaining the gap. Never fabricate fixture data to fill a gap.

| Gap | Correct action |
|---|---|
| No draft data (old log) | Keep `draft-surface.json` `expected_empty: true` with the session_id note |
| No GRE timeline data | Keep `match-detail-timeline.json` `expected_empty: true` |
| No collection snapshot | Keep `collection-updated.log` FORMAT-CONFIRMED until a collection-screen session is captured |

---

## Idempotency invariant

The same corpus replay must produce the same manifests on every run. Guarantee:

1. `event_id` is derived from `clientID + filename + 1-based sequence` (stable).
2. `INSERT … ON CONFLICT DO NOTHING` deduplicates on second replay.
3. `OccurredAt` is a fixed deterministic epoch (`2026-06-02T00:00:00Z`) — not `time.Now()`.

Any non-determinism (e.g. UUIDs in API responses) must be stripped from the
manifest `assertions` block before commit.

---

## Related

- ADR-052: `vault-mtg-docs/engineering/architecture/adr/2026-06-ADR-052-golden-corpus-replay-reconcile-harness.md`
- Injector: `services/bff/layer5_reconcile_test.go`
- Manifests: `services/daemon/testdata/corpus/layer5-expected/`
- Regeneration tool: `tools/layer5-manifest-gen/regenerate.sh`
- Mode B spec: `frontend/tests/e2e/layer5-render-reconcile.spec.ts`
