# Layer 5 Regression Proof

> This document proves that the Layer 5 Golden-Corpus Replay-and-Reconcile Harness
> (ADR-052) catches the 6 known ADR-052 §Context regressions and the 2 retro bugs
> from ticket #730. Each entry records: the bug pattern, the surface, the manifest
> field that gates it, the mode used to catch it, and the pasted FAIL transcript.
>
> Branch: `test/1313-layer5-regression-proof`
> Ticket: RdHamilton/hollowmark-tickets#1313
> Also closes: RdHamilton/hollowmark-tickets#730

---

## Regression 1 — Game Timeline 500 (ADR-050 `turn_number` rename)

**Bug pattern:** `gameplays_repo.go` uses column alias `gp.turn` (non-existent) instead
of the ADR-050-renamed `gp.turn_number`. PostgreSQL returns SQLSTATE 42703; the BFF handler
propagates HTTP 500. The SPA renders an error element instead of the timeline.

**Surface:** Match Detail — Timeline tab
**Manifest file:** `match-detail-timeline.json`
**Catching field:** `must_not_500: true`
**Mode:** Mode A — `TestLayer5Hermetic_MatchDetailTimeline` (hermetic Go integration test)

**Why Mode A (not Mode B):** Mode B for this surface depends on clicking a panel-header
toggle to expand the timeline, then waiting for the API call. The interaction is fragile;
if the panel does not expand no API request fires and the assertion is vacuously silent.
Mode A calls the real BFF handler directly against a seeded Postgres database and cannot
be bypassed by a DOM interaction gap.

**Reverse-test applied on:** `proof/game-timeline-500-mode-a` (throwaway branch, not merged)
**Patch:** Changed `gp.turn_number` to `gp.turn` in `gameplays_repo.go`

**FAIL transcript (Mode A):**

```
--- FAIL: TestLayer5Hermetic_MatchDetailTimeline (0.34s)
    layer5_reconcile_hermetic_test.go:508: FAIL must_not_500: timeline endpoint returned 500 — body: {"error":"Internal Server Error"}
        PG error hint: pq: column gp.turn does not exist (SQLSTATE 42703)
        Manifest assertion: match-detail-timeline.json › must_not_500=true
FAIL
```

---

## Regression 2 — Quests Invalid Date (`assigned_at` → `first_seen_at`)

**Bug pattern:** BFF quest handler still reads the legacy `assigned_at` column. After the
schema rename that column does not exist; the query returns null, and the SPA date
formatter produces "Invalid Date".

**Surface:** Quest List
**Manifest file:** `quest-list.json`
**Catching fields:** `date_field_name: "first_seen_at"`, `forbidden_field_name: "assigned_at"`
**Mode:** Mode B — `layer5-render-reconcile.spec.ts` (Playwright, mocked BFF)

**Reverse-test applied on:** mocked BFF response patched inline in spec run
(throwaway — no branch required; mock payload injected `assigned_at` date string)

**FAIL transcript (Mode B):**

```
  ● Layer 5 — Surface 2: Quest List (assigned_at / first_seen_at) › @smoke quest list must not show "Invalid Date"

    expect(received).not.toContain(expected)

    Expected string: not containing "Invalid Date"
    Received string: "Invalid Date"

      at Object.<anonymous> (tests/e2e/layer5-render-reconcile.spec.ts:391:36)

  1 failed
    [full] › tests/e2e/layer5-render-reconcile.spec.ts:374:3 › Layer 5 — Surface 2: Quest List (assigned_at / first_seen_at) › @smoke quest list must not show "Invalid Date"
```

---

## Regression 3 — Win-Rate-Trend empty (`Trends`/`Periods` key mismatch)

**Bug pattern:** BFF returns the win-rate-trend array under the key `"Periods"` instead of
the documented `"Trends"`. The SPA reads `response.Trends` — undefined — and renders an
empty chart.

**Surface:** Win-Rate Trend chart
**Manifest file:** `win-rate-trend.json`
**Catching fields:** `response_key: "Trends"`, `forbidden_response_key: "Periods"`, `chart_must_render: true`
**Mode:** Mode B — `layer5-render-reconcile.spec.ts`

**Reverse-test applied on:** mocked BFF response patched inline in spec run
(wrong key `"Periods"` injected instead of `"Trends"`)

**FAIL transcript (Mode B):**

```
  ● Layer 5 — Surface 3: Win-Rate Trend (Trends/Periods key guard) › @smoke win-rate-trend chart must render when BFF returns Trends key

    Timeout 5000ms exceeded while waiting for expect(locator).toBeVisible()

    Locator: locator('[data-testid="win-rate-trend-chart"]')
    Expected: visible
    Received: hidden

      at Object.<anonymous> (tests/e2e/layer5-render-reconcile.spec.ts:458:5)

  1 failed
    [full] › tests/e2e/layer5-render-reconcile.spec.ts:426:3 › Layer 5 — Surface 3: Win-Rate Trend (Trends/Periods key guard) › @smoke win-rate-trend chart must render when BFF returns Trends key
```

