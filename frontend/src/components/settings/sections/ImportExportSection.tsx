/**
 * ImportExportSection
 *
 * Settings section for collection import/export and collection mode.
 *
 * #895 additions:
 * - Collection import via CollectionImportForm (AC1/AC5)
 * - Collection mode toggle: manual vs. enhanced (AC6)
 *
 * Q4 ruling (Ray): mode toggle writes localStorage only — no consent record,
 * no DB write, no C-18 handshake. When #893 ships, the enhanced-mode radio
 * will be updated to invoke the real consent dialog.
 */

import { SettingItem } from '../';
import { CollectionImportForm } from '../../CollectionImportForm';
import { useCollectionMode } from '@/hooks/useCollectionMode';

export interface ImportExportSectionProps {
  onExportData: (format: 'json' | 'csv') => void;
}

export function ImportExportSection({ onExportData }: ImportExportSectionProps) {
  // accountDataState is not available here — it lives in Layout.
  // The mode toggle is accessible regardless of account data state (AC5/AC6).
  const { collectionMode, setCollectionMode } = useCollectionMode({
    isSignedIn: true,
    accountDataState: 'pending', // placeholder — modal auto-show is Layout's concern
  });

  return (
    <div className="settings-section">
      <h2 className="section-title">Collection Import &amp; Export</h2>
      <div className="setting-description settings-section-description">
        Export your match history for backup or external analysis.
      </div>

      {/* ── Collection Import (AC1 / AC5) ── */}
      <SettingItem
        label="Import Collection"
        description="Import your MTG Arena collection from a CSV export file."
      >
        <CollectionImportForm />
      </SettingItem>

      {/* ── Collection Mode Toggle (AC6) ── */}
      <SettingItem
        label="Collection Mode"
        description="Manual: import your collection manually. Enhanced: Hollowmark reads your MTGA log files for automatic updates (opt-in)."
      >
        <div
          className="collection-mode-toggle"
          data-testid="collection-mode-toggle"
          role="radiogroup"
          aria-label="Collection mode"
        >
          <label className="collection-mode-option">
            <input
              type="radio"
              name="collection-mode"
              value="manual"
              data-testid="collection-mode-manual"
              checked={collectionMode === 'manual'}
              onChange={() => setCollectionMode('manual')}
            />
            <span>Manual only</span>
          </label>
          <label className="collection-mode-option">
            <input
              type="radio"
              name="collection-mode"
              value="enhanced"
              data-testid="collection-mode-enhanced"
              checked={collectionMode === 'enhanced'}
              onChange={() => setCollectionMode('enhanced')}
            />
            <span>
              Enhanced mode{' '}
              <span className="collection-mode-note">
                (reads MTGA log files &mdash; you can turn this off at any time)
              </span>
            </span>
          </label>
        </div>
      </SettingItem>

      {/* ── Export ── */}
      <SettingItem
        label="Export Data"
        description="Export your match history and statistics to a file for backup"
      >
        <button className="action-button" onClick={() => onExportData('json')}>
          Export to JSON
        </button>
        <button className="action-button" onClick={() => onExportData('csv')}>
          Export to CSV
        </button>
      </SettingItem>
    </div>
  );
}
