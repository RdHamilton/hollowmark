/**
 * Collection API service.
 *
 * Phase 2 PR #2: cloud-data collection reads now hit the BFF directly via
 * apiClient at /api/v1/collection/*. Dead Wails-era wrappers
 * (getMissingCardsForSet, getMissingCards, getCollectionBySet,
 * getCollectionByRarity, getRecentChanges, getMissingCardsForDeck,
 * getDeckValue) were removed in this PR — no component referenced them.
 *
 * Plan tracker: .claude/plans/spa-route-migration.md
 */

import { get, post, postFormData } from '../apiClient';
import { gui, models } from '@/types/models';

// Re-export types for convenience
export type CollectionCard = gui.CollectionCard;
export type CollectionStats = gui.CollectionStats;
export type CollectionChangeEntry = gui.CollectionChangeEntry;

/**
 * Filter for collection queries.
 */
export interface CollectionFilter {
  set_code?: string;
  rarity?: string;
  colors?: string[];
  owned_only?: boolean;
  missing_only?: boolean;
}

/**
 * Response from collection API.
 */
export interface CollectionResponse {
  cards: CollectionCard[];
  totalCount: number;
  filterCount: number;
  unknownCardsRemaining: number;   // Cards without metadata that need Scryfall lookup
  unknownCardsFetched: number;     // Cards fetched from Scryfall in this request
}

/**
 * Get collection with optional filters.
 * Returns full response including metadata counts.
 */
export async function getCollectionWithMetadata(filter: CollectionFilter = {}): Promise<CollectionResponse> {
  const response = await post<CollectionResponse>('/collection', filter);
  // Defensive: BFF will always return a populated envelope, but the SPA
  // historically tolerated nulls so keep the fallback.
  return {
    cards: response?.cards ?? [],
    totalCount: response?.totalCount ?? 0,
    filterCount: response?.filterCount ?? 0,
    unknownCardsRemaining: response?.unknownCardsRemaining ?? 0,
    unknownCardsFetched: response?.unknownCardsFetched ?? 0,
  };
}

/**
 * Get collection with optional filters.
 * Returns just the cards array for backward compatibility.
 */
export async function getCollection(filter: CollectionFilter = {}): Promise<CollectionCard[]> {
  const response = await getCollectionWithMetadata(filter);
  return response.cards;
}

/**
 * Get collection statistics.
 */
export async function getCollectionStats(): Promise<CollectionStats> {
  return get<CollectionStats>('/collection/stats');
}

/**
 * Get set completion progress.
 * Returns completion statistics for all sets.
 */
export async function getSetCompletion(): Promise<models.SetCompletion[]> {
  return get<models.SetCompletion[]>('/collection/sets');
}

/**
 * Card value information.
 */
export interface CardValue {
  cardId: number;
  name: string;
  setCode: string;
  rarity: string;
  quantity: number;
  priceUsd: number;
  totalUsd: number;
}

/**
 * Collection value response.
 */
export interface CollectionValue {
  totalValueUsd: number;
  totalValueEur: number;
  uniqueCardsWithPrice: number;
  cardCount: number;
  valueByRarity: Record<string, number>;
  topCards: CardValue[];
  lastUpdated?: number;
}

/**
 * Get the estimated value of the collection.
 */
export async function getCollectionValue(): Promise<CollectionValue> {
  return get<CollectionValue>('/collection/value');
}

/**
 * Result from the collection import endpoint.
 *
 * - accepted: number of rows successfully upserted into card_inventory
 * - rejected: number of rows that failed to parse or couldn't be resolved to an arena_id
 */
export interface ImportCollectionResult {
  accepted: number;
  rejected: number;
}

/**
 * Import a collection from an MTGA-format CSV file.
 *
 * File format (one card per line):
 *   <quantity> <CardName> (<SetCode>) <collectorNumber>
 * Example:
 *   4 Lightning Bolt (ONS) 197
 *
 * Posts as multipart/form-data with a single "file" field.
 * The BFF resolves each row to an arena_id via set_cards, upserts into
 * card_inventory, and returns accepted/rejected counts.
 */
export async function importCollection(file: File): Promise<ImportCollectionResult> {
  const fd = new FormData();
  fd.append('file', file);
  return postFormData<ImportCollectionResult>('/collection/import', fd);
}
