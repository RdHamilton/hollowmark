/**
 * Tests for CollectionImportForm component.
 *
 * The form is the shared import UI used by both ManualImportModal and
 * ImportExportSection. It owns:
 * - File input rendering
 * - Client-side CSV validation (rejects non-.csv files)
 * - Upload/loading state while submitting
 * - Success state with accepted/rejected counts
 * - Error state from BFF 400/500 responses
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { CollectionImportForm } from './CollectionImportForm';

// Mock the collection API at the module boundary per Frank's Rule 1
vi.mock('@/services/api/collection', () => ({
  importCollection: vi.fn(),
}));

import { importCollection } from '@/services/api/collection';
const mockImportCollection = vi.mocked(importCollection);

function makeFile(name = 'collection.csv', content = '4 Lightning Bolt (ONS) 197\n') {
  return new File([content], name, { type: 'text/csv' });
}

describe('CollectionImportForm', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the file input in idle state', () => {
    render(<CollectionImportForm />);
    expect(screen.getByTestId('manual-import-file-input')).toBeInTheDocument();
    expect(screen.getByTestId('manual-import-submit')).toBeInTheDocument();
  });

  it('submit button is disabled until a file is selected', () => {
    render(<CollectionImportForm />);
    expect(screen.getByTestId('manual-import-submit')).toBeDisabled();
  });

  it('submit button is enabled after a valid CSV file is selected', () => {
    render(<CollectionImportForm />);
    const input = screen.getByTestId('manual-import-file-input');
    fireEvent.change(input, { target: { files: [makeFile()] } });
    expect(screen.getByTestId('manual-import-submit')).not.toBeDisabled();
  });

  it('shows a validation error for a non-CSV file without calling the API', async () => {
    render(<CollectionImportForm />);
    const input = screen.getByTestId('manual-import-file-input');
    const txtFile = new File(['bad data'], 'not-a-collection.txt', {
      type: 'text/plain',
    });
    fireEvent.change(input, { target: { files: [txtFile] } });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(screen.getByTestId('manual-import-error')).toBeInTheDocument();
    });
    expect(mockImportCollection).not.toHaveBeenCalled();
  });

  it('shows uploading state while the API call is in-flight', async () => {
    let resolve!: (v: { accepted: number; rejected: number }) => void;
    mockImportCollection.mockReturnValueOnce(new Promise((r) => { resolve = r; }));

    render(<CollectionImportForm />);
    fireEvent.change(screen.getByTestId('manual-import-file-input'), {
      target: { files: [makeFile()] },
    });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(screen.getByTestId('manual-import-uploading')).toBeInTheDocument();
    });

    // Clean up
    resolve({ accepted: 1, rejected: 0 });
  });

  it('shows success state with accepted count after a successful upload', async () => {
    mockImportCollection.mockResolvedValueOnce({ accepted: 8, rejected: 1 });

    render(<CollectionImportForm />);
    fireEvent.change(screen.getByTestId('manual-import-file-input'), {
      target: { files: [makeFile()] },
    });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(screen.getByTestId('manual-import-success')).toBeInTheDocument();
    });
    expect(screen.getByTestId('manual-import-success')).toHaveTextContent('8');
  });

  it('calls onSuccess callback after a successful upload', async () => {
    mockImportCollection.mockResolvedValueOnce({ accepted: 5, rejected: 0 });
    const onSuccess = vi.fn();

    render(<CollectionImportForm onSuccess={onSuccess} />);
    fireEvent.change(screen.getByTestId('manual-import-file-input'), {
      target: { files: [makeFile()] },
    });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(onSuccess).toHaveBeenCalledWith({ accepted: 5, rejected: 0 });
    });
  });

  it('shows error state when the API returns an error', async () => {
    mockImportCollection.mockRejectedValueOnce(new Error('internal server error'));

    render(<CollectionImportForm />);
    fireEvent.change(screen.getByTestId('manual-import-file-input'), {
      target: { files: [makeFile()] },
    });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() => {
      expect(screen.getByTestId('manual-import-error')).toBeInTheDocument();
    });
  });

  it('allows re-import after a successful upload', async () => {
    mockImportCollection.mockResolvedValueOnce({ accepted: 3, rejected: 0 });

    render(<CollectionImportForm />);
    fireEvent.change(screen.getByTestId('manual-import-file-input'), {
      target: { files: [makeFile()] },
    });
    fireEvent.click(screen.getByTestId('manual-import-submit'));

    await waitFor(() =>
      expect(screen.getByTestId('manual-import-success')).toBeInTheDocument()
    );

    fireEvent.click(screen.getByTestId('manual-import-reimport'));

    expect(screen.queryByTestId('manual-import-success')).not.toBeInTheDocument();
    expect(screen.getByTestId('manual-import-file-input')).toBeInTheDocument();
  });
});
