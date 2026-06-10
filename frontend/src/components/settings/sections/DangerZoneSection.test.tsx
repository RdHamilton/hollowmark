import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { DangerZoneSection } from './DangerZoneSection';

/**
 * DangerZoneSection tests — covers the standalone Danger Zone accordion
 * section extracted from DataRecoverySection in #2027.
 *
 * All uninstall tests were previously in DataRecoverySection.test.tsx and
 * are now maintained here as the single source of truth for uninstall UI.
 */

describe('DangerZoneSection', () => {
  // ---------------------------------------------------------------------------
  // AC1: DangerZoneSection is its own component and renders its own title
  // ---------------------------------------------------------------------------

  describe('AC1 — standalone section with its own title', () => {
    it('renders null when onUninstallDaemon is not provided', () => {
      const { container } = render(
        <DangerZoneSection isConnected={true} />,
      );
      expect(container.firstChild).toBeNull();
    });

    it('fires console.warn in DEV mode when onUninstallDaemon is omitted', () => {
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      render(<DangerZoneSection isConnected={true} />);
      expect(warnSpy).toHaveBeenCalledWith(
        '[DangerZoneSection] onUninstallDaemon prop is undefined — component will render null. Check parent component.',
      );
      warnSpy.mockRestore();
    });

    it('does not fire console.warn when onUninstallDaemon is provided', () => {
      const warnSpy = vi.spyOn(console, 'warn').mockImplementation(() => {});
      render(<DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />);
      expect(warnSpy).not.toHaveBeenCalled();
      warnSpy.mockRestore();
    });

    it('renders the section title when onUninstallDaemon is provided', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByText(/Danger Zone/i)).toBeInTheDocument();
    });

    it('renders the section data-testid', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByTestId('danger-zone-section')).toBeInTheDocument();
    });

    it('renders the section description', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByText(/Stop the local daemon and remove its startup entry/i)).toBeInTheDocument();
    });
  });

  // ---------------------------------------------------------------------------
  // AC3 — uninstall functionality is unchanged: all existing flow tests
  // ---------------------------------------------------------------------------

  describe('AC3 — uninstall flow unchanged', () => {
    it('renders the uninstall button when onUninstallDaemon is provided', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i })).toBeInTheDocument();
    });

    it('disables the uninstall button when daemon is not connected', () => {
      render(
        <DangerZoneSection isConnected={false} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i })).toBeDisabled();
    });

    it('shows daemon-must-be-running hint when not connected', () => {
      render(
        <DangerZoneSection isConnected={false} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByText(/Daemon must be running to trigger uninstall/i)).toBeInTheDocument();
    });

    it('enables the uninstall button when connected', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i })).not.toBeDisabled();
    });

    it('does NOT show daemon hint when connected', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      expect(
        screen.queryByText(/Daemon must be running to trigger uninstall/i),
      ).not.toBeInTheDocument();
    });

    it('shows the confirmation panel after clicking Uninstall', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      expect(screen.getByRole('button', { name: /Confirm Uninstall/i })).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /Cancel/i })).toBeInTheDocument();
      expect(screen.getByText(/Also wipe my local config/i)).toBeInTheDocument();
    });

    it('cancel returns to the initial state', () => {
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={vi.fn()} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Cancel/i }));
      expect(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i })).toBeInTheDocument();
      expect(screen.queryByRole('button', { name: /Confirm Uninstall/i })).not.toBeInTheDocument();
    });

    it('confirm fires onUninstallDaemon with purge=false and renders the backend message', async () => {
      const onUninstallDaemon = vi
        .fn()
        .mockResolvedValue(
          'Daemon stopped and removed from launchd. Drag VaultMTG to the Trash to remove the app bundle.',
        );
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(onUninstallDaemon).toHaveBeenCalledWith(false);
      });
      await waitFor(() => {
        expect(
          screen.getByText(/Drag VaultMTG to the Trash to remove the app bundle/i),
        ).toBeInTheDocument();
      });
    });

    it('confirm passes purge=true when the checkbox is ticked', async () => {
      const onUninstallDaemon = vi
        .fn()
        .mockResolvedValue(
          'Daemon stopped, removed from launchd, and config wiped. Drag VaultMTG to the Trash to remove the app bundle.',
        );
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      const purgeCheckbox = screen.getByRole('checkbox', { name: /Also wipe my local config/i });
      fireEvent.click(purgeCheckbox);
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(onUninstallDaemon).toHaveBeenCalledWith(true);
      });
      await waitFor(() => {
        expect(screen.getByText(/config wiped/i)).toBeInTheDocument();
      });
    });

    it('falls back to a neutral message when the backend returns an empty string', async () => {
      const onUninstallDaemon = vi.fn().mockResolvedValue('');
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(screen.getByText(/Daemon uninstall scheduled/i)).toBeInTheDocument();
      });
    });

    it('renders an error message when the uninstall call rejects', async () => {
      const onUninstallDaemon = vi.fn().mockRejectedValue(new Error('boom'));
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(screen.getByText(/boom/i)).toBeInTheDocument();
      });
    });

    it('shows success result testid after successful uninstall', async () => {
      const onUninstallDaemon = vi.fn().mockResolvedValue('Daemon stopped.');
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(screen.getByTestId('danger-zone-success-result')).toBeInTheDocument();
      });
    });

    it('shows error result testid when uninstall fails', async () => {
      const onUninstallDaemon = vi.fn().mockRejectedValue(new Error('failed'));
      render(
        <DangerZoneSection isConnected={true} onUninstallDaemon={onUninstallDaemon} />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Uninstall VaultMTG Daemon/i }));
      fireEvent.click(screen.getByRole('button', { name: /Confirm Uninstall/i }));

      await waitFor(() => {
        expect(screen.getByTestId('danger-zone-error-result')).toBeInTheDocument();
      });
    });
  });

  // ---------------------------------------------------------------------------
  // AC2 — DataRecoverySection no longer contains Danger Zone
  // (tested implicitly — the DataRecoverySection test file asserts this)
  // ---------------------------------------------------------------------------
});

