/**
 * Component tests for DaemonAuthStatusBadge.
 *
 * Ray's #144 verdict §3 contract:
 *   "authenticated"   → healthy state (green-adjacent, positive label)
 *   "setup_required"  → setup prompt (neutral/info, no error, no Retry)
 *   "keychain_error"  → actionable error with guidance text
 *   "auth_paused"     → paused indicator
 *   "unknown"         → NEUTRAL / setup-prompt state — NO Retry affordance,
 *                        NO error toast, no error class. This is the
 *                        BFF-only absence-of-data sentinel.
 */

import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DaemonAuthStatusBadge } from './DaemonAuthStatusBadge';
import type { DaemonAuthStatus } from '@/services/api/bffHealth';

describe('DaemonAuthStatusBadge', () => {
  // ── authenticated ──────────────────────────────────────────────────────────

  it('renders authenticated label for auth_status: authenticated', () => {
    render(<DaemonAuthStatusBadge auth_status="authenticated" />);
    expect(screen.getByTestId('daemon-auth-status')).toBeInTheDocument();
    expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Authenticated');
  });

  it('applies authenticated class for auth_status: authenticated', () => {
    render(<DaemonAuthStatusBadge auth_status="authenticated" />);
    expect(screen.getByTestId('daemon-auth-status')).toHaveClass('daemon-auth-authenticated');
  });

  it('does not render a Retry button for auth_status: authenticated', () => {
    render(<DaemonAuthStatusBadge auth_status="authenticated" />);
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  // ── setup_required ─────────────────────────────────────────────────────────

  it('renders setup prompt label for auth_status: setup_required', () => {
    render(<DaemonAuthStatusBadge auth_status="setup_required" />);
    expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Setup Required');
  });

  it('applies setup_required class for auth_status: setup_required', () => {
    render(<DaemonAuthStatusBadge auth_status="setup_required" />);
    expect(screen.getByTestId('daemon-auth-status')).toHaveClass('daemon-auth-setup-required');
  });

  it('does not render a Retry button for auth_status: setup_required', () => {
    render(<DaemonAuthStatusBadge auth_status="setup_required" />);
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  // ── keychain_error ─────────────────────────────────────────────────────────

  it('renders keychain error label for auth_status: keychain_error', () => {
    render(<DaemonAuthStatusBadge auth_status="keychain_error" />);
    expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Keychain Error');
  });

  it('applies keychain_error class for auth_status: keychain_error', () => {
    render(<DaemonAuthStatusBadge auth_status="keychain_error" />);
    expect(screen.getByTestId('daemon-auth-status')).toHaveClass('daemon-auth-keychain-error');
  });

  it('renders actionable guidance text for auth_status: keychain_error', () => {
    render(<DaemonAuthStatusBadge auth_status="keychain_error" />);
    // Must show some guidance — text may reference restart/reinstall
    expect(screen.getByTestId('daemon-auth-guidance')).toBeInTheDocument();
  });

  // ── auth_paused ────────────────────────────────────────────────────────────

  it('renders paused label for auth_status: auth_paused', () => {
    render(<DaemonAuthStatusBadge auth_status="auth_paused" />);
    expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Auth Paused');
  });

  it('applies auth_paused class for auth_status: auth_paused', () => {
    render(<DaemonAuthStatusBadge auth_status="auth_paused" />);
    expect(screen.getByTestId('daemon-auth-status')).toHaveClass('daemon-auth-paused');
  });

  it('does not render a Retry button for auth_status: auth_paused', () => {
    render(<DaemonAuthStatusBadge auth_status="auth_paused" />);
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  // ── unknown — critical contract: must render NEUTRAL, no Retry, no error ──

  it('renders neutral/setup label for auth_status: unknown (not an error)', () => {
    render(<DaemonAuthStatusBadge auth_status="unknown" />);
    // Must NOT contain "Error" or "error" in its label
    const text = screen.getByTestId('daemon-auth-status').textContent ?? '';
    expect(text.toLowerCase()).not.toContain('error');
    // Must show a neutral / checking label
    expect(text.length).toBeGreaterThan(0);
  });

  it('applies unknown/neutral class for auth_status: unknown (not an error class)', () => {
    render(<DaemonAuthStatusBadge auth_status="unknown" />);
    const el = screen.getByTestId('daemon-auth-status');
    expect(el).toHaveClass('daemon-auth-unknown');
    // Must NOT have any error class
    expect(el).not.toHaveClass('daemon-auth-keychain-error');
  });

  it('does NOT render a Retry button for auth_status: unknown (Ray verdict §3)', () => {
    render(<DaemonAuthStatusBadge auth_status="unknown" />);
    expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
  });

  it('does NOT render any error toast or error guidance for auth_status: unknown', () => {
    render(<DaemonAuthStatusBadge auth_status="unknown" />);
    expect(screen.queryByTestId('daemon-auth-guidance')).not.toBeInTheDocument();
  });

  // ── type safety ────────────────────────────────────────────────────────────

  it('renders something for every valid DaemonAuthStatus value', () => {
    const statuses: DaemonAuthStatus[] = [
      'authenticated',
      'setup_required',
      'keychain_error',
      'auth_paused',
      'unknown',
    ];
    for (const s of statuses) {
      const { unmount } = render(<DaemonAuthStatusBadge auth_status={s} />);
      expect(screen.getByTestId('daemon-auth-status')).toBeInTheDocument();
      unmount();
    }
  });
});
