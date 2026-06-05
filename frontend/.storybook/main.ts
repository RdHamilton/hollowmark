import { fileURLToPath } from 'node:url';
import type { StorybookConfig } from '@storybook/react-vite';

const config: StorybookConfig = {
  // Stories live alongside their component as `ComponentName.stories.tsx`.
  // Restricted to `.ts`/`.tsx` — the SPA is TypeScript-only.
  stories: ['../src/**/*.stories.@(ts|tsx)'],

  // Storybook 10 ships docs, controls, backgrounds, and actions in core, so no
  // separate addons are required for autodocs or the controls panel.
  // @storybook/addon-a11y surfaces axe-core violations in the Accessibility
  // panel during development so contrast and ARIA issues are caught before
  // Chromatic and before code review.
  addons: ['@storybook/addon-a11y', 'msw-storybook-addon'],

  framework: {
    // Vite builder — the SPA is built with Vite (see vite.config.ts).
    // Do not switch to the Webpack builder.
    name: '@storybook/react-vite',
    options: {},
  },

  // viteFinal lets the Storybook Vite build diverge from the app build.
  // Here we alias modules to local mocks so every story renders fully offline
  // and deterministically — no real publishable key, no live auth session.
  //
  // Aliases:
  //   @clerk/react → clerk-mock.tsx
  //     Provides stable useAuth()/useUser() stubs so every story gets a
  //     predictable session token without a real Clerk environment.
  //
  // BFF network calls (bffWildcardAdvisor and others) are intercepted at
  // runtime by MSW (initialized in preview.ts) rather than via module aliasing.
  // Module aliasing was fragile: the component imports bffWildcardAdvisor
  // through the @/services/api barrel, not the deep path, so the deep-path
  // alias was never resolved and the real fetch was still called.
  viteFinal: async (viteConfig) => {
    viteConfig.resolve = viteConfig.resolve ?? {};
    viteConfig.resolve.alias = {
      ...viteConfig.resolve.alias,
      '@clerk/react': fileURLToPath(new URL('./clerk-mock.tsx', import.meta.url)),
    };
    return viteConfig;
  },
};

export default config;
