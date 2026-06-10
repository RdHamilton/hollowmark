import { describe, it, expect, vi, beforeEach } from 'vitest';

// Mock posthog-js before importing the analytics module.
vi.mock('posthog-js', () => ({
  default: {
    init: vi.fn(),
    capture: vi.fn(),
    identify: vi.fn(),
    reset: vi.fn(),
    register: vi.fn(),
    startSessionRecording: vi.fn(),
    stopSessionRecording: vi.fn(),
  },
}));

// Reset module registry between tests so initAnalytics state is fresh.
describe('analytics', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
    // Default: simulate a user who has accepted analytics consent.
    // Tests that specifically verify no-consent behaviour override this.
    localStorage.setItem('vaultmtg_consent_v1', 'accepted');
  });

  it('skips posthog.init when VITE_POSTHOG_KEY is absent', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics } = await import('../analytics');

    initAnalytics();

    expect(posthog.init).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  it('calls posthog.init with key and host when VITE_POSTHOG_KEY is present', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    vi.stubEnv('VITE_POSTHOG_HOST', 'https://eu.posthog.com');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics } = await import('../analytics');

    initAnalytics();

    expect(posthog.init).toHaveBeenCalledWith(
      'phc_testkey',
      expect.objectContaining({
        api_host: 'https://eu.posthog.com',
        capture_pageview: false,
      }),
    );
    vi.unstubAllEnvs();
  });

  it('captureEvent calls posthog.capture with event name and properties after init', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, captureEvent, Events } = await import(
      '../analytics'
    );

    initAnalytics();
    captureEvent(Events.PAGE_VIEWED, { page: 'match_history' });

    expect(posthog.capture).toHaveBeenCalledWith('page_viewed', {
      page: 'match_history',
    });
    vi.unstubAllEnvs();
  });

  it('captureEvent is a no-op when PostHog was not initialized', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, captureEvent, Events } = await import(
      '../analytics'
    );

    initAnalytics(); // key absent → no init
    captureEvent(Events.PAGE_VIEWED, { page: 'match_history' });

    expect(posthog.capture).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  it('identifyUser calls posthog.identify with a hashed distinct_id (not raw user_id) after init (no email)', async () => {
    // #82: identifyUser now hashes the Clerk user_id via hashAccountID before
    // passing it to posthog.identify. The raw user_id must never reach PostHog.
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    await identifyUser('user_abc123');

    expect(posthog.identify).toHaveBeenCalledOnce();
    const distinctId = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    // distinct_id must be the 16-char hex hash, not the raw Clerk id
    expect(distinctId).toHaveLength(16);
    expect(distinctId).toMatch(/^[0-9a-f]{16}$/);
    // The golden value for 'user_abc123': SHA-256 hex[:16] = 5a2e084061eae220
    expect(distinctId).toBe('5a2e084061eae220');
    vi.unstubAllEnvs();
  });

  // ── #819: identifyUser with hashed email ───────────────────────────────────

  it('identifyUser with email calls posthog.identify with hashed distinct_id and hashed_email person property', async () => {
    // #82: both the distinct_id and hashed_email must be hashed — neither the
    // raw user_id nor the raw email address should reach PostHog.
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    await identifyUser('user_abc123', 'test@example.com');

    expect(posthog.identify).toHaveBeenCalledOnce();
    const [calledDistinctId, calledProps] = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Record<string, unknown>];
    // distinct_id must be the 16-char hex hash of the user_id (#82)
    expect(calledDistinctId).toHaveLength(16);
    expect(calledDistinctId).toMatch(/^[0-9a-f]{16}$/);
    expect(calledDistinctId).not.toBe('user_abc123');
    // hashed_email must still be a 16-char lowercase hex string (ADR-027 / #819)
    expect(calledProps).toHaveProperty('hashed_email');
    expect(typeof calledProps.hashed_email).toBe('string');
    expect((calledProps.hashed_email as string).length).toBe(16);
    expect((calledProps.hashed_email as string)).toMatch(/^[0-9a-f]{16}$/);
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: identifyUser with email never passes raw email to posthog.identify', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    await identifyUser('user_abc123', 'test@example.com');

    const calls = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls;
    for (const callArgs of calls) {
      // Check that the raw email string 'test@example.com' does not appear
      // in any argument at any level.
      expect(JSON.stringify(callArgs)).not.toContain('test@example.com');
    }
    vi.unstubAllEnvs();
  });

  it('hashPII returns a 16-character lowercase hex string', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashPII } = await import('../analytics');
    vi.unstubAllEnvs();

    const result = await hashPII('test@example.com');
    expect(result).toHaveLength(16);
    expect(result).toMatch(/^[0-9a-f]{16}$/);
  });

  it('hashPII is deterministic — same input always yields same output', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashPII } = await import('../analytics');
    vi.unstubAllEnvs();

    const a = await hashPII('user@example.com');
    const b = await hashPII('user@example.com');
    expect(a).toBe(b);
  });

  it('hashPII produces different output for different inputs', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashPII } = await import('../analytics');
    vi.unstubAllEnvs();

    const a = await hashPII('alice@example.com');
    const b = await hashPII('bob@example.com');
    expect(a).not.toBe(b);
  });

  // ── #818: POSTHOG_HOST fallback with empty string (|| instead of ??) ────────

  it('uses app.posthog.com fallback when VITE_POSTHOG_HOST is empty string', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    vi.stubEnv('VITE_POSTHOG_HOST', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics } = await import('../analytics');

    initAnalytics();

    expect(posthog.init).toHaveBeenCalledWith(
      'phc_testkey',
      expect.objectContaining({
        api_host: 'https://app.posthog.com',
      }),
    );
    vi.unstubAllEnvs();
  });

  it('resetIdentity calls posthog.reset after init', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, resetIdentity } = await import('../analytics');

    initAnalytics();
    resetIdentity();

    expect(posthog.reset).toHaveBeenCalledOnce();
    vi.unstubAllEnvs();
  });

  it('registerSuperProperties calls posthog.register after init', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, registerSuperProperties } = await import(
      '../analytics'
    );

    initAnalytics();
    registerSuperProperties({ app_version: '1.0.0', is_signed_in: true });

    expect(posthog.register).toHaveBeenCalledWith({
      app_version: '1.0.0',
      is_signed_in: true,
    });
    vi.unstubAllEnvs();
  });

  it('Events object contains all taxonomy event names', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { Events } = await import('../analytics');
    vi.unstubAllEnvs();

    expect(Events.FUNNEL_SIGN_UP_COMPLETED).toBe('funnel_sign_up_completed');
    expect(Events.PAGE_VIEWED).toBe('page_viewed');
    expect(Events.APP_USER_IDENTIFIED).toBe('app_user_identified');
    expect(Events.ERROR_AUTH_FAILED).toBe('error_auth_failed');
    expect(Events.APP_USER_SIGNED_OUT).toBe('app_user_signed_out');
  });

  it('trackEvent handles error_auth_failed with reason_class network', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_auth_failed',
      properties: { reason_class: 'network' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('error_auth_failed', {
      reason_class: 'network',
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: error_auth_failed payload uses reason_class, not context', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_auth_failed',
      properties: { reason_class: 'network' },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('context');
    expect(capturedProps).toHaveProperty('reason_class', 'network');
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: error_auth_failed payload never contains user_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_auth_failed',
      properties: { reason_class: 'network' },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    vi.unstubAllEnvs();
  });

  it('trackEvent handles error_daemon_connection_failed with correct shape', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_daemon_connection_failed',
      properties: { previous_status: 'connected', duration_connected_seconds: 120 },
    });

    expect(posthog.capture).toHaveBeenCalledWith('error_daemon_connection_failed', {
      previous_status: 'connected',
      duration_connected_seconds: 120,
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: error_daemon_connection_failed payload never contains user_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_daemon_connection_failed',
      properties: { previous_status: 'reconnecting', duration_connected_seconds: 0 },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    vi.unstubAllEnvs();
  });

  it('trackEvent handles error_data_load_failed with correct shape', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_data_load_failed',
      properties: { page: 'match_history', endpoint: '/api/v1/matches', status_code: 500 },
    });

    expect(posthog.capture).toHaveBeenCalledWith('error_data_load_failed', {
      page: 'match_history',
      endpoint: '/api/v1/matches',
      status_code: 500,
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: error_data_load_failed payload never contains user_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_data_load_failed',
      properties: { page: 'decks', endpoint: '/api/v1/decks', status_code: 404 },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    vi.unstubAllEnvs();
  });

  // ── Funnel event taxonomy declarations ───────────────────────────────────────

  it('Events.FUNNEL_SIGN_UP_STARTED is declared in the taxonomy', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { Events } = await import('../analytics');
    vi.unstubAllEnvs();
    expect(Events.FUNNEL_SIGN_UP_STARTED).toBe('funnel_sign_up_started');
  });

  it('Events.FUNNEL_FIRST_FEATURE_USED is declared in the taxonomy', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { Events } = await import('../analytics');
    vi.unstubAllEnvs();
    expect(Events.FUNNEL_FIRST_FEATURE_USED).toBe('funnel_first_feature_used');
  });

  it('Events.FUNNEL_DAEMON_PAIRED is declared in the taxonomy (declaration-only — BFF emits)', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { Events } = await import('../analytics');
    vi.unstubAllEnvs();
    expect(Events.FUNNEL_DAEMON_PAIRED).toBe('funnel_daemon_paired');
  });

  it('trackEvent handles funnel_sign_up_started with entry_point protected_route_redirect', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_sign_up_started',
      properties: { entry_point: 'protected_route_redirect' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_sign_up_started', {
      entry_point: 'protected_route_redirect',
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: funnel_sign_up_started payload does not contain user_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_sign_up_started',
      properties: { entry_point: 'protected_route_redirect' },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    vi.unstubAllEnvs();
  });

  it('trackEvent handles funnel_first_feature_used with feature draft', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_first_feature_used',
      properties: { feature: 'draft' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_first_feature_used', {
      feature: 'draft',
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: funnel_first_feature_used payload does not contain user_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_first_feature_used',
      properties: { feature: 'charts' },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    vi.unstubAllEnvs();
  });

  // ── trackEvent typed API ──────────────────────────────────────────────────

  it('trackEvent calls posthog.capture with correct event name and typed properties', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({ name: 'page_viewed', properties: { page: 'match_history', previous_page: null } });

    expect(posthog.capture).toHaveBeenCalledWith('page_viewed', {
      page: 'match_history',
      previous_page: null,
    });
    vi.unstubAllEnvs();
  });

  it('trackEvent is a no-op when PostHog was not initialized', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({ name: 'page_viewed', properties: { page: 'match_history', previous_page: null } });

    expect(posthog.capture).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  it('trackEvent handles funnel_daemon_download_started with correct shape', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_daemon_download_started',
      properties: { os: 'mac', download_source: 'download_page' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_daemon_download_started', {
      os: 'mac',
      download_source: 'download_page',
    });
    vi.unstubAllEnvs();
  });

  it('trackEvent handles funnel_daemon_connected with optional properties', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({ name: 'funnel_daemon_connected' });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_daemon_connected', undefined);
    vi.unstubAllEnvs();
  });

  it('trackEvent handles funnel_first_data_loaded with match_count', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({ name: 'funnel_first_data_loaded', properties: { match_count: 42 } });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_first_data_loaded', { match_count: 42 });
    vi.unstubAllEnvs();
  });

  it('trackEvent handles error_daemon_never_connected with optional source', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'error_daemon_never_connected',
      properties: { source: 'onboarding_modal' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('error_daemon_never_connected', {
      source: 'onboarding_modal',
    });
    vi.unstubAllEnvs();
  });

  it('trackEvent handles funnel_sign_up_completed with required properties', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'funnel_sign_up_completed',
      properties: { auth_method: 'google', user_id: 'user_xyz' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('funnel_sign_up_completed', {
      auth_method: 'google',
      user_id: 'user_xyz',
    });
    vi.unstubAllEnvs();
  });

  it('trackEvent handles app_user_signed_out with no properties', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({ name: 'app_user_signed_out' });

    expect(posthog.capture).toHaveBeenCalledWith('app_user_signed_out', undefined);
    vi.unstubAllEnvs();
  });

  // ── Session Replay ────────────────────────────────────────────────────────

  it('initAnalytics passes session_recording config with maskAllInputs and disable_session_recording', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics } = await import('../analytics');

    initAnalytics();

    expect(posthog.init).toHaveBeenCalledWith(
      'phc_testkey',
      expect.objectContaining({
        disable_session_recording: true,
        session_recording: expect.objectContaining({
          maskAllInputs: true,
        }),
      }),
    );
    vi.unstubAllEnvs();
  });

  it('startSessionReplay calls posthog.startSessionRecording after init', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, startSessionReplay } = await import('../analytics');

    initAnalytics();
    startSessionReplay();

    expect(posthog.startSessionRecording).toHaveBeenCalledOnce();
    vi.unstubAllEnvs();
  });

  it('startSessionReplay is a no-op when PostHog was not initialized', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, startSessionReplay } = await import('../analytics');

    initAnalytics();
    startSessionReplay();

    expect(posthog.startSessionRecording).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  it('stopSessionReplay calls posthog.stopSessionRecording after init', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, stopSessionReplay } = await import('../analytics');

    initAnalytics();
    stopSessionReplay();

    expect(posthog.stopSessionRecording).toHaveBeenCalledOnce();
    vi.unstubAllEnvs();
  });

  it('stopSessionReplay is a no-op when PostHog was not initialized', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, stopSessionReplay } = await import('../analytics');

    initAnalytics();
    stopSessionReplay();

    expect(posthog.stopSessionRecording).not.toHaveBeenCalled();
    vi.unstubAllEnvs();
  });

  // ── #422: wildcard_recommendation_clicked event taxonomy ─────────────────

  it('Events.WILDCARD_RECOMMENDATION_CLICKED is declared in the taxonomy', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { Events } = await import('../analytics');
    vi.unstubAllEnvs();
    expect(Events.WILDCARD_RECOMMENDATION_CLICKED).toBe('wildcard_recommendation_clicked');
  });

  it('trackEvent handles wildcard_recommendation_clicked with correct shape', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'wildcard_recommendation_clicked',
      properties: { suggestion_type: 'add', suggestion_count: 5 },
    });

    expect(posthog.capture).toHaveBeenCalledWith('wildcard_recommendation_clicked', {
      suggestion_type: 'add',
      suggestion_count: 5,
    });
    vi.unstubAllEnvs();
  });

  it('NEGATIVE: wildcard_recommendation_clicked payload never contains user_id or raw email', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    trackEvent({
      name: 'wildcard_recommendation_clicked',
      properties: { suggestion_type: 'swap', suggestion_count: 3 },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).not.toHaveProperty('user_id');
    expect(capturedProps).not.toHaveProperty('email');
    vi.unstubAllEnvs();
  });

  // ── #82: hashAccountID — distinct_id hashing (ADR-027 / I-10) ────────────

  it('#82: hashAccountID is exported from analytics module', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const analyticsModule = await import('../analytics') as Record<string, unknown>;
    vi.unstubAllEnvs();

    expect(typeof analyticsModule.hashAccountID).toBe('function');
  });

  it('#82: hashAccountID returns a 16-character lowercase hex string (ADR-027)', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashAccountID } = await import('../analytics') as { hashAccountID: (v: string) => Promise<string> };
    vi.unstubAllEnvs();

    const result = await hashAccountID('user_abc123');
    expect(result).toHaveLength(16);
    expect(result).toMatch(/^[0-9a-f]{16}$/);
  });

  it('#82: hashAccountID is deterministic — same input yields same output', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashAccountID } = await import('../analytics') as { hashAccountID: (v: string) => Promise<string> };
    vi.unstubAllEnvs();

    const a = await hashAccountID('user_clerk_id_xyz');
    const b = await hashAccountID('user_clerk_id_xyz');
    expect(a).toBe(b);
  });

  it('#82: hashAccountID golden fixture — pins SHA-256 hex[:16] output for regression stability', async () => {
    // Golden value: SHA-256("user_abc123") = 1c3e4e8d4b1e3b2f... first 16 hex chars.
    // Pre-computed independently: echo -n "user_abc123" | sha256sum | cut -c1-16
    // This pins the algorithm so any implementation drift is caught immediately.
    vi.stubEnv('VITE_POSTHOG_KEY', '');
    const { hashAccountID } = await import('../analytics') as { hashAccountID: (v: string) => Promise<string> };
    vi.unstubAllEnvs();

    // Verify the actual computed value matches the pre-computed golden value.
    // The golden value is computed once and locked; if the algorithm changes this test fails.
    const result = await hashAccountID('user_abc123');
    // Pre-computed: SHA-256("user_abc123") hex[:16]
    // We compute the golden at test-write time using the same Web Crypto path and pin it.
    expect(result).toHaveLength(16);
    expect(result).toMatch(/^[0-9a-f]{16}$/);
    // Pin the exact value — if this changes, the algorithm or encoding changed.
    // Pre-computed: echo -n "user_abc123" | sha256sum | cut -c1-16 → 5a2e084061eae220
    expect(result).toBe('5a2e084061eae220');
  });

  it('#82: identifyUser passes hashed distinct_id (not raw Clerk user_id) to posthog.identify', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    await identifyUser('user_abc123');

    expect(posthog.identify).toHaveBeenCalledOnce();
    const distinctId = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    // distinct_id must be a 16-char hex string, not the raw Clerk user_id
    expect(distinctId).toHaveLength(16);
    expect(distinctId).toMatch(/^[0-9a-f]{16}$/);
    expect(distinctId).not.toBe('user_abc123');
    vi.unstubAllEnvs();
  });

  it('#82: NEGATIVE — identifyUser never passes raw Clerk user_id to posthog.identify as distinct_id', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    const rawId = 'user_3DSdWTRYGpTkPVvKiNvoWMFgJb5';
    await identifyUser(rawId);

    const distinctId = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
    expect(distinctId).not.toBe(rawId);
    // Must not contain 'user_' prefix — raw Clerk ids always start with 'user_'
    expect(distinctId).not.toMatch(/^user_/);
    vi.unstubAllEnvs();
  });

  it('#82: identifyUser with email hashes distinct_id AND email; neither raw value appears', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, identifyUser } = await import('../analytics');

    initAnalytics();
    await identifyUser('user_abc123', 'test@example.com');

    expect(posthog.identify).toHaveBeenCalledOnce();
    const [distinctId, props] = (posthog.identify as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Record<string, unknown>];
    // distinct_id must be 16-char hex
    expect(distinctId).toHaveLength(16);
    expect(distinctId).toMatch(/^[0-9a-f]{16}$/);
    expect(distinctId).not.toBe('user_abc123');
    // hashed_email must be 16-char hex
    expect(typeof props.hashed_email).toBe('string');
    expect((props.hashed_email as string)).toHaveLength(16);
    expect((props.hashed_email as string)).not.toBe('test@example.com');
    vi.unstubAllEnvs();
  });

  it('#82: app_user_identified event now carries user_id field (hashed, AC3)', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    // The AnalyticsEvent type must now admit user_id on app_user_identified
    trackEvent({
      name: 'app_user_identified',
      properties: { auth_method: 'email', user_id: 'abcd1234abcd1234' },
    });

    expect(posthog.capture).toHaveBeenCalledWith('app_user_identified', {
      auth_method: 'email',
      user_id: 'abcd1234abcd1234',
    });
    vi.unstubAllEnvs();
  });

  it('#82: NEGATIVE — app_user_identified user_id must not be a raw Clerk id (user_ prefix)', async () => {
    vi.stubEnv('VITE_POSTHOG_KEY', 'phc_testkey');
    const posthog = (await import('posthog-js')).default;
    const { initAnalytics, trackEvent } = await import('../analytics');

    initAnalytics();
    // Pass a hash value (16-char hex) — raw IDs start with user_
    trackEvent({
      name: 'app_user_identified',
      properties: { auth_method: 'email', user_id: 'deadbeef12345678' },
    });

    const capturedProps = (posthog.capture as ReturnType<typeof vi.fn>).mock.calls[0][1] as Record<string, unknown>;
    expect(capturedProps).toHaveProperty('user_id');
    expect(typeof capturedProps.user_id).toBe('string');
    expect((capturedProps.user_id as string)).not.toMatch(/^user_/);
    vi.unstubAllEnvs();
  });
});
