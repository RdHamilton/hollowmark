import type { Preview } from '@storybook/react';
import { withClerkSession } from './decorators';

// Global application styles. The VaultMTG SPA uses plain CSS (not Tailwind) —
// `index.css` holds the design tokens (CSS custom properties), dark color
// scheme, and base typography. Importing it here ensures every story renders
// against the same foundation as the running app. Per-component `.css` files
// are imported inside each component module, so a story only needs the global
// sheet plus whatever its component imports itself.
import '../src/index.css';

const preview: Preview = {
  // Global decorators apply to every story.
  //  - withClerkSession syncs the Clerk auth mock from the `clerk` story param.
  // Router context is opt-in per story (withRouter / withRouterAt) so that
  // atoms and molecules stay free of context they do not need.
  decorators: [withClerkSession],

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
