/**
 * ConfigErrorScreen — ADR-077 config-load failure UI.
 *
 * Renders when `/config.json` cannot be fetched, parsed, or validated before
 * the SPA mounts. Three distinct branches drive copy and retry affordance.
 *
 * Per Tim's UX spec:
 *   hollowmark-docs/engineering/design/specs/adr-077-config-load-error-screen.md
 *
 * Stateless component — all branch logic is driven by the `branch` prop.
 * Retry loading state is owned by main.tsx (boot-state owner), not here.
 *
 * IMPORTANT: This component does NOT call Sentry.init, trackEvent, or
 * fireBootBeacon. Those are boot-sequence concerns owned by main.tsx.
 *
 * data-testid values (Tim spec §7 — stable selectors, no variations):
 *   config-error-screen           Root element (all branches)
 *   config-error-screen-icon      Icon wrapper
 *   config-error-screen-headline  <h1> (receives focus on mount)
 *   config-error-screen-body      <p>  body copy
 *   config-error-screen-retry     <button> (network branch + onRetry only)
 *   config-error-screen-version   version footnote (when appVersion provided)
 */

import { useEffect, useRef } from 'react';
import { SignalSlashIcon, WrenchScrewdriverIcon } from '@heroicons/react/24/outline';
import type { ConfigErrorScreenBranch } from '../config/runtimeConfig';

export type { ConfigErrorScreenBranch };

export interface ConfigErrorScreenProps {
  /**
   * Which failure branch triggered this screen.
   * - 'network':        fetch threw or returned non-2xx
   * - 'parse':          response.ok=true but body failed JSON.parse
   * - 'missing-fields': valid JSON but required fields absent/invalid
   */
  branch: ConfigErrorScreenBranch;

  /**
   * Callback invoked when the player clicks "Try Again".
   * Only rendered as a button when branch === 'network' and onRetry is defined.
   * Wire this to re-invoke the config fetch — NOT window.location.reload().
   */
  onRetry?: () => void;

  /**
   * App version string rendered as version footnote.
   * Pass import.meta.env.VITE_APP_VERSION (stays baked per ADR-077).
   * Footnote is omitted when absent or empty string.
   */
  appVersion?: string;
}

// ---------------------------------------------------------------------------
// Copy strings (Tim spec §2)
// ---------------------------------------------------------------------------

const COPY = {
  network: {
    headline: 'Could not reach VaultMTG',
    body: 'Check your network connection and try again. If the problem persists, VaultMTG may be temporarily unavailable.',
  },
  parse: {
    headline: 'VaultMTG has a setup problem',
    body: 'This is not your fault — something went wrong on our end. Our team has been notified. Please check back shortly.',
  },
  'missing-fields': {
    headline: 'VaultMTG has a setup problem',
    body: 'This is not your fault — something went wrong on our end. Our team has been notified. Please check back shortly.',
  },
} as const;

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export default function ConfigErrorScreen({
  branch,
  onRetry,
  appVersion,
}: ConfigErrorScreenProps) {
  const headlineRef = useRef<HTMLHeadingElement>(null);

  // Focus management — spec §6.2: focus headline on mount for screen readers
  useEffect(() => {
    headlineRef.current?.focus();
  }, []);

  const copy = COPY[branch];
  const showRetry = branch === 'network' && onRetry !== undefined;
  const showVersion = appVersion !== undefined && appVersion !== '';

  const Icon = branch === 'network' ? SignalSlashIcon : WrenchScrewdriverIcon;

  return (
    <div
      role="alert"
      aria-live="assertive"
      aria-atomic="true"
      data-testid="config-error-screen"
      style={{
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        justifyContent: 'center',
        minHeight: '100vh',
        textAlign: 'center',
        backgroundColor: 'var(--surface-base, #0D1117)',
      }}
    >
      <div
        style={{
          maxWidth: '400px',
          width: '100%',
          padding: '0 var(--space-4, 16px)',
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
        }}
      >
        {/* Icon */}
        <span
          data-testid="config-error-screen-icon"
          style={{ marginBottom: 'var(--space-4, 16px)' }}
        >
          <Icon
            aria-hidden="true"
            style={{
              width: '40px',
              height: '40px',
              color: 'var(--text-muted, #4E6080)',
            }}
          />
        </span>

        {/* Headline */}
        <h1
          ref={headlineRef}
          tabIndex={-1}
          data-testid="config-error-screen-headline"
          style={{
            fontFamily: 'var(--font-display, "Space Grotesk", sans-serif)',
            fontSize: 'var(--text-xl, 20px)',
            fontWeight: 600,
            lineHeight: 1.3,
            color: 'var(--text-primary, #F1F5F9)',
            marginBottom: 'var(--space-3, 12px)',
            marginTop: 0,
          }}
        >
          {copy.headline}
        </h1>

        {/* Body copy */}
        <p
          data-testid="config-error-screen-body"
          style={{
            fontFamily: 'var(--font-body, "Inter", sans-serif)',
            fontSize: 'var(--text-sm, 13px)',
            fontWeight: 400,
            lineHeight: 1.6,
            color: 'var(--text-secondary, #94A3B8)',
            maxWidth: '320px',
            marginBottom: 'var(--space-6, 24px)',
            marginTop: 0,
          }}
        >
          {copy.body}
        </p>

        {/* Retry button — Branch A only */}
        {showRetry && (
          <button
            type="button"
            data-testid="config-error-screen-retry"
            onClick={onRetry}
            style={{
              backgroundColor: 'var(--color-primary, #F5A623)',
              color: 'var(--text-inverse, #0D1117)',
              fontFamily: 'var(--font-body, "Inter", sans-serif)',
              fontSize: 'var(--text-sm, 13px)',
              fontWeight: 500,
              height: '36px',
              padding: '8px 20px',
              borderRadius: 'var(--radius-md, 8px)',
              border: 'none',
              cursor: 'pointer',
              marginBottom: 'var(--space-4, 16px)',
              // Mobile: full-width at ≤375px (handled via inline style; Tailwind not used here)
              width: '100%',
              maxWidth: '320px',
            }}
          >
            Try Again
          </button>
        )}

        {/* Version footnote */}
        {showVersion && (
          <span
            data-testid="config-error-screen-version"
            style={{
              fontFamily: 'var(--font-mono, "JetBrains Mono", monospace)',
              fontSize: 'var(--text-xs, 11px)',
              color: 'var(--text-muted, #4E6080)',
            }}
          >
            v{appVersion}
          </span>
        )}
      </div>
    </div>
  );
}
