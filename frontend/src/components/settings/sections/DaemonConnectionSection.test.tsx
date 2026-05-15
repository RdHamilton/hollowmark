import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { DaemonConnectionSection } from './DaemonConnectionSection';
import { gui } from '@/types/models';

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
});
