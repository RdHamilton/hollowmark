import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DaemonConnectionSection } from './DaemonConnectionSection';
import { gui } from '@/types/models';
import type { DaemonAuthStatus } from '@/services/api/bffHealth';

describe('DaemonConnectionSection', () => {
  const createConnectionStatus = (status: string) => new gui.ConnectionStatus({
    status,
    connected: status === 'connected',
    mode: status === 'connected' ? 'daemon' : 'standalone',
    url: 'ws://localhost:9999',
    port: 9999,
  });

  it('renders section title', () => {
    render(<DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />);
    expect(screen.getByText('Daemon Connection')).toBeInTheDocument();
  });

  // AC1–AC3: connection mode dropdown, daemon port input, reconnect button must NOT be present.
  it('does not render Connection Mode dropdown (AC1)', () => {
    render(<DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />);
    expect(screen.queryByText('Connection Mode')).not.toBeInTheDocument();
  });

  it('does not render Daemon Port input (AC2)', () => {
    render(<DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />);
    expect(screen.queryByText('Daemon Port')).not.toBeInTheDocument();
  });

  it('does not render Reconnect button (AC3)', () => {
    render(<DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />);
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  // AC5: ws://localhost:9999 hardcoded string must not appear in rendered output.
  it('does not render ws://localhost string (AC5)', () => {
    const { container } = render(
      <DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />
    );
    expect(container.textContent).not.toContain('ws://localhost');
  });

  // AC4 / AC8: Connection Status badge is retained and reflects real daemon health.
  describe('connection status badge (AC4 / AC8)', () => {
    it('shows connected status', () => {
      render(
        <DaemonConnectionSection connectionStatus={createConnectionStatus('connected')} />
      );
      expect(screen.getByText('Connected to Daemon')).toBeInTheDocument();
    });

    it('shows standalone status', () => {
      render(
        <DaemonConnectionSection connectionStatus={createConnectionStatus('standalone')} />
      );
      expect(screen.getByText('Standalone Mode')).toBeInTheDocument();
    });

    it('shows reconnecting status', () => {
      render(
        <DaemonConnectionSection connectionStatus={createConnectionStatus('reconnecting')} />
      );
      expect(screen.getByText('Reconnecting...')).toBeInTheDocument();
    });

    it('applies correct status class to badge', () => {
      render(
        <DaemonConnectionSection connectionStatus={createConnectionStatus('connected')} />
      );
      const badge = screen.getByTestId('connection-badge');
      expect(badge).toHaveClass('status-connected');
    });
  });

  // ── auth_status display (#112) ─────────────────────────────────────────────
  // DaemonConnectionSection accepts an optional auth_status prop and delegates
  // rendering to DaemonAuthStatusBadge. When auth_status is omitted the section
  // must still render without the auth row (graceful no-op).

  describe('auth_status display (#112)', () => {
    const renderWithAuth = (authStatus: DaemonAuthStatus) =>
      render(
        <DaemonConnectionSection
          connectionStatus={createConnectionStatus('connected')}
          auth_status={authStatus}
        />
      );

    it('renders the auth status row when auth_status is provided', () => {
      renderWithAuth('authenticated');
      expect(screen.getByTestId('daemon-auth-status')).toBeInTheDocument();
    });

    it('does NOT render auth status row when auth_status is omitted', () => {
      render(<DaemonConnectionSection connectionStatus={createConnectionStatus('connected')} />);
      expect(screen.queryByTestId('daemon-auth-status')).not.toBeInTheDocument();
    });

    it('shows authenticated state for auth_status: authenticated', () => {
      renderWithAuth('authenticated');
      expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Authenticated');
    });

    it('shows setup prompt for auth_status: setup_required (not an error)', () => {
      renderWithAuth('setup_required');
      expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Setup Required');
    });

    it('shows neutral/info for auth_status: unknown — no Retry button (Ray verdict §3)', () => {
      renderWithAuth('unknown');
      // Must NOT label this as an error
      const text = screen.getByTestId('daemon-auth-status').textContent ?? '';
      expect(text.toLowerCase()).not.toContain('error');
      // Must NOT show a Retry button
      expect(screen.queryByRole('button', { name: /retry/i })).not.toBeInTheDocument();
    });

    it('shows keychain error with guidance for auth_status: keychain_error', () => {
      renderWithAuth('keychain_error');
      expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Keychain Error');
      expect(screen.getByTestId('daemon-auth-guidance')).toBeInTheDocument();
    });

    it('shows paused indicator for auth_status: auth_paused', () => {
      renderWithAuth('auth_paused');
      expect(screen.getByTestId('daemon-auth-status').textContent).toContain('Auth Paused');
    });
  });
});
