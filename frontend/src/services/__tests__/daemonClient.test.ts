import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { get, post, put, del, postFormData } from '../daemonClient';

// We need the real getApiKey/setApiKey from apiClient for the auth header tests
import { setApiKey } from '../apiClient';
import { setRuntimeConfig, _resetRuntimeConfig } from '../../config/runtimeConfig';

// Mock fetch globally
const mockFetch = vi.fn();
global.fetch = mockFetch;

// ADR-077: daemonClient.getDaemonConfig() calls getRuntimeConfig().daemonUrl at request time.
const testRuntimeConfig = {
  clerkPublishableKey: 'pk_test_dGVzdA',
  bffUrl: 'http://localhost:8080/api/v1',
  sentryEnv: 'test',
  envLabel: 'test',
  daemonUrl: 'http://localhost:9001/api/v1',
  posthogHost: 'https://app.posthog.com',
};

describe('daemonClient', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    setRuntimeConfig(testRuntimeConfig);
  });

  afterEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    _resetRuntimeConfig();
  });

  // ---------------------------------------------------------------------------
  // Authorization header
  // ---------------------------------------------------------------------------

  describe('Authorization header injection', () => {
    it('should NOT include Authorization header when no API key is set', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { id: 1 } }),
      });

      await get('/test');

      const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      const headers = init.headers as Record<string, string>;
      expect(headers['Authorization']).toBeUndefined();
    });

    it('should include Authorization header when API key is stored', async () => {
      setApiKey('daemon-api-key-abc');
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { ok: true } }),
      });

      await get('/test');

      const [, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      const headers = init.headers as Record<string, string>;
      expect(headers['Authorization']).toBe('Bearer daemon-api-key-abc');
    });
  });

  // ---------------------------------------------------------------------------
  // Base URL routing — daemon routes hit 127.0.0.1:9001, NOT the cloud BFF
  // ---------------------------------------------------------------------------

  describe('base URL routing (#1436)', () => {
    it('GET /system/status targets daemon base URL (not cloud BFF)', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { status: 'ok' } }),
      });

      await get('/system/status');

      const [url] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/system/status');
      expect(url).not.toContain('8080');
    });

    it('POST /drafts/grade-pick targets daemon base URL (not cloud BFF)', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { grade: 'A' } }),
      });

      await post('/drafts/grade-pick', { session_id: 'abc' });

      const [url] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/drafts/grade-pick');
      expect(url).not.toContain('8080');
    });

    it('GET /drafts/{id}/current-pack targets daemon base URL', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { session_id: 'sess1', cards: [] } }),
      });

      await get('/drafts/sess1/current-pack');

      const [url] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/drafts/sess1/current-pack');
      expect(url).not.toContain('8080');
    });
  });

  // ---------------------------------------------------------------------------
  // GET
  // ---------------------------------------------------------------------------

  describe('get', () => {
    it('should make GET request to daemon base URL', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { items: [] } }),
      });

      const result = await get('/drafts');

      expect(mockFetch).toHaveBeenCalledWith(
        'http://127.0.0.1:9001/api/v1/drafts',
        expect.objectContaining({ method: 'GET' })
      );
      expect(result).toEqual({ items: [] });
    });

    it('should throw ApiRequestError on 4xx response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        statusText: 'Not Found',
        json: async () => ({ error: 'draft not found' }),
      });

      await expect(get('/drafts/missing')).rejects.toMatchObject({
        status: 404,
        message: 'draft not found',
      });
    });
  });

  // ---------------------------------------------------------------------------
  // POST
  // ---------------------------------------------------------------------------

  describe('post', () => {
    it('should make POST request with JSON body', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { created: true } }),
      });

      const result = await post('/matches', { format: 'Ranked' });

      const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/matches');
      expect(init.method).toBe('POST');
      expect(init.body).toBe(JSON.stringify({ format: 'Ranked' }));
      expect(result).toEqual({ created: true });
    });
  });

  // ---------------------------------------------------------------------------
  // PUT (#1436 — previously absent from daemonClient)
  // ---------------------------------------------------------------------------

  describe('put', () => {
    it('should make PUT request with JSON body to daemon base URL', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { updated: true } }),
      });

      const result = await put('/system/config', { key: 'value' });

      const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/system/config');
      expect(init.method).toBe('PUT');
      expect(init.body).toBe(JSON.stringify({ key: 'value' }));
      expect(result).toEqual({ updated: true });
      expect(url).not.toContain('8080');
    });

    it('should throw ApiRequestError on 4xx from PUT', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 400,
        statusText: 'Bad Request',
        json: async () => ({ error: 'invalid config' }),
      });

      await expect(put('/system/config', {})).rejects.toMatchObject({
        status: 400,
        message: 'invalid config',
      });
    });
  });

  // ---------------------------------------------------------------------------
  // DELETE (#1436 — previously absent from daemonClient)
  // ---------------------------------------------------------------------------

  describe('del', () => {
    it('should make DELETE request to daemon base URL', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: async () => ({}),
      });

      const result = await del('/system/session');

      const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/system/session');
      expect(init.method).toBe('DELETE');
      expect(result).toBeUndefined();
      expect(url).not.toContain('8080');
    });

    it('should throw ApiRequestError on 4xx from DELETE', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 404,
        statusText: 'Not Found',
        json: async () => ({ error: 'session not found' }),
      });

      await expect(del('/system/session')).rejects.toMatchObject({
        status: 404,
        message: 'session not found',
      });
    });
  });

  // ---------------------------------------------------------------------------
  // postFormData (#1436 — previously absent from daemonClient)
  // ---------------------------------------------------------------------------

  describe('postFormData', () => {
    it('should make POST request with FormData to daemon base URL', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { imported: 5 } }),
      });

      const fd = new FormData();
      fd.append('file', new Blob(['data']), 'test.csv');

      const result = await postFormData('/system/import', fd);

      const [url, init] = mockFetch.mock.calls[0] as [string, RequestInit];
      expect(url).toBe('http://127.0.0.1:9001/api/v1/system/import');
      expect(init.method).toBe('POST');
      expect(init.body).toBeInstanceOf(FormData);
      // Content-Type must NOT be set — browser sets it with multipart boundary
      const headers = init.headers as Record<string, string>;
      expect(headers['Content-Type']).toBeUndefined();
      expect(result).toEqual({ imported: 5 });
      expect(url).not.toContain('8080');
    });

    it('should throw ApiRequestError on error response', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: false,
        status: 422,
        statusText: 'Unprocessable Entity',
        json: async () => ({ error: 'invalid file format' }),
      });

      const fd = new FormData();
      await expect(postFormData('/system/import', fd)).rejects.toMatchObject({
        status: 422,
        message: 'invalid file format',
      });
    });
  });

  // ---------------------------------------------------------------------------
  // 204 No Content
  // ---------------------------------------------------------------------------

  describe('204 No Content handling', () => {
    it('should return undefined for 204 responses in GET', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: async () => ({}),
      });

      const result = await get('/quests/active');
      expect(result).toBeUndefined();
    });

    it('should return undefined for 204 responses in PUT', async () => {
      mockFetch.mockResolvedValueOnce({
        ok: true,
        status: 204,
        json: async () => ({}),
      });

      const result = await put('/system/config', {});
      expect(result).toBeUndefined();
    });
  });

  // ---------------------------------------------------------------------------
  // Timeout / abort
  // ---------------------------------------------------------------------------

  describe('request timeout', () => {
    it('should throw ApiRequestError with 408 status on AbortError', async () => {
      mockFetch.mockImplementationOnce(() => {
        const err = new Error('The operation was aborted');
        err.name = 'AbortError';
        return Promise.reject(err);
      });

      await expect(get('/slow')).rejects.toMatchObject({
        status: 408,
        message: 'Request timeout',
      });
    });
  });
});
