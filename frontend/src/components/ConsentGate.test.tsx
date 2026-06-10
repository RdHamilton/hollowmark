/**
 * Component tests for ConsentGate (COPPA #884).
 *
 * ConsentGate wraps children and:
 *   - while status === 'loading': renders <LoadingSpinner>, does NOT render children
 *   - when  status === 'done':   renders children normally
 *   - when  status === 'error':  renders an error message + Retry button, NOT children
 *   - when  status === 'idle':   renders children (unsigned-in user; gate is a no-op)
 *
 * The gate also must NOT render for unauthenticated users (signed-out state
 * bypasses the consent check because the BFF endpoint requires a Clerk session).
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';

// ── hook mock ──────────────────────────────────────────────────────────────
type ConsentStatus = 'idle' | 'loading' | 'done' | 'error';

const mockRetry = vi.fn();
const mockUseSignupConsentRecorder = vi.fn<[], { status: ConsentStatus; retry: () => void }>();

vi.mock('@/hooks/useSignupConsentRecorder', () => ({
  useSignupConsentRecorder: () => mockUseSignupConsentRecorder(),
}));

import ConsentGate from './ConsentGate';

// ─────────────────────────────────────────────────────────────────────────────

describe('ConsentGate', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  const child = <div data-testid="protected-child">App Content</div>;

  it('renders children when status is done', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'done', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    expect(screen.getByTestId('protected-child')).toBeInTheDocument();
    expect(screen.queryByRole('status')).not.toBeInTheDocument();
  });

  it('renders children when status is idle (unauthenticated pass-through)', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'idle', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    expect(screen.getByTestId('protected-child')).toBeInTheDocument();
  });

  it('renders LoadingSpinner and NOT children when status is loading', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'loading', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    expect(screen.queryByTestId('protected-child')).not.toBeInTheDocument();
    // LoadingSpinner renders a spinner element; we detect via the loading container
    expect(screen.getByTestId('consent-gate-loading')).toBeInTheDocument();
  });

  it('renders error state and NOT children when status is error', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'error', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    expect(screen.queryByTestId('protected-child')).not.toBeInTheDocument();
    expect(screen.getByTestId('consent-gate-error')).toBeInTheDocument();
  });

  it('error state contains a Retry button', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'error', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    expect(screen.getByTestId('consent-gate-retry-btn')).toBeInTheDocument();
  });

  it('Retry button calls retry()', () => {
    mockUseSignupConsentRecorder.mockReturnValue({ status: 'error', retry: mockRetry });
    render(<ConsentGate>{child}</ConsentGate>);
    fireEvent.click(screen.getByTestId('consent-gate-retry-btn'));
    expect(mockRetry).toHaveBeenCalledOnce();
  });
});
