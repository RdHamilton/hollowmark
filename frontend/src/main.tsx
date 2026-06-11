import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ClerkProvider } from '@clerk/react'
import { ui } from '@clerk/ui'
import * as Sentry from '@sentry/react'
import './index.css'
import App from './App.tsx'
import { AppProvider } from './context/AppContext'
import { DownloadProvider } from './context/DownloadContext'
import { TaskProgressProvider } from './context/TaskProgressContext'
import { initializeServices, servicesInitMs } from './services/adapter'
import { configureApi } from './services/apiClient'
import { configureWebSocket } from './services/websocketClient'
import { trackEvent, initAnalytics } from './services/analytics'
import StagingErrorBoundary from './components/StagingErrorBoundary'
import ConfigErrorScreen from './components/ConfigErrorScreen'
import { runLocalStorageMigration, runLocalStorageMigrationV2 } from './utils/localStorageMigration'
import { clerkLocalization } from './config/clerkLocalization'
import {
  loadConfig,
  mapErrorToBranches,
  fireBootBeacon,
  getRuntimeConfig,
  type ConfigErrorScreenBranch,
} from './config/runtimeConfig'

// Run localStorage key migrations BEFORE rendering so every component reads
// from the canonical hollowmark-* keys only.
//
// V1: mtga-companion-* → vaultmtg-*  (gated by vaultmtg-migration-v1 sentinel)
// V2: vaultmtg-*      → hollowmark-* (gated by hollowmark-migration-v2 sentinel)
//
// V2 must run after V1 so a user on the original mtga-companion-* namespace
// chains both hops in a single load. Both functions are idempotent and safe to
// call unconditionally on every app mount.
runLocalStorageMigration()
runLocalStorageMigrationV2()

// Social OAuth providers (Google, Facebook, Apple) are enabled in the Clerk Dashboard
// under "Social connections" — no additional code required here.
// Dashboard: https://dashboard.clerk.com → Social connections
// `ui` from @clerk/ui pins the bundled Clerk component version so structural CSS
// selectors like .api-keys-content .cl-apiKeys are stable across Clerk CDN updates.
// This suppresses the structural_css_pin_clerk_ui console warning (#2006).

const rootElement = document.getElementById('root')!

function renderErrorScreen(branch: ConfigErrorScreenBranch, onRetry?: () => void) {
  // VITE_APP_VERSION stays baked — it is the artifact identity constant
  // (ADR-075 §IV-3), not a per-environment runtime value.
  const appVersion = import.meta.env.VITE_APP_VERSION as string | undefined
  createRoot(rootElement).render(
    <StrictMode>
      <ConfigErrorScreen
        branch={branch}
        onRetry={onRetry}
        appVersion={appVersion}
      />
    </StrictMode>,
  )
}

const renderApp = () => {
  const cfg = getRuntimeConfig()

  createRoot(rootElement).render(
    <StrictMode>
      <Sentry.ErrorBoundary fallback={<p>Something went wrong</p>}>
        <StagingErrorBoundary>
          <ClerkProvider
            publishableKey={cfg.clerkPublishableKey}
            ui={ui}
            localization={clerkLocalization}
          >
            <AppProvider>
              <DownloadProvider>
                <TaskProgressProvider>
                  <App />
                </TaskProgressProvider>
              </DownloadProvider>
            </AppProvider>
          </ClerkProvider>
        </StagingErrorBoundary>
      </Sentry.ErrorBoundary>
    </StrictMode>,
  )
}

const SESSION_STARTED_KEY = 'vaultmtg_ph_app_session_started_fired'

// ---------------------------------------------------------------------------
// ADR-077 boot sequence
//
// 1. Fetch + validate /config.json (same-origin CloudFront sidecar)
// 2. On success: init Sentry, analytics, services → renderApp()
// 3. On failure: fireBootBeacon → renderErrorScreen (no Sentry, no Clerk init)
//
// Retry: for ConfigNetworkError only, the error screen exposes a "Try Again"
// button that re-runs boot() from the top. Parse and missing-field errors are
// configuration bugs (not transient) so they do not offer retry.
// ---------------------------------------------------------------------------

async function boot(): Promise<void> {
  let cfg
  try {
    cfg = await loadConfig()
  } catch (err) {
    const { screenBranch, beaconType } = mapErrorToBranches(err)

    // AC11: fire beacon BEFORE rendering error screen. Never call Sentry.init here —
    // no DSN is available. Never call initAnalytics() here (AC6).
    fireBootBeacon(beaconType)

    const onRetry = screenBranch === 'network' ? () => {
      // Clear the root and retry the full boot sequence.
      rootElement.innerHTML = ''
      void boot()
    } : undefined

    renderErrorScreen(screenBranch, onRetry)
    return
  }

  // --- config loaded successfully ---

  // Initialize Sentry only when sentryDsn is provided (absent in dev/test).
  // AC6: Sentry.init is ONLY called on the success path — never in the catch block.
  if (cfg.sentryDsn) {
    // VITE_APP_VERSION is injected at build time by the CI deploy workflow:
    //   - Production (deploy-spa.yml): resolved from `git describe --tags --match 'app/v*'`
    //   - Staging (deploy-spa-staging.yml): "staging-<full-git-sha>"
    //   - Local dev / unset: undefined (Sentry.init is skipped — sentryDsn is empty locally)
    // If the resolved value is empty, the `release` field is omitted entirely.
    const appVersion = import.meta.env.VITE_APP_VERSION as string | undefined
    const release = appVersion || undefined

    Sentry.init({
      dsn: cfg.sentryDsn,
      environment: cfg.sentryEnv,
      ...(release ? { release } : {}),
      // sendDefaultPii: false is the SDK default; set explicitly so the PII stance
      // is on record in code.
      sendDefaultPii: false,
      integrations: [
        Sentry.browserTracingIntegration(),
        // feedbackIntegration: enables Sentry.getFeedback() in ReportBugButton.
        // autoInject: false — we render our own trigger button in Layout instead of
        // the default floating widget so the button only appears for signed-in users.
        Sentry.feedbackIntegration({ autoInject: false }),
      ],
    })
  }

  // Initialize analytics (PostHog). posthogKey is optional — absent disables analytics.
  initAnalytics(cfg.posthogKey, cfg.posthogHost)

  // Wire the BFF URL from runtimeConfig into the REST and SSE clients.
  configureApi({ baseUrl: cfg.bffUrl })
  configureWebSocket({ url: `${cfg.bffUrl}/events` })

  // Initialize services (REST API health check) before rendering.
  await initializeServices().then(() => {
    // Fire app_session_started once per browser session after services initialize.
    // Guarded by sessionStorage so page-reloads within the same tab don't double-fire.
    if (!sessionStorage.getItem(SESSION_STARTED_KEY)) {
      trackEvent({ name: 'app_session_started', properties: { services_init_ms: servicesInitMs } })
      sessionStorage.setItem(SESSION_STARTED_KEY, '1')
    }
    renderApp()
  }).catch((error) => {
    console.error('Failed to initialize services:', error)
    // Render anyway - the app should handle missing services gracefully
    renderApp()
  })
}

void boot()
