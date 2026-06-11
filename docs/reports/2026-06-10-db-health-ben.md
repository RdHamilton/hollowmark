# Database Health Report — 2026-06-10 (Ben, wave v0.4.3 kickoff)

## Environment
- DB: **prod** (`vaultmtg` database — `vaultmtg-postgres.cujuc62msfbv.us-east-1.rds.amazonaws.com`)
- DB IP: `172.31.21.199`
- Connection method: SSM RunShellScript via EC2 `i-0226bf51fcf09b506` (prod EC2, `vaultmtg_app` role)
- Verified with: `SELECT current_database(), inet_server_addr();`

## Slowest Queries
| Query (truncated) | Mean Exec (ms) | Calls | Total Exec (ms) |
|---|---|---|---|
| `<insufficient privilege>` (superuser query) | 21310.53 | 1 | 21311 |
| `<insufficient privilege>` (superuser query) | 408.58 | 1 | 409 |
| `WITH sc AS (SELECT DISTINCT ON (arena_id)... FROM set_cards) SELECT ... FROM card_inventory ci LEFT JOIN sc ... WHERE ci.account_id = $1` | 367.81 | 5 | 1839 |
| `<insufficient privilege>` (superuser query) | 312.03 | 1 | 312 |
| `<insufficient privilege>` (superuser query) | 303.49 | 1 | 303 |

**Notes:**
- The `<insufficient privilege>` rows are superuser-initiated queries not visible to `vaultmtg_app`. These are likely maintenance/vacuum/analyze statements from RDS or the `rds_superuser` role — normal.
- The card_inventory join query (367ms mean, 5 calls) is the top **app-visible** slow query. It performs a `DISTINCT ON (arena_id)` window across `set_cards` on every invocation. Worth monitoring — 5 calls is low, but if cardinality grows with more user collections this could degrade.

## Index Usage Issues
| Table | seq_scan | idx_scan | idx_usage_pct |
|---|---|---|---|
| `schema_migrations` | 711,729 | 14 | 0.0% |
| `daemon_api_keys` | 115,276 | 135 | 0.1% |
| `users` | 60,861 | 3,139 | 4.9% |
| `accounts` | 22,225 | 4 | 0.0% |
| `sync_hashes` | 16,823 | 1,176 | 6.5% |
| `mtgzone_archetypes` | 5,879 | 4,347 | 42.5% |
| `daemon_events` | 2,308 | 203,577 | 98.9% |
| `set_cards` | 1,490 | 7,506,181 | 100.0% |
| `decks` | 833 | 1,586 | 65.6% |
| `sets` | 461 | 36 | 7.2% |

**Notable findings:**

1. **`schema_migrations` — 711K seq_scans, 0% index** — Tiny table (~120 rows), full scan is effectively free. Not actionable.

2. **`daemon_api_keys` — 115K seq_scans, 0.1% index** — High seq_scan relative to idx_scan. If token-validation lookups query by key value without an index, this is a hot path with no index. Flag for audit.

3. **`users` — 60K seq_scans, 4.9% index** — High sequential scan count; likely missing index on `clerk_user_id` or `email` for lookup paths.

4. **`accounts` — 22K seq_scans, near-zero index** — Similar pattern to users. Lookup by non-PK column without an index suspected.

5. **`sync_hashes` — 16K seq_scans, 6.5% index** — Card sync Lambda may be doing full scans; expected on small tables but worth auditing if sync frequency increases.

## Table Sizes
| Table | Total Size |
|---|---|
| `set_cards` | 51 MB |
| `daemon_events` | 20 MB |
| `card_inventory` | 6,144 kB |
| `draft_card_ratings` | 2,568 kB |
| `mtgzone_archetype_cards` | 1,440 kB |
| `projection_errors` | 360 kB |
| `matches` | 248 kB |
| `decks` | 208 kB |
| `deck_cards` | 184 kB |
| `quests` | 176 kB |

**Notes:**
- All tables well within healthy range on `db.t3.micro`.
- `daemon_events` at 20 MB will grow with user base — confirm pruning/archival policy exists.
- `projection_errors` at 360 kB — verify TTL/pruning logic to prevent silent accumulation.

## Recommendations
| Finding | Severity | Action |
|---|---|---|
| `daemon_api_keys` 0.1% idx_usage, 115K seq_scans | Medium | File index audit ticket via Pam |
| `users` 4.9% idx_usage, 60K seq_scans | Medium | File index audit ticket via Pam — audit clerk_user_id/email indexes |
| `accounts` near-zero idx_usage, 22K seq_scans | Medium | File index audit ticket via Pam |
| card_inventory join query 367ms mean | Low-Medium | Monitor; DISTINCT ON set_cards(arena_id) may need index if call count grows |
| `daemon_events` 20 MB growing | Low | Confirm pruning/archival policy |
