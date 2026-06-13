/**
 * Shared date helpers for chart pages.
 *
 * The key invariant: dates sent to the BFF must be LOCAL calendar dates
 * (YYYY-MM-DD), not UTC-derived dates. Using toISOString() for a user who is
 * behind UTC (e.g. UTC-5) at 22:00 local returns the NEXT day's date, which
 * shifts the query window by one day. Using local Date methods (getFullYear,
 * getMonth, getDate) always gives the user's actual local date.
 *
 * See #1391 for the pairing fix in the BFF (Ben): BFF applies a +1-day
 * exclusive UTC bound that absorbs the local→UTC offset, and that pairing is
 * only correct when the SPA sends a LOCAL date.
 */

/**
 * Serialises a Date to a YYYY-MM-DD string using the LOCAL calendar date,
 * not the UTC date. Zero-pads month and day.
 */
export function toLocalDateString(d: Date): string {
  const year = d.getFullYear();
  const month = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

/**
 * Builds the start/end Date objects for a "last N days" window.
 *
 * - startDate  = today - N days, local midnight (00:00:00.000)
 * - endDate    = today + 1 day,  local midnight (exclusive upper bound for BFF)
 *
 * Using setDate() with local getDate() keeps arithmetic in local calendar time,
 * so there is no DST-crossing or UTC-offset error.
 */
export function buildLastNDaysWindow(n: number): { startDate: Date; endDate: Date } {
  const now = new Date();

  const start = new Date();
  start.setDate(now.getDate() - n);
  start.setHours(0, 0, 0, 0);

  const end = new Date(now);
  end.setDate(now.getDate() + 1);
  end.setHours(0, 0, 0, 0);

  return { startDate: start, endDate: end };
}
