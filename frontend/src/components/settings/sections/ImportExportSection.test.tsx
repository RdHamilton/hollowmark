import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { ImportExportSection } from './ImportExportSection';

// Mock the collection API used by the embedded CollectionImportForm
vi.mock('@/services/api/collection', () => ({
  importCollection: vi.fn(),
}));

import { importCollection } from '@/services/api/collection';
const mockImportCollection = vi.mocked(importCollection);

describe('ImportExportSection', () => {
  const defaultProps = {
    onExportData: vi.fn(),
  };

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.removeItem('vaultmtg_collection_mode');
  });

  // ---- Existing export tests -----------------------------------------------

  it('renders section title as Collection Import & Export', () => {
    render(<ImportExportSection {...defaultProps} />);
    expect(
      screen.getByText('Collection Import & Export')
    ).toBeInTheDocument();
  });

  it('renders section description', () => {
    render(<ImportExportSection {...defaultProps} />);
    expect(
      screen.getByText(/Export your match history for backup or external analysis/)
    ).toBeInTheDocument();
  });

  describe('export buttons', () => {
    it('renders export to JSON button', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(screen.getByRole('button', { name: 'Export to JSON' })).toBeInTheDocument();
    });

    it('renders export to CSV button', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(screen.getByRole('button', { name: 'Export to CSV' })).toBeInTheDocument();
    });

    it('calls onExportData with json when JSON button clicked', () => {
      const onExportData = vi.fn();
      render(<ImportExportSection {...defaultProps} onExportData={onExportData} />);
      fireEvent.click(screen.getByRole('button', { name: 'Export to JSON' }));
      expect(onExportData).toHaveBeenCalledWith('json');
    });

    it('calls onExportData with csv when CSV button clicked', () => {
      const onExportData = vi.fn();
      render(<ImportExportSection {...defaultProps} onExportData={onExportData} />);
      fireEvent.click(screen.getByRole('button', { name: 'Export to CSV' }));
      expect(onExportData).toHaveBeenCalledWith('csv');
    });
  });

  describe('labels and descriptions', () => {
    it('renders export data label', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(screen.getByText('Export Data')).toBeInTheDocument();
    });
  });

  // ---- New import tests (#895) ----------------------------------------------

  describe('collection import', () => {
    it('renders the manual-import file input', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(
        screen.getByTestId('manual-import-file-input')
      ).toBeInTheDocument();
    });

    it('renders the import submit button', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(screen.getByTestId('manual-import-submit')).toBeInTheDocument();
    });

    it('shows success state after a successful import', async () => {
      mockImportCollection.mockResolvedValueOnce({ accepted: 5, rejected: 0 });

      render(<ImportExportSection {...defaultProps} />);
      const input = screen.getByTestId('manual-import-file-input');
      const file = new File(
        ['4 Lightning Bolt (ONS) 197\n'],
        'collection.csv',
        { type: 'text/csv' }
      );
      fireEvent.change(input, { target: { files: [file] } });
      fireEvent.click(screen.getByTestId('manual-import-submit'));

      await waitFor(() => {
        expect(screen.getByTestId('manual-import-success')).toBeInTheDocument();
      });
    });
  });

  // ---- Collection mode toggle (#895 Q2) ------------------------------------

  describe('collection mode toggle', () => {
    it('renders the collection mode toggle', () => {
      render(<ImportExportSection {...defaultProps} />);
      expect(
        screen.getByTestId('collection-mode-toggle')
      ).toBeInTheDocument();
    });

    it('shows Manual as the default selected mode', () => {
      render(<ImportExportSection {...defaultProps} />);
      // The radio/toggle for manual-only should be checked by default
      const manualRadio = screen.getByTestId('collection-mode-manual');
      expect(manualRadio).toBeChecked();
    });

    it('switching to enhanced mode writes localStorage', () => {
      render(<ImportExportSection {...defaultProps} />);
      const enhancedRadio = screen.getByTestId('collection-mode-enhanced');
      fireEvent.click(enhancedRadio);
      expect(localStorage.getItem('vaultmtg_collection_mode')).toBe('enhanced');
    });

    it('shows the import button even in enhanced mode (AC5)', () => {
      localStorage.setItem('vaultmtg_collection_mode', 'enhanced');
      render(<ImportExportSection {...defaultProps} />);
      expect(screen.getByTestId('manual-import-file-input')).toBeInTheDocument();
    });
  });
});
