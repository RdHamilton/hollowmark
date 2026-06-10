/**
 * Account profile API adapter — GDPR Right to Rectification (#888)
 *
 * Calls Ben's BFF endpoint: PATCH /api/v1/account/profile
 *
 * This is a fire-and-sync adapter: the Clerk mutation (useUser().update() or
 * createEmailAddress chain) is always the primary source of truth. This call
 * propagates the change to VaultMTG's PostgreSQL user record (audit log + email
 * sync). Callers MUST call this only AFTER a successful Clerk mutation. If
 * this call fails, the caller swallows the error (non-blocking) because Clerk
 * is authoritative — the BFF will re-sync from Clerk on the next authenticated
 * request.
 *
 * Routes through the BFF apiClient (Clerk-session-authed endpoint, mounted
 * under ClerkAuthMiddleware). A valid Clerk session JWT is required.
 */

import { patch } from '../apiClient';

/** Fields that can be rectified on the profile. All optional (partial update). */
export interface AccountProfilePatchRequest {
  first_name?: string;
  last_name?: string;
  email?: string;
}

/**
 * Propagate a profile update to the VaultMTG BFF after a successful Clerk
 * mutation. Non-blocking on failure — Clerk is the source of truth.
 *
 * @param payload - Only the fields that changed; unused fields must be omitted.
 */
export async function patchAccountProfile(
  payload: AccountProfilePatchRequest,
): Promise<void> {
  return patch('/account/profile', payload);
}
