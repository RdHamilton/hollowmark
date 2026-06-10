/**
 * Tests for ManualImportModal component.
 *
 * The modal wraps CollectionImportForm with:
 * - Overlay/dialog chrome (title, close button)
 * - "Enable enhanced mode" secondary link
 * - Escape key dismissal
 * - onDismiss/onImportComplete callbacks
 * - Renders nothing when isOpen=false
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ManualImportModal } from './ManualImportModal';

// Mock the collection API used by the embedded CollectionImportForm
vi.mock('@/services/api/collection', () => ({
  importCollection: vi.fn(),
}));

import { importCollection } from '@/services/api/collection';
const mockImportCollection = vi.mocked(importCollection);

describe('ManualImportModal', () => {
  const defaultProps = {
    isOpen: true,
    onDismiss: vi.fn(),
    onImportComplete: vi.fn(),
    onEnableEnhancedMode: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders nothing when isOpen is false', () => {
    render(<ManualImportModal {...defaultProps} isOpen={false} />);
    expect(screen.queryByTestId('manual-import-modal')).not.toBeInTheDocument();
  });

  it('renders the modal dialog when isOpen is true', () => {
    render(<ManualImportModal {...defaultProps} />);
    expect(screen.getByTestId('manual-import-modal')).toBeInTheDocument();
  });

  it('renders the import collection title', () => {
    render(<ManualImportModal {...defaultProps} />);
    expect(screen.getByRole('heading')).toHaveTextContent(/import.*collection/i);
  });

  it('renders a close button that calls onDismiss', () => {
    render(<ManualImportModal {...defaultProps} />);
    fireEvent.click(screen.getByTestId('manual-import-modal-close'));
    expect(defaultProps.onDismiss).toHaveBeenCalledOnce();
  });

  it('calls onDismiss when Escape key is pressed', () => {
    render(<ManualImportModal {...defaultProps} />);
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(defaultProps.onDismiss).toHaveBeenCalledOnce();
  });

  it('renders an "Enable enhanced mode" secondary link', () => {
    render(<ManualImportModal {...defaultProps} />);
    const link = screen.getByTestId('manual-import-enable-enhanced');
    expect(link).toBeInTheDocument();
    expect(link).toHaveTextContent(/enhanced mode/i);
  });

  it('calls onEnableEnhancedMode when the enhanced-mode link is clicked', () => {
    render(<ManualImportModal {...defaultProps} />);
    fireEvent.click(screen.getByTestId('manual-import-enable-enhanced'));
    expect(defaultProps.onEnableEnhancedMode).toHaveBeenCalledOnce();
  });

  it('calls onImportComplete after a successful upload', async () => {
    mockImportCollection.mockResolvedValueOnce({ accepted: 6, rejected: 0 });

    render(<ManualImportModal {...defaultProps} />);
    const input = screen.getByTestId('manual-import-file-input');
    const file = new File(['4 Lightning Bolt (ONS) 197\n'], 'collection.csv', {
      type: 'text/csv',
    });
    fireEvent.change(input, { target: { files: [file] } });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(defaultProps.onImportComplete).toHaveBeenCalledWith({
        accepted: 6,
        rejected: 0,
      });
    });
  });
});
