/**
 * account.ts API adapter tests — #887 GDPR Right to Erasure
 *
 * Tests that the adapter exposes the correct types and calls the right BFF
 * paths. The apiClient module is mocked so these tests do NOT hit the network.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { deleteAccount, getAccountDeletionStatus } from './account';

// ---------------------------------------------------------------------------
// Mock the BFF apiClient so no real HTTP calls are made.
// ---------------------------------------------------------------------------

vi.mock('../apiClient', () => ({
  del: vi.fn(),
  get: vi.fn(),
}));

import { del, get } from '../apiClient';

const mockDel = vi.mocked(del);
const mockGet = vi.mocked(get);

beforeEach(() => {
  vi.clearAllMocks();
});

describe('deleteAccount', () => {
  it('calls del with /account', async () => {
    mockDel.mockResolvedValue({ job_id: 'abc-123', message: 'Deletion scheduled.' });

    const result = await deleteAccount();

    expect(mockDel).toHaveBeenCalledWith('/account');
    expect(result.job_id).toBe('abc-123');
  });

  it('surfaces the optional message field when present', async () => {
    mockDel.mockResolvedValue({ job_id: 'xyz', message: 'ok' });

    const result = await deleteAccount();
    expect(result.message).toBe('ok');
  });

  it('works when message is absent (optional field)', async () => {
    mockDel.mockResolvedValue({ job_id: 'no-msg' });

    const result = await deleteAccount();
    expect(result.job_id).toBe('no-msg');
    expect(result.message).toBeUndefined();
  });

  it('propagates errors thrown by del', async () => {
    mockDel.mockRejectedValue(new Error('network error'));

    await expect(deleteAccount()).rejects.toThrow('network error');
  });
});

describe('getAccountDeletionStatus', () => {
  it('calls get with the correct path and skipErrorAnalytics:true', async () => {
    mockGet.mockResolvedValue({
      job_id: 'abc-123',
      status: 'pending',
      requested_at: '2026-06-10T00:00:00Z',
    });

    await getAccountDeletionStatus('abc-123');

    expect(mockGet).toHaveBeenCalledWith(
      '/account/deletion-status/abc-123',
      { skipErrorAnalytics: true },
    );
  });

  it('returns pending status correctly', async () => {
    mockGet.mockResolvedValue({
      job_id: 'abc-123',
      status: 'pending',
      requested_at: '2026-06-10T00:00:00Z',
    });

    const result = await getAccountDeletionStatus('abc-123');
    expect(result.status).toBe('pending');
  });

  it('returns completed status correctly', async () => {
    mockGet.mockResolvedValue({
      job_id: 'abc-123',
      status: 'completed',
      requested_at: '2026-06-10T00:00:00Z',
      completed_at: '2026-06-10T00:01:00Z',
    });

    const result = await getAccountDeletionStatus('abc-123');
    expect(result.status).toBe('completed');
    expect(result.completed_at).toBe('2026-06-10T00:01:00Z');
  });

  it('propagates errors thrown by get (transport error mid-poll)', async () => {
    mockGet.mockRejectedValue(new Error('503 Service Unavailable'));

    await expect(getAccountDeletionStatus('abc-123')).rejects.toThrow('503 Service Unavailable');
  });
});
