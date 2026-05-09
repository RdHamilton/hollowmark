# 2026-05-09 — feat(bff): DraftAnalytics, RankProgression, ResultBreakdown, Collection endpoints (#1514)

## Type
feature

## Summary
Adds four new BFF analytics endpoints for issue #1514, completing the stats surface alongside #1513:

- `GET /api/v1/stats/draft-analytics` — per-draft pick efficiency and record data, cursor-paginated by `started_at DESC`. Optional `set_code` filter. Uses the ADR-018 keyset pagination standard (`listing.BuildEnvelope`).
- `GET /api/v1/stats/rank-progression` — rank changes over time (matches with non-null `rank_before` or `rank_after`), cursor-paginated by `occurred_at DESC`. Optional `format` filter.
- `GET /api/v1/stats/result-breakdown` — aggregate wins/losses/draws grouped by format. Optional `format` filter. Returns `{"data": [...]}` (not paginated — small aggregate).
- `GET /api/v1/collection` — v1 alias for the existing `/api/v2/collection` cursor-paginated endpoint (both served by `ListV2Handler.GetCollection`).

All four endpoints are mounted inside the `ClerkAuthMiddl`-protected group and the `APIKeyAuthMiddl` fallback group. Multi-tenant scoping enforced at the SQL layer via `account_id`.

## Files Changed
- `services/bff/internal/storage/repository/stats_repo.go` — added `DraftAnalyticsRow`, `RankProgressionRow`, `ResultBreakdownRow` types and `ListDraftAnalytics`, `ListRankProgression`, `GetResultBreakdown` methods with 4-case keyset switch patterns
- `services/bff/internal/api/handlers/stats.go` — added `DraftAnalyticsReader`, `RankProgressionReader`, `ResultBreakdownReader` interfaces; added `WithDraftAnalytics`, `WithRankProgression`, `WithResultBreakdown` setters; implemented `GetDraftAnalytics`, `GetRankProgression`, `GetResultBreakdown` handlers
- `services/bff/cmd/main.go` — wired `WithDraftAnalytics(statsRepo).WithRankProgression(statsRepo).WithResultBreakdown(statsRepo)`; registered all 6 stats routes and `/api/v1/collection` alias in both Clerk and APIKey route groups
- `services/bff/internal/api/handlers/stats_test.go` — added 15 new handler tests (401 unauthorized, no-account empty response, happy path, invalid params 400, 500 on repo error) for the 3 new endpoints

## Closes
#1514
