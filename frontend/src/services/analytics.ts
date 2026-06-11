/**
 * PostHog analytics module for VaultMTG.
 *
 * Initialization is guarded by VITE_POSTHOG_KEY — if the env var is absent
 * (local dev, test) all calls are no-ops so components need no branching.
 *
 * Event taxonomy: docs/analytics/event-taxonomy.md
 */
import posthog from 'posthog-js';
import { CMPStorageKey, EventConsentCategory } from './analytics-taxonomy.gen';

// ── Initialization ────────────────────────────────────────────────────────────

let initialized = false;

/**
 * Initialize PostHog analytics.
 *
 * ADR-077: key and host should be passed from main.tsx after loadConfig()
 * populates runtimeConfig (production path). Parameters take precedence over
 * VITE_* env vars. When parameters are absent, falls back to VITE_POSTHOG_KEY /
 * VITE_POSTHOG_HOST so `vite dev` and existing unit tests work unchanged.
 *
 * If the resolved key is absent or empty, initialization is skipped (no-op).
 *
 * @param key  PostHog project API key (runtimeConfig.posthogKey)
 * @param host PostHog ingest host (runtimeConfig.posthogHost)
 */
export function initAnalytics(key?: string, host?: string): void {
  const POSTHOG_KEY = key ?? (import.meta.env.VITE_POSTHOG_KEY as string | undefined);
  const POSTHOG_HOST =
    (host || (import.meta.env.VITE_POSTHOG_HOST as string | undefined) || 'https://app.posthog.com');
  if (!POSTHOG_KEY) return;
  posthog.init(POSTHOG_KEY, {
    api_host: POSTHOG_HOST,
    // Privacy hardening (ADR-027 §5): disable IP collection, limit person
    // profiles to identified users only, and honour DNT headers.
    ip: false,
    person_profiles: 'identified_only',
    respect_dnt: true,
    // We fire page_viewed manually so we can attach properties.
    capture_pageview: false,
    // Disable autocapture to keep event taxonomy clean.
    autocapture: false,
    // Session replay: disabled by default; enabled per-user after auth.
    // maskAllInputs prevents any typed text from appearing in replays.
    // maskAllText: false — we use .ph-no-capture class for selective masking.
    // disable_session_recording: true until startSessionReplay() is called.
    session_recording: {
      maskAllInputs: true,
      maskTextSelector: '.sensitive, .ph-no-capture',
    },
    disable_session_recording: true,
  });
  initialized = true;
}

/**
 * Start PostHog session replay for the current user.
 * Must only be called once Clerk has confirmed isSignedIn — never for
 * unauthenticated sessions.
 */
export function startSessionReplay(): void {
  if (!initialized) return;
  posthog.startSessionRecording();
}

/**
 * Stop PostHog session replay (e.g. on sign-out).
 */
export function stopSessionReplay(): void {
  if (!initialized) return;
  posthog.stopSessionRecording();
}

// ── Event name constants (locked taxonomy) ────────────────────────────────────

