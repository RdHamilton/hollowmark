/**
 * Unit tests for localStorageMigration.ts
 *
 * Covers V1 (mtga-companion-* → vaultmtg-*):
 *  - Old-only case: legacy keys present, new keys absent → migrated correctly
 *  - Already-migrated case: sentinel already set → no-op
 *  - Empty case: no legacy keys present → sentinel is still written
 *  - Partial overlap: new key already exists → old value NOT overwritten
 *  - Legacy key removed after migration in every case
 *  - Meta key carve-out: mtga-companion-meta-refresh-timestamps NOT in map
 *
 * Covers V2 (vaultmtg-* → hollowmark-*):
 *  - Old-only case: old keys present, new keys absent → copied correctly
 *  - Idempotent: sentinel gates re-runs (no double-write)
 *  - Empty case: no old keys → sentinel still written, no new keys created
 *  - Both-present: new key already has a value → old value NOT overwritten
 *  - No-delete: old vaultmtg-* keys survive after migration (D16/AC8)
 *  - Sentinel: MIGRATION_V2_SENTINEL set after run
 */

import { describe, it, expect, beforeEach } from 'vitest';
import {
  runLocalStorageMigration,
  MIGRATION_SENTINEL,
  MIGRATION_MAP,
  runLocalStorageMigrationV2,
  MIGRATION_V2_SENTINEL,
  MIGRATION_V2_MAP,
} from './localStorageMigration';

