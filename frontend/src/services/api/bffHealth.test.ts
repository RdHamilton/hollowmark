/**
 * Tests for the bffHealth adapter — specifically the auth_status field
 * added in #112 / BFF PR #144.
 *
 * The adapter must:
 *   - Include auth_status in DaemonHealthResponse
 *   - Pass the full response body (including auth_status) to callers
 *   - Handle all 5 auth_status values transparently
 *
 * setup.ts globally mocks @/services/api/bffHealth so that other test files
 * don't have to care about the network. This file tests the REAL adapter, so
 * it unmocks first and uses MSW to intercept the real fetch call.
 */

// Must appear before any imports that might trigger the mock.
import { vi } from 'vitest';
vi.unmock('@/services/api/bffHealth');

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { getDaemonHealth } from './bffHealth';
import type { DaemonAuthStatus } from './bffHealth';

// The default apiClient baseUrl in test/dev is http://localhost:8080/api/v1
const HEALTH_URL = 'http://localhost:8080/api/v1/health/daemon';

const server = setupServer();

beforeEach(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => {
  server.resetHandlers();
  server.close();
});

function mockHealthEndpoint(body: object, status = 200) {
  server.use(
    http.get(HEALTH_URL, () =>
      HttpResponse.json(body, { status })
    )
  );
}

describe('getDaemonHealth — auth_status field', () => {
  it('returns auth_status: authenticated from response body', async () => {
    mockHealthEndpoint({ status: 'connected', auth_status: 'authenticated' });
    const result = await getDaemonHealth('tok');
    expect(result.auth_status).toBe('authenticated');
  });

  it('returns auth_status: setup_required from response body', async () => {
    mockHealthEndpoint({ status: 'disconnected', auth_status: 'setup_required' });
    const result = await getDaemonHealth('tok');
    expect(result.auth_status).toBe('setup_required');
  });

  it('returns auth_status: keychain_error from response body', async () => {
    mockHealthEndpoint({ status: 'connected', auth_status: 'keychain_error' });
    const result = await getDaemonHealth('tok');
    expect(result.auth_status).toBe('keychain_error');
  });

  it('returns auth_status: auth_paused from response body', async () => {
    mockHealthEndpoint({ status: 'connected', auth_status: 'auth_paused' });
    const result = await getDaemonHealth('tok');
    expect(result.auth_status).toBe('auth_paused');
  });

  it('returns auth_status: unknown from response body', async () => {
    mockHealthEndpoint({ status: 'disconnected', auth_status: 'unknown' });
    const result = await getDaemonHealth('tok');
    expect(result.auth_status).toBe('unknown');
  });

  it('still returns the status field alongside auth_status', async () => {
    mockHealthEndpoint({ status: 'connected', auth_status: 'authenticated' });
    const result = await getDaemonHealth('tok');
    expect(result.status).toBe('connected');
    expect(result.auth_status).toBe('authenticated');
  });
});

// Type-level: DaemonAuthStatus must be exported and cover all 5 values.
describe('DaemonAuthStatus type coverage', () => {
  it('all 5 auth_status values are assignable to DaemonAuthStatus', () => {
    const values: DaemonAuthStatus[] = [
      'authenticated',
      'setup_required',
      'keychain_error',
      'auth_paused',
      'unknown',
    ];
    expect(values).toHaveLength(5);
  });
});
