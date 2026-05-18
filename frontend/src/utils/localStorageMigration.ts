/**
 * localStorage migration shim — ADR-022 Phase 2 (VaultMTG rename).
 *
 * Copies any value stored under the legacy `mtga-companion-*` key to the new
 * `vaultmtg-*` equivalent, then removes the old key so it never persists
 * across future sessions. The migration is gated by a `vaultmtg-migration-v1`
 * sentinel flag so it runs ONCE per browser profile regardless of how many
 * times the app is mounted.
 *
 * IMPORTANT (daemon scope): this module deletes the old browser localStorage
 * keys. Do NOT use the same delete-and-replace pattern for daemon-side config
 * — the daemon migration shim is a separate ticket and must retain old state
 * for backward compatibility with older daemon versions.
 */

/** Sentinel key — presence indicates the v1 migration already ran. */
export const MIGRATION_SENTINEL = 'vaultmtg-migration-v1';

/** Pairs of [legacyKey, newKey] to migrate. */
export const MIGRATION_MAP: [string, string][] = [
  ['mtga-companion-api-key', 'vaultmtg-api-key'],
  ['mtga-companion-settings-expanded', 'vaultmtg-settings-expanded'],
  ['mtga-companion-developer-mode', 'vaultmtg-developer-mode'],
  ['mtga-companion-filters', 'vaultmtg-filters'],
  // mtga-companion-meta-refresh-timestamps was already migrated by Meta.tsx
  // (PR #2077 — per-component inline migration). Listed here for reference
  // but intentionally excluded from the map so we do not overwrite an already-
  // correct new key value with a potentially-stale legacy copy.
];

/**
 * Run the one-time localStorage key migration.
 *
 * For each entry in MIGRATION_MAP:
 *  - If the legacy key is present and the new key is absent, copies the value
 *    to the new key.
 *  - Always removes the legacy key (so it never persists after migration).
 *
 * Sets the MIGRATION_SENTINEL when done so subsequent calls are no-ops.
 *
 * Safe to call multiple times — once the sentinel is set the function returns
 * immediately without touching any keys.
 */
export function runLocalStorageMigration(): void {
  try {
    // Already migrated — nothing to do.
    if (localStorage.getItem(MIGRATION_SENTINEL) !== null) {
      return;
    }

    for (const [legacyKey, newKey] of MIGRATION_MAP) {
      const legacyValue = localStorage.getItem(legacyKey);
      if (legacyValue !== null) {
        // Only write to new key when it has no value yet (don't overwrite
        // data the user may have written under the new key in a previous
        // partial migration attempt).
        if (localStorage.getItem(newKey) === null) {
          localStorage.setItem(newKey, legacyValue);
        }
        localStorage.removeItem(legacyKey);
      }
    }

    // Mark migration as complete.
    localStorage.setItem(MIGRATION_SENTINEL, '1');
  } catch {
    // localStorage may be unavailable (private browsing, quota exceeded, etc.).
    // Silently skip — the app will operate with defaults for any missing keys.
  }
}
