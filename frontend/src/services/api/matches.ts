/**
 * Matches API service.
 *
 * Phase 2 (see docs/product/milestones/v0.3.1/daemon-local-api-phase2-audit.md):
 * cloud-data reads now hit the BFF directly via apiClient. The BFF emits the
 * camelCase wire format defined under /api/v1/matches/*; the
 * adaptMatchListItem helper below maps that back to the SPA's existing
 * models.Match shape so consuming components do not need PascalCase renames
 * in this PR.
 *
 * Live-state paths (draft pick grading, in-progress match win probability)
 * remain on daemonClient and are added back as Phase 2 Bucket C lands.
 */

import { get as bffGet, post as bffPost } from '../apiClient';
import { models, storage } from '@/types/models';

// Re-export types for convenience
export type Match = models.Match;
export type StatsFilter = models.StatsFilter;
export type Statistics = models.Statistics;
export type PerformanceMetrics = models.PerformanceMetrics;

/**
 * Filter request for API calls.
 */
export interface StatsFilterRequest {
  accountID?: number;
  startDate?: string;
  endDate?: string;
  format?: string;
  formats?: string[];
  deckFormat?: string;
  deckID?: string;
  eventName?: string;
  eventNames?: string[];
  opponentName?: string;
  opponentID?: string;
  result?: string;
  rankClass?: string;
  rankMinClass?: string;
  rankMaxClass?: string;
  resultReason?: string;
}

/**
 * Trend analysis request.
 */
export interface TrendAnalysisRequest {
  startDate: string;
  endDate: string;
  periodType: string;
  formats?: string[];
}

/**
 * Get matches with optional filters.
 */
export async function getMatches(filter: StatsFilterRequest = {}): Promise<Match[]> {
  return bffPost<Match[]>('/matches', filter);
}

/**
 * Get a single match by ID.
 */
export async function getMatch(matchId: string): Promise<Match> {
  return bffGet<Match>(`/matches/${matchId}`);
}

/**
 * Get games for a specific match.
 */
export async function getMatchGames(matchId: string): Promise<models.Game[]> {
  return bffGet<models.Game[]>(`/matches/${matchId}/games`);
}

/**
 * Get statistics with optional filters.
 */
export async function getStats(filter: StatsFilterRequest = {}): Promise<Statistics> {
  return bffPost<Statistics>('/matches/stats', filter);
}

/**
 * Get trend analysis over time.
 */
export async function getTrendAnalysis(request: TrendAnalysisRequest): Promise<unknown> {
  return bffPost('/matches/trends', request);
}

/**
 * Get all available match formats.
 */
export async function getFormats(): Promise<string[]> {
  return bffGet<string[]>('/matches/formats');
}

/**
 * Get all available archetypes.
 */
export async function getArchetypes(): Promise<string[]> {
  return bffGet<string[]>('/matches/archetypes');
}

/**
 * Get match distribution by format.
 */
export async function getFormatDistribution(
  filter: StatsFilterRequest = {}
): Promise<Record<string, Statistics>> {
  return bffPost<Record<string, Statistics>>('/matches/format-distribution', filter);
}

/**
 * Get win rate trends over time.
 */
export async function getWinRateOverTime(request: TrendAnalysisRequest): Promise<unknown> {
  return bffPost('/matches/win-rate-over-time', request);
}

/**
 * Get performance metrics by hour.
 */
export async function getPerformanceByHour(
  filter: StatsFilterRequest = {}
): Promise<PerformanceMetrics> {
  return bffPost<PerformanceMetrics>('/matches/performance-by-hour', filter);
}

/**
 * Get matchup matrix (win rates against different decks).
 */
export async function getMatchupMatrix(
  filter: StatsFilterRequest = {}
): Promise<Record<string, Statistics>> {
  return bffPost<Record<string, Statistics>>('/matches/matchup-matrix', filter);
}

/**
 * Get performance metrics with optional filters.
 */
export async function getPerformanceMetrics(
  filter: StatsFilterRequest = {}
): Promise<PerformanceMetrics> {
  return bffPost<PerformanceMetrics>('/matches/performance', filter);
}

/**
 * Get rank progression for a format.
 */
export async function getRankProgression(format: string): Promise<models.RankProgression> {
  return bffGet<models.RankProgression>(`/matches/rank-progression/${encodeURIComponent(format)}`);
}

/**
 * Get rank progression timeline for a format.
 */
