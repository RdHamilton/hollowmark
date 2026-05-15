import { describe, it, expect, vi, beforeEach } from 'vitest';
import * as system from '../system';

// Mock the daemonClient — handles daemon-lifecycle routes (port 9001).
vi.mock('../../daemonClient', () => ({
  get: vi.fn(),
  post: vi.fn(),
}));

// Mock the BFF apiClient — handles user-data routes (port 8080).
// Only the named exports consumed by system.ts are needed.
vi.mock('../../apiClient', () => ({
  get: vi.fn(),
  post: vi.fn(),
  // Stub the rest so imports don't break other modules.
  getApiKey: vi.fn(() => ''),
  setApiKey: vi.fn(),
  configureApi: vi.fn(),
  getApiConfig: vi.fn(() => ({ baseUrl: 'http://localhost:8080/api/v1' })),
  ApiRequestError: class ApiRequestError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.name = 'ApiRequestError';
      this.status = status;
    }
  },
}));

import { get as daemonGet, post as daemonPost } from '../../daemonClient';
import { get as bffGet } from '../../apiClient';

describe('system API', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // ---------------------------------------------------------------------------
  // Daemon-client routes (port 9001)
  // ---------------------------------------------------------------------------

  describe('getStatus — daemon client', () => {
    it('routes through the daemon client with /system/status', async () => {
      const mockStatus = { connected: true };
      vi.mocked(daemonGet).mockResolvedValue(mockStatus);

      const result = await system.getStatus();

      expect(daemonGet).toHaveBeenCalledWith('/system/status');
      expect(bffGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockStatus);
    });
  });

  describe('getDaemonStatus — daemon client', () => {
    it('routes through the daemon client with /system/daemon/status', async () => {
      const mockStatus = { status: 'running', connected: true };
      vi.mocked(daemonGet).mockResolvedValue(mockStatus);

      const result = await system.getDaemonStatus();

      expect(daemonGet).toHaveBeenCalledWith('/system/daemon/status');
      expect(bffGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockStatus);
    });
  });

  describe('connectDaemon — daemon client', () => {
    it('routes through the daemon client with /system/daemon/connect', async () => {
      const mockResult = { status: 'connected' };
      vi.mocked(daemonPost).mockResolvedValue(mockResult);

      const result = await system.connectDaemon();

      expect(daemonPost).toHaveBeenCalledWith('/system/daemon/connect');
      expect(result).toEqual(mockResult);
    });
  });

  describe('disconnectDaemon — daemon client', () => {
    it('routes through the daemon client with /system/daemon/disconnect', async () => {
      const mockResult = { status: 'disconnected' };
      vi.mocked(daemonPost).mockResolvedValue(mockResult);

      const result = await system.disconnectDaemon();

      expect(daemonPost).toHaveBeenCalledWith('/system/daemon/disconnect');
      expect(result).toEqual(mockResult);
    });
  });

  describe('getVersion — daemon client', () => {
    it('routes through the daemon client with /system/version', async () => {
      const mockVersion = { version: '1.0.0', service: 'mtga-companion' };
      vi.mocked(daemonGet).mockResolvedValue(mockVersion);

      const result = await system.getVersion();

      expect(daemonGet).toHaveBeenCalledWith('/system/version');
      expect(bffGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockVersion);
    });
  });

  describe('getDatabasePath — daemon client', () => {
    it('routes through the daemon client with /system/database/path', async () => {
      const mockPath = { path: '/path/to/db' };
      vi.mocked(daemonGet).mockResolvedValue(mockPath);

      const result = await system.getDatabasePath();

      expect(daemonGet).toHaveBeenCalledWith('/system/database/path');
      expect(bffGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockPath);
    });
  });

  describe('setDatabasePath — daemon client', () => {
    it('routes through the daemon client with /system/database/path', async () => {
      const mockResult = { status: 'ok' };
      vi.mocked(daemonPost).mockResolvedValue(mockResult);

      const result = await system.setDatabasePath('/new/path');

      expect(daemonPost).toHaveBeenCalledWith('/system/database/path', { path: '/new/path' });
      expect(result).toEqual(mockResult);
    });
  });

  describe('uninstallDaemon — daemon client', () => {
    it('POSTs through daemon client without purge by default', async () => {
      vi.mocked(daemonPost).mockResolvedValue({ status: 'scheduled', message: 'ok' });

      const result = await system.uninstallDaemon();

      expect(daemonPost).toHaveBeenCalledWith('/system/uninstall');
      expect(result).toEqual({ status: 'scheduled', message: 'ok' });
    });

    it('appends ?purge=true through daemon client when opts.purge is true', async () => {
      vi.mocked(daemonPost).mockResolvedValue({ status: 'scheduled', message: 'ok' });

      await system.uninstallDaemon({ purge: true });

      expect(daemonPost).toHaveBeenCalledWith('/system/uninstall?purge=true');
    });

    it('omits the query string when opts.purge is false', async () => {
      vi.mocked(daemonPost).mockResolvedValue({ status: 'scheduled', message: 'ok' });

      await system.uninstallDaemon({ purge: false });

      expect(daemonPost).toHaveBeenCalledWith('/system/uninstall');
    });
  });

  // ---------------------------------------------------------------------------
  // BFF-client routes (port 8080) — fixes #2007 and #2008
  // ---------------------------------------------------------------------------

  describe('getHealth — BFF client (#2007)', () => {
    it('routes through the BFF client with /system/health, not the daemon client', async () => {
      const mockHealth = {
        status: 'ok',
        version: '1.0.0',
        uptime: 3600,
        database: { status: 'ok' },
        logMonitor: { status: 'ok' },
        websocket: { status: 'ok', connectedClients: 0 },
        metrics: { totalProcessed: 100, totalErrors: 0 },
      };
      vi.mocked(bffGet).mockResolvedValue(mockHealth);

      const result = await system.getHealth();

      expect(bffGet).toHaveBeenCalledWith('/system/health');
      // Daemon client must NOT be called — this was the bug in #2007.
      expect(daemonGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockHealth);
    });
  });

  describe('getCurrentAccount — BFF client (#2008)', () => {
    it('routes through the BFF client with /system/account, not the daemon client', async () => {
      const mockAccount = { id: 123, name: 'Player', mtgaPlayerId: 'abc' };
      vi.mocked(bffGet).mockResolvedValue(mockAccount);

      const result = await system.getCurrentAccount();

      expect(bffGet).toHaveBeenCalledWith('/system/account');
      // Daemon client must NOT be called — this was the bug in #2008.
      expect(daemonGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockAccount);
    });
  });

  describe('exportMLTrainingData — BFF client', () => {
    it('routes through the BFF client with limit in query', async () => {
      const mockData = { records: [] };
      vi.mocked(bffGet).mockResolvedValue(mockData);

      const result = await system.exportMLTrainingData(100);

      expect(bffGet).toHaveBeenCalledWith('/feedback/ml-training?limit=100');
      expect(daemonGet).not.toHaveBeenCalled();
      expect(result).toEqual(mockData);
    });
  });
});