export const Events = {
  // Setup / onboarding
  SETUP_PAGE_VIEWED: 'setup_page_viewed',
  SETUP_PAIRING_SUCCESS: 'setup_pairing_success',
  SETUP_PAIRING_TIMEOUT: 'setup_pairing_timeout',

  // Activation funnel
  FUNNEL_LANDING_PAGE_VIEWED: 'funnel_landing_page_viewed',
  FUNNEL_SIGN_UP_STARTED: 'funnel_sign_up_started',
  FUNNEL_SIGN_UP_COMPLETED: 'funnel_sign_up_completed',
  FUNNEL_DAEMON_DOWNLOAD_STARTED: 'funnel_daemon_download_started',
  FUNNEL_DAEMON_CONNECTED: 'funnel_daemon_connected',
  FUNNEL_DAEMON_INSTALLED: 'funnel_daemon_installed',
  FUNNEL_FIRST_GAME_PLAYED: 'funnel_first_game_played',
  FUNNEL_FIRST_DATA_LOADED: 'funnel_first_data_loaded',
  FUNNEL_FIRST_FEATURE_USED: 'funnel_first_feature_used',
  // ADR-027: BFF-only emission — taxonomy declared here for type safety;
  // the SPA never calls trackEvent with this name.
  FUNNEL_DAEMON_PAIRED: 'funnel_daemon_paired',

  // Page views
  PAGE_VIEWED: 'page_viewed',

  // Feature usage
  FEATURE_MATCH_HISTORY_FILTERED: 'feature_match_history_filtered',
  FEATURE_MATCH_DETAILS_OPENED: 'feature_match_details_opened',
  FEATURE_DRAFT_ADVISOR_PICK_VIEWED: 'feature_draft_advisor_pick_viewed',
  FEATURE_DRAFT_ANALYTICS_VIEWED: 'feature_draft_analytics_viewed',
  FEATURE_DECK_BUILDER_OPENED: 'feature_deck_builder_opened',
  FEATURE_DECK_BUILD_AROUND_STARTED: 'feature_deck_build_around_started',
  FEATURE_COLLECTION_VIEWED: 'feature_collection_viewed',
  FEATURE_META_VIEWED: 'feature_meta_viewed',
  FEATURE_ML_SUGGESTIONS_VIEWED: 'feature_ml_suggestions_viewed',
  WILDCARD_RECOMMENDATION_CLICKED: 'wildcard_recommendation_clicked',
  FEATURE_CHART_INTERACTED: 'feature_chart_interacted',
  FEATURE_OPPONENT_ANALYSIS_VIEWED: 'feature_opponent_analysis_viewed',
  FEATURE_COMMUNITY_COMPARISON_VIEWED: 'feature_community_comparison_viewed',
  FEATURE_SETTINGS_CHANGED: 'feature_settings_changed',
  FEATURE_REPLAY_STARTED: 'feature_replay_started',
  FEATURE_REPLAY_COMPLETED: 'feature_replay_completed',

  // Errors
  ERROR_DAEMON_CONNECTION_FAILED: 'error_daemon_connection_failed',
  ERROR_DAEMON_NEVER_CONNECTED: 'error_daemon_never_connected',
  ERROR_DATA_LOAD_FAILED: 'error_data_load_failed',
  ERROR_AUTH_FAILED: 'error_auth_failed',
  ERROR_EMPTY_STATE_SHOWN: 'error_empty_state_shown',

  // Engagement
  APP_SESSION_STARTED: 'app_session_started',
  APP_USER_IDENTIFIED: 'app_user_identified',
  APP_USER_SIGNED_OUT: 'app_user_signed_out',
} as const;

export type EventName = (typeof Events)[keyof typeof Events];

/** Platform on which the setup page is running. */
export type Platform = 'macos' | 'windows' | 'unknown';

// ── Typed property shapes per event ──────────────────────────────────────────
//
// Every entry in the discriminated union covers one event from the taxonomy.
// Adding a new event requires adding a new branch here — the compiler will
// enforce completeness at every trackEvent call site.

