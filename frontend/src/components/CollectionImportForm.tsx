/**
 * CollectionImportForm
 *
 * Shared import UI used by both ManualImportModal and ImportExportSection.
 * Owns file selection, client-side CSV validation, upload state, and
 * success/error display.
 *
 * The form posts the file through importCollection() in the collection API
 * adapter — never calls fetch directly (Frank Rule 1).
 */

import { useState, useRef } from 'react';
import { importCollection, type ImportCollectionResult } from '@/services/api/collection';
import './CollectionImportForm.css';

export interface CollectionImportFormProps {
  /** Called after a successful import with the server's accepted/rejected counts. */
  onSuccess?: (result: ImportCollectionResult) => void;
}

type FormState = 'idle' | 'uploading' | 'success' | 'error';

/**
 * Validate the file is a CSV (by extension — the authoritative check).
 * MTGA exports always produce a .csv file regardless of MIME type, so
 * the extension is the most reliable signal.
 */
function validateFile(file: File): string | null {
  if (!file.name.toLowerCase().endsWith('.csv')) {
    return 'Please select a CSV file exported from MTG Arena.';
  }
  if (file.size === 0) {
    return 'The selected file is empty.';
  }
  return null;
}

export function CollectionImportForm({ onSuccess }: CollectionImportFormProps) {
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [formState, setFormState] = useState<FormState>('idle');
  const [importResult, setImportResult] = useState<ImportCollectionResult | null>(null);
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0] ?? null;
    setSelectedFile(file);
    // Clear any previous error when a new file is chosen
    if (formState === 'error') {
      setFormState('idle');
      setErrorMessage(null);
    }
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!selectedFile) return;

    const validationError = validateFile(selectedFile);
    if (validationError) {
      setFormState('error');
      setErrorMessage(validationError);
      return;
    }

    setFormState('uploading');
    setErrorMessage(null);

    try {
      const result = await importCollection(selectedFile);
      setImportResult(result);
      setFormState('success');
      onSuccess?.(result);
    } catch (err) {
      const msg =
        err instanceof Error ? err.message : 'Import failed. Please try again.';
      setFormState('error');
      setErrorMessage(msg);
    }
  };

  const handleReimport = () => {
    setFormState('idle');
    setSelectedFile(null);
    setImportResult(null);
    setErrorMessage(null);
    // Reset the file input
    if (fileInputRef.current) {
      fileInputRef.current.value = '';
    }
  };

  if (formState === 'success' && importResult) {
    return (
      <div className="collection-import-form collection-import-form--success">
        <div
          className="collection-import-success-icon"
          aria-hidden="true"
        >
          <svg width="32" height="32" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <circle cx="12" cy="12" r="10" />
            <polyline points="20 6 9 17 4 12" />
          </svg>
        </div>
        <p
          className="collection-import-success-text"
          data-testid="manual-import-success"
        >
          Import complete. <strong>{importResult.accepted}</strong> card
          {importResult.accepted !== 1 ? 's' : ''} imported
          {importResult.rejected > 0 ? ` (${importResult.rejected} skipped)` : ''}.
        </p>
        <button
          type="button"
          className="collection-import-reimport-btn"
          data-testid="manual-import-reimport"
          onClick={handleReimport}
        >
          Import another file
        </button>
      </div>
    );
  }

  if (formState === 'uploading') {
    return (
      <div
        className="collection-import-form collection-import-form--uploading"
        data-testid="manual-import-uploading"
      >
        <div className="collection-import-spinner" aria-label="Uploading collection..." />
        <p className="collection-import-uploading-text">Uploading collection&hellip;</p>
      </div>
    );
  }

  return (
    <form
      className="collection-import-form"
      onSubmit={handleSubmit}
      data-testid="collection-import-form"
    >
      <div className="collection-import-file-row">
        <input
          ref={fileInputRef}
          type="file"
          accept=".csv,text/csv,text/plain"
          className="collection-import-file-input"
          data-testid="manual-import-file-input"
          onChange={handleFileChange}
          aria-label="Select MTGA collection CSV file"
        />
      </div>

      {formState === 'error' && errorMessage && (
        <p
          className="collection-import-error"
          data-testid="manual-import-error"
          role="alert"
        >
          {errorMessage}
        </p>
      )}

      <div className="collection-import-actions">
        <button
          type="submit"
          className="collection-import-submit-btn"
          data-testid="manual-import-submit"
          disabled={!selectedFile}
        >
          Import Collection
        </button>
      </div>

      <p className="collection-import-hint">
        Export your collection from MTG Arena: Options &rarr; Exports &rarr; Export Collection.
        Then select the exported <code>.csv</code> file above.
      </p>
    </form>
  );
}

export default CollectionImportForm;
