/**
 * Utility: derive which Arena set-release annotations are visible for a given
 * win-rate trend chart, and map each to the x-axis label it should overlay.
 *
 * Strategy
 * --------
 * The chart's x-axis uses `period.name` (the human label produced by the BFF,
 * e.g. "Jan 1", "Week of Jan 6", "Jan 2024"). Each period also carries a
 * `startDate` string (YYYY-MM-DD). For each set release date we:
 *   1. Check that the release date falls within the chart's overall date range.
 *   2. Find the period whose startDate is the closest one that does NOT exceed
 *      the release date (i.e. the period that was already underway when the set
 *      dropped). That period's label becomes the ReferenceLine `x` value.
 *
 * If the release date precedes all periods, or if no periods are provided, no
 * annotation is emitted for that set.
 */

import { ARENA_SET_RELEASES, type ArenaSetRelease } from '@/constants/arenaSetReleases';

export interface SetReleaseAnnotation {
  /** x-axis label of the period to anchor the annotation to. */
  xLabel: string;
  /** Short set code, e.g. "DSK". */
  code: string;
  /** Human name for the tooltip / label. */
  name: string;
  /** The original release date (YYYY-MM-DD). */
  releaseDate: string;
}

export interface ChartPeriodMeta {
  /** Must match the `name` field used as `dataKey` on the XAxis. */
  name: string;
  /** YYYY-MM-DD — the start of this period (used for date comparison). */
  startDate: string;
}

/**
 * Given an array of chart periods (each with a label and startDate) return the
 * set-release annotations that are visible within the chart's date range.
 *
 * @param periods - Array of chart data periods with label + startDate.
 * @param releases - Set-release constants (defaults to ARENA_SET_RELEASES).
 * @returns Annotations whose release date falls within the chart window.
 */
export function getVisibleSetAnnotations(
  periods: ChartPeriodMeta[],
  releases: readonly ArenaSetRelease[] = ARENA_SET_RELEASES,
): SetReleaseAnnotation[] {
  if (periods.length === 0) return [];

  // Determine the chart's date window from the first and last period startDates.
  const chartStart = periods[0].startDate;
  const chartEnd = periods[periods.length - 1].startDate;

  const annotations: SetReleaseAnnotation[] = [];

  for (const release of releases) {
    const rd = release.releaseDate;

    // Skip if the release date is outside the chart window entirely.
    if (rd < chartStart || rd > chartEnd) continue;

    // Find the period whose startDate is the closest one <= the release date.
    // Periods are in ascending chronological order; iterate forward and keep
    // the last one that does not exceed the release date.
    let matchedLabel: string | null = null;
    for (const period of periods) {
      if (period.startDate <= rd) {
        matchedLabel = period.name;
      } else {
        break;
      }
    }

    if (matchedLabel !== null) {
      annotations.push({
        xLabel: matchedLabel,
        code: release.code,
        name: release.name,
        releaseDate: rd,
      });
    }
  }

  return annotations;
}
