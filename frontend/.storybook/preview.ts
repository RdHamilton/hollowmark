import type { Preview } from '@storybook/react';
import { initialize, mswLoader } from 'msw-storybook-addon';
import { withClerkSession } from './decorators';

// Global application styles. The VaultMTG SPA uses plain CSS (not Tailwind) —
// `index.css` holds the design tokens (CSS custom properties), dark color
// scheme, and base typography. Importing it here ensures every story renders
// against the same foundation as the running app. Per-component `.css` files
// are imported inside each component module, so a story only needs the global
// sheet plus whatever its component imports itself.
import '../src/index.css';

// ---------------------------------------------------------------------------
// ADR-077 runtimeConfig bootstrap — seed the singleton for every story.
//
// Storybook runs the `storybook build` (production Vite build) for Chromatic,
// which means import.meta.env.DEV === false and the DEV fallback in loadConfig()
// is dead-code-eliminated. Any story whose render/mount path reaches a
// call-time getter (getDaemonApiBaseUrl, isStaging, useDraftEventStream.connect,
// etc.) will throw "loadConfig() has not completed" unless the singleton is
// seeded here.
//
// This mirrors the `beforeEach(() => setRuntimeConfig(testDefaults))` pattern
// used in Vitest integration tests — the same class of fix for the same problem
// (third render context alongside app boot and Vitest).
//
// Values are Storybook-appropriate defaults: no real DSNs, no real keys.
// The `clerkPublishableKey` and `bffUrl` match the test fixture so that any
// story that renders a Clerk-wrapped or BFF-calling component gets consistent
// default behaviour. daemonUrl matches the stable local daemon port.
// ---------------------------------------------------------------------------
import { setRuntimeConfig } from '../src/config/runtimeConfig';

setRuntimeConfig({
  clerkPublishableKey: 'pk_test_dGVzdA',
  bffUrl: 'http://localhost:8080/api/v1',
  sentryDsn: undefined,
  sentryEnv: 'storybook',
  posthogKey: undefined,
  posthogHost: 'https://app.posthog.com',
  envLabel: 'storybook',
  daemonUrl: 'http://localhost:9001/api/v1',
});

// Initialize MSW. `onUnhandledRequest: 'bypass'` lets Storybook's own asset
// requests (fonts, HMR, etc.) pass through without noisy console warnings.
initialize({ onUnhandledRequest: 'bypass' });

const preview: Preview = {
  // Global decorators apply to every story.
  //  - withClerkSession syncs the Clerk auth mock from the `clerk` story param.
  // Router context is opt-in per story (withRouter / withRouterAt) so that
  // atoms and molecules stay free of context they do not need.
  decorators: [withClerkSession],

  // mswLoader activates the per-story `parameters.msw.handlers` array before
  // each story renders. Required for MSW v2 with msw-storybook-addon v2.
  loaders: [mswLoader],

  parameters: {
    // a11y — axe-core rules applied globally across all stories.
    // `runOnly` restricts the default run to WCAG 2.1 AA rules, which is the
    // project's target compliance level (see ADR-042 / visual-testing-strategy.md).
    // Individual stories may override `parameters.a11y` to tighten or loosen
    // the rule set, or to disable checks for a known-acceptable violation.
    //
    // NOTE: `text-muted` (--color-text-muted, #8a8a8a on #1e1e1e) sits at 4.6:1
    // contrast — right at the WCAG AA floor for normal text (4.5:1 minimum).
    // Any increase in font size or lightening of the background could push this
    // token below AA compliance.  Track at-risk in follow-on Frank ticket.
    a11y: {
      config: {
        rules: [
          {
            // Disable the `color-contrast` rule globally in Storybook because
            // many component stories render without the dark app background,
            // producing false positives.  Re-enable per-story for contrast audits.
            id: 'color-contrast',
            enabled: false,
          },
        ],
      },
      options: {
        runOnly: {
          type: 'tag',
          values: ['wcag2a', 'wcag2aa', 'wcag21aa'],
        },
      },
    },
    // The app runs on a dark surface (#1e1e1e). Match it so component contrast
    // in stories reflects production, and so Chromatic snapshots are stable.
    backgrounds: {
      default: 'app',
      values: [
        { name: 'app', value: '#1e1e1e' },
        { name: 'light', value: '#ffffff' },
      ],
    },
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    // Canonical responsive breakpoints for VaultMTG. These drive the Storybook
    // viewport toolbar and will be wired to Chromatic multi-viewport builds once
    // TurboSnap (A-1) is confirmed merged (quota guard — see ticket #287).
    viewport: {
      viewports: {
        desktop: { name: 'Desktop', styles: { width: '1280px', height: '800px' } },
        tablet: { name: 'Tablet', styles: { width: '768px', height: '1024px' } },
        mobile: { name: 'Mobile', styles: { width: '375px', height: '812px' } },
      },
      defaultViewport: 'desktop',
    },
  },
};

export default preview;
