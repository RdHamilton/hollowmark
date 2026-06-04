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
 * expressed as fractions.
 *
 * The band table is re-centered on the ~0.55 set mean (#793): the `C` band
 * straddles the mean, the `F` line sits at 0.42, and the A+/A bombs anchor
 * unchanged. The previous bands were mis-centered ~6pts low — an average
 * common (~0.51) graded `C+` and a filler card (~0.43) graded `F`. With the
 * reanchored table a set-mean card lands in the `C` band and only genuinely
 * unplayable cards (<0.42) grade `F`. Static anchor 0.55 per #793; the dynamic
 * per-set/per-format recalibration is the P2 follow-up (#794).
 */
export function gradeFromGihwr(gihwr: number | undefined | null): string {
  if (gihwr === undefined || gihwr === null || gihwr === 0) return '—';
  if (gihwr >= 0.65) return 'A+';   // unchanged (AC3)
  if (gihwr >= 0.62) return 'A';    // unchanged (AC3)
  if (gihwr >= 0.605) return 'A-';
  if (gihwr >= 0.59) return 'B+';
  if (gihwr >= 0.575) return 'B';
  if (gihwr >= 0.5625) return 'B-';
  if (gihwr >= 0.55) return 'C+';
  if (gihwr >= 0.5375) return 'C';   // C band straddles the ~0.55 set mean (AC1)
  if (gihwr >= 0.525) return 'C-';
  if (gihwr >= 0.42) return 'D';     // D spans down to the F line
  return 'F';                         // F < 0.42 (AC2)
}
