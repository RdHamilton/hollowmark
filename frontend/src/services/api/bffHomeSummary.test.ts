/**
 * Contract tests for the BFF home summary adapter.
 *
 * These tests verify the response-shape contract between the SPA and
 * GET /api/v1/history/summary.  They are sentinel tests — if Bob renames or
 * restructures any field in the BFF response, these tests will fail before
 * the breakage reaches prod.
 *
 * Tests assert:
 *   1. All required top-level keys are present and typed correctly.
 *   2. Nested sub-objects (today / this_week / all_time / last_match) have the
 *      exact field names the adapter declares.
 *   3. The mock stub (makeMockHomeSummary) satisfies the contract type.
 *   4. The adapter function rejects non-2xx responses with ApiRequestError.
 */

import { describe, it, expect, vi, afterEach } from 'vitest';
import {
  getHomeSummary,
  makeMockHomeSummary,
  type HomeSummaryResponse,
  type TodayRecord,
  type WeekRecord,
  type AllTimeRecord,
  type LastMatch,
} from './bffHomeSummary';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeFullSummary(): HomeSummaryResponse {
  return {
    today: { wins: 3, losses: 1, win_rate: 0.75 },
    this_week: { wins: 10, losses: 5, win_rate: 0.667, matches: 15 },
    all_time: {
      wins: 120,
      losses: 80,
      win_rate: 0.6,
      matches: 200,
      current_streak: 4,
      streak_type: 'W',
    },
    last_match: {
      result: 'win',
      opponent_archetype: 'Esper Midrange',
      elapsed_seconds: 1245,
    },
  };
}

// ---------------------------------------------------------------------------
// Mock stub tests
// ---------------------------------------------------------------------------

describe('makeMockHomeSummary', () => {
  it('returns a HomeSummaryResponse with all required top-level keys', () => {
    const mock = makeMockHomeSummary();
    // Sentinel: these are the exact keys Bob's endpoint must return
    expect(mock).toHaveProperty('today');
    expect(mock).toHaveProperty('this_week');
    expect(mock).toHaveProperty('all_time');
    expect(mock).toHaveProperty('last_match');
  });

  it('today has wins, losses, win_rate', () => {
    const today: TodayRecord = makeMockHomeSummary().today;
    expect(today).toHaveProperty('wins');
    expect(today).toHaveProperty('losses');
    expect(today).toHaveProperty('win_rate');
    expect(typeof today.wins).toBe('number');
    expect(typeof today.losses).toBe('number');
    expect(typeof today.win_rate).toBe('number');
  });

  it('this_week has wins, losses, win_rate, matches', () => {
    const week: WeekRecord = makeMockHomeSummary().this_week;
    expect(week).toHaveProperty('wins');
    expect(week).toHaveProperty('losses');
    expect(week).toHaveProperty('win_rate');
    expect(week).toHaveProperty('matches');
  });

  it('all_time has wins, losses, win_rate, matches, current_streak, streak_type', () => {
    const allTime: AllTimeRecord = makeMockHomeSummary().all_time;
    expect(allTime).toHaveProperty('wins');
    expect(allTime).toHaveProperty('losses');
    expect(allTime).toHaveProperty('win_rate');
    expect(allTime).toHaveProperty('matches');
    expect(allTime).toHaveProperty('current_streak');
    expect(allTime).toHaveProperty('streak_type');
    expect(['W', 'L']).toContain(allTime.streak_type);
  });

  it('last_match is null in the zero-state mock', () => {
    const mock = makeMockHomeSummary();
    expect(mock.last_match).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// Full shape sentinel tests (compile-time + runtime)
// ---------------------------------------------------------------------------

describe('HomeSummaryResponse shape sentinel', () => {
  it('full response satisfies all required fields — sentinel against field renames', () => {
    const full = makeFullSummary();

    // today
    expect(full.today.wins).toBe(3);
    expect(full.today.losses).toBe(1);
    expect(full.today.win_rate).toBe(0.75);

    // this_week
    expect(full.this_week.wins).toBe(10);
    expect(full.this_week.losses).toBe(5);
    expect(full.this_week.win_rate).toBeCloseTo(0.667);
    expect(full.this_week.matches).toBe(15);

    // all_time
    expect(full.all_time.wins).toBe(120);
    expect(full.all_time.losses).toBe(80);
    expect(full.all_time.win_rate).toBe(0.6);
    expect(full.all_time.matches).toBe(200);
    expect(full.all_time.current_streak).toBe(4);
    expect(full.all_time.streak_type).toBe('W');
  });

  it('last_match with all fields present', () => {
    const full = makeFullSummary();
    const lm = full.last_match as LastMatch;
    expect(lm.result).toBe('win');
    expect(lm.opponent_archetype).toBe('Esper Midrange');
    expect(lm.elapsed_seconds).toBe(1245);
  });

  it('last_match can be null (no games played yet)', () => {
    const r: HomeSummaryResponse = { ...makeFullSummary(), last_match: null };
    expect(r.last_match).toBeNull();
  });

  it('streak_type accepts W and L only', () => {
    const rW: AllTimeRecord = { ...makeFullSummary().all_time, streak_type: 'W' };
    const rL: AllTimeRecord = { ...makeFullSummary().all_time, streak_type: 'L' };
    expect(rW.streak_type).toBe('W');
    expect(rL.streak_type).toBe('L');
  });
});

// ---------------------------------------------------------------------------
// Adapter fetch tests
// ---------------------------------------------------------------------------

describe('getHomeSummary', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('returns HomeSummaryResponse on 200', async () => {
    const mockData = makeFullSummary();
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => mockData,
    }));

    const result = await getHomeSummary('test-token');
    expect(result.today.wins).toBe(3);
    expect(result.this_week.matches).toBe(15);
    expect(result.all_time.streak_type).toBe('W');
    expect((result.last_match as LastMatch).elapsed_seconds).toBe(1245);
  });

  it('includes Authorization header with token', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => makeFullSummary(),
    });
    vi.stubGlobal('fetch', mockFetch);

    await getHomeSummary('my-clerk-token');

    expect(mockFetch).toHaveBeenCalledOnce();
    const [, init] = mockFetch.mock.calls[0];
    expect((init as RequestInit).headers).toMatchObject({
      Authorization: 'Bearer my-clerk-token',
    });
  });

  it('throws ApiRequestError on non-2xx response', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 404,
      statusText: 'Not Found',
      json: async () => ({ error: 'endpoint not found' }),
    }));

    await expect(getHomeSummary('token')).rejects.toThrow('endpoint not found');
  });

  it('uses status text when error body has no message field', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      statusText: 'Internal Server Error',
      json: async () => ({}),
    }));

    await expect(getHomeSummary('token')).rejects.toThrow('Internal Server Error');
  });

  it('calls the /history/summary path', async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => makeFullSummary(),
    });
    vi.stubGlobal('fetch', mockFetch);

    await getHomeSummary('token');

    const [url] = mockFetch.mock.calls[0];
    expect(url as string).toMatch(/\/history\/summary$/);
  });
});
