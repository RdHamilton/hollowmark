import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as matches from '../matches';

// Mock the apiClient — Phase 2 routes matches.* through the BFF.
vi.mock('../../apiClient', () => ({
  get: vi.fn(),
  post: vi.fn(),
}));

import { get, post } from '../../apiClient';

describe('matches API', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('statsFilterToRequest', () => {
    it('should convert StatsFilter to StatsFilterRequest', () => {
      const filter = {
        AccountID: 123,
        StartDate: new Date('2024-01-01'),
        EndDate: new Date('2024-01-31'),
        Format: 'standard',
        Formats: ['standard', 'historic'],
        DeckFormat: 'standard',
        DeckID: 'deck-123',
        EventName: 'Ranked',
        EventNames: ['Ranked', 'Premier'],
        OpponentName: 'opponent',
        OpponentID: 'opp-123',
        Result: 'win',
        RankClass: 'Gold',
        RankMinClass: 'Silver',
        RankMaxClass: 'Platinum',
        ResultReason: 'concede',
      } as unknown as matches.StatsFilter;

      const result = matches.statsFilterToRequest(filter);

      expect(result).toEqual({
        accountID: 123,
        startDate: '2024-01-01',
        endDate: '2024-01-31',
        format: 'standard',
        formats: ['standard', 'historic'],
        deckFormat: 'standard',
        deckID: 'deck-123',
        eventName: 'Ranked',
        eventNames: ['Ranked', 'Premier'],
        opponentName: 'opponent',
        opponentID: 'opp-123',
        result: 'win',
        rankClass: 'Gold',
        rankMinClass: 'Silver',
        rankMaxClass: 'Platinum',
        resultReason: 'concede',
      });
    });

    it('should handle empty filter', () => {
      const filter = {} as unknown as matches.StatsFilter;
      const result = matches.statsFilterToRequest(filter);

      expect(result).toEqual({
        accountID: undefined,
        startDate: undefined,
        endDate: undefined,
        format: undefined,
        formats: undefined,
        deckFormat: undefined,
        deckID: undefined,
        eventName: undefined,
        eventNames: undefined,
        opponentName: undefined,
        opponentID: undefined,
        result: undefined,
        rankClass: undefined,
        rankMinClass: undefined,
        rankMaxClass: undefined,
        resultReason: undefined,
      });
    });

    it('should handle ISO string dates', () => {
      const filter = {
        StartDate: '2024-01-01T00:00:00.000Z' as unknown as Date,
        EndDate: '2024-01-31T23:59:59.999Z' as unknown as Date,
      } as unknown as matches.StatsFilter;

      const result = matches.statsFilterToRequest(filter);

      expect(result.startDate).toBe('2024-01-01');
      expect(result.endDate).toBe('2024-01-31');
    });
  });

  describe('getMatches', () => {
    it('should call post with correct path and unwrap MatchListEnvelope.Matches', async () => {
      const mockMatches = [{ ID: '1', Result: 'win' }];
      vi.mocked(post).mockResolvedValue({ Matches: mockMatches, Total: 1, Page: 1, Limit: 50 });

      const result = await matches.getMatches({ format: 'standard' });

      expect(post).toHaveBeenCalledWith('/matches', { format: 'standard' });
      expect(result).toEqual(mockMatches);
    });

    it('should return [] when the envelope has no Matches field', async () => {
      vi.mocked(post).mockResolvedValue(undefined as unknown as { Matches: unknown[] });
      const result = await matches.getMatches();
      expect(result).toEqual([]);
    });
  });

  describe('getMatch', () => {
    it('should call get with correct path', async () => {
      const mockMatch = { id: 'match-123', result: 'win' };
      vi.mocked(get).mockResolvedValue(mockMatch);

      const result = await matches.getMatch('match-123');

      expect(get).toHaveBeenCalledWith('/matches/match-123');
      expect(result).toEqual(mockMatch);
    });
  });

  describe('getMatchGames', () => {
    it('should call get with correct path', async () => {
      const mockGames = [{ id: 'game-1' }];
      vi.mocked(get).mockResolvedValue(mockGames);

      const result = await matches.getMatchGames('match-123');

      expect(get).toHaveBeenCalledWith('/matches/match-123/games');
      expect(result).toEqual(mockGames);
    });
  });

  describe('getStats', () => {
    it('should call post with correct path and filter', async () => {
      const mockStats = { wins: 10, losses: 5 };
      vi.mocked(post).mockResolvedValue(mockStats);

      const result = await matches.getStats({ format: 'historic' });

      expect(post).toHaveBeenCalledWith('/matches/stats', { format: 'historic' });
      expect(result).toEqual(mockStats);
    });
  });

  describe('getFormats', () => {
    it('should call get with correct path', async () => {
      const mockFormats = ['standard', 'historic'];
      vi.mocked(get).mockResolvedValue(mockFormats);

      const result = await matches.getFormats();

      expect(get).toHaveBeenCalledWith('/matches/formats');
      expect(result).toEqual(mockFormats);
    });
  });

  describe('getRankProgression', () => {
    it('should call get with correct path', async () => {
      const mockProgression = { currentRank: 'Gold' };
      vi.mocked(get).mockResolvedValue(mockProgression);

      const result = await matches.getRankProgression('standard');

      expect(get).toHaveBeenCalledWith('/matches/rank-progression/standard');
      expect(result).toEqual(mockProgression);
    });

    it('should encode format with special characters', async () => {
      vi.mocked(get).mockResolvedValue({});

      await matches.getRankProgression('format/with/slashes');

      expect(get).toHaveBeenCalledWith('/matches/rank-progression/format%2Fwith%2Fslashes');
    });
  });

  describe('exportMatches', () => {
    it('should call get with correct format parameter', async () => {
      vi.mocked(get).mockResolvedValue({ data: 'exported' });

      await matches.exportMatches('json');

      expect(get).toHaveBeenCalledWith('/matches/export?format=json');
    });

    it('should handle csv format', async () => {
      vi.mocked(get).mockResolvedValue('csv,data');

      await matches.exportMatches('csv');

      expect(get).toHaveBeenCalledWith('/matches/export?format=csv');
    });
  });

  // ── Phase 2 PR #1 expansion: 12 new endpoints ─────────────────────────────
  // One assertion per function: that it forwards to the documented URL with
  // the expected body (POST) or query (GET). Response-shape concerns live in
  // the BFF handler tests; here we lock the wire contract from the SPA side.

  describe('getStats', () => {
    it('forwards filter to POST /matches/stats', async () => {
      vi.mocked(post).mockResolvedValue({ TotalMatches: 1 });
      await matches.getStats({ format: 'standard' });
      expect(post).toHaveBeenCalledWith('/matches/stats', { format: 'standard' });
    });
  });

  describe('getTrendAnalysis', () => {
    it('forwards request to POST /matches/trends', async () => {
      vi.mocked(post).mockResolvedValue({});
      const req = { startDate: '2026-01-01', endDate: '2026-01-31', periodType: 'week' };
      await matches.getTrendAnalysis(req);
      expect(post).toHaveBeenCalledWith('/matches/trends', req);
    });
  });

  describe('getArchetypes', () => {
    it('calls GET /matches/archetypes', async () => {
      vi.mocked(get).mockResolvedValue(['Mono Red']);
      await matches.getArchetypes();
      expect(get).toHaveBeenCalledWith('/matches/archetypes');
    });
  });

  describe('getFormatDistribution', () => {
    it('forwards filter to POST /matches/format-distribution', async () => {
      vi.mocked(post).mockResolvedValue({});
      await matches.getFormatDistribution({ format: 'standard' });
      expect(post).toHaveBeenCalledWith('/matches/format-distribution', { format: 'standard' });
    });
  });

  describe('getPerformanceByHour', () => {
    it('forwards filter to POST /matches/performance-by-hour', async () => {
      vi.mocked(post).mockResolvedValue({});
      await matches.getPerformanceByHour({ format: 'standard' });
      expect(post).toHaveBeenCalledWith('/matches/performance-by-hour', { format: 'standard' });
    });
  });

  describe('getMatchupMatrix', () => {
    it('forwards filter to POST /matches/matchup-matrix', async () => {
      vi.mocked(post).mockResolvedValue({});
      await matches.getMatchupMatrix({ format: 'standard' });
      expect(post).toHaveBeenCalledWith('/matches/matchup-matrix', { format: 'standard' });
    });
  });

  describe('getRankProgressionTimeline', () => {
    it('forwards GET /matches/rank-progression-timeline with query string', async () => {
      vi.mocked(get).mockResolvedValue({ entries: [] });
      const start = new Date('2026-01-01T00:00:00Z');
      const end = new Date('2026-02-01T00:00:00Z');
      await matches.getRankProgressionTimeline('standard', start, end, 'week');
      expect(get).toHaveBeenCalledWith(expect.stringMatching(
        /^\/matches\/rank-progression-timeline\?format=standard&start_date=.+&end_date=.+&period=week$/,
      ));
    });
  });

  describe('compareMatches', () => {
    it('forwards request to POST /matches/compare', async () => {
      vi.mocked(post).mockResolvedValue({});
      const req = { groups: [{ label: 'Last week', filter: {} }] };
      await matches.compareMatches(req);
      expect(post).toHaveBeenCalledWith('/matches/compare', req);
    });
  });

  describe('compareFormats', () => {
    it('forwards request to POST /matches/compare/formats', async () => {
      vi.mocked(post).mockResolvedValue({});
      const req = { formats: ['standard', 'historic'] };
      await matches.compareFormats(req);
      expect(post).toHaveBeenCalledWith('/matches/compare/formats', req);
    });
  });

  describe('compareDecks', () => {
    it('forwards request to POST /matches/compare/decks', async () => {
      vi.mocked(post).mockResolvedValue({});
      const req = { deckIDs: ['d1', 'd2'] };
      await matches.compareDecks(req);
      expect(post).toHaveBeenCalledWith('/matches/compare/decks', req);
    });
  });

  describe('compareTimePeriods', () => {
    it('forwards request to POST /matches/compare/time-periods', async () => {
      vi.mocked(post).mockResolvedValue({});
      const req = { periods: [{ label: 'Jan', startDate: '2026-01-01', endDate: '2026-01-31' }] };
      await matches.compareTimePeriods(req);
      expect(post).toHaveBeenCalledWith('/matches/compare/time-periods', req);
    });
  });

  describe('getMatchGames (covered above)', () => {
    it('still calls GET /matches/{id}/games', async () => {
      vi.mocked(get).mockResolvedValue([]);
      await matches.getMatchGames('m1');
      expect(get).toHaveBeenCalledWith('/matches/m1/games');
    });
  });
});
