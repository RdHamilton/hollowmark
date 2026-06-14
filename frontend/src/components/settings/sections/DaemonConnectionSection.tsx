import { gui } from '@/types/models';
import type { DaemonAuthStatus } from '@/services/api/bffHealth';
import { DaemonAuthStatusBadge } from './DaemonAuthStatusBadge';

export interface DaemonConnectionSectionProps {
  connectionStatus: gui.ConnectionStatus;
  /**
   * Optional auth_status from GET /api/v1/health/daemon (#144).
   * When provided, renders the daemon auth status row below the connection
   * badge. When omitted (e.g. old BFF, not yet loaded), the auth row is hidden.
   */
  auth_status?: DaemonAuthStatus;
}

export function DaemonConnectionSection({
  connectionStatus,
  auth_status,
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

      {auth_status !== undefined && (
        <div className="setting-item">
          <label className="setting-label">
            Authentication Status
            <span className="setting-description">Current daemon authentication state</span>
          </label>
          <div className="setting-control">
            <DaemonAuthStatusBadge auth_status={auth_status} />
          </div>
        </div>
      )}
    </div>
  );
}
