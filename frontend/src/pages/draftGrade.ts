/**
 * draftGrade — grading helpers for the live draft advisor.
 *
 * Extracted from DraftLive.tsx so the grade function can be shared with tests
 * without making the page module export a non-component value (which trips
 * `react-refresh/only-export-components`). Logic is unchanged.
 */

/**
 * Grade letter for a card's GIHWR (Game-In-Hand Win Rate).
 *
 * `gihwr` is a FRACTION in the range 0.0–1.0 — this is the canonical unit
 * served by the BFF `/api/v1/draft-ratings` endpoint (the sync lambda stores
 * 17Lands' `ever_drawn_win_rate` verbatim, which is itself a fraction; neither
 * the sync lambda, the BFF handler, nor this adapter multiplies by 100). A
 * 63.1% GIHWR card therefore arrives as `0.631`, so the thresholds below are
 * expressed as fractions. (The earlier percent thresholds — `>= 65` — graded
 * every real card "F" because `0.631 < 45`.)
 */
export function gradeFromGihwr(gihwr: number | undefined | null): string {
  if (gihwr === undefined || gihwr === null || gihwr === 0) return '—';
  if (gihwr >= 0.65) return 'A+';
  if (gihwr >= 0.62) return 'A';
  if (gihwr >= 0.59) return 'A-';
  if (gihwr >= 0.57) return 'B+';
  if (gihwr >= 0.55) return 'B';
  if (gihwr >= 0.53) return 'B-';
  if (gihwr >= 0.51) return 'C+';
  if (gihwr >= 0.49) return 'C';
  if (gihwr >= 0.47) return 'C-';
  if (gihwr >= 0.45) return 'D';
  return 'F';
}
