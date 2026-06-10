/**
 * Consent API adapter — COPPA/ToS click-wrap evidence (COPPA #884).
 *
 * The BFF endpoint (POST /api/v1/account/consent) writes a server-canonical
 * consent_log row: user ID + timestamp + tos_version + privacy_policy_version
 * sourced from SSM config.  The SPA sends only the event_type; all version
 * fields are server-canonical and must NOT be supplied by the client.
 *
 * The endpoint requires a valid Clerk session JWT — call only when signed in.
 */

import { post } from '../apiClient';

/** Minimal request body for signup consent. Server ignores any extra fields. */
interface RecordSignupConsentRequest {
  event_type: 'signup';
}

/**
 * Record the signup consent event.
 *
 * Called exactly once per new account immediately after Clerk confirms the
 * new user.  Idempotency is enforced client-side by the
 * `useSignupConsentRecorder` hook (localStorage guard per Clerk user ID).
 *
 * Throws on any non-2xx response — the caller (ConsentGate) must handle
 * the error and block app entry until the write succeeds.
 */
export async function recordSignupConsent(): Promise<void> {
  const body: RecordSignupConsentRequest = { event_type: 'signup' };
  await post<void>('/account/consent', body);
}
