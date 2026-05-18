/**
 * Unit tests for localStorageMigration.ts
 *
 * Covers:
 *  - Old-only case: legacy keys present, new keys absent → migrated correctly
 *  - Already-migrated case: sentinel already set → no-op
 *  - Empty case: no legacy keys present → sentinel is still written
 *  - Partial overlap: new key already exists → old value NOT overwritten
 *  - Legacy key removed after migration in every case
 *  - Meta key carve-out: mtga-companion-meta-refresh-timestamps NOT in map
 */

import { describe, it, expect, beforeEach } from 'vitest';
import {
  runLocalStorageMigration,
  MIGRATION_SENTINEL,
  MIGRATION_MAP,
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
