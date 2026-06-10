/**
 * ManualImportModal
 *
 * First-run collection import modal for new users.
 * Manual import is the default (D3) — this modal fires before daemon onboarding.
 *
 * Structure:
 * - Modal overlay with header, CollectionImportForm, and an "Enable enhanced mode" link
 * - Enhanced mode link calls onEnableEnhancedMode (caller writes localStorage via useCollectionMode)
 * - Escape key and overlay click both dismiss the modal
 *
 * Q1 ruling (Ray): separate component from OnboardingModal — different lifecycles.
 * Q4 ruling (Ray): #895 writes localStorage only — no consent record, no DB write.
 */

import { useEffect } from 'react';
import { CollectionImportForm } from './CollectionImportForm';
import type { ImportCollectionResult } from '@/services/api/collection';
import './ManualImportModal.css';

export interface ManualImportModalProps {
  /** Whether the modal is visible */
  isOpen: boolean;
  /** Called when the user dismisses the modal */
  onDismiss: () => void;
  /** Called after a successful import */
  onImportComplete: (result: ImportCollectionResult) => void;
  /** Called when the user clicks "Enable enhanced mode" */
  onEnableEnhancedMode: () => void;
}

function ManualImportModalContent({
  onDismiss,
  onImportComplete,
  onEnableEnhancedMode,
}: Omit<ManualImportModalProps, 'isOpen'>) {
  // Prevent body scroll while open
  useEffect(() => {
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }, []);

  // Escape key dismissal
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onDismiss();
    };
    document.addEventListener('keydown', handleKeyDown);
    return () => document.removeEventListener('keydown', handleKeyDown);
  }, [onDismiss]);

  return (
    <div
      className="manual-import-modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="manual-import-modal-title"
      data-testid="manual-import-modal"
      onClick={(e) => {
        if (e.target === e.currentTarget) onDismiss();
      }}
    >
      <div className="manual-import-modal">
        {/* Header */}
        <div className="manual-import-modal-header">
          <h2 id="manual-import-modal-title" className="manual-import-modal-title">
            Import your collection
          </h2>
          <button
            type="button"
            className="manual-import-modal-close"
            onClick={onDismiss}
            aria-label="Dismiss import modal"
            data-testid="manual-import-modal-close"
          >
            &times;
          </button>
        </div>

        {/* Body */}
        <div className="manual-import-modal-body">
          <p className="manual-import-modal-description">
            Import your MTG Arena collection to see card availability, deck
            completion, and wildcard advice.
          </p>

          <CollectionImportForm onSuccess={onImportComplete} />

          {/* Enhanced mode secondary link (AC1 — single secondary link) */}
          <div className="manual-import-modal-footer">
            <button
              type="button"
              className="manual-import-enhanced-link"
              data-testid="manual-import-enable-enhanced"
              onClick={onEnableEnhancedMode}
            >
              Want collection updates automatically? Enable enhanced mode &rarr;
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}

export function ManualImportModal({
  isOpen,
  onDismiss,
  onImportComplete,
  onEnableEnhancedMode,
}: ManualImportModalProps) {
  if (!isOpen) return null;
  return (
    <ManualImportModalContent
      onDismiss={onDismiss}
      onImportComplete={onImportComplete}
      onEnableEnhancedMode={onEnableEnhancedMode}
    />
  );
}

export default ManualImportModal;
