import { useState, useEffect, useRef } from 'react';
import LoadingButton from '../../LoadingButton';
import { AccountDeletionModal } from '../AccountDeletionModal';
import type {
  AccountDeletionResponse,
  AccountDeletionStatusResponse,
} from '../../../services/api/account';

/**
 * DangerZoneSection — top-level Settings accordion section for destructive
 * daemon-lifecycle actions (currently: uninstall daemon).
 *
 * Previously this lived as a sub-section inside DataRecoverySection.
 * Extracted in #2027 so that log-replay (Data Recovery) and daemon uninstall
 * (Danger Zone) are distinct, clearly labelled top-level concerns.
 *
 * The uninstall call delegates entirely to the onUninstallDaemon prop — no
 * direct fetch calls here (REST API adapter pattern per CLAUDE.md).
 *
 * The returned string from onUninstallDaemon is the backend's user-facing
 * residual-action message (platform-specific steps, e.g. "Drag VaultMTG to
 * the Trash to remove the app bundle"). It surfaces verbatim in the success
 * panel — the component does not fabricate its own copy.
 *
 * Account deletion flow (#887) — state machine:
 *   idle -> confirming (modal open) -> submitting (DELETE in-flight)
 *     -> polling (202 received, job_id available, status: 'pending')
 *     -> terminal-success (status: 'completed')
 *     -> terminal-error (transport error OR poll-cap timeout)
 *
 * terminal-error is driven ONLY by:
 *   1. deleteAccount() throwing (any transport / non-2xx error)
 *   2. getAccountDeletionStatus() throwing (non-2xx mid-poll)
 *   3. Poll-cap timeout: 10 minutes (120 ticks x 5 s)
 *
 * There is no 'failed' status field on the deletion-status endpoint.
 * Cascade failure surfaces via Bianca's AC5 email + Sentry tail only.
 */

// Poll interval and cap constants
const POLL_INTERVAL_MS = 5_000;
const POLL_MAX_TICKS = 120; // 120 x 5s = 10 minutes

type DeletionPhase =
  | 'idle'
  | 'confirming'
  | 'submitting'
  | 'polling'
  | 'terminal-success'
  | 'terminal-error';

export interface DangerZoneSectionProps {
  isConnected: boolean;
  /**
   * Called when the user confirms the uninstall. The resolved string is the
   * backend's user-facing residual-action message rendered verbatim in the
   * success panel. When omitted, the section renders nothing (hidden).
   */
  onUninstallDaemon?: (purge: boolean) => Promise<string>;
  /**
   * Called when the user confirms account deletion. Returns the BFF 202
   * response with a job_id. When omitted, the "Delete my account" button
   * is not rendered.
   */
  onDeleteAccount?: () => Promise<AccountDeletionResponse>;
  /**
   * Called on each poll tick to check the deletion job status.
   * Paired with onDeleteAccount -- both must be provided together.
   */
  onGetDeletionStatus?: (jobId: string) => Promise<AccountDeletionStatusResponse>;
}

