/**
 * Tests for the consent API adapter (COPPA #884).
 *
 * recordSignupConsent() must:
 *   - POST to /account/consent with { event_type: 'signup' }
 *   - resolve (void) on a 201 response
 *   - reject (throw) on any non-201 response
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ── module-level mock BEFORE the module under test is imported ──────────────
const mockPost = vi.fn();
vi.mock('../apiClient', () => ({
  post: (...args: unknown[]) => mockPost(...args),
}));

// Import AFTER mock is set up
import { recordSignupConsent } from './consent';

describe('recordSignupConsent', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('calls POST /account/consent with event_type signup', async () => {
    mockPost.mockResolvedValueOnce(undefined);
    await recordSignupConsent();
    expect(mockPost).toHaveBeenCalledOnce();
    expect(mockPost).toHaveBeenCalledWith('/account/consent', { event_type: 'signup' });
  });

  it('resolves void on success', async () => {
    mockPost.mockResolvedValueOnce(undefined);
    await expect(recordSignupConsent()).resolves.toBeUndefined();
  });

  it('propagates rejection when the POST fails', async () => {
    mockPost.mockRejectedValueOnce(new Error('network error'));
    await expect(recordSignupConsent()).rejects.toThrow('network error');
  });

  it('does NOT include tos_version or privacy_policy_version in the request body (server-canonical)', async () => {
    mockPost.mockResolvedValueOnce(undefined);
    await recordSignupConsent();
    const body = mockPost.mock.calls[0][1] as Record<string, unknown>;
    expect(body).not.toHaveProperty('tos_version');
    expect(body).not.toHaveProperty('privacy_policy_version');
  });
});
