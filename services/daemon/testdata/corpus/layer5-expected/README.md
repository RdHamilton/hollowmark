# Layer 5 Expected-Truth Manifest

This directory contains the expected-truth manifest for the Layer 5 Golden-Corpus
Replay-and-Reconcile Harness (ADR-052). Each JSON file asserts the rendered surface
values that the SPA must produce when the BFF is seeded with real projected corpus
data.

## Manifest format

Each file follows this envelope:

```json
{
  "corpus_mtga_version": "2026.59.20.4846.1277160",
  "corpus_provenance": "REAL | REAL-DERIVED | SYNTHETIC",
  "expected_empty": false,
  "assertions": [
    {
      "surface": "<surface-name>",
      "bff_endpoint": "<endpoint>",
      "fields": { ... }
    }
  ]
}
```

`expected_empty: true` means the assertion passes on an empty BFF response. This
encodes corpus gaps (e.g. draft write paths not yet built per ADR-051) so the
harness does not generate false failures.

## Files

| File | Surface | expected_empty | Notes |
|---|---|---|---|
| `match-detail-timeline.json` | Game Timeline `/matches/{id}/plays/timeline` | false | 7 corpus matches project; 1128 game_plays from GRE session manager flush (#807 fix + #808 replay) |
| `match-list.json` | Match History `/history/matches` | false | REAL corpus: 12 unique matches from 36-log snapshot; first row QuickDraft_SOS_20260526, result=win |
| `quest-list.json` | Quests `/quests/active` | false | REAL corpus: 5 unique quests from 36-log snapshot (3 named SOS quests + 2 additional) |
| `win-rate-trend.json` | Win-Rate Trend `/matches/trends` | false | SYNTHETIC: seeded from test-data.sql player_stats rows |
| `rank-progression.json` | Rank Progression `/matches/rank-progression-timeline` | false | SYNTHETIC: seeded from test-data.sql rank_history rows |
| `deck-builder-resolution.json` | Deck Builder `/decks/{id}/cards` + `/cards?grp_ids=` | false | REAL corpus: 4 decks project; deck-004 for card assertions |
| `draft-surface.json` | Draft Analytics `/drafts/{id}/analysis` + Draft History `/history/drafts` | false | Mode B grade-pill assertion (#829): seeded fixture draft-session-sos-003 (3W-3L SOS, overall_grade=B-); draft write path not yet built but grade-pill uses seeded-fixture bridge pattern |

## Full Corpus Promotion (2026-06-02)

The manifests above were promoted from `corpus-snapshot-20260602T170441Z/` (36 log files) via Bob's replay injector (#2919).

| Surface | Reconstructed | Notes |
|---|---|---|
| Matches | 12 | 200 parsed → 12 unique IDs after ON CONFLICT dedup |
| Quests | 5 | 119 parsed → 5 unique quest IDs after upsert |
| Decks | 4 | 82 parsed → 4 unique deck IDs |
| Draft sessions | 0 | 1,142 packs + 1,136 picks parse; 0 project — historical logs predate session_id |
| Game plays (GRE) | 1128 | GRE session manager wired into replay package (#807 fix + #808 replay support); 1128 game_plays from 36 raw log files |

Determinism: `TestLayer5ReplayDeterminism` PASS — run-1 and run-2 produce identical row sets.

To advance draft surfaces: play new drafts on the fixed daemon, capture the log, promote per `ADD-CORPUS-ENTRY.md`, and regenerate.

## Update protocol

This manifest MUST be updated in the same PR as any corpus refresh (ADR-041 G3
protocol). To regenerate:

```bash
./tools/layer5-manifest-gen/regenerate.sh
```

The regeneration script seeds a local Postgres (same migrations as CI), runs the
BFF read endpoints, and writes the manifest files. The diff shows exactly what the
MTGA patch changed — review it before merging.

A corpus-refresh PR that does not include a manifest update is blocked at review.
