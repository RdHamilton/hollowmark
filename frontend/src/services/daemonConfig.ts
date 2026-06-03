/**
 * Daemon URL source-of-truth.
 *
 * The local MTGA log-parsing daemon binds a channel-specific port on the
 * loopback interface (stable=9001, staging=9011 — see internal/install and
 * services/daemon/internal/localapi). The SPA must target the SAME port the
 * daemon for that channel actually binds.
 *
 * `VITE_DAEMON_URL` is the single coupling point between the SPA and the
 * daemon's local API (CLAUDE.md rule 4). Every site that talks to the daemon
 * — the daemon REST client, the Setup health probe, and the Copy-Diagnostics
 * fetch — derives its URL from the ONE origin parsed here. We deliberately do
 * NOT add a second `VITE_CHANNEL` → port derivation: a second source could
 * drift from the build pipeline's `VITE_DAEMON_URL` value.
 *
 * The build pipeline sets `VITE_DAEMON_URL` per channel:
 *   - stable / prod / preview / local: http://localhost:9001/api/v1
 *   - staging:                          http://localhost:9011/api/v1
 *
 * Host normalization: callers historically split between `localhost` (Setup,
 * daemonClient) and `127.0.0.1` (CopyDiagnostics). We normalize every derived
 * URL onto `127.0.0.1` so all three sites are consistent and immune to the
 * IPv6-`localhost` (`::1`) resolution edge case that can race the daemon's
 * IPv4 loopback bind.
 */

const DEFAULT_DAEMON_URL = 'http://localhost:9001/api/v1';

export interface DaemonUrls {
  /** Origin only, normalized to 127.0.0.1 — e.g. `http://127.0.0.1:9011`. */
  origin: string;
  /** REST API base, includes the `/api/v1` prefix — e.g. `http://127.0.0.1:9011/api/v1`. */
  apiBaseUrl: string;
  /** Health probe URL — e.g. `http://127.0.0.1:9011/health`. */
  healthUrl: string;
}

/**
 * Pure derivation of the daemon URLs from a raw `VITE_DAEMON_URL` value.
 *
 * Exported (not just the constants below) so it is unit-testable without
 * stubbing `import.meta.env`. The single parsed origin drives BOTH the API
 * base and the health URL — they can never disagree on host or port.
 *
 * @param rawUrl the `VITE_DAEMON_URL` env value (may be undefined/empty)
 */
export function deriveDaemonUrls(rawUrl?: string | null): DaemonUrls {
  const raw = rawUrl && rawUrl.trim().length > 0 ? rawUrl.trim() : DEFAULT_DAEMON_URL;

  let parsed: URL;
  try {
    parsed = new URL(raw);
  } catch {
    // Malformed value (e.g. a bare host:port) — fall back to the default so the
    // SPA degrades to the prod port rather than throwing at module load.
    parsed = new URL(DEFAULT_DAEMON_URL);
  }

  // Normalize the loopback host: localhost / ::1 → 127.0.0.1. Leave any other
  // host untouched, brackets included (the daemon is always loopback, but we
  // don't silently rewrite a deliberately non-loopback override). Note:
  // URL.hostname returns IPv6 hosts wrapped in brackets (`[::1]`), so strip
  // those for the loopback comparison only.
  const rawHost = parsed.hostname;
  const unbracketed = rawHost.replace(/^\[|\]$/g, '');
  const isLoopback =
    unbracketed === 'localhost' || unbracketed === '::1' || unbracketed === '127.0.0.1';
  const normalizedHost = isLoopback ? '127.0.0.1' : rawHost;
  const portSuffix = parsed.port ? `:${parsed.port}` : '';
  const origin = `${parsed.protocol}//${normalizedHost}${portSuffix}`;

  return {
    origin,
    apiBaseUrl: `${origin}/api/v1`,
    healthUrl: `${origin}/health`,
  };
}

/**
 * The resolved daemon URLs for this build, derived once from the single
 * coupling point. Import these everywhere instead of hardcoding a port.
 */
export const daemonUrls: DaemonUrls = deriveDaemonUrls(import.meta.env.VITE_DAEMON_URL);

export const daemonApiBaseUrl = daemonUrls.apiBaseUrl;
export const daemonHealthUrl = daemonUrls.healthUrl;
