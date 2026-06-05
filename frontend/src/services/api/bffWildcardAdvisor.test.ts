/**
 * Unit tests for bffWildcardAdvisor adapter.
 *
 * Verifies:
 * - Happy path: successful 200 response, headers parsed.
 * - 409 → ApiRequestError with status 409 (collection not synced).
 * - 503 → ApiRequestError with status 503 (BFF degraded).
 * - Cache-degraded header parsing.
 * - Authorization header is set when token is provided.
 * - Authorization header is omitted when token is null.
 *
 * Ray's note: detection of 409/503 MUST use ApiRequestError.status, NOT body
 * string matching — the body "error" field is informational, not a contract.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { getWildcardRecommendations } from './bffWildcardAdvisor';
import { ApiRequestError } from '../apiClient';

// Mock the apiClient module so getApiConfig() returns a predictable baseUrl.
vi.mock('../apiClient', async () => {
  const actual = await vi.importActual<typeof import('../apiClient')>('../apiClient');
  return {
    ...actual,
    getApiConfig: () => ({ baseUrl: 'http://localhost:8080/api/v1' }),
  };
});

const mockRecommendation = {
  arena_id: 12345,
  name: 'Sheoldred, the Apocalypse',
  rarity: 'mythic' as const,
  owned_copies: 2,
  missing_copies: 2,
  gihwr: 62.1,
  archetype_count: 5,
  format_context: 'Appears in 5 top Standard archetypes',
  set_code: 'DMU',
};

const mockResponse = {
  format: 'Standard' as const,
  recommendations: [mockRecommendation],
  wildcard_budget: { common: 10, uncommon: 8, rare: 4, mythic: 1 },
  ratings_cached_at: '2026-06-04T10:00:00Z',
};

function makeFetchResponse(
  body: unknown,
  status = 200,
  headers: Record<string, string> = {}
): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? 'OK' : 'Error',
    json: async () => body,
    headers: {
      get: (name: string) => headers[name.toLowerCase()] ?? null,
    },
  } as unknown as Response;
}

describe('bffWildcardAdvisor', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  describe('getWildcardRecommendations', () => {
    it('returns data on a successful 200 response', async () => {
      vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse(mockResponse)
      );

      const result = await getWildcardRecommendations('Standard', 'tok_test');

      expect(result.data).toEqual(mockResponse);
      expect(result.cacheDegraded).toBe(false);
      expect(result.cacheAgeHours).toBeUndefined();
    });

    it('parses x-cache-degraded and x-cache-age-hours headers', async () => {
      vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse(mockResponse, 200, {
          'x-cache-degraded': 'true',
          'x-cache-age-hours': '36.5',
        })
      );

      const result = await getWildcardRecommendations('Standard', 'tok_test');

      expect(result.cacheDegraded).toBe(true);
      expect(result.cacheAgeHours).toBe(36.5);
    });

    it('throws ApiRequestError with status 409 when collection is not synced', async () => {
      vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse({ error: 'collection_not_synced' }, 409)
      );

      await expect(
        getWildcardRecommendations('Standard', 'tok_test')
      ).rejects.toMatchObject({ status: 409 });
    });

    it('throws ApiRequestError with status 503 when BFF is degraded', async () => {
      vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse({ error: 'service_unavailable' }, 503)
      );

      await expect(
        getWildcardRecommendations('Standard', 'tok_test')
      ).rejects.toMatchObject({ status: 503 });
    });

    it('includes Authorization header when token is provided', async () => {
      const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse(mockResponse)
      );

      await getWildcardRecommendations('Historic', 'my-clerk-token');

      const [, options] = fetchSpy.mock.calls[0];
      const headers = (options as RequestInit).headers as Record<string, string>;
      expect(headers['Authorization']).toBe('Bearer my-clerk-token');
    });

    it('omits Authorization header when token is null', async () => {
      const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse(mockResponse)
      );

      await getWildcardRecommendations('Standard', null);

      const [, options] = fetchSpy.mock.calls[0];
      const headers = (options as RequestInit).headers as Record<string, string>;
      expect(headers['Authorization']).toBeUndefined();
    });

    it('encodes the format in the query string', async () => {
      const fetchSpy = vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse(mockResponse)
      );

      await getWildcardRecommendations('Explorer', 'tok_test');

      const [url] = fetchSpy.mock.calls[0];
      expect(url as string).toContain('format=Explorer');
    });

    it('the thrown error is an instance of ApiRequestError for non-ok responses', async () => {
      vi.spyOn(global, 'fetch').mockResolvedValueOnce(
        makeFetchResponse({ error: 'unauthorized' }, 401)
      );

      const err = await getWildcardRecommendations('Standard', null).catch(
        (e) => e
      );
      expect(err).toBeInstanceOf(ApiRequestError);
      expect(err.status).toBe(401);
    });
  });
});
