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
| `match-detail-timeline.json` | Game Timeline `/matches/{id}/plays/timeline` | true | game_plays not written by daemon yet; ADR-050 write path is separate |
| `match-list.json` | Match History `/history/matches` | false | REAL corpus: QuickDraft_SOS_20260526, result=win |
| `quest-list.json` | Quests `/quests/active` | false | REAL corpus: 3 quests with first_seen_at field |
| `win-rate-trend.json` | Win-Rate Trend `/matches/trends` | false | Seeded from test-data.sql player_stats rows |
| `rank-progression.json` | Rank Progression `/matches/rank-progression-timeline` | false | Seeded from test-data.sql rank_history rows |
| `deck-builder-resolution.json` | Deck Builder `/decks/{id}/cards` + `/cards?grp_ids=` | false | REAL corpus: deck-updated fixture + set_cards in test-data.sql |
| `draft-surface.json` | Draft History `/draft-sessions` | true | ADR-051 write paths not yet built; empty is the correct assertion |

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
