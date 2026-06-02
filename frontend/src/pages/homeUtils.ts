/**
 * Pure utility functions for the Home Command Strip page.
 * Extracted to a dedicated file because react-refresh/only-export-components
 * requires component files to export only components.
 */

/** Win-rate threshold coloring per the design-system win-rate badge spec. */
export function winRateColor(rate: number): string {
  if (rate >= 0.57) return 'var(--vault-success)';
  if (rate >= 0.5) return 'var(--vault-fg-secondary)';
  return 'var(--vault-danger)';
}

/**
 * Format elapsed seconds as a human-readable string.
 * e.g. 1245 → "20 min ago", 90 → "1 min ago"
 */
export function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s ago`;
  const mins = Math.floor(seconds / 60);
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  return `${hours}h ago`;
}

/** Whether a format is Limited (Sealed or Draft). */
export function isLimitedFormat(format: string): boolean {
  const lower = format.toLowerCase();
  return lower.includes('sealed') || lower.includes('draft') || lower.includes('limited');
}
