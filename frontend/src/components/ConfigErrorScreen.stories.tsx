import type { Meta, StoryObj } from '@storybook/react';
import ConfigErrorScreen from './ConfigErrorScreen';

/**
 * ConfigErrorScreen — ADR-077 config-load failure UI.
 *
 * Renders when `/config.json` cannot be fetched, parsed, or validated before
 * the SPA mounts. Three branches drive copy and retry affordance per Tim's
 * spec (hollowmark-docs/engineering/design/specs/adr-077-config-load-error-screen.md).
 *
 * The runtime-config singleton is seeded globally in .storybook/preview.ts
 * (ADR-077 Storybook bootstrap), so this component renders without any
 * per-story config setup.
 *
 * Layout is fullscreen so Chromatic captures the component at its natural
 * full-viewport size (min-height: 100vh, --surface-base background).
 */
const meta: Meta<typeof ConfigErrorScreen> = {
  title: 'Components/ConfigErrorScreen',
  component: ConfigErrorScreen,
  parameters: {
    // Fullscreen: the error screen occupies the full viewport — no card,
    // no padding wrapper. Centered layout would clip the background or add
    // a white ring that doesn't reflect production.
    layout: 'fullscreen',
    // Pin the dark app background so Chromatic snapshots match production.
    backgrounds: { default: 'app' },
  },
  tags: ['autodocs'],
  argTypes: {
    branch: {
      control: { type: 'select' },
      options: ['network', 'parse', 'missing-fields'],
      description: 'Which failure branch triggered this screen (drives copy and retry affordance)',
    },
    onRetry: {
      action: 'retried',
      description: 'Called when the player clicks "Try Again" (network branch only)',
    },
    appVersion: {
      control: 'text',
      description: 'Build version string rendered as the version footnote (VITE_APP_VERSION)',
    },
  },
};

export default meta;
type Story = StoryObj<typeof ConfigErrorScreen>;

/**
 * Branch A — network failure.
 * fetch('/config.json') threw or returned non-2xx. Player's connectivity is the
 * likely cause. Shows retry button — "Try Again" re-invokes the config fetch.
 */
export const NetworkError: Story = {
  args: {
    branch: 'network',
    onRetry: () => {},
    appVersion: '0.4.3',
  },
};

/**
 * Branch A — network error without retry handler.
 * Edge case: error screen rendered outside the boot sequence where no retry
 * callback is wired. The retry button is omitted.
 */
export const NetworkErrorNoRetry: Story = {
  args: {
    branch: 'network',
    appVersion: '0.4.3',
  },
};

/**
 * Branch B — parse / bad JSON.
 * response.ok === true but body failed JSON.parse (includes the CloudFront
 * 403/404→index.html rewrite at HTTP 200 case). No retry — VaultMTG must
 * fix the deployment.
 */
export const ParseError: Story = {
  args: {
    branch: 'parse',
    appVersion: '0.4.3',
  },
};

/**
 * Branch C — missing / invalid fields.
 * Valid JSON but required fields are absent, empty, or fail format-shape
 * validation (clerkPublishableKey regex). No retry — deploy error on VaultMTG's
 * side.
 */
export const MissingFields: Story = {
  args: {
    branch: 'missing-fields',
    appVersion: '0.4.3',
  },
};
