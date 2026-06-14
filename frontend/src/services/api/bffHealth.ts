/**
 * BFF daemon health adapter.
 *
 * Targets GET /api/v1/health/daemon on the BFF.
 * The endpoint is Clerk-protected and returns connection + auth status.
 *
 * Response shape (#144):
 *   { "status": "connected" | "disconnected",
 *     "auth_status": "authenticated" | "setup_required" | "keychain_error" | "auth_paused" | "unknown" }
 *
 * NOTE on "unknown": this is a BFF-only absence-of-data sentinel (old daemon /
 * no heartbeat yet). It is NOT an error state — the SPA must render it as a
 * neutral / setup-prompt, never show a Retry affordance or error toast for it.
 * (Ray verdict #144 §3.)
 */

import { getApiConfig, ApiRequestError } from '../apiClient';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export type DaemonHealthStatus = 'connected' | 'disconnected' | 'reconnecting';

/**
 * The 5-value auth_status union returned by GET /api/v1/health/daemon (#144).
 *
 * - "authenticated"  — daemon is signed in and healthy
 * - "setup_required" — daemon is running but the user has not yet signed in
 * - "keychain_error" — keychain access failed; actionable error, show guidance
 * - "auth_paused"    — auth flow is paused (user-initiated or rate-limited)
 * - "unknown"        — BFF-only sentinel: no heartbeat row yet, or pre-#144
 *                       daemon. NOT an error — render neutral/setup-prompt.
 */
export type DaemonAuthStatus =
  | 'authenticated'
  | 'setup_required'
  | 'keychain_error'
  | 'auth_paused'
  | 'unknown';

export interface DaemonHealthResponse {
  status: DaemonHealthStatus;
  /** Auth status from the most recent daemon heartbeat. Present on BFF >= #144. */
  auth_status: DaemonAuthStatus;
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/**
 * Fetch the daemon health status from the BFF.
 *
 * Targets: GET /api/v1/health/daemon
 *
 * The endpoint is Clerk-protected — pass the Clerk session token obtained via
 * `useAuth().getToken()` as the `clerkToken` parameter.
 *
 * @param clerkToken  Clerk session JWT returned by useAuth().getToken()
 */
export async function getDaemonHealth(
  clerkToken: string
): Promise<DaemonHealthResponse> {
  const { baseUrl } = getApiConfig();
  const url = `${baseUrl}/health/daemon`;

  const response = await fetch(url, {
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

  return response.json() as Promise<DaemonHealthResponse>;
}