export async function getRankProgressionTimeline(
  format: string,
  startDate: Date,
  endDate: Date,
  period: string
): Promise<storage.RankTimeline> {
  const params = new URLSearchParams({
    format,
    start_date: startDate.toISOString(),
    end_date: endDate.toISOString(),
    period,
  });
  return bffGet<storage.RankTimeline>(`/matches/rank-progression-timeline?${params.toString()}`);
}

/**
 * Export matches in specified format.
 */
export async function exportMatches(format: 'json' | 'csv'): Promise<unknown> {
  return bffGet(`/matches/export?format=${format}`);
}

/**
 * Helper to convert a time value to a date string (YYYY-MM-DD).
 * Handles both Date objects and time.Time (which serializes to ISO string).
 */
function formatDateParam(date: unknown): string | undefined {
  if (!date) return undefined;
  if (typeof date === 'string') {
    return date.split('T')[0];
  }
  if (date instanceof Date) {
    return date.toISOString().split('T')[0];
  }
  // Handle time.Time which may have been serialized
  const dateObj = date as { toString?: () => string };
  if (dateObj.toString) {
    const str = dateObj.toString();
    if (str.includes('T')) {
      return str.split('T')[0];
    }
  }
  return undefined;
}

/**
 * Helper to convert StatsFilter model to API request format.
 */
export function statsFilterToRequest(filter: StatsFilter): StatsFilterRequest {
  return {
    accountID: filter.AccountID,
    startDate: formatDateParam(filter.StartDate),
    endDate: formatDateParam(filter.EndDate),
    format: filter.Format,
    formats: filter.Formats,
    deckFormat: filter.DeckFormat,
    deckID: filter.DeckID,
    eventName: filter.EventName,
    eventNames: filter.EventNames,
    opponentName: filter.OpponentName,
    opponentID: filter.OpponentID,
    result: filter.Result,
    rankClass: filter.RankClass,
    rankMinClass: filter.RankMinClass,
    rankMaxClass: filter.RankMaxClass,
    resultReason: filter.ResultReason,
  };
}

// ==================
// Comparison Types
// ==================

/**
 * ComparisonGroup represents a labeled group of matches for comparison.
 */
export interface ComparisonGroup {
  Label: string;
  Filter: StatsFilter;
  Statistics: Statistics | null;
  MatchCount: number;
}

/**
 * ComparisonResult represents the result of comparing two or more groups.
 */
export interface ComparisonResult {
  Groups: ComparisonGroup[];
  BestGroup: ComparisonGroup | null;
  WorstGroup: ComparisonGroup | null;
  WinRateDiff: number;
  TotalMatches: number;
  ComparisonDate: string;
}

/**
 * ComparisonDiff represents the difference between two specific groups.
 */
export interface ComparisonDiff {
  Group1Label: string;
  Group2Label: string;
  WinRateDiff: number;
  GameWinRateDiff: number;
  MatchCountDiff: number;
  GamesPlayedDiff: number;
  Trend: string;
}

/**
 * Request types for comparison API calls.
 */
export interface ComparisonGroupRequest {
  label: string;
  filter: StatsFilterRequest;
}

export interface CompareMatchesRequest {
  groups: ComparisonGroupRequest[];
}

export interface CompareFormatsRequest {
  formats: string[];
  baseFilter?: StatsFilterRequest;
}

export interface CompareDecksRequest {
  deckIDs: string[];
  baseFilter?: StatsFilterRequest;
}

export interface TimePeriodRequest {
  label: string;
  startDate: string;
  endDate: string;
}

export interface CompareTimePeriodsRequest {
  periods: TimePeriodRequest[];
  baseFilter?: StatsFilterRequest;
}

// ==================
// Comparison API Functions
// ==================

/**
 * Compare multiple groups of matches.
 */
export async function compareMatches(
  request: CompareMatchesRequest
): Promise<ComparisonResult> {
  return bffPost<ComparisonResult>('/matches/compare', request);
}

/**
 * Compare performance across different formats.
 */
export async function compareFormats(
  request: CompareFormatsRequest
): Promise<ComparisonResult> {
  return bffPost<ComparisonResult>('/matches/compare/formats', request);
}

/**
 * Compare performance across different decks.
 */
export async function compareDecks(
  request: CompareDecksRequest
): Promise<ComparisonResult> {
  return bffPost<ComparisonResult>('/matches/compare/decks', request);
}

/**
 * Compare performance across different time periods.
 */
export async function compareTimePeriods(
  request: CompareTimePeriodsRequest
): Promise<ComparisonResult> {
  return bffPost<ComparisonResult>('/matches/compare/time-periods', request);
}
