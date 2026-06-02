/**
 * MTG Arena set release dates — static constants for Win-Rate Trend annotations.
 *
 * Each entry is the date the set became available on MTG Arena (Arena release,
 * not paper release, since the app tracks Arena play data).
 *
 * Source: official Wizards Arena release dates.
 * Add new entries at the top of the array as sets release; keep in reverse
 * chronological order so that the most-recent sets are easiest to find.
 *
 * Do NOT add pre-Arena sets here — they will never fall within a player's
 * Arena match history and would only add lookup noise.
 */

export interface ArenaSetRelease {
  /** Short set code (3–4 uppercase letters, MTG canonical). */
  code: string;
  /** Human-readable name for annotation labels. */
  name: string;
  /**
   * Arena release date in ISO 8601 format (YYYY-MM-DD, local/Arena time).
   * Charts use local-timezone dates throughout (see WinRateTrend.tsx formatDate),
   * so release dates are stored as plain calendar dates — no UTC offset.
   */
  releaseDate: string;
}

/**
 * Recent MTG Arena set releases, newest first.
 * Covers the window most likely to appear in a player's trend chart.
 */
export const ARENA_SET_RELEASES: readonly ArenaSetRelease[] = [
  // 2025 sets
  { code: 'TDM', name: 'Tarkir: Dragonstorm',   releaseDate: '2025-04-29' },
  { code: 'INK', name: 'Aetherdrift',             releaseDate: '2025-02-11' },
  // 2024 sets
  { code: 'FDN', name: 'Foundations',             releaseDate: '2024-11-15' },
  { code: 'DSK', name: 'Duskmourn',               releaseDate: '2024-09-24' },
  { code: 'BLB', name: 'Bloomburrow',             releaseDate: '2024-07-30' },
  { code: 'OTJ', name: 'Outlaws of Thunder Junction', releaseDate: '2024-04-16' },
  { code: 'MKM', name: 'Murders at Karlov Manor', releaseDate: '2024-02-06' },
  // 2023 sets
  { code: 'LCI', name: 'Lost Caverns of Ixalan',  releaseDate: '2023-11-14' },
  { code: 'WOE', name: 'Wilds of Eldraine',       releaseDate: '2023-09-05' },
  { code: 'MAT', name: 'March of the Machine: Aftermath', releaseDate: '2023-05-09' },
  { code: 'MOM', name: 'March of the Machine',    releaseDate: '2023-04-18' },
  { code: 'ONE', name: 'Phyrexia: All Will Be One', releaseDate: '2023-02-07' },
] as const;