// ---------------------------------------------------------------------------
// DangerZoneSection — account deletion (#887)
// ---------------------------------------------------------------------------

import { within, act } from '@testing-library/react';
import type { AccountDeletionStatusResponse } from '../AccountDeletionModal';
import type { DangerZoneSectionProps } from './DangerZoneSection';

describe('DangerZoneSection — account deletion (#887)', () => {
  const baseProps: DangerZoneSectionProps = {
    isConnected: true,
    onUninstallDaemon: vi.fn().mockResolvedValue(''),
  };

  it('renders "Delete my account" button when onDeleteAccount + onGetDeletionStatus are provided', () => {
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={vi.fn()}
        onGetDeletionStatus={vi.fn()}
      />,
    );
    expect(screen.getByTestId('danger-zone-delete-account-button')).toBeInTheDocument();
  });

  it('does NOT render "Delete my account" button when props are omitted', () => {
    render(<DangerZoneSection {...baseProps} />);
    expect(screen.queryByTestId('danger-zone-delete-account-button')).not.toBeInTheDocument();
  });

  it('opens the confirmation modal when "Delete my account" is clicked', () => {
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={vi.fn()}
        onGetDeletionStatus={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('cancel: modal closes and entry button is visible again', () => {
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={vi.fn()}
        onGetDeletionStatus={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    fireEvent.click(within(dialog).getByRole('button', { name: /Cancel/i }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    expect(screen.getByTestId('danger-zone-delete-account-button')).toBeInTheDocument();
  });

  it('happy path: confirm calls onDeleteAccount + transitions to polling then terminal-success', async () => {
    const mockStatus: AccountDeletionStatusResponse = {
      job_id: 'job-1',
      status: 'completed',
      requested_at: '2026-06-10T00:00:00Z',
    };
    const onDeleteAccount = vi.fn().mockResolvedValue({ job_id: 'job-1' });
    const onGetDeletionStatus = vi.fn().mockResolvedValue(mockStatus);

    vi.useFakeTimers();
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={onDeleteAccount}
        onGetDeletionStatus={onGetDeletionStatus}
      />,
    );

    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: /Delete my account/i }));
    });

    // Advance one poll tick
    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });

    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByTestId('account-deletion-success')).toBeInTheDocument();
    });
    expect(onDeleteAccount).toHaveBeenCalledTimes(1);
    expect(onGetDeletionStatus).toHaveBeenCalledWith('job-1');
  });

  it('DELETE transport error: terminal-error is shown', async () => {
    const onDeleteAccount = vi.fn().mockRejectedValue(new Error('network error'));
    const onGetDeletionStatus = vi.fn();

    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={onDeleteAccount}
        onGetDeletionStatus={onGetDeletionStatus}
      />,
    );

    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: /Delete my account/i }));
    });

    await waitFor(() => {
      expect(screen.getByTestId('account-deletion-error')).toBeInTheDocument();
    });
    expect(onGetDeletionStatus).not.toHaveBeenCalled();
  });

  it('status GET transport error mid-poll: terminal-error is shown', async () => {
    const onDeleteAccount = vi.fn().mockResolvedValue({ job_id: 'job-2' });
    const onGetDeletionStatus = vi.fn().mockRejectedValue(new Error('503'));

    vi.useFakeTimers();
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={onDeleteAccount}
        onGetDeletionStatus={onGetDeletionStatus}
      />,
    );

    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: /Delete my account/i }));
    });

    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });

    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByTestId('account-deletion-error')).toBeInTheDocument();
    });
  });

  it('poll-cap-timeout: after 120 ticks without completion, terminal-error is shown', async () => {
    const onDeleteAccount = vi.fn().mockResolvedValue({ job_id: 'job-cap' });
    const pendingStatus: AccountDeletionStatusResponse = {
      job_id: 'job-cap',
      status: 'pending',
      requested_at: '2026-06-10T00:00:00Z',
    };
    const onGetDeletionStatus = vi.fn().mockResolvedValue(pendingStatus);

    vi.useFakeTimers();
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={onDeleteAccount}
        onGetDeletionStatus={onGetDeletionStatus}
      />,
    );

    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: /Delete my account/i }));
    });

    // Advance past the 120-tick cap (121 ticks x 5000ms = 605_000ms)
    await act(async () => {
      vi.advanceTimersByTime(605_000);
    });

    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByTestId('account-deletion-error')).toBeInTheDocument();
    });
  });

  it('polling phase: shows polling indicator after DELETE 202', async () => {
    const onDeleteAccount = vi.fn().mockResolvedValue({ job_id: 'job-poll' });
    // onGetDeletionStatus never resolves — keeps polling indefinitely
    const onGetDeletionStatus = vi.fn().mockReturnValue(new Promise(() => {}));

    vi.useFakeTimers();
    render(
      <DangerZoneSection
        {...baseProps}
        onDeleteAccount={onDeleteAccount}
        onGetDeletionStatus={onGetDeletionStatus}
      />,
    );

    fireEvent.click(screen.getByTestId('danger-zone-delete-account-button'));
    const dialog = screen.getByRole('dialog');
    await act(async () => {
      fireEvent.click(within(dialog).getByRole('button', { name: /Delete my account/i }));
    });

    vi.useRealTimers();

    await waitFor(() => {
      expect(screen.getByTestId('account-deletion-polling')).toBeInTheDocument();
    });
  });
});