export type AnalyticsEvent =
  // Setup / onboarding
  | {
      name: 'setup_page_viewed';
      properties: { platform: Platform };
    }
  | {
      name: 'setup_pairing_success';
      properties: { platform: Platform };
    }
  | {
      name: 'setup_pairing_timeout';
      properties: { platform: Platform };
    }
  // Activation funnel
  | {
      name: 'funnel_landing_page_viewed';
      properties: {
        referrer: string;
        utm_source: string;
        utm_medium: string;
        utm_campaign: string;
      };
    }
  | {
      name: 'funnel_sign_up_started';
      properties: {
        entry_point: 'landing_page' | 'auth_bar' | 'protected_route_redirect';
      };
    }
  | {
      name: 'funnel_sign_up_completed';
      properties: {
        auth_method: 'email' | 'google' | 'apple' | 'facebook';
        user_id: string;
      };
    }
  | {
      name: 'funnel_daemon_download_started';
      properties: {
        os: string;
        download_source: 'download_page' | 'prompt_modal' | 'onboarding_modal';
      };
    }
  | {
      name: 'funnel_daemon_connected';
      properties?: {
        time_since_signup_seconds?: number;
        source?: string;
      };
    }
  | {
      name: 'funnel_daemon_installed';
      properties?: {
        /** Daemon version string if known */
        daemon_version?: string;
        /** Source page where the event fired */
        source?: string;
      };
    }
  | {
      name: 'funnel_first_game_played';
      properties?: {
        /** Format of the first game (e.g. "Standard", "Limited") */
        format?: string;
        /** Source page where the event fired */
        source?: string;
      };
    }
  | {
      name: 'funnel_first_data_loaded';
      properties: { match_count: number };
    }
  | {
      name: 'funnel_first_feature_used';
      properties: {
        feature:
          | 'draft'
          | 'draft_analytics'
          | 'decks'
          | 'collection'
          | 'meta'
          | 'charts'
          | 'quests';
      };
    }
  // ADR-027: BFF emits this event server-side when the daemon completes its
  // first pairing handshake. The SPA never calls trackEvent with this name —
  // this branch exists only so the constant is type-safe if referenced.
  | {
      name: 'funnel_daemon_paired';
      properties?: Record<string, never>;
    }
  // Page views
  | {
      name: 'page_viewed';
      properties: { page: string; previous_page: string | null };
    }
  // Feature usage
  | {
      name: 'feature_match_history_filtered';
      properties: {
        filter_type: 'format' | 'deck' | 'date_range' | 'result';
        filter_value: string;
      };
    }
  | {
      name: 'feature_match_details_opened';
      properties: {
        match_result: 'win' | 'loss' | 'draw';
        format: string;
      };
    }
  | {
      name: 'feature_draft_advisor_pick_viewed';
      properties: {
        set_code: string;
        pack_number: number;
        pick_number: number;
      };
    }
  | {
      name: 'feature_draft_analytics_viewed';
      properties: { draft_count: number };
    }
  | {
      name: 'feature_deck_builder_opened';
      properties: {
        entry_point: 'decks_list' | 'draft_build_around' | 'direct_link';
      };
    }
  | {
      name: 'feature_deck_build_around_started';
      properties: { seed_type: 'card' | 'archetype' | 'color_pair' };
    }
  | {
      name: 'feature_collection_viewed';
      properties: { card_count: number };
    }
  | { name: 'feature_meta_viewed'; properties?: Record<string, never> }
  | {
      name: 'feature_ml_suggestions_viewed';
      properties: {
        suggestion_count: number;
        context: 'deck_builder' | 'draft' | 'collection';
      };
    }
  | {
      name: 'wildcard_recommendation_clicked';
      properties: {
        /** Present when fired from MLSuggestionsPanel (deck-builder context). */
        suggestion_type?: 'add' | 'remove' | 'swap';
        /** Present when fired from WildcardAdvisorPanel (collection context). */
        rarity?: 'common' | 'uncommon' | 'rare' | 'mythic';
        suggestion_count: number;
      };
    }
  | {
      name: 'feature_chart_interacted';
      properties: {
        chart: string;
        interaction: 'filter_applied' | 'time_range_changed' | 'format_changed';
      };
    }
  | {
      name: 'feature_opponent_analysis_viewed';
      properties: { opponent_match_count: number };
    }
  | {
      name: 'feature_community_comparison_viewed';
      properties?: Record<string, never>;
    }
  | {
      name: 'feature_settings_changed';
      properties: {
        setting_section: 'daemon_connection' | 'preferences' | 'display';
        setting_key: string;
      };
    }
  | { name: 'feature_replay_started'; properties?: Record<string, never> }
  | { name: 'feature_replay_completed'; properties?: Record<string, never> }
  // Errors
  | {
      name: 'error_daemon_connection_failed';
      properties: {
        previous_status: 'connected' | 'reconnecting';
        duration_connected_seconds: number;
      };
    }
  | {
      name: 'error_daemon_never_connected';
      properties: {
        time_since_signin_seconds?: number;
        source?: string;
      };
    }
  | {
      name: 'error_data_load_failed';
      properties: { page: string; endpoint: string; status_code: number };
    }
  | {
      name: 'error_auth_failed';
      // reason_class replaces the original free-form `context: string` per Ray
      // Q2 amendment — enum prevents raw error message PII leaking into PostHog.
      // 'network' is the only value emitted this PR; 'invalid_credentials' and
      // 'rate_limited' are deferred to a custom Clerk sign-in follow-up ticket.
      properties: { reason_class: 'network' | 'invalid_credentials' | 'rate_limited' };
    }
  | {
      name: 'error_empty_state_shown';
      properties: { page: string };
    }
  // Engagement
  | {
      name: 'app_session_started';
      properties: { services_init_ms: number };
    }
  | {
      name: 'app_user_identified';
      // user_id is the hashed Clerk user_id (hashAccountID — SHA-256 hex[:16]).
      // ADR-027 / I-10 / ticket #82: raw Clerk user_ids must never reach PostHog.
      // Previously omitted pending this ticket; added per AC3 (2026-06-10).
      properties: {
        auth_method: 'email' | 'google' | 'apple' | 'facebook';
        user_id: string;
      };
    }
  | { name: 'app_user_signed_out'; properties?: Record<string, never> };

// ── Typed capture entry point ─────────────────────────────────────────────────

/**
 * Typed PostHog event capture. Property shapes are enforced per event name.
 * No-op when PostHog is not initialized (key absent).
 * Never include PII — use opaque Clerk user_id only.
 */
