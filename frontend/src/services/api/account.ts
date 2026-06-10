/**
 * Account API adapter — GDPR Right to Erasure (#887)
 *
 * Routes through the BFF apiClient (Clerk-session-authed endpoints).
 * The BFF mounts these under ClerkAuthMiddleware — a valid Clerk session
 * JWT is required for both calls.
 *
 * Status contract: GET /account/deletion-status/{job_id} returns ONLY
 * 'pending' | 'completed'. There is no 'running', 'failed', or 'error'
 * field at this endpoint. Cascade failure surfaces via Bianca's AC5
 * email + Sentry tail — NOT through this endpoint. (Verified against
 * account_deletion_status.go + DeletionJobStatus struct, PR #3088.)
 */

import { del, get } from '../apiClient';

export interface AccountDeletionResponse {
  job_id: string;
  /** Present server-side; the SPA ignores the value but types it to
   * avoid runtime surprise if the field appears in the response body. */
  message?: string;
}

/** Only 'pending' and 'completed' — the BFF cannot produce other values. */
export type DeletionJobStatus = 'pending' | 'completed';

export interface AccountDeletionStatusResponse {
  job_id: string;
  status: DeletionJobStatus;
  requested_at: string;
  completed_at?: string;
}

/**
 * Trigger account deletion (GDPR Art. 17). Returns a job_id for polling.
 * Idempotent — a second call returns the existing job_id.
 * Caller must be Clerk-session-authed.
 */
export async function deleteAccount(): Promise<AccountDeletionResponse> {
  return del<AccountDeletionResponse>('/account');
}

/**
 * Poll deletion job status.
 *
 * Returns 404 if the job_id does not belong to the caller — cross-user
 * reads are blocked server-side (BFF scopes by ClerkUserID from context).
 *
 * skipErrorAnalytics is mandatory: this endpoint is polled on a 5-second
 * interval and must not emit error_data_load_failed events on every tick
 * if the request is temporarily unreachable.
 */
export async function getAccountDeletionStatus(
  jobId: string,
): Promise<AccountDeletionStatusResponse> {
  return get<AccountDeletionStatusResponse>(
    `/account/deletion-status/${jobId}`,
    { skipErrorAnalytics: true },
  );
}