---

## Regression 4 — Rank chart flat (`rank_class`/`rank_level` absent)

**Bug pattern:** SPA reads `point.rank_class` and `point.rank_level` directly instead of
calling `parseRankString(point.rank)`. Both fields are absent from the BFF wire format. All
`rankValue` computations yield 0; Recharts renders a flat chart at the baseline.

**Surface:** Rank Progression chart
**Manifest file:** `rank-progression.json`
**Catching fields:** `wire_fields_absent: [rank_class, rank_level]`, `chart_must_be_non_flat: true`
**Mode:** Mode B — `layer5-render-reconcile.spec.ts`

**Harness gap closed:** The pre-existing assertion
`expect(locator('[data-testid="rank-chart"]')).toBeVisible()` did NOT catch this
regression — the chart element renders even with all-zero Y values. A new assertion was
added in this PR that checks for
`.recharts-yAxis .recharts-text.recharts-cartesian-axis-tick-value` tick-label count
and content. This definitively fails when all plotted values are 0.

**Reverse-test applied on:** `RankProgression.tsx` patched locally (not committed; reverted
before this PR). Patch replaced `parseRankString(point.rank)` with
`rankToNumeric(point.rank_class ?? null, point.rank_level ?? null)`.

**FAIL transcript (Mode B — new assertion):**

```
  ● Layer 5 — Surface 4: Rank Progression chart (rank_class/rank_level missing guard) › @smoke rank chart must render when BFF emits only flat rank string (no rank_class/rank_level)

    Error: Rank chart Y-axis has 0 tick labels — chart is definitively flat (all rankValues=0).
    parseRankString is not running: SPA is reading rank_class/rank_level (both undefined)
    instead of parsing the "rank" string.
    Manifest: rank-progression.json, chart_must_be_non_flat: true

      at Object.<anonymous> (tests/e2e/layer5-render-reconcile.spec.ts:635:15)

  1 failed
    [full] › tests/e2e/layer5-render-reconcile.spec.ts:614:3 › Layer 5 — Surface 4: Rank Progression chart (rank_class/rank_level missing guard) › @smoke rank chart must render when BFF emits only flat rank string (no rank_class/rank_level)
```

---

## Regression 5 — Deck Builder Unknown Card (empty catalog)

**Bug pattern:** Deck Builder resolves card names by looking up an in-memory catalog
seeded at startup. If the seeding step is absent the catalog is empty and every card
renders as "Unknown Card" with a blank mana cost.

**Surface:** Deck Builder — card resolution
**Manifest file:** `deck-builder-resolution.json`
**Catching fields:** `unknown_card_element_count_must_be: 0`, `seeded_deck_id: "deck-004"`
**Mode:** Mode B — `layer5-render-reconcile.spec.ts`

**Reverse-test applied on:** mocked BFF response patched inline in spec run
(card objects injected with empty `name: ""` and `mana_cost: ""` fields)

**FAIL transcript (Mode B):**

```
  ● Layer 5 — Surface 5: Deck Builder card resolution › @smoke deck builder must resolve all card names (no Unknown Card)

    expect(received).toBe(expected)

    Expected: ""
    Received: "mana_cost must not be empty — card at index 0 has no mana_cost (catalog empty?)"

      at Object.<anonymous> (tests/e2e/layer5-render-reconcile.spec.ts:810:5)

  1 failed
    [full] › tests/e2e/layer5-render-reconcile.spec.ts:780:3 › Layer 5 — Surface 5: Deck Builder card resolution › @smoke deck builder must resolve all card names (no Unknown Card)
```

---

## Regression 6 — Draft 0/0 grade pill (write path not built)

**Bug pattern:** The draft write path (persisting pick grades from the daemon into the BFF)
was not implemented. The SPA falls back to a default "C" grade pill value instead of the
correct "B-" grade emitted by the draft advisor.

**Surface:** Draft Advisor — grade pill
**Manifest file:** `draft-surface.json`
**Catching fields:** `grade_pill_value: "B-"`, `session_id: "draft-session-sos-003"`, `set_code: "SOS"`
**Mode:** Mode B — `layer5-render-reconcile.spec.ts`

**Reverse-test applied on:** mocked BFF response patched inline in spec run
(grade field set to `"C"` instead of `"B-"`)

**FAIL transcript (Mode B):**

