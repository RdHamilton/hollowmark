import { describe, it, expect } from 'vitest';
import { deriveDaemonUrls } from '../daemonConfig';

describe('deriveDaemonUrls', () => {
  describe('parses VITE_DAEMON_URL', () => {
    it('derives API base and health URL from the staging value (port 9011)', () => {
      const urls = deriveDaemonUrls('http://localhost:9011/api/v1');
      expect(urls.origin).toBe('http://127.0.0.1:9011');
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9011/api/v1');
      expect(urls.healthUrl).toBe('http://127.0.0.1:9011/health');
    });

    it('derives API base and health URL from the stable value (port 9001)', () => {
      const urls = deriveDaemonUrls('http://localhost:9001/api/v1');
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
      expect(urls.healthUrl).toBe('http://127.0.0.1:9001/health');
    });

    it('ignores any path on the env value — health and API derive from origin only', () => {
      // VITE_DAEMON_URL includes /api/v1; the health URL must NOT become
      // /api/v1/health — it derives from the parsed origin, not the path.
      const urls = deriveDaemonUrls('http://localhost:9011/api/v1');
      expect(urls.healthUrl).toBe('http://127.0.0.1:9011/health');
      expect(urls.healthUrl).not.toContain('/api/v1');
    });
  });

  describe('default fallback', () => {
    it('falls back to the stable port (9001) when value is undefined', () => {
      const urls = deriveDaemonUrls(undefined);
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
      expect(urls.healthUrl).toBe('http://127.0.0.1:9001/health');
    });

    it('falls back to the stable port (9001) when value is null', () => {
      const urls = deriveDaemonUrls(null);
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
    });

    it('falls back to the stable port (9001) when value is an empty string', () => {
      const urls = deriveDaemonUrls('');
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
    });

    it('falls back to the stable port (9001) when value is whitespace only', () => {
      const urls = deriveDaemonUrls('   ');
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
    });

    it('falls back to the default when value is unparseable', () => {
      const urls = deriveDaemonUrls('not a url');
      expect(urls.apiBaseUrl).toBe('http://127.0.0.1:9001/api/v1');
      expect(urls.healthUrl).toBe('http://127.0.0.1:9001/health');
    });
  });

  describe('127.0.0.1 normalization', () => {
    it('normalizes localhost → 127.0.0.1', () => {
      expect(deriveDaemonUrls('http://localhost:9011/api/v1').origin).toBe(
        'http://127.0.0.1:9011'
      );
    });

    it('normalizes IPv6 ::1 → 127.0.0.1', () => {
      expect(deriveDaemonUrls('http://[::1]:9011/api/v1').origin).toBe(
        'http://127.0.0.1:9011'
      );
    });

    it('leaves an explicit 127.0.0.1 host unchanged', () => {
      expect(deriveDaemonUrls('http://127.0.0.1:9001/api/v1').origin).toBe(
        'http://127.0.0.1:9001'
      );
    });

    it('does not rewrite a deliberately non-loopback host', () => {
      const urls = deriveDaemonUrls('http://daemon.internal:9011/api/v1');
      expect(urls.origin).toBe('http://daemon.internal:9011');
      expect(urls.apiBaseUrl).toBe('http://daemon.internal:9011/api/v1');
    });

    it('preserves the protocol (https stays https)', () => {
      expect(deriveDaemonUrls('https://localhost:9011/api/v1').origin).toBe(
        'https://127.0.0.1:9011'
      );
    });
  });

  describe('API + health URL construction agree on host and port', () => {
    it('both derive from the same origin for every channel', () => {
      for (const raw of [
        'http://localhost:9001/api/v1',
        'http://localhost:9011/api/v1',
        'http://127.0.0.1:9001/api/v1',
      ]) {
        const urls = deriveDaemonUrls(raw);
        expect(urls.apiBaseUrl.startsWith(urls.origin)).toBe(true);
        expect(urls.healthUrl.startsWith(urls.origin)).toBe(true);
        expect(urls.apiBaseUrl).toBe(`${urls.origin}/api/v1`);
        expect(urls.healthUrl).toBe(`${urls.origin}/health`);
      }
    });
  });
});