export function trackEvent(event: AnalyticsEvent): void {
  if (!initialized) return;
  // Consent gate (ADR-027 §4): drop analytics-tier events until the user
  // has accepted analytics cookies. 'necessary' events always fire.
  const consent = localStorage.getItem(CMPStorageKey);
  const tier = EventConsentCategory[event.name as keyof typeof EventConsentCategory];
  if (tier === 'analytics' && consent !== 'accepted') return;
  posthog.capture(event.name, event.properties);
}

// ── Core helpers ──────────────────────────────────────────────────────────────

/**
 * @deprecated Use `trackEvent` which enforces typed property shapes and the
 * consent gate. This function will be removed in a future PR.
 */
export function captureEvent(
  name: EventName,
  properties?: Record<string, unknown>,
): void {
  trackEvent({ name, properties } as AnalyticsEvent);
}

// ── PII hashing (ADR-027) ─────────────────────────────────────────────────────

/**
 * Core SHA-256 hex[:16] primitive. Uses the Web Crypto API (available in all
 * modern browsers and Vite/JSDOM). Unsalted — a secret salt cannot be held
 * in a browser bundle; a bundled "salt" is obfuscation, not a secret (Ray Q2,
 * ticket #82).
 */
async function sha256Hex16(value: string): Promise<string> {
  const buf = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value),
  );
  return Array.from(new Uint8Array(buf))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('')
    .slice(0, 16);
}

/**
 * Hash a generic PII value (e.g. email) using SHA-256 hex[:16] (ADR-027).
 * Use this for human-readable PII fields such as email addresses.
 * Do NOT use for Clerk user_id analytics distinct_id — use hashAccountID().
 */
export async function hashPII(value: string): Promise<string> {
  return sha256Hex16(value);
}

/**
 * Hash a Clerk user_id for use as the PostHog analytics distinct_id
 * (ADR-027 / I-10 / ticket #82). Unsalted SHA-256 hex[:16] via Web Crypto API.
 *
 * Design rationale: the BFF's HashAccountID uses the same algorithm on the
 * internal numeric account_id; the SPA uses this on the Clerk user_id string.
 * These two inputs are structurally different so the resulting distinct_ids are
 * always different — the cross-surface identity-join gap is accepted for beta
 * (Ray Q1 ruling, #82 plan comment 4674550690). The clean break is intentional:
 * posthog.alias() would re-transmit the raw Clerk id, violating I-10.
 */
export async function hashAccountID(userId: string): Promise<string> {
  return sha256Hex16(userId);
}

/**
 * Identify the current user by their opaque Clerk user ID.
 *
 * The Clerk user_id is hashed via hashAccountID() (unsalted SHA-256 hex[:16],
 * ADR-027 / I-10 / ticket #82) before being passed to posthog.identify() as
 * the distinct_id. The raw Clerk user_id NEVER reaches PostHog.
 *
 * Optionally accepts the user's email — it is SHA-256 hashed (hex[:16]) before
 * being sent to PostHog as the `hashed_email` person property.
 *
 * Clean-break migration note: users who had PostHog person profiles under their
 * raw Clerk user_id will now appear as a new person under the hashed distinct_id.
 * posthog.alias() is intentionally omitted — it would re-transmit the raw id,
 * violating I-10 (Ray Q3 ruling, #82 plan comment 4674550690).
 *
 * Must only be called once per session after Clerk confirms isSignedIn.
 */
export async function identifyUser(
  userId: string,
  email?: string,
): Promise<void> {
  if (!initialized) return;
  const hashedUserId = await hashAccountID(userId);
  if (email) {
    const hashedEmail = await hashPII(email);
    posthog.identify(hashedUserId, { hashed_email: hashedEmail });
  } else {
    posthog.identify(hashedUserId);
  }
}

/**
 * Reset PostHog identity on sign-out.
 */
export function resetIdentity(): void {
  if (!initialized) return;
  posthog.reset();
}

/**
 * Register super-properties sent on every subsequent event.
 */
export function registerSuperProperties(
  properties: Record<string, unknown>,
): void {
  if (!initialized) return;
  posthog.register(properties);
}

/**
 * Remove a single super-property so it is no longer sent on subsequent events.
 * Must be called BEFORE resetIdentity() on sign-out — posthog.reset() clears
 * the distinct_id and would orphan the super-property.
 */
export function unregisterSuperProperty(propertyName: string): void {
  if (!initialized) return;
  posthog.unregister(propertyName);
}
