/**
 * Unit tests for matches API adapter — date serialisation.
 *
 * Focus: statsFilterToRequest must emit LOCAL calendar dates (YYYY-MM-DD),
 * not UTC-derived dates from toISOString(). This matters for users behind
 * UTC who are active near midnight — toISOString() returns tomorrow's date.
 */
import { describe, it, expect } from 'vitest';
import { statsFilterToRequest } from './matches';
import { models } from '@/types/models';

describe('statsFilterToRequest — date serialisation', () => {
  it('serialises StartDate as local YYYY-MM-DD, not UTC', () => {
    // A Date at 22:00 local in UTC-5 is 03:00 UTC next day.
    // toISOString() → "YYYY-(day+1)" — wrong.
    // Expected: local calendar date.
    const utcMs = Date.UTC(2024, 5, 16, 3, 0, 0); // 2024-06-16T03:00Z
    const d = new Date(utcMs);

    const filter = new models.StatsFilter();
    filter.StartDate = d;

    const req = statsFilterToRequest(filter);

    // Compute the expected LOCAL date dynamically (portable across all TZ).
    const expectedYear = d.getFullYear();
    const expectedMonth = String(d.getMonth() + 1).padStart(2, '0');
    const expectedDay = String(d.getDate()).padStart(2, '0');
    const expectedLocal = `${expectedYear}-${expectedMonth}-${expectedDay}`;

    expect(req.startDate).toBe(expectedLocal);

    // On non-UTC machines, verify it differs from the UTC date.
    const utcStr = d.toISOString().split('T')[0];
    if (expectedLocal !== utcStr) {
      expect(req.startDate).not.toBe(utcStr);
    }
  });

  it('serialises EndDate as local YYYY-MM-DD, not UTC', () => {
    const utcMs = Date.UTC(2024, 5, 16, 3, 0, 0); // 2024-06-16T03:00Z
    const d = new Date(utcMs);

    const filter = new models.StatsFilter();
    filter.EndDate = d;

    const req = statsFilterToRequest(filter);

    const expectedYear = d.getFullYear();
    const expectedMonth = String(d.getMonth() + 1).padStart(2, '0');
    const expectedDay = String(d.getDate()).padStart(2, '0');
    const expectedLocal = `${expectedYear}-${expectedMonth}-${expectedDay}`;

    expect(req.endDate).toBe(expectedLocal);
  });

  it('serialises a plain local-midnight Date correctly', () => {
    const d = new Date(2024, 5, 15); // 2024-06-15 local midnight
    const filter = new models.StatsFilter();
    filter.StartDate = d;
    filter.EndDate = new Date(2024, 5, 16); // 2024-06-16 local midnight

    const req = statsFilterToRequest(filter);
    expect(req.startDate).toBe('2024-06-15');
    expect(req.endDate).toBe('2024-06-16');
  });

  it('returns undefined for unset StartDate / EndDate', () => {
    const filter = new models.StatsFilter();
    const req = statsFilterToRequest(filter);
    expect(req.startDate).toBeUndefined();
    expect(req.endDate).toBeUndefined();
  });

  it('passes through a string date unchanged (already a YYYY-MM-DD string)', () => {
    const filter = new models.StatsFilter();
    // StatsFilter.StartDate can be assigned a string-shaped date in some paths
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (filter as any).StartDate = '2024-06-15';
    const req = statsFilterToRequest(filter);
    expect(req.startDate).toBe('2024-06-15');
  });
});
