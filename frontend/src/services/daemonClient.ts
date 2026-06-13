/**
 * Daemon API client — communicates with the local MTGA log-parsing daemon.
 *
 * Targets the daemon's local API at the URL derived in `./daemonConfig`
 * from the single coupling point `VITE_DAEMON_URL` (defaults to the stable
 * port 9001; staging builds set it to 9011). The same source-of-truth feeds
 * the Setup /health probe and the Copy-Diagnostics fetch, so the SPA only
 * ever has to discover one local daemon port per channel.
 *
 * Surface (post #1436): `get`, `post`, `put`, `del`, `postFormData`. Three
 * modules call us:
 *   - system.ts        (the surviving /system/* + /feedback/ml-training routes)
 *   - drafts.ts        (the 3 Bucket C live-state wrappers from PR #14)
 *
 * `put`, `del`, and `postFormData` were absent before #1436 — they match the
 * three-line patterns from apiClient.ts and are added here so any future daemon
 * endpoint needing those verbs has a daemon-scoped wrapper (not a BFF one).
 *
 * Cloud/BFF routes must continue to import from ./apiClient.
 */

import type { ApiConfig, ApiError } from './apiClient';
import { ApiRequestError, getApiKey } from './apiClient';
import { getDaemonApiBaseUrl } from './daemonConfig';

// ---------------------------------------------------------------------------
// Configuration — ADR-077 call-time derivation
//
// getDaemonApiBaseUrl() is called inside getDaemonConfig() at request time, not
// at module load. This prevents a "loadConfig() has not completed" throw when
// daemonClient is imported before the boot sequence sets the runtime config.
// ---------------------------------------------------------------------------

function getDaemonConfig(): ApiConfig {
  return {
    baseUrl: getDaemonApiBaseUrl(),
    timeout: 30000,
  };
}

// ---------------------------------------------------------------------------
// Auth header (same localStorage key as apiClient)
// ---------------------------------------------------------------------------

function authHeaders(): Record<string, string> {
  const key = getApiKey();
  return key ? { Authorization: `Bearer ${key}` } : {};
}

// ---------------------------------------------------------------------------
// Core request
// ---------------------------------------------------------------------------

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  options: RequestInit = {}
): Promise<T> {
  const daemonConfig = getDaemonConfig();
  const url = `${daemonConfig.baseUrl}${path}`;

  const controller = new globalThis.AbortController();
  const timeoutId = setTimeout(() => controller.abort(), daemonConfig.timeout);

  try {
    const response = await fetch(url, {
      method,
      headers: {
        'Content-Type': 'application/json',
        ...authHeaders(),
        ...options.headers,
      },
      body: body ? JSON.stringify(body) : undefined,
      signal: controller.signal,
      ...options,
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      let errorData: ApiError = { error: 'Unknown error' };
      try {
        errorData = await response.json();
      } catch {
        errorData = { error: response.statusText || 'Request failed' };
      }
      const errorMessage = errorData.message || errorData.error;
      throw new ApiRequestError(
        errorMessage,
        response.status,
        errorData.code,
        errorData.details
      );
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const data = await response.json();
    return data.data as T;
  } catch (error) {
    clearTimeout(timeoutId);

    if (error instanceof ApiRequestError) {
      throw error;
    }

    if (error instanceof Error) {
      if (error.name === 'AbortError') {
        throw new ApiRequestError('Request timeout', 408);
      }
      throw new ApiRequestError(error.message, 0);
    }

    throw new ApiRequestError('Unknown error', 0);
  }
}

// ---------------------------------------------------------------------------
// HTTP helpers
// ---------------------------------------------------------------------------

export function get<T>(path: string, options?: RequestInit): Promise<T> {
  return request<T>('GET', path, undefined, options);
}

export function post<T>(path: string, body?: unknown, options?: RequestInit): Promise<T> {
  return request<T>('POST', path, body, options);
}

export function put<T>(path: string, body?: unknown, options?: RequestInit): Promise<T> {
  return request<T>('PUT', path, body, options);
}

export function del<T>(path: string, options?: RequestInit): Promise<T> {
  return request<T>('DELETE', path, undefined, options);
}

/**
 * HTTP POST with multipart/form-data body targeting the daemon.
 *
 * Mirrors apiClient.postFormData: the caller builds the FormData and we
 * omit Content-Type so the browser sets it with the multipart boundary.
 */
export async function postFormData<T>(path: string, formData: FormData, options: RequestInit = {}): Promise<T> {
  const daemonConfig = getDaemonConfig();
  const url = `${daemonConfig.baseUrl}${path}`;

  const controller = new globalThis.AbortController();
  const timeoutId = setTimeout(() => controller.abort(), daemonConfig.timeout);

  try {
    const response = await fetch(url, {
      method: 'POST',
      headers: {
        ...authHeaders(),
        ...options.headers,
      },
      body: formData,
      signal: controller.signal,
      ...options,
    });

    clearTimeout(timeoutId);

    if (!response.ok) {
      let errorData: ApiError = { error: 'Unknown error' };
      try {
        errorData = await response.json();
      } catch {
        errorData = { error: response.statusText || 'Request failed' };
      }
      const errorMessage = errorData.message || errorData.error;
      throw new ApiRequestError(errorMessage, response.status, errorData.code, errorData.details);
    }

    if (response.status === 204) {
      return undefined as T;
    }

    const data = await response.json();
    return data.data as T;
  } catch (error) {
    clearTimeout(timeoutId);

    if (error instanceof ApiRequestError) {
      throw error;
    }

    if (error instanceof Error) {
      if (error.name === 'AbortError') {
        throw new ApiRequestError('Request timeout', 408);
      }
      throw new ApiRequestError(error.message, 0);
    }

    throw new ApiRequestError('Unknown error', 0);
  }
}