export function DangerZoneSection({
  isConnected,
  onUninstallDaemon,
  onDeleteAccount,
  onGetDeletionStatus,
}: DangerZoneSectionProps) {
  // ---------------------------------------------------------------------------
  // Uninstall state (pre-existing)
  // ---------------------------------------------------------------------------
  const [confirmingUninstall, setConfirmingUninstall] = useState(false);
  const [purgeConfig, setPurgeConfig] = useState(false);
  const [uninstalling, setUninstalling] = useState(false);
  const [uninstallResult, setUninstallResult] = useState<
    { kind: 'success'; message: string } | { kind: 'error'; message: string } | null
  >(null);

  // ---------------------------------------------------------------------------
  // Account deletion state (#887)
  // ---------------------------------------------------------------------------
  const [deletionPhase, setDeletionPhase] = useState<DeletionPhase>('idle');
  const [deletionErrorMsg, setDeletionErrorMsg] = useState<string | null>(null);

  // Interval + tick counter held in refs so they do not trigger re-renders
  const pollIntervalRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const pollTicksRef = useRef(0);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (pollIntervalRef.current !== null) {
        clearInterval(pollIntervalRef.current);
        pollIntervalRef.current = null;
      }
    };
  }, []);

  const stopPolling = () => {
    if (pollIntervalRef.current !== null) {
      clearInterval(pollIntervalRef.current);
      pollIntervalRef.current = null;
    }
    pollTicksRef.current = 0;
  };

  const startPolling = (jobId: string) => {
    if (!onGetDeletionStatus) return;

    pollTicksRef.current = 0;

    pollIntervalRef.current = setInterval(async () => {
      pollTicksRef.current += 1;

      // Poll-cap: 10 minutes reached without terminal state
      if (pollTicksRef.current > POLL_MAX_TICKS) {
        stopPolling();
        setDeletionErrorMsg(
          "Deletion is taking longer than expected, but it's still processing — your data will be removed. You can safely close this page.",
        );
        setDeletionPhase('terminal-error');
        return;
      }

      try {
        const status = await onGetDeletionStatus(jobId);
        if (status.status === 'completed') {
          stopPolling();
          setDeletionPhase('terminal-success');
        }
        // 'pending' -- continue polling
      } catch {
        // Transport error mid-poll -- stop and show error
        stopPolling();
        setDeletionErrorMsg(
          'Account deletion encountered an error. Please contact support if the problem persists.',
        );
        setDeletionPhase('terminal-error');
      }
    }, POLL_INTERVAL_MS);
  };

  const handleDeletionConfirm = async () => {
    if (!onDeleteAccount) return;
    setDeletionPhase('submitting');
    try {
      const response = await onDeleteAccount();
      setDeletionPhase('polling');
      startPolling(response.job_id);
    } catch {
      setDeletionErrorMsg(
        'Account deletion encountered an error. Please contact support if the problem persists.',
      );
      setDeletionPhase('terminal-error');
    }
  };

  const handleDeletionCancel = () => {
    if (deletionPhase === 'confirming') {
      setDeletionPhase('idle');
    }
  };

  // ---------------------------------------------------------------------------
  // Uninstall handlers (pre-existing)
  // ---------------------------------------------------------------------------
  const handleConfirmUninstall = async () => {
    if (!onUninstallDaemon) return;
    setUninstalling(true);
    try {
      const backendMessage = await onUninstallDaemon(purgeConfig);
      // Render the backend message verbatim -- it carries the platform-specific
      // residual steps and reflects whether purge ran. Fall back to a neutral
      // message only if the backend returned an empty string.
      const message =
        backendMessage && backendMessage.trim().length > 0
          ? backendMessage
          : 'Daemon uninstall scheduled. The daemon will shut down momentarily — you can close this tab.';
      setUninstallResult({ kind: 'success', message });
    } catch (err) {
      setUninstallResult({
        kind: 'error',
        message:
          err instanceof Error
            ? err.message
            : 'Uninstall failed. Try the manual steps in the docs.',
      });
    } finally {
      setUninstalling(false);
      setConfirmingUninstall(false);
    }
  };

  if (!onUninstallDaemon) {
    if (import.meta.env.DEV) {
      console.warn(
        '[DangerZoneSection] onUninstallDaemon prop is undefined — component will render null. Check parent component.',
      );
    }
    return null;
  }

  return (
    <>
      <div className="settings-section" data-testid="danger-zone-section">
        <h2 className="section-title">Danger Zone — Uninstall Daemon</h2>
        <div className="setting-description settings-section-description">
          Stop the local daemon and remove its startup entry. Your VaultMTG account and cloud match
          history are not affected — that data lives on vaultmtg.app.
        </div>

        {uninstallResult ? (
          <div
            className={`setting-hint ${
              uninstallResult.kind === 'error' ? 'settings-error-box' : 'settings-success-box'
            }`}
            data-testid={
              uninstallResult.kind === 'error'
                ? 'danger-zone-error-result'
                : 'danger-zone-success-result'
            }
          >
            {uninstallResult.message}
          </div>
        ) : confirmingUninstall ? (
          <div className="setting-item">
            <div className="setting-control">
              <div className="checkbox-container">
                <label className="checkbox-label">
                  <input
                    type="checkbox"
                    checked={purgeConfig}
                    onChange={(e) => setPurgeConfig(e.target.checked)}
                    className="checkbox-input"
                    disabled={uninstalling}
                  />
                  <span>Also wipe my local config + cached data (irreversible)</span>
                </label>
              </div>
              <LoadingButton
                loading={uninstalling}
                loadingText="Uninstalling..."
                onClick={handleConfirmUninstall}
                variant="danger"
              >
                Confirm Uninstall
              </LoadingButton>
              <button
                className="action-button"
                onClick={() => {
                  setConfirmingUninstall(false);
                  setPurgeConfig(false);
                }}
                disabled={uninstalling}
              >
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <div className="setting-item">
            <div className="setting-control">
              <button
                className="danger-button"
                onClick={() => setConfirmingUninstall(true)}
                disabled={!isConnected}
                data-testid="danger-zone-uninstall-button"
              >
                Uninstall VaultMTG Daemon
              </button>
              {!isConnected && (
                <div className="setting-hint settings-daemon-hint">
                  Daemon must be running to trigger uninstall
                </div>
              )}
            </div>
          </div>
        )}

        {/* Account Deletion (#887) */}
        {onDeleteAccount && onGetDeletionStatus && (
          <div className="settings-section-divider" style={{ marginTop: 24 }}>
            <h2 className="section-title">Danger Zone — Delete Account</h2>
            <div className="setting-description settings-section-description">
              Permanently delete your VaultMTG account and all associated personal data per your
              GDPR right to erasure (Art. 17). This action cannot be undone.
            </div>

            {deletionPhase === 'idle' && (
              <div className="setting-item">
                <div className="setting-control">
                  <button
                    className="danger-button"
                    onClick={() => setDeletionPhase('confirming')}
                    data-testid="danger-zone-delete-account-button"
                  >
                    Delete my account
                  </button>
                </div>
              </div>
            )}

            {deletionPhase === 'polling' && (
              <div
                className="setting-hint settings-section-description"
                data-testid="account-deletion-polling"
              >
                Your account deletion is being processed. All personal data associated with your
                account will be permanently removed. This may take a few moments.
              </div>
            )}

            {deletionPhase === 'terminal-success' && (
              <div
                className="setting-hint settings-success-box"
                data-testid="account-deletion-success"
              >
                Your account deletion has been scheduled. Your data will be permanently removed
                within 30 days.
              </div>
            )}

            {deletionPhase === 'terminal-error' && (
              <div
                className="setting-hint settings-error-box"
                data-testid="account-deletion-error"
              >
                {deletionErrorMsg ??
                  'Account deletion encountered an error. Please contact support if the problem persists.'}
              </div>
            )}
          </div>
        )}
      </div>

      {/* Account deletion modal -- rendered outside the section div so it
          is not clipped by any overflow:hidden ancestor */}
      {onDeleteAccount && onGetDeletionStatus && (
        <AccountDeletionModal
          isOpen={deletionPhase === 'confirming'}
          isSubmitting={deletionPhase === 'submitting'}
          onConfirm={handleDeletionConfirm}
          onCancel={handleDeletionCancel}
        />
      )}
    </>
  );
}
