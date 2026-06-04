import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { getDraftRatings } from './bffDraftRatings';
import { ApiRequestError } from '../apiClient';
import type { BffDraftRatingsResponse } from './bffDraftRatings';

const API_BASE = 'http://localhost:8080/api/v1';
const TEST_TOKEN = 'clerk-session-jwt-abc123';

const mockRatingsResponse: BffDraftRatingsResponse = {
  set_code: 'ONE',
  draft_format: 'PremierDraft',
  cached_at: '2024-01-15T12:00:00Z',
  card_ratings: [
    {
      arena_id: 12345,
      name: 'Phyrexian Obliterator',
      color: 'B',
      rarity: 'M',
      gihwr: 0.62,
      ohwr: 0.58,
      alsa: 2.1,
      ata: 3.4,
      gih_count: 1200,
    },
    {
      arena_id: 12346,
      name: 'Sword to Plowshares',
      color: 'W',
      rarity: 'U',
      gihwr: 0.59,
    },
  ],
  color_ratings: [
    {
      color_combination: 'WU',
      win_rate: 0.55,
      games_played: 5000,
    },
    {
      color_combination: 'BG',
      win_rate: 0.52,
    },
  ],
};

// MSW server — intercepts fetch calls made by the adapter.
//
// IMPORTANT: this handler enforces the Authorization header the way the real
// BFF (RequireClerkAuth) does — a request without a Bearer token gets a 401.
// The previous version of this test intercepted regardless of auth, which is
// why it never caught the missing-Authorization-header defect.
const server = setupServer(
  http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ params, request }) => {
    const auth = request.headers.get('Authorization');
    if (!auth || !auth.startsWith('Bearer ')) {
      return HttpResponse.json({ error: 'unauthorized' }, { status: 401 });
    }

    const { setCode, format } = params as { setCode: string; format: string };

    if (setCode === 'MISSING') {
      return HttpResponse.json(
        { error: 'no ratings found for the requested set/format' },
        { status: 404 }
      );
    }

    if (setCode === 'DEGRADED') {
      return HttpResponse.json(
        {
          ...mockRatingsResponse,
          set_code: setCode,
          draft_format: format as string,
        },
        {
          status: 200,
          headers: {
            'X-Cache-Degraded': 'true',
            'X-Cache-Age-Hours': '48',
          },
        }
      );
    }

    return HttpResponse.json(
      {
        ...mockRatingsResponse,
        set_code: setCode,
        draft_format: format as string,
      },
      { status: 200 }
    );
  })
);

beforeEach(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  server.close();
});

describe('getDraftRatings — Authorization header (Defect 1)', () => {
  it('sends Authorization: Bearer <token> when a token is provided', async () => {
    let capturedAuth: string | null = null;
    server.use(
      http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ request }) => {
        capturedAuth = request.headers.get('Authorization');
        return HttpResponse.json(mockRatingsResponse, { status: 200 });
      })
    );

    await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);

    expect(capturedAuth).toBe(`Bearer ${TEST_TOKEN}`);
  });

  it('omits the Authorization header when token is null', async () => {
    let hadAuthHeader = true;
    server.use(
      http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ request }) => {
        hadAuthHeader = request.headers.has('Authorization');
        return HttpResponse.json({ error: 'unauthorized' }, { status: 401 });
      })
    );

    await expect(getDraftRatings('ONE', 'PremierDraft', null)).rejects.toThrow();
    expect(hadAuthHeader).toBe(false);
  });

  it('throws ApiRequestError with status 401 when the BFF rejects an unauthenticated request', async () => {
    // Default handler 401s any request without a Bearer token — passing null
    // reproduces the original "no Authorization header" defect end-to-end.
    await expect(getDraftRatings('ONE', 'PremierDraft', null)).rejects.toMatchObject({
      status: 401,
    });
  });

  it('surfaces the BFF "unauthorized" error message on 401', async () => {
    try {
      await getDraftRatings('ONE', 'PremierDraft', null);
      throw new Error('expected getDraftRatings to throw');
    } catch (err) {
      expect(err).toBeInstanceOf(ApiRequestError);
      expect((err as ApiRequestError).message).toBe('unauthorized');
    }
  });
});

describe('getDraftRatings', () => {
  it('calls the correct BFF URL', async () => {
    let capturedUrl = '';
    server.use(
      http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(mockRatingsResponse, { status: 200 });
      })
    );

    await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);

    expect(capturedUrl).toBe(`${API_BASE}/draft-ratings/ONE/PremierDraft`);
  });

  it('returns card_ratings and color_ratings from response', async () => {
    const result = await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);

    expect(result.data.set_code).toBe('ONE');
    expect(result.data.draft_format).toBe('PremierDraft');
    expect(result.data.card_ratings).toHaveLength(2);
    expect(result.data.card_ratings[0].arena_id).toBe(12345);
    expect(result.data.card_ratings[0].name).toBe('Phyrexian Obliterator');
    expect(result.data.color_ratings).toHaveLength(2);
    expect(result.data.color_ratings[0].color_combination).toBe('WU');
  });

  it('returns cacheDegraded: false when X-Cache-Degraded header is absent', async () => {
    const result = await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);
    expect(result.cacheDegraded).toBe(false);
  });

  it('returns cacheDegraded: true and cacheAgeHours when headers are set', async () => {
    const result = await getDraftRatings('DEGRADED', 'PremierDraft', TEST_TOKEN);

    expect(result.cacheDegraded).toBe(true);
    expect(result.cacheAgeHours).toBe(48);
  });

  it('returns cacheAgeHours: undefined when X-Cache-Age-Hours header is absent', async () => {
    const result = await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);
    expect(result.cacheAgeHours).toBeUndefined();
  });

  it('URL-encodes setCode and format parameters', async () => {
    let capturedUrl = '';
    server.use(
      http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(mockRatingsResponse, { status: 200 });
      })
    );

    await getDraftRatings('ONE', 'Premier Draft', TEST_TOKEN);

    expect(capturedUrl).toContain('Premier%20Draft');
  });

  it('throws ApiRequestError on 404 response', async () => {
    await expect(getDraftRatings('MISSING', 'PremierDraft', TEST_TOKEN)).rejects.toThrow();
  });

  it('includes card rating optional fields when present', async () => {
    const result = await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);
    const card = result.data.card_ratings[0];

    expect(card.gihwr).toBe(0.62);
    expect(card.ohwr).toBe(0.58);
    expect(card.alsa).toBe(2.1);
    expect(card.ata).toBe(3.4);
    expect(card.gih_count).toBe(1200);
  });

  it('handles cards with only required fields', async () => {
    const result = await getDraftRatings('ONE', 'PremierDraft', TEST_TOKEN);
    const card = result.data.card_ratings[1];

    expect(card.arena_id).toBe(12346);
    expect(card.name).toBe('Sword to Plowshares');
    expect(card.ohwr).toBeUndefined();
  });
});

describe('getDraftRatings URL construction', () => {
  it('constructs the correct full URL including /api/v1 prefix', async () => {
    let capturedUrl = '';
    server.use(
      http.get(`${API_BASE}/draft-ratings/:setCode/:format`, ({ request }) => {
        capturedUrl = request.url;
        return HttpResponse.json(mockRatingsResponse, { status: 200 });
      })
    );

    await getDraftRatings('DSK', 'QuickDraft', TEST_TOKEN);

    expect(capturedUrl).toBe(`${API_BASE}/draft-ratings/DSK/QuickDraft`);
  });
});