describe('localStorageMigration', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  // -----------------------------------------------------------------------
  // MIGRATION_MAP shape
  // -----------------------------------------------------------------------

  describe('MIGRATION_MAP', () => {
    it('contains exactly the four expected key pairs', () => {
      const legacyKeys = MIGRATION_MAP.map(([legacy]) => legacy);
      expect(legacyKeys).toContain('mtga-companion-api-key');
      expect(legacyKeys).toContain('mtga-companion-settings-expanded');
      expect(legacyKeys).toContain('mtga-companion-developer-mode');
      expect(legacyKeys).toContain('mtga-companion-filters');
      expect(legacyKeys).toHaveLength(4);
    });

    it('maps each legacy key to the correct vaultmtg-* counterpart', () => {
      const map = Object.fromEntries(MIGRATION_MAP);
      expect(map['mtga-companion-api-key']).toBe('vaultmtg-api-key');
      expect(map['mtga-companion-settings-expanded']).toBe('vaultmtg-settings-expanded');
      expect(map['mtga-companion-developer-mode']).toBe('vaultmtg-developer-mode');
      expect(map['mtga-companion-filters']).toBe('vaultmtg-filters');
    });

    it('does NOT include mtga-companion-meta-refresh-timestamps (handled by Meta.tsx)', () => {
      const legacyKeys = MIGRATION_MAP.map(([legacy]) => legacy);
      expect(legacyKeys).not.toContain('mtga-companion-meta-refresh-timestamps');
    });
  });

  // -----------------------------------------------------------------------
  // Old-only case
  // -----------------------------------------------------------------------

  describe('runLocalStorageMigration — old-only case', () => {
    it('copies api-key from legacy key to new key', () => {
      localStorage.setItem('mtga-companion-api-key', 'my-api-key-value');
      runLocalStorageMigration();
      expect(localStorage.getItem('vaultmtg-api-key')).toBe('my-api-key-value');
    });

    it('copies settings-expanded from legacy key to new key', () => {
      localStorage.setItem('mtga-companion-settings-expanded', JSON.stringify(['connection']));
      runLocalStorageMigration();
      expect(localStorage.getItem('vaultmtg-settings-expanded')).toBe(
        JSON.stringify(['connection'])
      );
    });

    it('copies developer-mode from legacy key to new key', () => {
      localStorage.setItem('mtga-companion-developer-mode', 'true');
      runLocalStorageMigration();
      expect(localStorage.getItem('vaultmtg-developer-mode')).toBe('true');
    });

    it('copies filters from legacy key to new key', () => {
      const filters = JSON.stringify({ matchHistory: { dateRange: '30days' } });
      localStorage.setItem('mtga-companion-filters', filters);
      runLocalStorageMigration();
      expect(localStorage.getItem('vaultmtg-filters')).toBe(filters);
    });

    it('removes every legacy key after migration', () => {
      for (const [legacyKey] of MIGRATION_MAP) {
        localStorage.setItem(legacyKey, 'test-value');
      }
      runLocalStorageMigration();
      for (const [legacyKey] of MIGRATION_MAP) {
        expect(localStorage.getItem(legacyKey)).toBeNull();
      }
    });

    it('sets the migration sentinel after a successful migration', () => {
      localStorage.setItem('mtga-companion-api-key', 'key');
      runLocalStorageMigration();
      expect(localStorage.getItem(MIGRATION_SENTINEL)).toBe('1');
    });

    it('migrates all four keys in a single call', () => {
      localStorage.setItem('mtga-companion-api-key', 'key-val');
      localStorage.setItem('mtga-companion-settings-expanded', '["a"]');
      localStorage.setItem('mtga-companion-developer-mode', 'false');
      localStorage.setItem('mtga-companion-filters', '{}');

      runLocalStorageMigration();

      expect(localStorage.getItem('vaultmtg-api-key')).toBe('key-val');
      expect(localStorage.getItem('vaultmtg-settings-expanded')).toBe('["a"]');
      expect(localStorage.getItem('vaultmtg-developer-mode')).toBe('false');
      expect(localStorage.getItem('vaultmtg-filters')).toBe('{}');
    });
  });

  // -----------------------------------------------------------------------
  // Already-migrated case (sentinel present)
  // -----------------------------------------------------------------------

  describe('runLocalStorageMigration — already-migrated (no-op)', () => {
    it('does NOT migrate keys when the sentinel is already set', () => {
      localStorage.setItem(MIGRATION_SENTINEL, '1');
      // Set a legacy key that would otherwise be migrated
      localStorage.setItem('mtga-companion-api-key', 'should-not-migrate');

      runLocalStorageMigration();

      // New key should remain absent (migration was skipped)
      expect(localStorage.getItem('vaultmtg-api-key')).toBeNull();
      // Legacy key should remain untouched
      expect(localStorage.getItem('mtga-companion-api-key')).toBe('should-not-migrate');
    });

    it('is idempotent — second call after migration is a no-op', () => {
      localStorage.setItem('mtga-companion-developer-mode', 'true');
      runLocalStorageMigration(); // first run — migrates
      // Manually put a stale legacy key back to confirm second run ignores it
      localStorage.setItem('mtga-companion-developer-mode', 'stale-value');
      runLocalStorageMigration(); // second run — must be no-op
      // New key retains first-migration value
      expect(localStorage.getItem('vaultmtg-developer-mode')).toBe('true');
    });
  });

  // -----------------------------------------------------------------------
  // Empty case (no legacy keys present)
  // -----------------------------------------------------------------------

  describe('runLocalStorageMigration — empty case', () => {
    it('runs without error when no legacy keys are present', () => {
      expect(() => runLocalStorageMigration()).not.toThrow();
    });

    it('sets the sentinel even when no legacy keys were present', () => {
      runLocalStorageMigration();
      expect(localStorage.getItem(MIGRATION_SENTINEL)).toBe('1');
    });

    it('does not create any new vaultmtg-* keys when no legacy data exists', () => {
      runLocalStorageMigration();
      for (const [, newKey] of MIGRATION_MAP) {
        expect(localStorage.getItem(newKey)).toBeNull();
      }
    });
  });

  // -----------------------------------------------------------------------
  // Partial overlap — new key already exists
  // -----------------------------------------------------------------------

  describe('runLocalStorageMigration — new key already exists', () => {
    it('does NOT overwrite new key when it already has a value', () => {
      localStorage.setItem('vaultmtg-api-key', 'already-set');
      localStorage.setItem('mtga-companion-api-key', 'legacy-value');

      runLocalStorageMigration();

      // New key must retain its existing value
      expect(localStorage.getItem('vaultmtg-api-key')).toBe('already-set');
    });

    it('removes the legacy key even when the new key already exists', () => {
      localStorage.setItem('vaultmtg-developer-mode', 'false');
      localStorage.setItem('mtga-companion-developer-mode', 'true');

      runLocalStorageMigration();

      expect(localStorage.getItem('mtga-companion-developer-mode')).toBeNull();
    });
  });
});

// ===========================================================================
// V2 migration — vaultmtg-* / vaultmtg_* → hollowmark-* / hollowmark_*
// ===========================================================================

