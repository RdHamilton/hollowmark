/**
 * accountProfile.ts API adapter tests — #888 GDPR Right to Rectification
 *
 * Tests that the adapter calls the correct BFF path with the right shape.
 * The apiClient module is mocked so these tests do NOT hit the network.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { patchAccountProfile } from './accountProfile';
import type { AccountProfilePatchRequest } from './accountProfile';

// ---------------------------------------------------------------------------
// Mock the BFF apiClient so no real HTTP calls are made.
// ---------------------------------------------------------------------------

vi.mock('../apiClient', () => ({
  patch: vi.fn(),
}));

import { patch } from '../apiClient';

const mockPatch = vi.mocked(patch);

beforeEach(() => {
  vi.clearAllMocks();
});

// ---------------------------------------------------------------------------
// patchAccountProfile
// ---------------------------------------------------------------------------

describe('patchAccountProfile', () => {
  it('calls patch with /account/profile and the full payload', async () => {
    mockPatch.mockResolvedValue(undefined);

    const payload: AccountProfilePatchRequest = {
      first_name: 'Janet',
      last_name: 'Smith',
      email: 'janet@example.com',
    };

    await patchAccountProfile(payload);

    expect(mockPatch).toHaveBeenCalledWith('/account/profile', payload);
  });

  it('calls patch with only changed fields (partial update)', async () => {
    mockPatch.mockResolvedValue(undefined);

    await patchAccountProfile({ first_name: 'Teferi' });

    expect(mockPatch).toHaveBeenCalledWith('/account/profile', { first_name: 'Teferi' });
  });

  it('calls patch with only email field when only email changes', async () => {
    mockPatch.mockResolvedValue(undefined);

    await patchAccountProfile({ email: 'new@example.com' });

    expect(mockPatch).toHaveBeenCalledWith('/account/profile', { email: 'new@example.com' });
  });

  it('propagates errors thrown by patch', async () => {
    mockPatch.mockRejectedValue(new Error('network error'));

    await expect(patchAccountProfile({ first_name: 'X' })).rejects.toThrow('network error');
  });

  it('resolves without value on success (204 No Content pattern)', async () => {
    mockPatch.mockResolvedValue(undefined);

    const result = await patchAccountProfile({ last_name: 'Hero' });
    expect(result).toBeUndefined();
  });
});
