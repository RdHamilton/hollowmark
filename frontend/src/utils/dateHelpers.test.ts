import { describe, it, expect, vi, afterEach } from 'vitest';
import { toLocalDateString, buildLastNDaysWindow } from './dateHelpers';

// ---------------------------------------------------------------------------
// toLocalDateString — unit tests
// ---------------------------------------------------------------------------
// Key invariant: a Date must serialise to its LOCAL calendar date (YYYY-MM-DD),
// not the UTC date. Behind-UTC users near midnight would get "tomorrow" from
// toISOString() — this helper must return "today" in their local timezone.
// ---------------------------------------------------------------------------

describe('toLocalDateString', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns YYYY-MM-DD for a local midnight date', () => {
    const d = new Date(2024, 5, 15); // 2024-06-15 local midnight
    expect(toLocalDateString(d)).toBe('2024-06-15');
  });

  it('pads single-digit month and day with leading zeros', () => {
    const d = new Date(2024, 0, 3); // 2024-01-03
    expect(toLocalDateString(d)).toBe('2024-01-03');
  });

  it('pads single-digit day when month is double-digit', () => {
    const d = new Date(2024, 11, 5); // 2024-12-05
    expect(toLocalDateString(d)).toBe('2024-12-05');
  });

  // Critical behind-UTC test:
  // A Date whose UTC representation is the NEXT day must still return TODAY's
  // local date — not tomorrow's UTC date as toISOString() would produce.
  it('returns LOCAL date, not UTC date, for a point in time that is tomorrow in UTC', () => {
    // UTC-5 at 22:00 local = next day 03:00 UTC
    // e.g. local 2024-06-15 22:00 @ UTC-5 → UTC 2024-06-16 03:00
    // toISOString() → "2024-06-16T03:00:00.000Z" → "2024-06-16" (wrong)
    // toLocalDateString() → getFullYear/Month/Date → local "2024-06-15" (correct)
    const utcMs = Date.UTC(2024, 5, 16, 3, 0, 0); // 2024-06-16T03:00:00Z
    const d = new Date(utcMs);

    // Compute expected values dynamically so the test is portable across all
    // host timezone offsets (it verifies LOCAL methods, not a fixed string).
    const expectedYear = d.getFullYear();
    const expectedMonth = String(d.getMonth() + 1).padStart(2, '0');
    const expectedDay = String(d.getDate()).padStart(2, '0');
    const expectedLocal = `${expectedYear}-${expectedMonth}-${expectedDay}`;

    expect(toLocalDateString(d)).toBe(expectedLocal);

    // On non-UTC machines the local date must differ from the UTC date string.
    const utcDateStr = d.toISOString().split('T')[0];
    if (expectedLocal !== utcDateStr) {
      expect(toLocalDateString(d)).not.toBe(utcDateStr);
    }
  });

  it('is deterministic: same Date always produces the same string', () => {
    const d = new Date(2024, 2, 20); // 2024-03-20
    expect(toLocalDateString(d)).toBe('2024-03-20');
    expect(toLocalDateString(d)).toBe(toLocalDateString(d));
  });
});

// ---------------------------------------------------------------------------
// buildLastNDaysWindow — unit tests
// ---------------------------------------------------------------------------
// startDate  = today - N days, midnight local
// endDate    = today + 1 day, midnight local  (exclusive upper bound for BFF)
// ---------------------------------------------------------------------------

describe('buildLastNDaysWindow', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns startDate 7 days before today and endDate tomorrow for 7-day window', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2024, 5, 15, 12, 0, 0)); // 2024-06-15 noon local

    const { startDate, endDate } = buildLastNDaysWindow(7);
    expect(toLocalDateString(startDate)).toBe('2024-06-08');
    expect(toLocalDateString(endDate)).toBe('2024-06-16');
  });

  it('returns startDate 30 days before today for 30-day window', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2024, 5, 15, 12, 0, 0)); // 2024-06-15

    const { startDate, endDate } = buildLastNDaysWindow(30);
    expect(toLocalDateString(startDate)).toBe('2024-05-16');
    expect(toLocalDateString(endDate)).toBe('2024-06-16');
  });

  it('returns startDate 90 days before today for 90-day window', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2024, 5, 15, 12, 0, 0)); // 2024-06-15

    const { startDate, endDate } = buildLastNDaysWindow(90);
    expect(toLocalDateString(startDate)).toBe('2024-03-17');
    expect(toLocalDateString(endDate)).toBe('2024-06-16');
  });

  it('normalises startDate to local midnight (zero h/m/s/ms)', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2024, 5, 15, 15, 30, 45, 500));

    const { startDate } = buildLastNDaysWindow(7);
    expect(startDate.getHours()).toBe(0);
    expect(startDate.getMinutes()).toBe(0);
    expect(startDate.getSeconds()).toBe(0);
    expect(startDate.getMilliseconds()).toBe(0);
  });

  it('normalises endDate to local midnight (exclusive bound)', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date(2024, 5, 15, 15, 30, 45, 500));

    const { endDate } = buildLastNDaysWindow(7);
    expect(endDate.getHours()).toBe(0);
    expect(endDate.getMinutes()).toBe(0);
    expect(endDate.getSeconds()).toBe(0);
    expect(endDate.getMilliseconds()).toBe(0);
  });
});
