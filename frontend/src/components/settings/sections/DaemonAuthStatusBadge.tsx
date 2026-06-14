/**
 * DaemonAuthStatusBadge
 *
 * Displays the daemon auth_status from GET /api/v1/health/daemon (#144).
 * Maps each of the 5 BFF values to a Settings display state.
 *
 * Ray verdict §3: "unknown" is a BFF-only absence-of-data sentinel — NOT an
 * error. Render it as a neutral / setup-prompt. Never show a Retry affordance
 * or error guidance for "unknown".
 */

import type { DaemonAuthStatus } from '@/services/api/bffHealth';

interface DaemonAuthStatusBadgeProps {
  auth_status: DaemonAuthStatus;
}

interface AuthStatusConfig {
  label: string;
  cssClass: string;
  /** Actionable guidance text. Only set for states that need user action. */
  guidance?: string;
}

function getConfig(auth_status: DaemonAuthStatus): AuthStatusConfig {
  switch (auth_status) {
    case 'authenticated':
      return {
        label: 'Authenticated',
        cssClass: 'daemon-auth-authenticated',
      };
    case 'setup_required':
      return {
        label: 'Setup Required',
        cssClass: 'daemon-auth-setup-required',
      };
    case 'keychain_error':
      return {
        label: 'Keychain Error',
        cssClass: 'daemon-auth-keychain-error',
        guidance: 'The daemon could not access the system keychain. Try restarting the daemon or reinstalling it from the download page.',
      };
    case 'auth_paused':
      return {
        label: 'Auth Paused',
        cssClass: 'daemon-auth-paused',
      };
    case 'unknown':
      // BFF-only sentinel: no heartbeat data yet (pre-#144 daemon or fresh
      // install). This is NOT an error — render neutral, no Retry.
      return {
        label: 'Connecting...',
        cssClass: 'daemon-auth-unknown',
      };
  }
}

export function DaemonAuthStatusBadge({ auth_status }: DaemonAuthStatusBadgeProps) {
  const config = getConfig(auth_status);

  return (
    <div className="daemon-auth-status-row">
      <span
        className={`daemon-auth-status-badge ${config.cssClass}`}
        data-testid="daemon-auth-status"
        aria-label={`Authentication status: ${config.label}`}
      >
        {config.label}
      </span>
      {config.guidance && (
        <p
          className="daemon-auth-guidance"
          data-testid="daemon-auth-guidance"
        >
          {config.guidance}
        </p>
      )}
    </div>
  );
}
