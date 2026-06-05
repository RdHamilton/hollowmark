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
  addons: ['@storybook/addon-a11y'],

  framework: {
    // Vite builder — the SPA is built with Vite (see vite.config.ts).
    // Do not switch to the Webpack builder.
    name: '@storybook/react-vite',
    options: {},
  },

  // viteFinal lets the Storybook Vite build diverge from the app build.
  // Here we alias network-calling modules to local mocks so every story
  // renders fully offline and deterministically — no real publishable key,
  // no real BFF URL, no network calls, no live auth session.
  //
  // Aliases:
  //   @clerk/react            → clerk-mock.tsx
  //   @/services/api/bffWildcardAdvisor → bffWildcardAdvisor-mock.ts
  //     The wildcard advisor adapter calls `fetch` at a real BFF URL. In
  //     Chromatic's render environment there is no server, so the fetch throws
  //     and the story crashes. Aliasing to the mock gives each story a spy-able
  //     `getWildcardRecommendations` function it can control via `beforeEach`.
  viteFinal: async (viteConfig) => {
    viteConfig.resolve = viteConfig.resolve ?? {};
    viteConfig.resolve.alias = {
      ...viteConfig.resolve.alias,
      '@clerk/react': fileURLToPath(new URL('./clerk-mock.tsx', import.meta.url)),
      '@/services/api/bffWildcardAdvisor': fileURLToPath(
        new URL('./bffWildcardAdvisor-mock.ts', import.meta.url)
      ),
    };
    return viteConfig;
  },
};

export default config;
