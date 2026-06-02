import { describe, it, expect } from 'vitest';
import { getVisibleSetAnnotations, type ChartPeriodMeta } from './setReleaseAnnotations';
import { type ArenaSetRelease } from '@/constants/arenaSetReleases';

// Stable fixture releases so tests are independent of the live constant list.
const FIXTURE_RELEASES: ArenaSetRelease[] = [
  { code: 'DSK', name: 'Duskmourn',   releaseDate: '2024-09-24' },
  { code: 'BLB', name: 'Bloomburrow', releaseDate: '2024-07-30' },
  { code: 'OTJ', name: 'Outlaws of Thunder Junction', releaseDate: '2024-04-16' },
];

// Helper to build a simple daily-period array from a list of YYYY-MM-DD strings.
function dailyPeriods(dates: string[]): ChartPeriodMeta[] {
  return dates.map(d => ({ name: d, startDate: d }));
}

describe('getVisibleSetAnnotations', () => {
  describe('empty input', () => {
    it('returns empty array when periods is empty', () => {
      expect(getVisibleSetAnnotations([], FIXTURE_RELEASES)).toEqual([]);
    });
  });

  describe('out-of-range set dates', () => {
    it('returns no annotations when all release dates precede the chart window', () => {
      // Chart window: 2025-01-01 to 2025-01-07 — all fixture releases are in 2024.
      const periods = dailyPeriods(['2025-01-01', '2025-01-02', '2025-01-03',
                                    '2025-01-04', '2025-01-05', '2025-01-06', '2025-01-07']);
      expect(getVisibleSetAnnotations(periods, FIXTURE_RELEASES)).toEqual([]);
    });

    it('returns no annotations when all release dates are after the chart window', () => {
      // Chart window: 2024-01-01 to 2024-04-01 — all fixture releases are after April 1.
      const periods = dailyPeriods(['2024-01-01', '2024-02-01', '2024-03-01', '2024-04-01']);
      expect(getVisibleSetAnnotations(periods, FIXTURE_RELEASES)).toEqual([]);
    });
  });

  describe('annotation placement', () => {
    it('maps a release date to the period whose startDate is the closest one that does not exceed the release date', () => {
      // Weekly periods around the DSK release (2024-09-24).
      const periods = dailyPeriods([
        '2024-09-17', // week before
        '2024-09-24', // exact match — DSK release day is the period start
        '2024-10-01', // week after
      ]);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const dsk = annotations.find(a => a.code === 'DSK');
      expect(dsk).toBeDefined();
      expect(dsk!.xLabel).toBe('2024-09-24');
    });

    it('maps a mid-period release date to the period that was already underway', () => {
      // Weekly periods where DSK release (2024-09-24) falls mid-period
      // (period starting 2024-09-17, next starting 2024-10-01).
      const periods = dailyPeriods(['2024-09-10', '2024-09-17', '2024-10-01', '2024-10-08']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const dsk = annotations.find(a => a.code === 'DSK');
      expect(dsk).toBeDefined();
      // Release falls between 2024-09-17 and 2024-10-01, so it anchors to 2024-09-17.
      expect(dsk!.xLabel).toBe('2024-09-17');
    });

    it('includes correct metadata on the annotation', () => {
      const periods = dailyPeriods(['2024-07-30', '2024-08-06']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const blb = annotations.find(a => a.code === 'BLB');
      expect(blb).toBeDefined();
      expect(blb).toMatchObject({
        code: 'BLB',
        name: 'Bloomburrow',
        releaseDate: '2024-07-30',
        xLabel: '2024-07-30',
      });
    });
  });

  describe('multiple visible annotations', () => {
    it('returns all release dates that fall within the chart window', () => {
      // Chart window: 2024-04-01 to 2024-10-15 — covers OTJ, BLB, DSK.
      const periods = dailyPeriods([
        '2024-04-01', '2024-04-16', '2024-05-01',
        '2024-07-30', '2024-08-15',
        '2024-09-24', '2024-10-01',
      ]);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const codes = annotations.map(a => a.code);
      expect(codes).toContain('DSK');
      expect(codes).toContain('BLB');
      expect(codes).toContain('OTJ');
      expect(annotations).toHaveLength(3);
    });
  });

  describe('boundary conditions', () => {
    it('includes a release date that exactly matches the last period startDate', () => {
      // DSK release date equals the last period startDate.
      const periods = dailyPeriods(['2024-09-17', '2024-09-24']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const dsk = annotations.find(a => a.code === 'DSK');
      expect(dsk).toBeDefined();
      expect(dsk!.xLabel).toBe('2024-09-24');
    });

    it('includes a release date that exactly matches the first period startDate', () => {
      // OTJ release date equals the first period startDate.
      const periods = dailyPeriods(['2024-04-16', '2024-04-23', '2024-04-30']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      const otj = annotations.find(a => a.code === 'OTJ');
      expect(otj).toBeDefined();
      expect(otj!.xLabel).toBe('2024-04-16');
    });

    it('does not emit an annotation when release date is one day before the first period', () => {
      // Chart starts 2024-09-25 (day after DSK) — DSK should be excluded.
      const periods = dailyPeriods(['2024-09-25', '2024-10-02', '2024-10-09']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      expect(annotations.find(a => a.code === 'DSK')).toBeUndefined();
    });
  });

  describe('single period edge case', () => {
    it('returns annotation when release date matches a single-period chart', () => {
      const periods = dailyPeriods(['2024-09-24']);
      const annotations = getVisibleSetAnnotations(periods, FIXTURE_RELEASES);
      expect(annotations).toHaveLength(1);
      expect(annotations[0].code).toBe('DSK');
    });
  });
});
