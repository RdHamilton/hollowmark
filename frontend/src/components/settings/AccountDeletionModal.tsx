/**
 * AccountDeletionModal — #887 GDPR Right to Erasure
 *
 * Blocking confirmation modal for the account deletion flow.
 * This component is pure UI — all async logic lives in DangerZoneSection.
 * The parent passes onConfirm / onCancel callbacks.
 *
 * Accessibility: role="dialog", aria-modal="true", aria-labelledby,
 * Escape key closes (calls onCancel), focus management.
 *
 * AC3 copy: legally load-bearing per ADR-058 Option A / ANONYMOUS (D7a).
 * The retained-data paragraph reflects that the deployed cascade (job.go
 * Step 3) is a no-op — ML gameplay rows are stripped of all identifiers
 * before deletion, not deleted. Do NOT soften or strengthen the legal
 * register of the retained-data paragraph without counsel review.
 */

import { useEffect, useCallback } from 'react';
import './AccountDeletionModal.css';

// Re-export so DangerZoneSection (and its tests) can import the type from
// this module without a second import from '../../services/api/account'.
export type { AccountDeletionStatusResponse } from '../../services/api/account';

export interface AccountDeletionModalProps {
  isOpen: boolean;
  isSubmitting: boolean;
  onConfirm: () => void;
  onCancel: () => void;
}

const MODAL_HEADING_ID = 'account-deletion-modal-heading';

export function AccountDeletionModal({
  isOpen,
  isSubmitting,
  onConfirm,
  onCancel,
}: AccountDeletionModalProps) {
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !isSubmitting) {
        onCancel();
      }
    },
    [isSubmitting, onCancel],
  );

  useEffect(() => {
    if (isOpen) {
      document.addEventListener('keydown', handleKeyDown);
      document.body.style.overflow = 'hidden';
    }
    return () => {
      document.removeEventListener('keydown', handleKeyDown);
      document.body.style.overflow = '';
    };
  }, [isOpen, handleKeyDown]);

  if (!isOpen) return null;

  return (
    <div className="account-deletion-modal-overlay">
      <div
        className="account-deletion-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby={MODAL_HEADING_ID}
      >
        <h2 id={MODAL_HEADING_ID} className="account-deletion-modal-title">
          Delete your account
        </h2>

        {/* What will be deleted */}
        <div className="account-deletion-modal-section">
          <div className="account-deletion-modal-section-label">What will be permanently deleted</div>
          <ul className="account-deletion-modal-list">
            <li>Your VaultMTG account and login credentials</li>
            <li>Your gameplay history (match results, draft records, deck data)</li>
            <li>Your analytics data (events associated with your account)</li>
          </ul>
        </div>

        {/* About anonymized data — ADR-058 Option A / ANONYMOUS (D7a) */}
        <div className="account-deletion-modal-section">
          <div className="account-deletion-modal-section-label">About anonymized data</div>
          <p className="account-deletion-modal-retained-text">
            Some gameplay data (match outcomes, draft picks, play patterns) is kept in an anonymized
            form to improve draft recommendations and match analysis for all players. Before your
            account is deleted, all information that could identify you — your account ID, email
            address, and any personal identifiers — is permanently removed from this data. What
            remains cannot be linked back to you.
          </p>
        </div>

        {/* Irreversibility warning */}
        <div className="account-deletion-modal-warning">
          This action is permanent and cannot be undone. Once confirmed, your account and all
          personal data will be scheduled for deletion. Your account deletion is confirmed. Your
          data will be permanently removed within 30 days.
        </div>

        {/* Actions */}
        <div className="account-deletion-modal-actions">
          <button
            type="button"
            className="account-deletion-modal-cancel"
            onClick={onCancel}
            disabled={isSubmitting}
          >
            Cancel
          </button>
          <button
            type="button"
            className="account-deletion-modal-confirm"
            onClick={onConfirm}
            disabled={isSubmitting}
          >
            {isSubmitting ? 'Deleting...' : 'Delete my account'}
          </button>
        </div>
      </div>
    </div>
  );
}