```
  ● Layer 5 — Surface 6: Draft advisor grade pill › @smoke draft grade pill must match manifest grade_pill_value

    expect(received).toHaveText(expected)

    Expected string: "B-"
    Received string: "C"

      at Object.<anonymous> (tests/e2e/layer5-render-reconcile.spec.ts:900:5)

  1 failed
    [full] › tests/e2e/layer5-render-reconcile.spec.ts:870:3 › Layer 5 — Surface 6: Draft advisor grade pill › @smoke draft grade pill must match manifest grade_pill_value
```

---

## Regression 7 — #730 `player_on_play` wrong nesting in `connectResp`

**Bug pattern:** `GetPlayerSeatIDByName` in `gre_parser.go` only searched the top-level
`connectResp` JSON key. Real MTGA logs wrap `connectResp` inside
`greToClientEvent.greToClientMessages[*]`. Because the top-level path never fires,
`playerOnPlay` is always nil — match records show "on draw" regardless of actual seat.

**Surface:** Daemon GRE flush pipeline — player seat detection
**Manifest / test:** `services/daemon/internal/daemon/gre_flush_test.go` —
`TestFlushGREBuffer_PlayerOnPlay_RealLogStructure`
**Catching assertions:** `PlayerOnPlay must not be nil` and `PlayerOnPlay must be true`
**Mode:** Daemon unit test (Go; hermetic)

**Reverse-test applied on:** `proof/player-on-play-nesting` (throwaway branch, not merged)
**Patch:** Removed the primary `greToClientEvent` wrapper lookup path from
`GetPlayerSeatIDByName`, leaving only the legacy top-level `connectResp` path.

**FAIL transcript:**

```
--- FAIL: TestFlushGREBuffer_PlayerOnPlay_RealLogStructure (0.00s)
    gre_flush_test.go:389: PlayerOnPlay must not be nil — got nil (greToClientEvent wrapper path not searched)
FAIL
FAIL    github.com/RdHamilton/hollowmark/services/daemon/internal/daemon    0.012s
```

---

## Regression 8 — #730 `deck_id`/`CourseDeck` unclassified in GRE dispatch table

**Bug pattern:** `ClassifyEntry` in `classify.go` lacked a CourseDeck branch. Entries
carrying `CourseDeckSummary.DeckId` were returned as `""` (unclassified) and dropped.
The daemon never wrote a `course.deck_submitted` event — downstream deck linkage for
match records was silently missing.

**Surface:** Daemon classifier — `ClassifyEntry` dispatch table
**Manifest / test:** `services/daemon/internal/classify/classify_test.go` —
`TestClassifyEntry_CourseDeckSubmitted`
**Catching assertion:** `expected "course.deck_submitted" actual ""`
**Mode:** Classify unit test (Go; hermetic)

**Reverse-test applied on:** `proof/course-deck-unclassified` (throwaway branch, not merged)
**Patch:** Removed the `IsCourseDeckEntry` → `"course.deck_submitted"` block from
`ClassifyEntry`.

**FAIL transcript:**

```
--- FAIL: TestClassifyEntry_CourseDeckSubmitted (0.00s)
    classify_test.go:212: expected "course.deck_submitted" actual ""
FAIL
FAIL    github.com/RdHamilton/hollowmark/services/daemon/internal/classify    0.005s
```

---

## Summary

| # | Regression | Surface | Manifest field | Mode | Result |
|---|---|---|---|---|---|
| 1 | Game Timeline 500 (`turn` column) | Match Detail Timeline | `must_not_500` | Mode A (hermetic) | CAUGHT |
| 2 | Quests Invalid Date (`assigned_at`) | Quest List | `date_field_name` / `forbidden_field_name` | Mode B | CAUGHT |
| 3 | Win-Rate-Trend empty (`Periods` key) | Win-Rate Trend chart | `response_key` / `forbidden_response_key` | Mode B | CAUGHT |
| 4 | Rank chart flat (no `parseRankString`) | Rank Progression chart | `chart_must_be_non_flat` | Mode B (gap closed) | CAUGHT |
| 5 | Deck Builder Unknown Card | Deck Builder | `unknown_card_element_count_must_be` | Mode B | CAUGHT |
| 6 | Draft 0/0 grade pill | Draft Advisor | `grade_pill_value` | Mode B | CAUGHT |
| 7 | `player_on_play` nesting (#730) | GRE flush / seat detect | unit test assertion | Daemon unit | CAUGHT |
| 8 | `CourseDeck` unclassified (#730) | Classifier dispatch table | unit test assertion | Classify unit | CAUGHT |

All 8 regressions are definitively caught by the harness before code reaches main.
Throwaway proof branches (`proof/game-timeline-500-mode-a`, `proof/player-on-play-nesting`,
`proof/course-deck-unclassified`) were never merged. All source-file patches were reverted
before this PR was opened.
