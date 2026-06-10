/**
 * AccountDeletionModal tests — #887 GDPR Right to Erasure
 *
 * Tests the confirmation modal component in isolation.
 * Parent (DangerZoneSection) owns all async logic; this component is pure UI.
 */

import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { AccountDeletionModal } from './AccountDeletionModal';

describe('AccountDeletionModal', () => {
  // ---------------------------------------------------------------------------
  // Visibility
  // ---------------------------------------------------------------------------

  describe('visibility', () => {
    it('renders null when isOpen=false', () => {
      const { container } = render(
        <AccountDeletionModal
          isOpen={false}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(container.firstChild).toBeNull();
    });

    it('renders the modal when isOpen=true', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });
  });

  // ---------------------------------------------------------------------------
  // Copy sections (AC3)
  // ---------------------------------------------------------------------------

  describe('AC3 — required copy sections', () => {
    it('renders the modal heading', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/Delete your account/i)).toBeInTheDocument();
    });

    it('renders what will be deleted — account and login credentials', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/account and login credentials/i)).toBeInTheDocument();
    });

    it('renders what will be deleted — gameplay history', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/gameplay history/i)).toBeInTheDocument();
    });

    it('renders what will be deleted — analytics data', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/analytics data/i)).toBeInTheDocument();
    });

    it('renders the ANONYMOUS retained-data paragraph — de-identified gameplay data', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/de-identified gameplay data/i)).toBeInTheDocument();
    });

    it('renders the ANONYMOUS retained-data paragraph — no information that could identify you', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/no information that could identify you/i)).toBeInTheDocument();
    });

    it('renders the irreversibility warning', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByText(/permanent and cannot be undone/i)).toBeInTheDocument();
    });
  });

  // ---------------------------------------------------------------------------
  // Actions
  // ---------------------------------------------------------------------------

  describe('actions', () => {
    it('calls onConfirm when "Delete my account" button is clicked', () => {
      const onConfirm = vi.fn();
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={onConfirm}
          onCancel={vi.fn()}
        />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Delete my account/i }));
      expect(onConfirm).toHaveBeenCalledTimes(1);
    });

    it('calls onCancel when "Cancel" button is clicked', () => {
      const onCancel = vi.fn();
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={onCancel}
        />,
      );
      fireEvent.click(screen.getByRole('button', { name: /Cancel/i }));
      expect(onCancel).toHaveBeenCalledTimes(1);
    });

    it('calls onCancel when Escape key is pressed', () => {
      const onCancel = vi.fn();
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={onCancel}
        />,
      );
      fireEvent.keyDown(document, { key: 'Escape' });
      expect(onCancel).toHaveBeenCalledTimes(1);
    });
  });

  // ---------------------------------------------------------------------------
  // Submitting state
  // ---------------------------------------------------------------------------

  describe('submitting state', () => {
    it('disables the confirm button when isSubmitting=true', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={true}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('button', { name: /Deleting|Delete my account/i })).toBeDisabled();
    });

    it('disables the cancel button when isSubmitting=true', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={true}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('button', { name: /Cancel/i })).toBeDisabled();
    });
  });

  // ---------------------------------------------------------------------------
  // Accessibility (ARIA)
  // ---------------------------------------------------------------------------

  describe('accessibility', () => {
    it('has role="dialog"', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('dialog')).toBeInTheDocument();
    });

    it('has aria-modal="true"', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      expect(screen.getByRole('dialog')).toHaveAttribute('aria-modal', 'true');
    });

    it('has aria-labelledby pointing to the heading', () => {
      render(
        <AccountDeletionModal
          isOpen={true}
          isSubmitting={false}
          onConfirm={vi.fn()}
          onCancel={vi.fn()}
        />,
      );
      const dialog = screen.getByRole('dialog');
      const labelledBy = dialog.getAttribute('aria-labelledby');
      expect(labelledBy).toBeTruthy();
      const heading = document.getElementById(labelledBy!);
      expect(heading).toBeInTheDocument();
    });
  });
});
