/**
 * System API service.
 * Replaces Wails system-related function bindings.
 *
 * Client routing:
 *   - daemonClient (port 9001): daemon lifecycle endpoints — /system/status,
 *     /system/daemon/*, /system/version, /system/database/*, /system/uninstall
 *   - apiClient / BFF (port 8080): user-data endpoints — /system/health,
 *     /system/account, /feedback/ml-training
 */

import { get as daemonGet, post as daemonPost } from '../daemonClient';
import { get as bffGet } from '../apiClient';
import { gui, models } from '@/types/models';

// Re-export types for convenience
export type ConnectionStatus = gui.ConnectionStatus;

/**
 * Version information.
 */
export interface VersionInfo {
  version: string;
  service: string;
}

/**
 * Daemon status response.
 */
export interface DaemonStatus {
  status: string;
  connected: boolean;
}

/**
 * Database health information.
 */
export interface DatabaseHealth {
  status: string;
  lastWrite?: string;
}

/**
 * Log monitor health information.
 */
export interface LogMonitorHealth {
  status: string;
  lastRead?: string;
}

/**
 * WebSocket health information.
 */
export interface WebSocketHealth {
  status: string;
  connectedClients: number;
}

/**
 * Health metrics.
 */
export interface HealthMetrics {
  totalProcessed: number;
  totalErrors: number;
}

/**
 * System health status including backend sync timestamps.
 */
export interface HealthStatus {
  status: string;
  version: string;
  uptime: number;
  database: DatabaseHealth;
  logMonitor: LogMonitorHealth;
  websocket: WebSocketHealth;
  metrics: HealthMetrics;
}

/**
 * Get the current connection status.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function getStatus(): Promise<ConnectionStatus> {
  return daemonGet<ConnectionStatus>('/system/status');
}

/**
 * Get the system health status including backend sync timestamps.
 * Routes through the BFF client (port 8080) — user-data endpoint, safe for
 * web app users who have no local daemon running (#2007).
 */
export async function getHealth(): Promise<HealthStatus> {
  return bffGet<HealthStatus>('/system/health');
}

/**
 * Get the daemon connection status.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function getDaemonStatus(): Promise<DaemonStatus> {
  return daemonGet<DaemonStatus>('/system/daemon/status');
}

/**
 * Connect to the daemon.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function connectDaemon(): Promise<{ status: string }> {
  return daemonPost<{ status: string }>('/system/daemon/connect');
}

/**
 * Disconnect from the daemon.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function disconnectDaemon(): Promise<{ status: string }> {
  return daemonPost<{ status: string }>('/system/daemon/disconnect');
}

/**
 * Get the application version.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function getVersion(): Promise<VersionInfo> {
  return daemonGet<VersionInfo>('/system/version');
}

/**
 * Get the database path.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function getDatabasePath(): Promise<{ path: string }> {
  return daemonGet<{ path: string }>('/system/database/path');
}

/**
 * Set the database path.
 * Routes through the daemon client (port 9001) — daemon-specific endpoint.
 */
export async function setDatabasePath(path: string): Promise<{ status: string }> {
  return daemonPost<{ status: string }>('/system/database/path', { path });
}

/**
 * Get current account.
 * Routes through the BFF client (port 8080) — user-data endpoint, not a
 * daemon-local endpoint. Fixes ERR_CONNECTION_REFUSED on Quest page (#2008).
 */
export async function getCurrentAccount(): Promise<models.Account> {
  return bffGet<models.Account>('/system/account');
}

/**
 * Daemon uninstall response shape.
 */
export interface UninstallResponse {
  status: string;
  message: string;
}

/**
 * Trigger a clean uninstall of the local daemon (Phase 2 PR #18).
 *
 * The daemon stops itself + removes the launchd plist / Task Scheduler
 * entry + (optionally) wipes its config directory. The daemon binary
 * itself stays on disk — the response message tells the user the
 * one remaining residual step (drag to Trash on macOS, Add/Remove
 * Programs on Windows). After the call returns the daemon will exit
 * within ~200ms.
 */
export async function uninstallDaemon(opts: { purge?: boolean } = {}): Promise<UninstallResponse> {
  const params = opts.purge ? '?purge=true' : '';
  return daemonPost<UninstallResponse>(`/system/uninstall${params}`);
}

/**
 * Export ML training data.
 * Routes through the BFF client (port 8080) — user-data endpoint.
 */
export async function exportMLTrainingData(limit: number): Promise<gui.MLTrainingDataExport> {
  return bffGet<gui.MLTrainingDataExport>(`/feedback/ml-training?limit=${limit}`);
}
