import { gui } from '@/types/models';

export interface DaemonConnectionSectionProps {
  connectionStatus: gui.ConnectionStatus;
}

export function DaemonConnectionSection({
  connectionStatus,
}: DaemonConnectionSectionProps) {
  return (
    <div className="settings-section">
      <h2 className="section-title">Daemon Connection</h2>

      <div className="setting-item">
        <label className="setting-label">
          Connection Status
          <span className="setting-description">Current connection state to the daemon service</span>
        </label>
        <div className="setting-control">
          <div className={`connection-badge status-${connectionStatus.status}`} data-testid="connection-badge">
            <span className="status-dot"></span>
            {connectionStatus.status === 'connected' && 'Connected to Daemon'}
            {connectionStatus.status === 'standalone' && 'Standalone Mode'}
            {connectionStatus.status === 'reconnecting' && 'Reconnecting...'}
          </div>
        </div>
      </div>
    </div>
  );
}
