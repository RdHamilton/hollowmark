/**
 * BFF draft history adapter.
 *
 * Targets GET /api/v1/history/drafts on the BFF.
 * The endpoint is Clerk-protected and returns a page-paginated list of drafts.
 *
 * Response shape from BFF (paginatedResponse + draftResponse in history.go):
 *   { data: DraftHistoryItem[], total: number, page: number, limit: number }
 *
 * getDraftHistory accepts offset-based pagination params (limit/offset) for
 * backward compatibility with callers, and internally converts offset → page
 * before calling the BFF.
 */

import { getApiConfig, ApiRequestError } from '../apiClient';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface DraftHistoryItem {
  id: string;
  set_code: string;
  format: string;
  started_at: string;
  completed_at: string | null;
  wins: number;
  losses: number;
}

export interface DraftHistoryParams {
  limit?: number;
  offset?: number;
}

export interface DraftHistoryResponse {
  drafts: DraftHistoryItem[];
  total: number;
  limit: number;
  offset: number;
}

// ---------------------------------------------------------------------------
// Wire shape — mirrors paginatedResponse + draftResponse from history.go.
// ---------------------------------------------------------------------------

interface BffDraftHistoryResponse {
  data: DraftHistoryItem[];
  total: number;
  page: number;
  limit: number;
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/**
 * Fetch paginated draft history from the BFF.
 *
 * Targets: GET /api/v1/history/drafts
 *
 * The endpoint is Clerk-protected — pass the Clerk session token obtained via
 * `useAuth().getToken()` as the `clerkToken` parameter.
 *
 * Accepts offset-based pagination (limit, offset) and converts to the BFF's
 * page-based pagination (limit, page = floor(offset / limit) + 1).
 *
 * @param clerkToken  Clerk session JWT returned by useAuth().getToken()
 * @param params      Optional pagination params (limit, offset)
 */
export async function getDraftHistory(
  clerkToken: string,
  params: DraftHistoryParams = {}
): Promise<DraftHistoryResponse> {
  const { baseUrl } = getApiConfig();
  const url = new URL(`${baseUrl}/history/drafts`);

  const limit = params.limit ?? 20;
  const offset = params.offset ?? 0;

  url.searchParams.set('limit', String(limit));

  // Convert offset → page (BFF uses 1-based page pagination).
  const page = Math.floor(offset / limit) + 1;
  url.searchParams.set('page', String(page));

  const response = await fetch(url.toString(), {
    method: 'GET',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${clerkToken}`,
    },
  });

  if (!response.ok) {
    let errorMessage = response.statusText || 'Request failed';
    try {
      const body = (await response.json()) as { error?: string; message?: string };
      errorMessage = body.message ?? body.error ?? errorMessage;
    } catch {
      // ignore parse failure — use statusText
    }
    throw new ApiRequestError(errorMessage, response.status);
  }

  const wire = (await response.json()) as BffDraftHistoryResponse;

  // Map BFF wire shape → component interface.
  return {
    drafts: wire.data ?? [],
    total: wire.total ?? 0,
    limit: wire.limit ?? limit,
    offset,
  };
}