describe('localStorageMigrationV2', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  // -------------------------------------------------------------------------
  // MIGRATION_V2_MAP shape
  // -------------------------------------------------------------------------

  describe('MIGRATION_V2_MAP', () => {
    it('contains exactly the seven expected key pairs', () => {
      expect(MIGRATION_V2_MAP).toHaveLength(7);
    });

    it('maps each vaultmtg key to the correct hollowmark counterpart', () => {
      const map = Object.fromEntries(MIGRATION_V2_MAP);
      expect(map['vaultmtg-api-key']).toBe('hollowmark-api-key');
      expect(map['vaultmtg-settings-expanded']).toBe('hollowmark-settings-expanded');
      expect(map['vaultmtg-developer-mode']).toBe('hollowmark-developer-mode');
      expect(map['vaultmtg-filters']).toBe('hollowmark-filters');
      expect(map['vaultmtg-meta-refresh-timestamps']).toBe('hollowmark-meta-refresh-timestamps');
      expect(map['vaultmtg_onboarding_dismissed']).toBe('hollowmark_onboarding_dismissed');
      expect(map['vaultmtg_onboarding_completed']).toBe('hollowmark_onboarding_completed');
    });

    it('does NOT include any sessionStorage sentinel keys', () => {
      const oldKeys = MIGRATION_V2_MAP.map(([old]) => old);
      expect(oldKeys).not.toContain('vaultmtg_ph_app_session_started_fired');
      expect(oldKeys).not.toContain('vaultmtg_ph_funnel_first_feature_used_fired');
      expect(oldKeys).not.toContain('vaultmtg_ph_funnel_sign_up_completed_fired');
      expect(oldKeys).not.toContain('vaultmtg_ph_funnel_first_data_loaded_fired');
      expect(oldKeys).not.toContain('vaultmtg_has_account_data');
    });
  });

  // -------------------------------------------------------------------------
  // Old-only case
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — old-only case', () => {
    it('copies api-key from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg-api-key', 'my-api-key');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark-api-key')).toBe('my-api-key');
    });

    it('copies settings-expanded from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg-settings-expanded', JSON.stringify(['connection']));
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark-settings-expanded')).toBe(
        JSON.stringify(['connection'])
      );
    });

    it('copies developer-mode from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg-developer-mode', 'true');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark-developer-mode')).toBe('true');
    });

    it('copies filters from vaultmtg key to hollowmark key', () => {
      const filters = JSON.stringify({ matchHistory: { dateRange: '30days' } });
      localStorage.setItem('vaultmtg-filters', filters);
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark-filters')).toBe(filters);
    });

    it('copies meta-refresh-timestamps from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg-meta-refresh-timestamps', '{}');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark-meta-refresh-timestamps')).toBe('{}');
    });

    it('copies onboarding_dismissed from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg_onboarding_dismissed', 'true');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark_onboarding_dismissed')).toBe('true');
    });

    it('copies onboarding_completed from vaultmtg key to hollowmark key', () => {
      localStorage.setItem('vaultmtg_onboarding_completed', 'true');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem('hollowmark_onboarding_completed')).toBe('true');
    });

    it('migrates all seven keys in a single call', () => {
      localStorage.setItem('vaultmtg-api-key', 'key-val');
      localStorage.setItem('vaultmtg-settings-expanded', '["a"]');
      localStorage.setItem('vaultmtg-developer-mode', 'false');
      localStorage.setItem('vaultmtg-filters', '{}');
      localStorage.setItem('vaultmtg-meta-refresh-timestamps', '{"set7":1}');
      localStorage.setItem('vaultmtg_onboarding_dismissed', 'true');
      localStorage.setItem('vaultmtg_onboarding_completed', 'true');

      runLocalStorageMigrationV2();

      expect(localStorage.getItem('hollowmark-api-key')).toBe('key-val');
      expect(localStorage.getItem('hollowmark-settings-expanded')).toBe('["a"]');
      expect(localStorage.getItem('hollowmark-developer-mode')).toBe('false');
      expect(localStorage.getItem('hollowmark-filters')).toBe('{}');
      expect(localStorage.getItem('hollowmark-meta-refresh-timestamps')).toBe('{"set7":1}');
      expect(localStorage.getItem('hollowmark_onboarding_dismissed')).toBe('true');
      expect(localStorage.getItem('hollowmark_onboarding_completed')).toBe('true');
    });
  });

  // -------------------------------------------------------------------------
  // No-delete (D16/AC8) — old vaultmtg-* keys must survive
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — old keys NOT deleted (D16/AC8)', () => {
    it('does NOT remove the old vaultmtg-* key after migration', () => {
      localStorage.setItem('vaultmtg-api-key', 'my-api-key');
      runLocalStorageMigrationV2();
      // Old key must still be present (reversibility until v0.3.9.1 delete pass)
      expect(localStorage.getItem('vaultmtg-api-key')).toBe('my-api-key');
    });

    it('does NOT remove any old key for all seven entries', () => {
      for (const [oldKey] of MIGRATION_V2_MAP) {
        localStorage.setItem(oldKey, 'test-value');
      }
      runLocalStorageMigrationV2();
      for (const [oldKey] of MIGRATION_V2_MAP) {
        expect(localStorage.getItem(oldKey)).toBe('test-value');
      }
    });
  });

  // -------------------------------------------------------------------------
  // Sentinel set after run
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — sentinel', () => {
    it('sets MIGRATION_V2_SENTINEL after a successful migration', () => {
      localStorage.setItem('vaultmtg-api-key', 'key');
      runLocalStorageMigrationV2();
      expect(localStorage.getItem(MIGRATION_V2_SENTINEL)).toBe('1');
    });

    it('sets MIGRATION_V2_SENTINEL even when no old keys were present', () => {
      runLocalStorageMigrationV2();
      expect(localStorage.getItem(MIGRATION_V2_SENTINEL)).toBe('1');
    });
  });

  // -------------------------------------------------------------------------
  // Idempotent — sentinel gates re-runs
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — idempotent', () => {
    it('does NOT migrate when the sentinel is already set', () => {
      localStorage.setItem(MIGRATION_V2_SENTINEL, '1');
      localStorage.setItem('vaultmtg-api-key', 'should-not-migrate');

      runLocalStorageMigrationV2();

      expect(localStorage.getItem('hollowmark-api-key')).toBeNull();
      // Old key untouched
      expect(localStorage.getItem('vaultmtg-api-key')).toBe('should-not-migrate');
    });

    it('is idempotent — second call after migration is a no-op', () => {
      localStorage.setItem('vaultmtg-developer-mode', 'true');
      runLocalStorageMigrationV2(); // first run — migrates

      // Simulate a stale old key re-appearing (should be ignored on re-run)
      localStorage.setItem('vaultmtg-developer-mode', 'stale-value');
      runLocalStorageMigrationV2(); // second run — must be no-op

      // hollowmark key retains first-migration value
      expect(localStorage.getItem('hollowmark-developer-mode')).toBe('true');
    });
  });

  // -------------------------------------------------------------------------
  // Empty case (no old keys present)
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — empty case', () => {
    it('runs without error when no old vaultmtg-* keys are present', () => {
      expect(() => runLocalStorageMigrationV2()).not.toThrow();
    });

    it('does not create any hollowmark-* keys when no old data exists', () => {
      runLocalStorageMigrationV2();
      for (const [, newKey] of MIGRATION_V2_MAP) {
        expect(localStorage.getItem(newKey)).toBeNull();
      }
    });
  });

  // -------------------------------------------------------------------------
  // Both-present — new key already has a value → no overwrite
  // -------------------------------------------------------------------------

  describe('runLocalStorageMigrationV2 — new key already exists', () => {
    it('does NOT overwrite the hollowmark key when it already has a value', () => {
      localStorage.setItem('hollowmark-api-key', 'already-set');
      localStorage.setItem('vaultmtg-api-key', 'old-value');

      runLocalStorageMigrationV2();

      expect(localStorage.getItem('hollowmark-api-key')).toBe('already-set');
    });

    it('does NOT overwrite hollowmark-filters when it already has a value', () => {
      localStorage.setItem('hollowmark-filters', '{"existing":true}');
      localStorage.setItem('vaultmtg-filters', '{"old":true}');

      runLocalStorageMigrationV2();

      expect(localStorage.getItem('hollowmark-filters')).toBe('{"existing":true}');
    });
  });
});
